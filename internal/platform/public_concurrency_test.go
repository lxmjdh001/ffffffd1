package platform

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestTrafficVisitsDoNotWaitForCustomerBalanceLock(t *testing.T) {
	s := meteredServer(t, 0)
	if s.DB.Dialector.Name() != "postgres" {
		t.Skip("requires PostgreSQL row locks")
	}
	u := seedUser(t, s, Dollar)
	for _, mode := range []string{"random", "sequential"} {
		pool := apkButtonPool(t, s, u, "https://target.example/a", "https://target.example/b")
		s.DB.Model(&pool).Update("mode", mode)
		tx := s.DB.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		if _, err := lockUser(tx, u.ID); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, err := s.trafficTarget(httptest.NewRequest("GET", "http://localhost/r/"+pool.Slug, nil), pool.Slug, u.ID)
			done <- err
		}()
		select {
		case err := <-done:
			tx.Rollback()
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			tx.Rollback()
			<-done
			t.Fatalf("%s routing waited for an unrelated balance transaction", mode)
		}
	}
}

func TestTrafficConcurrentSequentialCursor(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, Dollar)
	pool := apkButtonPool(t, s, u, "https://target.example/a", "https://target.example/b", "https://target.example/c")
	if err := s.DB.Model(&pool).Update("mode", "sequential").Error; err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	var mu sync.Mutex
	counts := map[string]int{}
	for range 60 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			target, err := s.trafficTarget(httptest.NewRequest("GET", "http://localhost/r/"+pool.Slug, nil), pool.Slug, u.ID)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			counts[target]++
			mu.Unlock()
		}()
	}
	workers.Wait()
	for _, target := range []string{"https://target.example/a", "https://target.example/b", "https://target.example/c"} {
		if counts[target] != 20 {
			t.Fatalf("sequential allocation lost updates: %v", counts)
		}
	}
	if usageOwner(t, s, u.ID).BalanceMicros != Dollar || countRows(t, s, &MeteredEvent{}) != 0 {
		t.Fatal("routing changed billing")
	}
}

func TestConcurrentRateLimitUsesEachAtomicIncrement(t *testing.T) {
	s := testServer(t)
	now := s.Now()
	s.Now = func() time.Time { return now }
	var workers sync.WaitGroup
	var accepted atomic.Int64
	for range 64 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			err := s.rate("concurrent-limit", 25, time.Minute)
			if err == nil {
				accepted.Add(1)
				return
			}
			var problem *apiError
			if !errors.As(err, &problem) || problem.Status != 429 {
				t.Errorf("unexpected limiter error: %v", err)
			}
		}()
	}
	workers.Wait()
	if accepted.Load() != 25 {
		t.Fatalf("accepted %d, want exactly 25", accepted.Load())
	}
	now = now.Add(time.Minute)
	if err := s.rate("concurrent-limit", 25, time.Minute); err != nil {
		t.Fatal("new window not usable", err)
	}
}

func TestVisitWriterDoesNotHoldHTTPResponseAndDrains(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	a := seedApp(t, s, u, 10)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	if err := s.DB.Callback().Create().Before("gorm:create").Register("test:slow-visits", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "access_visits" {
			once.Do(func() { close(started) })
			select {
			case <-release:
			case <-tx.Statement.Context.Done():
				tx.AddError(tx.Statement.Context.Err())
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	stop := s.StartVisitWriter()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = stop(ctx)
	})
	req := httptest.NewRequest("GET", "http://localhost/a/"+a.Slug+"/info", nil)
	s.recordAccess(req, u.ID, "apps", a.ID, "allowed")
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("visit batch did not start")
	}
	response := make(chan int, 1)
	go func() { response <- statsPublic(s, "GET", "/a/"+a.Slug+"/info", "198.51.100.9").Code }()
	select {
	case code := <-response:
		close(release)
		if code != 200 {
			t.Fatalf("page failed: %d", code)
		}
	case <-time.After(500 * time.Millisecond):
		close(release)
		<-response
		t.Fatal("HTTP waited for the statistics database write")
	}
	var workers sync.WaitGroup
	for range 500 {
		workers.Add(1)
		go func() { defer workers.Done(); s.recordAccess(req, u.ID, "apps", a.ID, "allowed") }()
	}
	workers.Wait()
	if err := stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if countRows(t, s, &AccessVisit{}) != 502 {
		t.Fatal("graceful drain lost visits")
	}
	if countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &Conversion{}) != 0 {
		t.Fatal("analytics changed billing or advertising")
	}
}

func TestVisitWriterJournalsDatabaseFailureAndDeduplicatesRecovery(t *testing.T) {
	s := testServer(t)
	v := &visitWriter{db: s.DB, dir: t.TempDir(), inbox: make(chan AccessVisit, 1)}
	rows := []AccessVisit{}
	for range 128 {
		rows = append(rows, AccessVisit{ID: randomHex(16), UserID: randomHex(16), Kind: "apps", ResourceID: randomHex(16), CreatedAt: s.Now()})
	}
	if err := s.DB.Callback().Create().Before("gorm:create").Register("test:visit-outage", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "access_visits" {
			tx.AddError(errors.New("isolated statistics outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	v.flush(rows)
	entries, err := os.ReadDir(v.dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("failed batch was not journaled", err)
	}
	if err = s.DB.Callback().Create().Remove("test:visit-outage"); err != nil {
		t.Fatal(err)
	}
	// A copied journal models an uncertain commit / retry after a crash.
	raw, err := os.ReadFile(filepath.Join(v.dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(v.dir, "retry.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	v.replay(context.Background())
	if countRows(t, s, &AccessVisit{}) != 128 {
		t.Fatal("recovery lost or duplicated visits")
	}
	entries, _ = os.ReadDir(v.dir)
	if len(entries) != 0 {
		t.Fatal("acknowledged journals not removed")
	}
	// Saturated inbox: one queued row, one durable overflow, neither discarded.
	first, second := rows[0], rows[0]
	first.ID, second.ID = randomHex(16), randomHex(16)
	v.record(first)
	v.record(second)
	if err = v.insert(context.Background(), []AccessVisit{<-v.inbox}); err != nil {
		t.Fatal(err)
	}
	v.replay(context.Background())
	if countRows(t, s, &AccessVisit{}) != 130 {
		t.Fatal("overflow record was lost")
	}
}

func TestVisitWriterRecoverySkipsUnfinishedAndQuarantinesInvalidFiles(t *testing.T) {
	s := testServer(t)
	v := &visitWriter{db: s.DB, dir: t.TempDir()}
	for i := range 40 {
		if err := os.WriteFile(filepath.Join(v.dir, fmt.Sprintf(".pending-%d", i)), []byte("partial"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(v.dir, "invalid.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	row := AccessVisit{ID: randomHex(16), UserID: randomHex(16), Kind: "apps", ResourceID: randomHex(16), CreatedAt: s.Now()}
	if err := v.persist([]AccessVisit{row}); err != nil {
		t.Fatal(err)
	}
	v.replay(context.Background())
	if countRows(t, s, &AccessVisit{}) != 1 {
		t.Fatal("unfinished journals starved valid recovery")
	}
	if _, err := os.Stat(filepath.Join(v.dir, "invalid", "invalid.json")); err != nil {
		t.Fatal("malformed journal not retained outside replay", err)
	}
}

func TestVisitRetentionDrainsMultipleBatchesWithoutDeletingCurrentRecords(t *testing.T) {
	s := testServer(t)
	now := s.Now()
	s.Now = func() time.Time { return now }
	old := make([]AccessVisit, 12001)
	for i := range old {
		old[i] = AccessVisit{ID: randomHex(16), UserID: "retention-owner", Kind: "apps", ResourceID: "retention-app", CreatedAt: now.AddDate(0, 0, -36)}
	}
	if err := s.DB.CreateInBatches(old, 128).Error; err != nil {
		t.Fatal(err)
	}
	current := old[0]
	current.ID, current.CreatedAt = randomHex(16), now
	if err := s.DB.Create(&current).Error; err != nil {
		t.Fatal(err)
	}
	s.cleanupVisitStatistics(context.Background())
	if countRows(t, s, &AccessVisit{}) != 1 {
		t.Fatal("retention did not drain more than one batch")
	}
	if err := s.DB.First(&AccessVisit{}, "id = ?", current.ID).Error; err != nil {
		t.Fatal("retention removed current data", err)
	}
	if err := s.DB.Create(&old[0]).Error; err != nil {
		t.Fatal(err)
	}
	s.cleanupVisitStatistics(context.Background())
	if countRows(t, s, &AccessVisit{}) != 2 {
		t.Fatal("cleanup replicas did not share the minute limit")
	}
	now = now.Add(time.Minute)
	s.cleanupVisitStatistics(context.Background())
	if countRows(t, s, &AccessVisit{}) != 1 {
		t.Fatal("cleanup did not resume next minute")
	}
	if countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &MeteredEvent{}) != 0 {
		t.Fatal("statistics retention changed financial records")
	}
}
