package platform

import (
	"encoding/json"
	"html"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

func externalClickURL(s *Server, kind, id, base, target string) string {
	return base + "external/open?key=" + randomHex(16) + "&token=" + url.QueryEscape(s.externalClickToken(kind, id, target))
}

func TestExternalBrowserValidation(t *testing.T) {
	for _, target := range []string{"", "example.com", "//example.com", "javascript:alert(1)", "https:example.com", "https://a:b@example.com", "https://example.com/a\\b", "https://example.com/a\nb", "https://example.com/a b", "https://example.com/a/abc/external/open", "https://example.com/p/abc/external%2fopen", "https://example.com/" + strings.Repeat("x", 2048)} {
		if normalizeExternalBrowser(&ExternalBrowserConfig{Enabled: true, URL: target}) == nil {
			t.Errorf("accepted invalid target %q", target)
		}
	}
	c := &ExternalBrowserConfig{Enabled: true, URL: " https://example.com/?campaign=1#/checkout?step=2 "}
	if e := normalizeExternalBrowser(c); e != nil || c.URL != "https://example.com/?campaign=1#/checkout?step=2" {
		t.Fatalf("query/hash lost: %+v %v", c, e)
	}
	// Turning the feature off must remain possible with an unfinished draft.
	if e := normalizeExternalBrowser(&ExternalBrowserConfig{URL: "unfinished"}); e != nil {
		t.Fatal(e)
	}
}

func externalRuntime(t *testing.T, page string) externalBrowserRuntime {
	t.Helper()
	match := regexp.MustCompile(`data-external-browser="([^"]*)"`).FindStringSubmatch(page)
	if len(match) != 2 {
		t.Fatal("external browser runtime missing")
	}
	var out externalBrowserRuntime
	if match[1] == "" {
		return out
	}
	if e := json.Unmarshal([]byte(html.UnescapeString(match[1])), &out); e != nil {
		t.Fatal(e)
	}
	return out
}

func TestExternalBrowserAPKSaveRenderRelayAndIsolation(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 5*Dollar)
	app := seedApp(t, s, owner, 128)
	cookie, csrf := sessionFor(t, s, owner)
	d := defaultPageDesign(app)
	d.ExternalBrowser = &ExternalBrowserConfig{Enabled: true, URL: "https://example.com/?source=apk#/checkout"}
	path := "/api/apps/" + app.ID + "/page-design"
	base := "/a/" + app.Slug
	otherCookie, otherCSRF := sessionFor(t, s, seedUser(t, s, 0))
	requireCode(t, call(t, s, "PUT", path, d, otherCookie, otherCSRF), 404)
	for _, layout := range []string{"store", "minimal", "google_play", "custom"} {
		d.Layout = layout
		requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 200)
		page := call(t, s, "GET", base+"/info", nil, nil, "")
		requireCode(t, page, 200)
		config := externalRuntime(t, page.Body.String())
		if !config.Enabled || config.URL != d.ExternalBrowser.URL || config.OpenURL != "http://localhost:8088"+base+"/external/open" || config.Continue {
			t.Fatalf("%s runtime: %+v", layout, config)
		}
	}
	relay := call(t, s, "GET", externalClickURL(s, "apk", app.ID, base+"/", d.ExternalBrowser.URL)+"&url=https://attacker.example", nil, nil, "")
	requireCode(t, relay, 302)
	if relay.Header().Get("Location") != d.ExternalBrowser.URL || relay.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("relay must use stored target and retain fragment")
	}
	if countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &Download{}) != 0 {
		t.Fatal("external opening must not prepare or bill a download")
	}
	blockedUA := "ExternalBrowserBlocked/1"
	if e := s.DB.Create(&UABlock{UserID: owner.ID, UAHash: uaFingerprint(blockedUA), CreatedAt: s.Now()}).Error; e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("GET", "http://localhost:8088"+base+"/external/open", nil)
	r.Header.Set("User-Agent", blockedUA)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 404)
	d.ExternalBrowser.URL = "javascript:alert(1)"
	requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 400)
	d.ExternalBrowser.Enabled = false
	requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 200)
	requireCode(t, call(t, s, "GET", base+"/external/open", nil, nil, ""), 404)
}

func TestExternalBrowserPWASaveTemplatesAndNoInstallation(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	app := createPWAFixture(t, s, owner, "External browser")
	cookie, csrf := sessionFor(t, s, owner)
	base := "/p/" + app.Slug + "/"
	for _, layout := range []string{"legacy", "google_play", "custom"} {
		c := landingFixture()
		c.Template = layout
		c.Settings.W2AAutoDownload = true
		c.Settings.APKURL = "https://example.com/test.apk"
		c.Settings.ExternalBrowser = &ExternalBrowserConfig{Enabled: true, URL: "https://example.com/?source=pwa#/checkout"}
		requireCode(t, call(t, s, "PUT", "/api/pwas/"+app.ID, map[string]any{"name": app.Name, "target_url": app.TargetURL, "enabled": true, "landing_config": c}, cookie, csrf), 200)
		page := pwaPublicCall(t, s, base, "", "")
		requireCode(t, page, 200)
		config := externalRuntime(t, page.Body.String())
		if !config.Enabled || config.URL != c.Settings.ExternalBrowser.URL {
			t.Fatalf("%s runtime: %+v", layout, config)
		}
		if err := s.DB.First(&app, "id = ?", app.ID).Error; err != nil {
			t.Fatal(err)
		}
		if externalRuntime(t, pwaPublicCall(t, s, base+"launch", "", "").Body.String()).Enabled {
			t.Fatal("installed launch page must not intercept")
		}
		runtime := s.pwaLandingRuntime(&app, httptest.NewRequest("GET", "http://localhost:8088"+base, nil))
		if runtime["app"].(map[string]any)["settings"].(map[string]any)["w2aAutoDownload"] != false {
			t.Fatal("external mode must pause automatic APK downloads")
		}
		relay := pwaPublicCall(t, s, externalClickURL(s, "pwa", app.ID, base, c.Settings.ExternalBrowser.URL)+"&url=https://attacker.example", "", "")
		requireCode(t, relay, 302)
		if relay.Header().Get("Location") != c.Settings.ExternalBrowser.URL {
			t.Fatal("PWA target/query/hash changed")
		}
	}
	if countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &MeteredEvent{}) != 0 {
		t.Fatal("external opening created a download or installation")
	}
}

func TestExternalBrowserSignedReturnAndDomainScope(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	app := seedApp(t, s, owner, 128)
	base := "/a/" + app.Slug
	d := defaultPageDesign(app)
	d.ExternalBrowser = &ExternalBrowserConfig{Enabled: true, URL: "http://localhost:8088" + base + "/info?campaign=1#/checkout"}
	if e := s.DB.Create(&AppPageDesign{AppID: app.ID, Design: d}).Error; e != nil {
		t.Fatal(e)
	}
	relay := call(t, s, "GET", externalClickURL(s, "apk", app.ID, base+"/", d.ExternalBrowser.URL), nil, nil, "")
	requireCode(t, relay, 302)
	target, e := url.Parse(relay.Header().Get("Location"))
	if e != nil || target.Fragment != "/checkout" || target.Query().Get("campaign") != "1" {
		t.Fatalf("return target invalid %v %v", target, e)
	}
	target.Fragment = "" // Browsers never send URL fragments in HTTP requests.
	r := httptest.NewRequest("GET", target.String(), nil)
	if !s.validExternalReturn(r, "apk", app.ID, d.ExternalBrowser.URL) || s.validExternalReturn(r, "pwa", app.ID, d.ExternalBrowser.URL) || s.validExternalReturn(r, "apk", "other", d.ExternalBrowser.URL) || s.validExternalReturn(r, "apk", app.ID, "https://other.example") {
		t.Fatal("return token scope invalid")
	}
	var runtime externalBrowserRuntime
	_ = json.Unmarshal([]byte(s.externalBrowserJSON(r, "apk", app.ID, app.Slug, app.UserID, d.ExternalBrowser)), &runtime)
	if !runtime.Continue || !runtime.SelfTarget {
		t.Fatal("external browser return must resume normal flow")
	}
	target.RawQuery = "__d1_browser_return=forged"
	if s.validExternalReturn(httptest.NewRequest("GET", target.String(), nil), "apk", app.ID, d.ExternalBrowser.URL) {
		t.Fatal("forged return accepted")
	}
	now := s.Now()
	s.Now = func() time.Time { return now.Add(11 * time.Minute) }
	if s.validExternalReturn(r, "apk", app.ID, d.ExternalBrowser.URL) {
		t.Fatal("expired return accepted")
	}
	foreign := seedUser(t, s, 0)
	if e := s.DB.Create(&Domain{ID: randomHex(16), UserID: foreign.ID, Hostname: "foreign.example", VerifiedAt: &now}).Error; e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse("https://foreign.example" + base + "/info")
	if s.externalLandingTarget(r, "apk", app.Slug, app.UserID, u) {
		t.Fatal("another tenant's domain treated as own landing")
	}
	requireCode(t, pwaPublicCall(t, s, base+"/external/open", "foreign.example", ""), 404)
	if e := s.DB.Model(&owner).Update("disabled", true).Error; e != nil {
		t.Fatal(e)
	}
	requireCode(t, call(t, s, "GET", base+"/external/open", nil, nil, ""), 404)
}
