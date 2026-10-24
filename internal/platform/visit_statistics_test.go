package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type statsResponse struct {
	Range              string             `json:"range"`
	AllowedRanges      []string           `json:"allowed_ranges"`
	StartAt            time.Time          `json:"start_at"`
	EndAt              time.Time          `json:"end_at"`
	Summary            visitSummary       `json:"summary"`
	Series             []visitSeriesPoint `json:"series"`
	Countries, Devices []visitBreakdown
	Records            struct {
		Data  []AccessVisit `json:"data"`
		Total int64         `json:"total"`
	} `json:"records"`
}

func statsRead(t *testing.T, s *Server, u User, kind, id, query string) statsResponse {
	t.Helper()
	cookie, _ := sessionFor(t, s, u)
	w := call(t, s, "GET", "/api/visit-statistics/"+kind+"/"+id+query, nil, cookie, "")
	requireCode(t, w, 200)
	var out statsResponse
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func statsPublic(s *Server, method, path, ip string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://localhost:8088"+path, nil)
	r.RemoteAddr = ip + ":1234"
	r.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 14; Mobile) Chrome/123")
	r.Header.Set("CF-IPCountry", "US")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestVisitStatisticsFourResourcesDetailsAndOwnership(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	foreign := seedUser(t, s, Dollar)
	a := seedApp(t, s, u, 128)
	pwa := createPWAFixture(t, s, u, "Stats PWA")
	pool := TrafficPool{UserID: u.ID, Slug: randomHex(4), Name: "Stats routing", Mode: "random", Enabled: true}
	if e := s.DB.Create(&pool).Error; e != nil {
		t.Fatal(e)
	}
	s.DB.Create(&TrafficLink{ID: randomHex(16), UserID: u.ID, PoolSlug: pool.Slug, URL: "https://example.com/a.apk"})
	cloak := Cloak{ID: randomHex(16), UserID: u.ID, Slug: randomHex(8), Name: "Stats cloak", TargetType: "url", TargetURL: "https://example.com/app", Enabled: true, BlockedUAs: []string{"bot"}}
	if e := s.DB.Create(&cloak).Error; e != nil {
		t.Fatal(e)
	}
	for _, item := range []struct {
		kind, id, path string
		status         int
	}{{"apps", a.ID, "/a/" + a.Slug + "/info", 200}, {"pwas", pwa.ID, "/p/" + pwa.Slug + "/", 200}, {"traffic", pool.Slug, "/r/" + pool.Slug, 302}, {"cloaks", cloak.ID, "/c/" + cloak.Slug, 302}} {
		t.Run(item.kind, func(t *testing.T) {
			for _, ip := range []string{"198.51.100.8", "198.51.100.8", "198.51.100.9"} {
				requireCode(t, statsPublic(s, "GET", item.path, ip), item.status)
			}
			requireCode(t, statsPublic(s, "HEAD", item.path, "198.51.100.8"), item.status)
			out := statsRead(t, s, u, item.kind, item.id, "")
			if out.Range != "today" || out.Summary.Views != 3 || out.Summary.UniqueIPs != 2 || out.Records.Total != 3 || len(out.Series) != 24 || len(out.AllowedRanges) != 2 {
				t.Fatalf("invalid statistics: %+v", out)
			}
			row := out.Records.Data[0]
			if row.IP == "" || row.UserAgent == "" || row.Country != "XX" || row.Device != "手机" || row.OS != "Android" || row.Browser != "Chrome" || row.CreatedAt.IsZero() {
				t.Fatalf("missing or untrusted visitor details: %+v", row)
			}
			cookie, _ := sessionFor(t, s, foreign)
			requireCode(t, call(t, s, "GET", "/api/visit-statistics/"+item.kind+"/"+item.id+"?range=30d", nil, cookie, ""), 404)
			requireCode(t, call(t, s, "GET", "/api/visit-statistics/"+item.kind+"/"+item.id, nil, nil, ""), 401)
		})
	}
	if countRows(t, s, &Download{}) != 0 || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("visits or reading statistics changed accounting")
	}
	// Assets, download POSTs, stream ranges and background requests are not page views.
	before := countRows(t, s, &AccessVisit{})
	requireCode(t, statsPublic(s, "GET", "/a/"+a.Slug+"/landing-config.js", "198.51.100.8"), 200)
	requireCode(t, statsPublic(s, "GET", "/p/"+pwa.Slug+"/manifest.webmanifest", "198.51.100.8"), 200)
	cookie, csrf := sessionFor(t, s, u)
	d := defaultPageDesign(a)
	requireCode(t, call(t, s, "POST", "/api/apps/"+a.ID+"/page-design/preview", d, cookie, csrf), 200)
	if countRows(t, s, &AccessVisit{}) != before {
		t.Fatal("asset or editor preview inflated visits")
	}
	r := httptest.NewRequest("GET", "http://localhost:8088/c/"+cloak.Slug, nil)
	r.RemoteAddr = "198.51.100.8:1234"
	r.Header.Set("User-Agent", "test-bot")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 404)
	out := statsRead(t, s, u, "cloaks", cloak.ID, "")
	if out.Summary.Views != 4 || out.Summary.Blocked != 1 || out.Summary.Allowed != 3 {
		t.Fatalf("cloak allowed/blocked split missing: %+v", out.Summary)
	}
}
func TestVisitStatisticsPlanBoundsTimezoneAndPagination(t *testing.T) {
	s := testServer(t)
	now := time.Date(2026, 10, 24, 16, 10, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	u := seedUser(t, s, Dollar)
	a := seedApp(t, s, u, 16)
	midnight := time.Date(2026, 10, 24, 16, 0, 0, 0, time.UTC)
	for i := 0; i < 25; i++ {
		s.DB.Create(&AccessVisit{ID: fmt.Sprintf("%032d", i+1), UserID: u.ID, Kind: "apps", ResourceID: a.ID, IP: fmt.Sprintf("198.51.100.%d", i%3+1), Country: "JP", Device: "手机", Outcome: "allowed", CreatedAt: midnight.Add(time.Duration(i) * time.Second)})
	}
	for i, when := range []time.Time{midnight.Add(-time.Second), midnight.AddDate(0, 0, -6), midnight.AddDate(0, 0, -7), midnight.AddDate(0, 0, -29), midnight.AddDate(0, 0, -30)} {
		s.DB.Create(&AccessVisit{ID: fmt.Sprintf("%032d", 100+i), UserID: u.ID, Kind: "apps", ResourceID: a.ID, IP: "203.0.113.1", Outcome: "allowed", Country: "US", Device: "桌面", CreatedAt: when})
	}
	today := statsRead(t, s, u, "apps", a.ID, "")
	if !today.StartAt.Equal(midnight) {
		t.Fatalf("timezone boundary is wrong: %s", today.StartAt)
	}
	if today.Summary.Views != 25 || today.Summary.UniqueIPs != 3 || len(today.Records.Data) != 20 || today.Series[0].Views != 25 {
		t.Fatalf("today boundary or hour bucket incorrect: %+v", today)
	}
	second := statsRead(t, s, u, "apps", a.ID, "?page=2")
	if len(second.Records.Data) != 5 || second.Records.Data[0].ID == today.Records.Data[0].ID {
		t.Fatal("record pagination repeated or lost rows")
	}
	seven := statsRead(t, s, u, "apps", a.ID, "?range=7d")
	if seven.Summary.Views != 27 || seven.Summary.UniqueIPs != 4 || len(seven.Series) != 7 {
		t.Fatalf("7-day bounds incorrect: %+v", seven)
	}
	cookie, _ := sessionFor(t, s, u)
	path := "/api/visit-statistics/apps/" + a.ID
	requireCode(t, call(t, s, "GET", path+"?range=30d", nil, cookie, ""), 403)
	requireCode(t, call(t, s, "GET", path+"?range=90d", nil, cookie, ""), 400)
	if e := s.DB.Model(&u).Update("pro_until", now.Add(time.Hour)).Error; e != nil {
		t.Fatal(e)
	}
	month := statsRead(t, s, u, "apps", a.ID, "?range=30d")
	if month.Summary.Views != 29 || len(month.Series) != 30 || len(month.AllowedRanges) != 3 {
		t.Fatalf("paid monthly range incorrect: %+v", month)
	}
	s.DB.Model(&u).Update("pro_until", now)
	requireCode(t, call(t, s, "GET", path+"?range=30d", nil, cookie, ""), 403)
	// Changing the public identifier must not expose historical app statistics to another owner.
	other := seedUser(t, s, 0)
	otherApp := seedApp(t, s, other, 16)
	if out := statsRead(t, s, other, "apps", otherApp.ID, ""); out.Summary.Views != 0 {
		t.Fatal("owner isolation failed")
	}
}
func TestVisitStatisticsRoutingChainsAndRename(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	a := seedApp(t, s, u, 16)
	s.DB.Model(&a).Update("download_mode", "info")
	initial := statsPublic(s, "GET", "/a/"+a.Slug, "198.51.100.1")
	requireCode(t, initial, 302)
	if out := statsRead(t, s, u, "apps", a.ID, ""); out.Summary.Views != 0 {
		t.Fatal("landing redirect counted twice")
	}
	requireCode(t, statsPublic(s, "GET", initial.Header().Get("Location"), "198.51.100.1"), 200)
	if out := statsRead(t, s, u, "apps", a.ID, ""); out.Summary.Views != 1 {
		t.Fatal("landing page not counted")
	}
	cookie, csrf := sessionFor(t, s, u)
	body := map[string]any{"slug": "stats-original", "enabled": true, "urls": []string{"https://example.com/a.apk"}}
	requireCode(t, call(t, s, "POST", "/api/traffic-pools", body, cookie, csrf), 200)
	requireCode(t, statsPublic(s, "GET", "/r/stats-original", "198.51.100.1"), 302)
	body["slug"] = "stats-renamed"
	requireCode(t, call(t, s, "PUT", "/api/traffic-pools/stats-original", body, cookie, csrf), 200)
	if out := statsRead(t, s, u, "traffic", "stats-renamed", ""); out.Summary.Views != 1 {
		t.Fatal("renaming a pool lost its visit history")
	}
	requireCode(t, call(t, s, "DELETE", "/api/traffic-pools/stats-renamed", nil, cookie, csrf), 200)
	requireCode(t, call(t, s, "POST", "/api/traffic-pools", body, cookie, csrf), 200)
	if out := statsRead(t, s, u, "traffic", "stats-renamed", ""); out.Summary.Views != 0 {
		t.Fatal("new routing pool inherited deleted pool history")
	}
	cloak := Cloak{ID: randomHex(16), UserID: u.ID, Slug: randomHex(8), TargetType: "app", TargetID: a.ID, AppTargetMode: "info", Enabled: true}
	s.DB.Create(&cloak)
	requireCode(t, statsPublic(s, "GET", "/c/"+cloak.Slug, "198.51.100.1"), 200)
	if out := statsRead(t, s, u, "cloaks", cloak.ID, ""); out.Summary.Views != 1 {
		t.Fatal("inline cloak rendering counted twice")
	}
	if out := statsRead(t, s, u, "apps", a.ID, ""); out.Summary.Views != 2 {
		t.Fatal("inline APK content access not counted")
	}
}
func TestVisitStatisticsConcurrentVisitsRetentionAndFailureIsolation(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	a := seedApp(t, s, u, 16)
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := statsPublic(s, "GET", "/a/"+a.Slug+"/info", "198.51.100.1")
			if w.Code != 200 {
				t.Errorf("visit failed: %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if out := statsRead(t, s, u, "apps", a.ID, ""); out.Summary.Views != 24 || out.Summary.UniqueIPs != 1 {
		t.Fatalf("parallel visits were lost or uniqueness inflated: %+v", out.Summary)
	}
	stale := AccessVisit{ID: randomHex(16), UserID: u.ID, Kind: "apps", ResourceID: a.ID, IP: "203.0.113.10", CreatedAt: s.Now().AddDate(0, 0, -36)}
	s.DB.Create(&stale)
	s.cleanupVisitStatistics(context.Background())
	if countRows(t, s, &AccessVisit{}) != 24 {
		t.Fatal("retention deleted current visits or kept expired data")
	}
	if countRows(t, s, &Download{}) != 0 || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("statistics changed financial records")
	}
	s.DB.Migrator().DropTable(&AccessVisit{})
	if w := statsPublic(s, "GET", "/a/"+a.Slug+"/info", "198.51.100.1"); w.Code != 200 || !strings.Contains(w.Body.String(), "d1-native-download") {
		t.Fatal("analytics failure interrupted public application delivery")
	}
}

func TestVisitStatisticsMigrationPreservesRoutingIdentityAndBalances(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	p := TrafficPool{UserID: u.ID, Slug: "legacy-stats", Name: "Existing pool", Mode: "sequential", Cursor: 4, Enabled: true}
	if e := s.DB.Create(&p).Error; e != nil {
		t.Fatal(e)
	}
	var before TrafficPool
	s.DB.First(&before, "slug = ?", p.Slug)
	beforeJSON, _ := json.Marshal(before)
	if e := s.DB.Migrator().DropColumn(&TrafficPool{}, "statistics_id"); e != nil {
		t.Fatal(e)
	}
	if e := s.DB.Migrator().DropTable(&AccessVisit{}); e != nil {
		t.Fatal(e)
	}
	s.DB.Where("version = ?", 18).Delete(&Migration{})
	migrateAfterRestart := func() {
		t.Helper()
		if s.DB.Dialector.Name() == "postgres" {
			// Startup uses fresh connections. This test deliberately changes the
			// SELECT * shape; do not reuse pgx's pre-DDL prepared query cache.
			db, err := OpenDB(s.Config)
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := db.DB()
			t.Cleanup(func() { _ = sqlDB.Close() })
			s.DB = db
		}
		if e := Migrate(s.DB); e != nil {
			t.Fatal(e)
		}
	}
	migrateAfterRestart()
	var after TrafficPool
	s.DB.First(&after, "slug = ?", p.Slug)
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) != string(afterJSON) || len(after.StatisticsID) != 32 {
		t.Fatalf("migration changed existing routing or failed to initialize identity: %s %s", beforeJSON, afterJSON)
	}
	first := after.StatisticsID
	migrateAfterRestart()
	s.DB.First(&after, "slug = ?", p.Slug)
	var owner User
	s.DB.First(&owner, "id = ?", u.ID)
	if after.StatisticsID != first || owner.BalanceMicros != Dollar || owner.HeldMicros != 0 || countRows(t, s, &AccessVisit{}) != 0 || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("migration replay changed identity, balances or business records")
	}
}
