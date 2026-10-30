package platform

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func apkButtonPool(t *testing.T, s *Server, u User, urls ...string) TrafficPool {
	t.Helper()
	p := TrafficPool{Slug: randomHex(4), UserID: u.ID, Enabled: true, Name: "Bound routing", Mode: "sequential"}
	if err := s.DB.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	for i, raw := range urls {
		if err := s.DB.Create(&TrafficLink{ID: randomHex(16), PoolSlug: p.Slug, UserID: u.ID, Position: i, URL: raw, Status: "pending"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return p
}
func apkButtonClick(s *Server, a Application, accept string) *httptest.ResponseRecorder {
	body := url.Values{"request_key": {randomHex(16)}}
	r := httptest.NewRequest("POST", "http://localhost:8088/a/"+a.Slug+"/button", strings.NewReader(body.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", accept)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestAPKTrafficButtonOwnedBindingAndAllTemplates(t *testing.T) {
	s := testServer(t)
	u, other := seedUser(t, s, 10*Dollar), seedUser(t, s, 0)
	a := seedApp(t, s, u, 32)
	p := apkButtonPool(t, s, u, "https://one.example/app.apk")
	foreign := apkButtonPool(t, s, other, "https://foreign.example/h5")
	cookie, csrf := sessionFor(t, s, u)
	path := "/api/apps/" + a.ID + "/page-design"
	d := defaultPageDesign(a)
	d.ButtonAction = "traffic"
	d.TrafficPoolID = foreign.StatisticsID
	requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 400)
	requireCode(t, call(t, s, "POST", path+"/preview", d, cookie, csrf), 400)
	d.TrafficPoolID = ""
	requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 400)
	d.TrafficPoolID = p.StatisticsID
	d.ExternalBrowser = &ExternalBrowserConfig{Enabled: true, URL: "https://external.example"}
	requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 400)
	d.ExternalBrowser = nil
	for _, layout := range []string{"google_play", "custom", "store", "minimal"} {
		d.Layout = layout
		requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 200)
		page := call(t, s, "GET", "/a/"+a.Slug+"/info", nil, nil, "")
		requireCode(t, page, 200)
		if !strings.Contains(page.Body.String(), `action="/a/`+a.Slug+`/button"`) || !strings.Contains(page.Body.String(), "data-traffic-button") {
			t.Fatalf("%s omitted routing action", layout)
		}
		requireCode(t, call(t, s, "POST", path+"/preview", d, cookie, csrf), 200)
	}
	diy := publishDIYFixture(t, s, u, createDIYFixture(t, s, u, "Button page"))
	d.Layout = "diy"
	d.DIYLanding = &DIYLandingReference{PageID: diy.ID, RevisionID: diy.LatestPublishedRevisionID}
	requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 200)
	page := call(t, s, "GET", "/a/"+a.Slug+"/info", nil, nil, "")
	requireCode(t, page, 200)
	if !strings.Contains(page.Body.String(), `action="/a/`+a.Slug+`/button"`) || !strings.Contains(page.Body.String(), "data-traffic-button") {
		t.Fatal("DIY omitted routing action")
	}
	if countRows(t, s, &Ticket{}) != 0 {
		t.Fatal("page previews or rendering created download tickets")
	}
	pools := call(t, s, "GET", "/api/traffic-pools", nil, cookie, "")
	requireCode(t, pools, 200)
	if !strings.Contains(pools.Body.String(), `"binding_id":"`+p.StatisticsID+`"`) || strings.Contains(pools.Body.String(), foreign.StatisticsID) {
		t.Fatal("binding choices ownership or identity mismatch")
	}
}
func TestAPKTrafficButtonSequentialRenameUnavailableAndDirectDownload(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, 100*Dollar)
	a := seedApp(t, s, u, 32)
	urls := []string{"https://apk.example/app.apk", "https://pwa.example/p/demo/", "https://external.example/h5"}
	p := apkButtonPool(t, s, u, urls...)
	d := defaultPageDesign(a)
	d.ButtonAction = "traffic"
	d.TrafficPoolID = p.StatisticsID
	cookie, csrf := sessionFor(t, s, u)
	requireCode(t, call(t, s, "PUT", "/api/apps/"+a.ID+"/page-design", d, cookie, csrf), 200)
	for _, expected := range append(urls, urls[0]) {
		w := apkButtonClick(s, a, "application/json")
		requireCode(t, w, 200)
		var out map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		if out["redirect_url"] != expected || out["download_url"] != "" {
			t.Fatal("sequential routing mismatch", out)
		}
	}
	if countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("routing created source download tickets or charges")
	}
	rename := map[string]any{"slug": "newroute", "name": p.Name, "enabled": true, "mode": "sequential", "risk_enabled": false, "urls": urls}
	requireCode(t, call(t, s, "PUT", "/api/traffic-pools/"+p.Slug, rename, cookie, csrf), 200)
	native := apkButtonClick(s, a, "text/html")
	requireCode(t, native, 200)
	if !strings.Contains(native.Body.String(), urls[0]) || !strings.Contains(native.Body.String(), `http-equiv="refresh"`) {
		t.Fatal("rename broke stable binding")
	}
	// The explicit direct link continues to download this APK, independently of the page button.
	direct := call(t, s, "GET", "/a/"+a.Slug+"/direct", nil, nil, "")
	requireCode(t, direct, 307)
	if !strings.HasPrefix(direct.Header().Get("Location"), "/d/") {
		t.Fatal("direct download changed")
	}
	tickets := countRows(t, s, &Ticket{})
	if err := s.DB.Model(&TrafficPool{}).Where("statistics_id = ?", p.StatisticsID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, apkButtonClick(s, a, "application/json"), 503)
	requireCode(t, call(t, s, "DELETE", "/api/traffic-pools/newroute", nil, cookie, csrf), 200)
	replacement := apkButtonPool(t, s, u, "https://replacement.example/")
	if err := s.DB.Model(&replacement).Update("slug", "newroute").Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, apkButtonClick(s, a, "application/json"), 503)
	if countRows(t, s, &Ticket{}) != tickets {
		t.Fatal("unavailable routing silently downloaded original APK")
	}
	d.ButtonAction = "download"
	d.TrafficPoolID = ""
	requireCode(t, call(t, s, "PUT", "/api/apps/"+a.ID+"/page-design", d, cookie, csrf), 200)
	out := apkButtonClick(s, a, "application/json")
	requireCode(t, out, 200)
	if !strings.Contains(out.Body.String(), `"download_url":"/d/`) {
		t.Fatal("removing routing did not restore downloads")
	}
}
func TestAPKTrafficButtonRiskFilterAndGlobalLanguage(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, 100*Dollar)
	a := seedApp(t, s, u, 32)
	p := apkButtonPool(t, s, u, "https://risky.example/one.apk", "https://risky.example/two", "https://good.example/pwa", "https://unknown.example/", "https://old.example/")
	if err := s.DB.Model(&p).Update("risk_enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	now := s.Now()
	old := now.Add(-121 * time.Minute)
	for pos, status := range []string{"unsafe", "clean", "clean", "unknown", "clean"} {
		checked := now
		if pos == 4 {
			checked = old
		}
		if err := s.DB.Model(&TrafficLink{}).Where("pool_slug = ? AND position = ?", p.Slug, pos).Updates(map[string]any{"status": status, "checked_at": checked}).Error; err != nil {
			t.Fatal(err)
		}
	}
	d := defaultPageDesign(a)
	d.ButtonAction = "traffic"
	d.TrafficPoolID = p.StatisticsID
	locale := d
	locale.ButtonAction = "download"
	locale.TrafficPoolID = ""
	locale.Language = "en-US"
	d.Localizations = []PageDesign{locale}
	d.LanguagePriority = []string{"en-US"}
	cookie, csrf := sessionFor(t, s, u)
	requireCode(t, call(t, s, "PUT", "/api/apps/"+a.ID+"/page-design", d, cookie, csrf), 200)
	if !d.localized("en-US").UsesTraffic() {
		t.Fatal("locale overrode global button binding")
	}
	w := apkButtonClick(s, a, "application/json")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), "https://good.example/pwa") {
		t.Fatal("confirmed unsafe host entered routing")
	}
	if err := s.DB.Model(&TrafficLink{}).Where("pool_slug = ? AND position = ?", p.Slug, 2).Update("status", "unsafe").Error; err != nil {
		t.Fatal(err)
	}
	w = apkButtonClick(s, a, "application/json")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), "https://unknown.example/") {
		t.Fatal("unchecked target was blocked by the detector")
	}
	if countRows(t, s, &Ticket{}) != 0 {
		t.Fatal("external routing created a source ticket")
	}
}

func TestAPKTrafficButtonVisitStatistics(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, 100*Dollar)
	a := seedApp(t, s, u, 32)
	p := apkButtonPool(t, s, u, "https://external.example/h5")
	d := defaultPageDesign(a)
	d.ButtonAction = "traffic"
	d.TrafficPoolID = p.StatisticsID
	cookie, csrf := sessionFor(t, s, u)
	requireCode(t, call(t, s, "PUT", "/api/apps/"+a.ID+"/page-design", d, cookie, csrf), 200)

	// Rendering and previewing the APK do not visit its bound routing pool.
	requireCode(t, statsPublic(s, "GET", "/a/"+a.Slug+"/info", "198.51.100.8"), 200)
	requireCode(t, call(t, s, "POST", "/api/apps/"+a.ID+"/page-design/preview", d, cookie, csrf), 200)
	if out := statsRead(t, s, u, "traffic", p.Slug, ""); out.Summary.Views != 0 {
		t.Fatal("page view or preview inflated routing visits")
	}

	click := func(accept, key string) *httptest.ResponseRecorder {
		body := url.Values{"request_key": {key}}
		r := httptest.NewRequest("POST", "http://localhost:8088/a/"+a.Slug+"/button", strings.NewReader(body.Encode()))
		r.RemoteAddr = "198.51.100.8:1234"
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Accept", accept)
		r.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 14; Mobile) Chrome/123")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	ticketsBefore, ledgerBefore := countRows(t, s, &Ticket{}), countRows(t, s, &Ledger{})
	// Invalid button submissions must not become visits.
	requireCode(t, click("application/json", "invalid"), 400)
	if out := statsRead(t, s, u, "traffic", p.Slug, ""); out.Summary.Views != 0 {
		t.Fatal("invalid button submission counted")
	}
	for i, accept := range []string{"application/json", "text/html"} {
		requireCode(t, click(accept, randomHex(16)), 200)
		out := statsRead(t, s, u, "traffic", p.Slug, "")
		if out.Summary.Views != int64(i+1) || out.Summary.UniqueIPs != 1 || out.Summary.Allowed != int64(i+1) || out.Records.Total != int64(i+1) {
			t.Fatalf("button click lost or counted twice: %+v", out)
		}
		row := out.Records.Data[0]
		if row.IP != "198.51.100.8" || row.Device != "手机" || row.OS != "Android" || row.Browser != "Chrome" || row.Outcome != "allowed" {
			t.Fatalf("button visitor details lost: %+v", row)
		}
	}
	// The public GET entry contributes to the same pool, while HEAD is ignored.
	requireCode(t, statsPublic(s, "GET", "/r/"+p.Slug, "198.51.100.9"), 302)
	requireCode(t, statsPublic(s, "HEAD", "/r/"+p.Slug, "198.51.100.9"), 302)
	out := statsRead(t, s, u, "traffic", p.Slug, "")
	if out.Summary.Views != 3 || out.Summary.UniqueIPs != 2 || out.Records.Total != 3 {
		t.Fatalf("GET and button statistics did not aggregate: %+v", out)
	}
	if out := statsRead(t, s, u, "apps", a.ID, ""); out.Summary.Views != 1 {
		t.Fatal("routing clicks inflated APK page views")
	}
	if countRows(t, s, &Ticket{}) != ticketsBefore || countRows(t, s, &Ledger{}) != ledgerBefore {
		t.Fatal("routing statistics changed downloads or charges")
	}
	// Renaming keeps the immutable statistics identity used by the APK binding.
	renamed := "button-stats-renamed"
	requireCode(t, call(t, s, "PUT", "/api/traffic-pools/"+p.Slug, map[string]any{"slug": renamed, "enabled": true, "urls": []string{"https://external.example/h5"}}, cookie, csrf), 200)
	requireCode(t, click("application/json", randomHex(16)), 200)
	if out := statsRead(t, s, u, "traffic", renamed, ""); out.Summary.Views != 4 {
		t.Fatal("renaming lost bound-button visit history")
	}
	// Analytics failure must never prevent the button from returning its target.
	if err := s.DB.Migrator().DropTable(&AccessVisit{}); err != nil {
		t.Fatal(err)
	}
	requireCode(t, click("application/json", randomHex(16)), 200)
}
