package platform

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestDownloadAdSignalNeverWaitsForDatabaseDiskOrCapacity(t *testing.T) {
	// No DB and an unusable data path: the actual streaming notification must
	// still succeed without attempting either I/O operation.
	s := NewServer(nil, Config{DataDir: "/not-a-download-ad-volume"})
	s.downloadAdStarts = make(chan downloadAdStart, 1)
	ticket := Ticket{Hash: strings.Repeat("a", 64), UserID: strings.Repeat("b", 32), AttributionID: strings.Repeat("c", 32)}
	done := make(chan struct{})
	go func() {
		w := &billingWriter{ResponseWriter: httptest.NewRecorder(), bill: func() error { return nil }, onStart: func() { s.noticeDownloadAdStart(ticket) }}
		_, _ = w.Write([]byte("first bytes"))
		_, _ = w.Write([]byte("remaining bytes"))
		for range 1000 {
			s.noticeDownloadAdStart(ticket)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream waited for the advertising inbox")
	}
	if len(s.downloadAdStarts) != 1 || s.downloadAdOverflows.Load() != 1000 {
		t.Fatal("inbox was unbounded or a stream signaled twice")
	}
}

func TestDownloadAdSlowDatabaseDoesNotPauseFileTransmission(t *testing.T) {
	f := routedAdFixtureFor(t, true)
	s := f.s
	s.DB.Model(&f.pool).Update("cursor", 1)
	file, cookies := f.targetTicket(t, f.click(t, true), f.apps[1])
	entered, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	if err := s.DB.Callback().Create().Before("gorm:create").Register("test:slow-download-ad", func(tx *gorm.DB) {
		if tx.Statement.Table == "conversions" {
			once.Do(func() { close(entered) })
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer s.DB.Callback().Create().Remove("test:slow-download-ad")
	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() { s.RunDownloadAdWorker(ctx); close(workerDone) }()
	defer func() { unblock(); cancel(); <-workerDone }()
	ts := httptest.NewServer(s.Handler())
	defer func() { unblock(); ts.Close() }()
	u, _ := url.Parse(file)
	result := make(chan error, 1)
	go func() {
		r, _ := http.NewRequest("GET", ts.URL+u.RequestURI(), nil)
		r.Host = u.Host
		r.Header.Set("User-Agent", "Route attribution test browser")
		for _, c := range cookies {
			r.AddCookie(c)
		}
		resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(r)
		if err != nil {
			result <- err
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err == nil && (resp.StatusCode != 200 || len(body) != 100) {
			err = errors.New("incorrect download response")
		}
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("advertising worker was not exercised")
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("APK transmission paused behind advertising SQL")
	}
	unblock()
}

func TestDownloadAdFailureCannotRollBackAccountingAndReplaysAfterRestart(t *testing.T) {
	f := routedAdFixtureFor(t, true)
	s := f.s
	s.DB.Model(&f.pool).Update("cursor", 1)
	file, cookies := f.targetTicket(t, f.click(t, true), f.apps[1])
	if err := s.DB.Callback().Create().Before("gorm:create").Register("test:fail-download-ad", func(tx *gorm.DB) {
		if tx.Statement.Table == "conversions" {
			tx.AddError(errors.New("injected advertising database failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer s.DB.Callback().Create().Remove("test:fail-download-ad")
	before := usageOwner(t, s, f.owner.ID)
	requireCode(t, routedAdRequestOnly(s, "GET", file, cookies, nil, ""), 200)
	if countRows(t, s, &Download{}) != 1 || countRows(t, s, &Conversion{}) != 0 {
		t.Fatal("accounting depended on advertising")
	}
	after := usageOwner(t, s, f.owner.ID)
	if after.BalanceMicros != before.BalanceMicros || after.HeldMicros != before.HeldMicros {
		t.Fatal("download preparation changed UV accounting")
	}
	event := <-s.downloadAdStarts
	path, err := s.writeDownloadAdStart(event)
	if err != nil {
		t.Fatal(err)
	}
	s.replayDownloadAds(context.Background())
	if _, err := os.Stat(path); err != nil {
		t.Fatal("failed event lost its persistent receipt")
	}
	s.DB.Callback().Create().Remove("test:fail-download-ad")
	restarted := NewServer(s.DB, s.Config)
	restarted.replayDownloadAds(context.Background())
	restarted.replayDownloadAds(context.Background())
	if countRows(t, s, &Conversion{}) != 1 || countRows(t, s, &Delivery{}) != 2 || countRows(t, s, &Download{}) != 1 {
		t.Fatal("restart lost or duplicated accounting/callbacks")
	}
	var saved Conversion
	s.DB.First(&saved)
	if !saved.EventTime.Equal(event.OccurredAt) || saved.AppID != f.apps[0].ID || saved.DownloadedAppID != f.apps[1].ID {
		t.Fatal("replay changed original time or attribution")
	}
	for _, dir := range []string{"download-ad-starts", "completions"} {
		entries, err := os.ReadDir(filepath.Join(s.Config.DataDir, dir))
		if err != nil || len(entries) != 0 {
			t.Fatal("successfully recovered receipts were not cleaned up")
		}
	}
}

func TestDownloadAdOverflowRecoversFromConfirmedRange(t *testing.T) {
	f := routedAdFixtureFor(t, true)
	s := f.s
	s.DB.Model(&f.pool).Update("cursor", 1)
	file, cookies := f.targetTicket(t, f.click(t, true), f.apps[1])
	s.downloadAdStarts = make(chan downloadAdStart, 1)
	s.downloadAdStarts <- downloadAdStart{TicketHash: strings.Repeat("f", 64), UserID: f.owner.ID, OccurredAt: s.Now()}
	requireCode(t, routedAdRequestOnly(s, "GET", file, cookies, nil, "bytes=0-0"), 206)
	if s.downloadAdOverflows.Load() != 1 || countRows(t, s, &Conversion{}) != 0 {
		t.Fatal("overflow blocked or synchronously prepared a callback")
	}
	s.replayDownloadAdCompletions(context.Background())
	s.replayDownloadAdCompletions(context.Background())
	if countRows(t, s, &Conversion{}) != 1 || countRows(t, s, &Delivery{}) != 2 || countRows(t, s, &Download{}) != 0 {
		t.Fatal("range fallback lost start or fabricated completion")
	}
}

func TestDownloadAdShutdownFlushesPendingSignals(t *testing.T) {
	s := NewServer(nil, Config{DataDir: t.TempDir()})
	for _, ch := range []string{"a", "b", "c"} {
		s.noticeDownloadAdStart(Ticket{Hash: strings.Repeat(ch, 64), UserID: strings.Repeat("d", 32), AttributionID: strings.Repeat("e", 32)})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.RunDownloadAdWorker(ctx)
	entries, err := os.ReadDir(filepath.Join(s.Config.DataDir, "download-ad-starts"))
	if err != nil || len(entries) != 3 {
		t.Fatal("shutdown discarded an accepted signal")
	}
}

func TestDownloadAdPreparationDoesNotLockBalance(t *testing.T) {
	f := routedAdFixtureFor(t, true)
	s := f.s
	if s.DB.Dialector.Name() != "postgres" {
		t.Skip("requires PostgreSQL row-lock concurrency")
	}
	s.DB.Model(&f.pool).Update("cursor", 1)
	file, cookies := f.targetTicket(t, f.click(t, true), f.apps[1])
	requireCode(t, routedAdRequestOnly(s, "GET", file, cookies, nil, "bytes=0-0"), 206)
	event := <-s.downloadAdStarts
	entered := make(chan struct{})
	var once sync.Once
	s.DB.Callback().Create().Before("gorm:create").Register("test:hold-ad-transaction", func(tx *gorm.DB) {
		if tx.Statement.Table == "conversions" {
			once.Do(func() { close(entered) })
			<-tx.Statement.Context.Done()
			tx.AddError(tx.Statement.Context.Err())
		}
	})
	defer s.DB.Callback().Create().Remove("test:hold-ad-transaction")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.recordDownloadAd(ctx, event) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("ad transaction not reached")
	}
	lockCtx, stop := context.WithTimeout(context.Background(), time.Second)
	err := s.DB.WithContext(lockCtx).Transaction(func(tx *gorm.DB) error { _, e := lockUser(tx, f.owner.ID); return e })
	stop()
	cancel()
	<-done
	if err != nil {
		t.Fatal("advertising transaction held the balance lock", err)
	}
}
