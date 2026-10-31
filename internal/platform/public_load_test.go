package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm/logger"
)

type loadSQLCounter struct {
	logger.Interface
	queries atomic.Int64
}

func (l *loadSQLCounter) Trace(ctx context.Context, begin time.Time, query func() (string, int64), err error) {
	l.queries.Add(1)
}

// Opt-in, open-loop HTTP load against a fresh PostgreSQL test schema. It never
// uses production records, downloads a real APK, or sends advertising events.
// D1_LOAD_SECONDS=60 D1_LOAD_QPS=200 D1_LOAD_CASE=mixed|cloak|apk|pwa|traffic
// D1_LOAD_WITH_BILLING=1 adds download / install / cancellation API traffic to
// the requested page-arrival rate. It validates local outboxes, never delivery.
//
//	TEST_DATABASE_URL=postgres://... D1_LOAD_TEST=1 go test ./internal/platform \
//	  -run '^TestPublicEntryLoad$' -count=1 -v -timeout=10m
func TestPublicEntryLoad(t *testing.T) {
	if os.Getenv("D1_LOAD_TEST") != "1" {
		t.Skip("explicit load-test opt-in required")
	}
	if os.Getenv("TEST_DATABASE_URL") == "" {
		t.Fatal("load tests require an isolated PostgreSQL database")
	}
	setting := func(name string, fallback, min, max int) int {
		t.Helper()
		if raw := os.Getenv(name); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < min || n > max {
				t.Fatalf("invalid %s", name)
			}
			return n
		}
		return fallback
	}
	qps, seconds := setting("D1_LOAD_QPS", 200, 1, 2000), setting("D1_LOAD_SECONDS", 30, 1, 600)
	s := meteredServer(t, 0)
	owner := seedUser(t, s, 100*Dollar)
	withBilling := os.Getenv("D1_LOAD_WITH_BILLING") == "1"
	if withBilling {
		owner.BalanceMicros, owner.GiftBalanceMicros = 10000*Dollar, 5*Dollar
		if err := s.DB.Save(&owner).Error; err != nil {
			t.Fatal(err)
		}
	}
	app := seedApp(t, s, owner, 100)
	if err := s.DB.Model(&app).Update("download_mode", "info").Error; err != nil {
		t.Fatal(err)
	}
	pwa := createPWAFixture(t, s, owner, "Isolated load fixture")
	landing, _ := json.Marshal(PWALandingConfig{Template: "google_play"})
	if err := s.DB.Model(&pwa).Update("landing_json", string(landing)).Error; err != nil {
		t.Fatal(err)
	}
	for _, a := range []Application{app, pwaAdApplication(pwa)} {
		cfg := quickAdConfig(a)
		cfg.FacebookToken, _ = s.encryptSecret("load-fixture-only-not-a-real-token")
		if err := s.DB.Create(&cfg).Error; err != nil {
			t.Fatal(err)
		}
	}
	addChosenDomain(t, s, owner, "entry.example")
	pool := apkButtonPool(t, s, owner, "https://entry.example/a/"+app.Slug+"/info", "https://entry.example/p/"+pwa.Slug+"/")
	cloak := Cloak{ID: randomHex(16), UserID: owner.ID, Slug: "load-cloak", Status: "off", TargetType: "traffic_pool", TrafficPoolSlug: pool.Slug}
	if err := s.DB.Create(&cloak).Error; err != nil {
		t.Fatal(err)
	}
	_, local, _ := net.ParseCIDR("127.0.0.0/8")
	s.Config.TrustedProxies = []*net.IPNet{local}
	sqlCounter := &loadSQLCounter{Interface: logger.Discard}
	s.DB.Logger = sqlCounter
	// Production prepares advertising outboxes in its own worker. Only start
	// that local worker; RunAdWorker (provider HTTP delivery) is never started.
	stopAds := func() {}
	if withBilling {
		adContext, cancelAds := context.WithCancel(context.Background())
		adDone := make(chan struct{})
		go func() { defer close(adDone); s.RunDownloadAdWorker(adContext) }()
		stopAds = func() { cancelAds(); <-adDone }
		t.Cleanup(stopAds)
	}
	// The assertion also compiles against the baseline, which writes visits
	// synchronously. Both builds use exactly the same traffic and fixtures.
	stopVisits := func(context.Context) error { return nil }
	if async, ok := any(s).(interface {
		StartVisitWriter() func(context.Context) error
	}); ok {
		stopVisits = async.StartVisitWriter()
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = stopVisits(ctx)
	})
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	transport := &http.Transport{MaxIdleConns: 256, MaxIdleConnsPerHost: 256, MaxConnsPerHost: 256}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	paths := map[string]string{"apk": "/a/" + app.Slug + "/info", "pwa": "/p/" + pwa.Slug + "/", "cloak": "/c/" + cloak.Slug, "traffic": "/r/" + pool.Slug}
	kinds := []string{"apk", "pwa", "cloak", "traffic"}
	scenario := os.Getenv("D1_LOAD_CASE")
	if scenario == "" {
		scenario = "mixed"
	}
	if scenario != "mixed" {
		if paths[scenario] == "" {
			t.Fatal("invalid D1_LOAD_CASE")
		}
		kinds = []string{scenario}
	}
	sqlDB, _ := s.DB.DB()
	beforeDB := sqlDB.Stats()
	type result struct {
		elapsed time.Duration
		status  int
		valid   bool
	}
	results := make([]result, qps*seconds)
	slots := make(chan struct{}, 256)
	var requests sync.WaitGroup
	var rejected atomic.Int64
	var actions loadActions
	start := time.Now()
	for i := range results {
		scheduled := start.Add(time.Duration(i) * time.Second / time.Duration(qps))
		if wait := time.Until(scheduled); wait > 0 {
			time.Sleep(wait)
		}
		select {
		case slots <- struct{}{}:
		default:
			rejected.Add(1)
			continue
		}
		requests.Add(1)
		go func(i int, scheduled time.Time) {
			defer requests.Done()
			defer func() { <-slots }()
			kind := kinds[i%len(kinds)]
			req, _ := http.NewRequest("GET", server.URL+paths[kind], nil)
			req.Host = "entry.example"
			req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.18.%d.%d", i/250%250, i%250+1))
			req.Header.Set("User-Agent", "D1 isolated load Chrome/130")
			req.Header.Set("Accept-Language", "en-US,en;q=0.9")
			response, err := client.Do(req)
			if err != nil {
				results[i] = result{elapsed: time.Since(scheduled)}
				return
			}
			body, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			valid := readErr == nil && response.StatusCode == 200 && response.Header.Get("Location") == "" && strings.Contains(string(body), "<html")
			if kind == "traffic" {
				valid = readErr == nil && response.StatusCode == 302 && strings.HasPrefix(response.Header.Get("Location"), "https://entry.example/")
			}
			results[i] = result{time.Since(scheduled), response.StatusCode, valid}
			// Keep 200 page arrivals/s while a subset also downloads or confirms /
			// cancels an install. These extra API requests are additional load.
			if withBilling && valid && i%12 < 2 && (kind == "apk" || kind == "pwa") {
				if err := actions.run(client, server.URL, req, response.Cookies(), string(body), kind, app, pwa, i); err != nil {
					actions.failed.Add(1)
					t.Errorf("load action %s: %v", kind, err)
				}
			}
		}(i, scheduled)
	}
	requests.Wait()
	elapsed := time.Since(start)
	if withBilling {
		deadline := time.Now().Add(10 * time.Second)
		for countRows(t, s, &Conversion{}) < actions.apk.Load() && time.Now().Before(deadline) {
			time.Sleep(25 * time.Millisecond)
		}
		stopAds()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := stopVisits(ctx); err != nil {
		t.Fatal(err)
	}
	afterDB := sqlDB.Stats()
	queryCount := sqlCounter.queries.Load()
	latencies := []float64{}
	statuses := map[int]int{}
	failed := int(rejected.Load())
	for _, result := range results {
		if result.elapsed == 0 {
			continue
		}
		latencies = append(latencies, float64(result.elapsed)/float64(time.Millisecond))
		statuses[result.status]++
		if !result.valid {
			failed++
		}
	}
	sort.Float64s(latencies)
	percentile := func(p float64) float64 {
		if len(latencies) == 0 {
			return 0
		}
		return latencies[min(len(latencies)-1, int(float64(len(latencies))*p))]
	}
	visits := countRows(t, s, &AccessVisit{})
	expectedVisits := 0
	for i := range results {
		if kinds[i%len(kinds)] == "cloak" {
			expectedVisits += 3
		} else {
			expectedVisits++
		}
	}
	report := map[string]any{"case": scenario, "target_qps": qps, "seconds": seconds, "requests": len(results), "achieved_qps": float64(len(results)-failed) / elapsed.Seconds(), "failed": failed, "generator_dropped": rejected.Load(), "p50_ms": percentile(.5), "p95_ms": percentile(.95), "p99_ms": percentile(.99), "statuses": statuses, "db_queries_per_request": float64(queryCount) / float64(len(results)), "db_wait_count": afterDB.WaitCount - beforeDB.WaitCount, "db_wait_ms": (afterDB.WaitDuration - beforeDB.WaitDuration).Milliseconds(), "visits": visits, "expected_visits": expectedVisits}
	if withBilling {
		apk, installed, cancelled := actions.apk.Load(), actions.installed.Load(), actions.cancelled.Load()
		charged := (apk + installed) * 50000
		usedGift := min(charged, owner.GiftBalanceMicros)
		got := usageOwner(t, s, owner.ID)
		var events int64
		s.DB.Model(&MeteredEvent{}).Where("state = ?", "charged").Count(&events)
		if got.GiftBalanceMicros != owner.GiftBalanceMicros-usedGift || got.BalanceMicros != owner.BalanceMicros-(charged-usedGift) || got.HeldMicros != 0 || got.GiftHeldMicros != 0 || events != apk+installed {
			t.Error("concurrent billing / cancellation / gift-first settlement mismatch")
		}
		if countRows(t, s, &Conversion{}) != apk || countRows(t, s, &Delivery{}) != apk {
			t.Error("APK start-download advertising was lost or duplicated")
		}
		report["billing"] = map[string]any{"apk_downloads": apk, "pwa_installed": installed, "pwa_cancelled": cancelled, "action_failures": actions.failed.Load(), "additional_http_requests": apk*4 + (installed+cancelled)*3, "charged_events": events, "charged_dollars": float64(charged) / float64(Dollar), "gift_used_dollars": float64(usedGift) / float64(Dollar), "local_ad_outboxes": countRows(t, s, &Delivery{})}
	}
	raw, _ := json.Marshal(report)
	t.Log("LOAD_REPORT " + string(raw))
	if failed != 0 || visits != int64(expectedVisits) {
		t.Error("load responses or visit accounting failed")
	}
	if got := usageOwner(t, s, owner.ID); !withBilling && (got.BalanceMicros != owner.BalanceMicros || countRows(t, s, &MeteredEvent{}) != 0 || countRows(t, s, &Conversion{}) != 0) {
		t.Error("page traffic changed billing or advertising")
	}
}
