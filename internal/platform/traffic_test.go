package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTrafficShortSlugsAndAtomicRename(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	body := map[string]any{"name": "短标识测试", "enabled": true, "mode": "sequential", "urls": []string{"https://one.example.com/app.apk", "https://two.example.com/app.apk"}}
	requireCode(t, call(t, s, "POST", "/api/traffic-pools", body, cookie, csrf), 200)
	var p TrafficPool
	if err := s.DB.First(&p, "user_id = ?", u.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(p.Slug) != 8 {
		t.Fatalf("expected 8 random characters: %s", p.Slug)
	}
	original := p.Slug
	body["slug"] = " My-Route "
	requireCode(t, call(t, s, "PUT", "/api/traffic-pools/"+original, body, cookie, csrf), 200)
	requireCode(t, call(t, s, "GET", "/r/"+original, nil, nil, ""), 404)
	requireCode(t, call(t, s, "GET", "/r/my-route", nil, nil, ""), 302)
	var links []TrafficLink
	if err := s.DB.Where("user_id = ?", u.ID).Order("position").Find(&links).Error; err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 || links[0].PoolSlug != "my-route" || links[1].PoolSlug != "my-route" {
		t.Fatal("rename lost link membership")
	}
	var count int64
	s.DB.Model(&TrafficPool{}).Where("user_id = ?", u.ID).Count(&count)
	if count != 1 {
		t.Fatal("rename created a duplicate pool")
	}
	requireCode(t, call(t, s, "POST", "/api/traffic-pools", body, cookie, csrf), 409)
	other := seedUser(t, s, 0)
	otherCookie, otherCSRF := sessionFor(t, s, other)
	requireCode(t, call(t, s, "POST", "/api/traffic-pools", body, otherCookie, otherCSRF), 409)
	requireCode(t, call(t, s, "PUT", "/api/traffic-pools/my-route", body, otherCookie, otherCSRF), 404)
	body["slug"] = "occupied"
	requireCode(t, call(t, s, "POST", "/api/traffic-pools", body, cookie, csrf), 200)
	body["name"] = "不应保存"
	requireCode(t, call(t, s, "PUT", "/api/traffic-pools/my-route", body, cookie, csrf), 409)
	p = TrafficPool{}
	if err := s.DB.First(&p, "slug = ?", "my-route").Error; err != nil {
		t.Fatal(err)
	}
	if p.Name != "短标识测试" {
		t.Fatal("conflicting rename changed pool settings")
	}
	requireCode(t, call(t, s, "GET", "/r/my-route", nil, nil, ""), 302)
	body["slug"] = "../invalid"
	requireCode(t, call(t, s, "PUT", "/api/traffic-pools/my-route", body, cookie, csrf), 400)
}

func TestTrafficManagedPublicDomainAudiences(t *testing.T) {
	s := testServer(t)
	s.Config.Env = "production"
	s.Config.PublicURL = "https://control.example.com"
	free, paid := seedUser(t, s, 0), seedUser(t, s, 0)
	if err := s.DB.Model(&paid).Update("pro_until", s.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	x, err := getSettings(s.DB)
	if err != nil {
		t.Fatal(err)
	}
	x.DownloadDomainRules = []DownloadDomainRule{
		{URL: "https://all1.download.example", Audience: "all"},
		{URL: "https://all2.download.example", Audience: "all"},
		{URL: "https://paid1.download.example", Audience: "paid"},
		{URL: "https://paid2.download.example", Audience: "paid"},
	}
	if err := s.DB.Save(&x).Error; err != nil {
		t.Fatal(err)
	}
	for _, owner := range []User{free, paid} {
		p := TrafficPool{Slug: randomHex(4), UserID: owner.ID, Enabled: true, Mode: "sequential"}
		if err := s.DB.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.DB.Create(&TrafficLink{ID: randomHex(16), UserID: owner.ID, PoolSlug: p.Slug, URL: "https://target.example/app.apk", Position: 1}).Error; err != nil {
			t.Fatal(err)
		}
		for _, rule := range x.DownloadDomainRules {
			request := func(want int) {
				t.Helper()
				var before TrafficPool
				if err := s.DB.First(&before, "slug = ?", p.Slug).Error; err != nil {
					t.Fatal(err)
				}
				r := httptest.NewRequest("GET", rule.URL+"/r/"+p.Slug, nil)
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				requireCode(t, w, want)
				if want == 302 && w.Header().Get("Location") != "https://target.example/app.apk" {
					t.Fatal("public traffic domain changed the pool target")
				}
				if want == 404 {
					var after TrafficPool
					if err := s.DB.First(&after, "slug = ?", p.Slug).Error; err != nil || after.Cursor != before.Cursor || w.Header().Get("Location") != "" {
						t.Fatal("denied route advanced the cursor or revealed its target", err)
					}
				}
			}
			want := 302
			if rule.Audience == "paid" && owner.ID == free.ID {
				want = 404
			}
			request(want)
			if rule.Audience == "paid" && owner.ID == paid.ID {
				if err := s.DB.Model(&paid).Update("pro_until", s.Now().Add(-time.Second)).Error; err != nil {
					t.Fatal(err)
				}
				request(404)
				if err := s.DB.Model(&paid).Update("pro_until", s.Now().Add(time.Hour)).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestTrafficMultiplePoolsSequentialAndOwnership(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	if err := s.DB.Model(&u).Update("pro_until", s.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := sessionFor(t, s, u)
	t.Setenv("GOOGLE_WEB_RISK_API_KEY", "fixture")
	urls := []string{"https://z.example.com/1.apk", "https://a.example.com/2.apk", "https://m.example.com/3.apk"}
	for _, mode := range []string{"sequential", "random"} {
		requireCode(t, call(t, s, "POST", "/api/traffic-pools", map[string]any{"name": mode, "enabled": true, "risk_enabled": true, "mode": mode, "urls": urls}, cookie, csrf), 200)
	}
	var pools []TrafficPool
	if err := s.DB.Where("user_id = ?", u.ID).Order("name").Find(&pools).Error; err != nil {
		t.Fatal(err)
	}
	if len(pools) != 2 {
		t.Fatalf("expected two independent pools: %v", pools)
	}
	var sequential TrafficPool
	for _, p := range pools {
		if p.Mode == "sequential" {
			sequential = p
		}
	}
	if err := s.DB.Model(&TrafficLink{}).Where("pool_slug = ?", sequential.Slug).Updates(map[string]any{"status": "clean", "checked_at": s.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&TrafficLink{}).Where("pool_slug = ? AND position = ?", sequential.Slug, 1).Update("status", "unsafe").Error; err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{urls[0], urls[2], urls[0], urls[2]} {
		w := call(t, s, "GET", "/r/"+sequential.Slug, nil, nil, "")
		requireCode(t, w, 302)
		if got := w.Header().Get("Location"); got != want {
			t.Fatalf("got %s want %s", got, want)
		}
	}
	// The other pool has not been checked and must not inherit eligibility.
	requireCode(t, call(t, s, "GET", "/r/"+pools[0].Slug, nil, nil, ""), 503)
	other := seedUser(t, s, 0)
	otherCookie, otherCSRF := sessionFor(t, s, other)
	requireCode(t, call(t, s, "PUT", "/api/traffic-pools/"+sequential.Slug, map[string]any{"enabled": false, "mode": "random", "urls": urls}, otherCookie, otherCSRF), 404)
	requireCode(t, call(t, s, "DELETE", "/api/traffic-pools/"+sequential.Slug, nil, otherCookie, otherCSRF), 404)
	list := call(t, s, "GET", "/api/traffic-pools", nil, cookie, "")
	requireCode(t, list, 200)
	var out struct {
		Data []struct {
			Links []TrafficLink `json:"links"`
		} `json:"data"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 2 || len(out.Data[0].Links) != 3 {
		t.Fatal("incorrect list response")
	}
	requireCode(t, call(t, s, "DELETE", "/api/traffic-pools/"+sequential.Slug, nil, cookie, csrf), 200)
	requireCode(t, call(t, s, "GET", "/r/"+sequential.Slug, nil, nil, ""), 404)
}

func TestTrafficLegacyMigrationKeepsEntryAndLinks(t *testing.T) {
	s := testServer(t)
	if err := s.DB.Exec("CREATE TABLE traffic_pools (user_id text PRIMARY KEY, slug text, enabled boolean, updated_at timestamp)").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Exec("INSERT INTO traffic_pools VALUES ('legacy-user','legacy-entry',false,CURRENT_TIMESTAMP)").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&TrafficLink{ID: "legacy-link", UserID: "legacy-user", URL: "https://legacy.example.com/app.apk", Status: "pending"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Delete(&Migration{}, "version = 6").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	var p TrafficPool
	var l TrafficLink
	if err := s.DB.First(&p, "slug = ?", "legacy-entry").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.First(&l, "id = ?", "legacy-link").Error; err != nil {
		t.Fatal(err)
	}
	if p.Mode != "random" || l.PoolSlug != p.Slug {
		t.Fatal("migration lost legacy entry")
	}
	if err := s.DB.Delete(&p).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	var count int64
	s.DB.Model(&TrafficPool{}).Where("slug = ?", p.Slug).Count(&count)
	if count != 0 {
		t.Fatal("restart resurrected deleted legacy pool")
	}
}

func TestTrafficRiskLookupHandlesFailures(t *testing.T) {
	for _, tc := range []struct {
		body       string
		code       int
		risk, fail bool
	}{
		{`{}`, 200, false, false}, {`{"threat":{"threatTypes":["MALWARE"]}}`, 200, true, false}, {`{}`, 403, false, true}, {`invalid`, 200, false, true},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Goog-Api-Key") != "fixture" || r.URL.Query().Get("uri") != "https://example.com/app.apk" {
				t.Error("missing credentials or complete URL")
			}
			w.WriteHeader(tc.code)
			_, _ = w.Write([]byte(tc.body))
		}))
		risk, err := lookupWebRisk(context.Background(), server.Client(), server.URL, "fixture", "https://example.com/app.apk")
		server.Close()
		if risk != tc.risk || (err != nil) != tc.fail {
			t.Fatalf("lookup returned risk=%v error=%v", risk, err)
		}
	}
}

func TestTrafficExcludesRiskyHostsUnknownAndStale(t *testing.T) {
	now := time.Now()
	old := now.Add(-121 * time.Minute)
	links := []TrafficLink{
		{URL: "https://bad.example.com/one.apk", Status: "unsafe", CheckedAt: &now},
		{URL: "https://bad.example.com/two.apk", Status: "clean", CheckedAt: &now},
		{URL: "https://unknown.example.com/a.apk", Status: "unknown", CheckedAt: &now},
		{URL: "https://old.example.com/a.apk", Status: "clean", CheckedAt: &old},
		{URL: "https://ok.example.com/a.apk", Status: "clean", CheckedAt: &now},
	}
	out := eligibleTrafficLinks(links, now)
	if len(out) != 1 || out[0].URL != links[4].URL {
		t.Fatalf("unexpected eligible links: %+v", out)
	}
}

func TestTrafficPaidGateAndPublicRouting(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	body := map[string]any{"enabled": true, "risk_enabled": true, "urls": []string{"https://one.example.com/app.apk", "https://two.example.com/app.apk"}}
	t.Setenv("GOOGLE_WEB_RISK_API_KEY", "fixture")
	requireCode(t, call(t, s, "PUT", "/api/traffic-pool", body, cookie, csrf), 403)
	if err := s.DB.Model(&u).Update("pro_until", s.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, call(t, s, "PUT", "/api/traffic-pool", body, cookie, csrf), 200)
	var pool TrafficPool
	s.DB.First(&pool, "user_id = ?", u.ID)
	requireCode(t, call(t, s, "GET", "/r/"+pool.Slug, nil, nil, ""), 503)
	s.DB.Model(&TrafficLink{}).Where("user_id = ? AND url = ?", u.ID, "https://one.example.com/app.apk").Updates(map[string]any{"status": "clean", "checked_at": s.Now()})
	s.DB.Model(&TrafficLink{}).Where("user_id = ? AND url = ?", u.ID, "https://two.example.com/app.apk").Updates(map[string]any{"status": "unsafe", "checked_at": s.Now()})
	w := call(t, s, "GET", "/r/"+pool.Slug, nil, nil, "")
	requireCode(t, w, 302)
	if w.Header().Get("Location") != "https://one.example.com/app.apk" {
		t.Fatal("routed to excluded link")
	}
	if err := s.DB.Model(&u).Update("pro_until", s.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, call(t, s, "GET", "/r/"+pool.Slug, nil, nil, ""), 302)
}

func TestFreeTrafficDoesNotRequireGoogleOrChecks(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	t.Setenv("GOOGLE_WEB_RISK_API_KEY", "")
	urls := []string{"https://one.example.com/a.apk", "https://two.example.com/b.apk"}
	for _, mode := range []string{"random", "sequential"} {
		body := map[string]any{"name": mode, "enabled": true, "risk_enabled": false, "mode": mode, "urls": urls}
		requireCode(t, call(t, s, "POST", "/api/traffic-pools", body, cookie, csrf), 200)
		var p TrafficPool
		if err := s.DB.First(&p, "user_id = ? AND mode = ?", u.ID, mode).Error; err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 4; i++ {
			w := call(t, s, "GET", "/r/"+p.Slug, nil, nil, "")
			requireCode(t, w, 302)
			got := w.Header().Get("Location")
			if mode == "sequential" && got != urls[i%2] {
				t.Fatalf("incorrect sequence: %s", got)
			}
			if got != urls[0] && got != urls[1] {
				t.Fatalf("unexpected target %s", got)
			}
		}
		body["risk_enabled"] = true
		requireCode(t, call(t, s, "PUT", "/api/traffic-pools/"+p.Slug, body, cookie, csrf), 403)
	}
	if err := s.DB.Model(&u).Update("pro_until", s.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, call(t, s, "POST", "/api/traffic-pools", map[string]any{"enabled": true, "risk_enabled": true, "urls": urls}, cookie, csrf), 503)
}
