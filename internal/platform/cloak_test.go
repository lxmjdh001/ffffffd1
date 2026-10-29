package platform

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func cloakFixture() map[string]any {
	return map[string]any{
		"name": "规则一", "enabled": true, "target_type": "url",
		"target_url":        "https://target.example/path?campaign=old&keep=yes#section",
		"safe_url":          "https://safe.example/help?fixed=yes",
		"blocked_languages": []string{"zh"}, "blocked_uas": []string{},
		"blocked_countries": []string{}, "allowed_platforms": []string{}, "blocked_devices": []CloakDeviceRule{},
	}
}

func createTestCloak(t *testing.T, s *Server, user User, input map[string]any) cloakView {
	t.Helper()
	cookie, csrf := sessionFor(t, s, user)
	w := call(t, s, "POST", "/api/cloaks", input, cookie, csrf)
	requireCode(t, w, 200)
	var out cloakView
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ID == "" || len(out.Slug) != 8 || out.EntryURL != s.Config.PublicURL+"/c/"+out.Slug {
		t.Fatalf("missing entry identity: %+v", out)
	}
	return out
}

func TestCloakCRUDIsolationCSRFAndMigration(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, 0), seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	otherCookie, otherCSRF := sessionFor(t, s, other)
	body := cloakFixture()
	requireCode(t, call(t, s, "GET", "/api/cloaks", nil, nil, ""), 401)
	requireCode(t, call(t, s, "POST", "/api/cloaks", body, cookie, "bad-csrf"), 403)
	c := createTestCloak(t, s, owner, body)
	w := call(t, s, "GET", "/api/cloaks", nil, cookie, "")
	requireCode(t, w, 200)
	if strings.Contains(w.Body.String(), "user_id") || !strings.Contains(w.Body.String(), c.ID) || !strings.Contains(w.Body.String(), `"country_detection_configured":true`) {
		t.Fatal("incorrect owned list response")
	}
	w = call(t, s, "GET", "/api/cloaks", nil, otherCookie, "")
	requireCode(t, w, 200)
	if strings.Contains(w.Body.String(), c.ID) {
		t.Fatal("list exposed another account's rules")
	}
	requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, otherCookie, otherCSRF), 404)
	requireCode(t, call(t, s, "DELETE", "/api/cloaks/"+c.ID, nil, otherCookie, otherCSRF), 404)
	requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, ""), 403)
	body["name"], body["enabled"] = "更新规则", false
	w = call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf)
	requireCode(t, w, 200)
	var updated cloakView
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Slug != c.Slug || updated.ID != c.ID || updated.Name != "更新规则" || updated.Enabled {
		t.Fatal("update changed stable entry identity or failed to save zero value")
	}
	for range 2 {
		if err := Migrate(s.DB); err != nil {
			t.Fatal(err)
		}
	}
	if countRows(t, s, &Cloak{}) != 1 {
		t.Fatal("migration lost or duplicated a rule")
	}
	// Disabling filtering still serves the configured destination.
	w = call(t, s, "GET", "/c/"+c.Slug, nil, nil, "")
	requireCode(t, w, 302)
	if !strings.HasPrefix(w.Header().Get("Location"), "https://target.example/path") {
		t.Fatal("disabled filtering did not bypass rules")
	}
	requireCode(t, call(t, s, "DELETE", "/api/cloaks/"+c.ID, nil, cookie, ""), 403)
	requireCode(t, call(t, s, "DELETE", "/api/cloaks/"+c.ID, nil, cookie, csrf), 200)
	requireCode(t, call(t, s, "GET", "/c/"+c.Slug, nil, nil, ""), 404)
	if countRows(t, s, &Audit{}) != 3 {
		t.Fatal("CRUD audit entries missing or unauthorized mutation was audited")
	}
}

func TestCloakInputValidationAndTrafficOwnership(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, 0), seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	for _, tc := range []struct {
		name, key string
		value     any
	}{
		{"target kind", "target_type", "unknown"},
		{"relative target", "target_url", "/somewhere"},
		{"script target", "target_url", "javascript:alert(1)"},
		{"credential target", "target_url", "https://user:secret@example.com"},
		{"bad port", "target_url", "https://example.com:70000/path"},
		{"relative safe", "safe_url", "//example.com"},
		{"newline", "safe_url", "https://example.com/\r\nLocation: bad"},
		{"too long name", "name", strings.Repeat("字", 61)},
		{"bad language", "blocked_languages", []string{"zh_foo"}},
		{"bad country", "blocked_countries", []string{"China"}},
		{"empty UA", "blocked_uas", []string{"  "}},
		{"too long UA", "blocked_uas", []string{strings.Repeat("字", 201)}},
		{"bad platform", "allowed_platforms", []string{"instagram"}},
		{"wrong device OS", "blocked_devices", []CloakDeviceRule{{Device: "desktop", Systems: []string{"ios"}}}},
		{"duplicate device", "blocked_devices", []CloakDeviceRule{{Device: "tablet", Systems: []string{"all"}}, {Device: "tablet", Systems: []string{"ios"}}}},
		{"local loop", "target_url", "http://localhost:8088/c/other"},
		{"safe loop", "safe_url", "http://localhost:8088/c/other"},
		{"cleaned loop", "target_url", "http://localhost:8088/other/../c/other"},
		{"encoded loop", "safe_url", "http://localhost:8088/%63/other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := cloakFixture()
			body[tc.key] = tc.value
			requireCode(t, call(t, s, "POST", "/api/cloaks", body, cookie, csrf), 400)
		})
	}
	if countRows(t, s, &Cloak{}) != 0 {
		t.Fatal("invalid inputs created rows")
	}
	pool := TrafficPool{Slug: randomHex(4), UserID: other.ID, Enabled: true, Mode: "sequential"}
	if err := s.DB.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	body := cloakFixture()
	body["target_type"], body["traffic_pool_slug"] = "traffic_pool", pool.Slug
	requireCode(t, call(t, s, "POST", "/api/cloaks", body, cookie, csrf), 404)
	body["traffic_pool_slug"] = "missing"
	requireCode(t, call(t, s, "POST", "/api/cloaks", body, cookie, csrf), 404)
	body = cloakFixture()
	body["blocked_languages"] = []string{" ZH ", "zh"}
	body["blocked_countries"] = []string{" cn ", "CN", "XX"}
	body["blocked_uas"] = []string{" A\x00B\t ", strings.Repeat("字", 200)}
	body["blocked_devices"] = []CloakDeviceRule{{Device: "desktop", Systems: []string{"windows", "all"}}, {Device: "mobile", Systems: []string{}}}
	c := createTestCloak(t, s, owner, body)
	if len(c.BlockedLanguages) != 1 || c.BlockedLanguages[0] != "zh" || len(c.BlockedCountries) != 2 || len(c.BlockedDevices) != 1 || len(c.BlockedDevices[0].Systems) != 1 || c.BlockedDevices[0].Systems[0] != "all" {
		t.Fatal("rules were not canonicalized")
	}
	if len(c.BlockedUAs) != 2 || c.BlockedUAs[0] != "AB" {
		t.Fatal("UA controls not cleaned or character limit treated as bytes")
	}
}

func TestCloakOptionalSafeURLSave(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	for _, safe := range []string{"", " \t\r\n "} {
		body := cloakFixture()
		body["safe_url"] = safe
		c := createTestCloak(t, s, owner, body)
		if c.SafeURL != "" {
			t.Fatal("blank safe URL was not normalized")
		}
		body["safe_url"] = " https://safe.example/contact "
		w := call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf)
		requireCode(t, w, 200)
		var updated cloakView
		if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil || updated.SafeURL != "https://safe.example/contact" {
			t.Fatalf("safe URL update was not normalized: %v", err)
		}
		body["safe_url"] = safe
		w = call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf)
		requireCode(t, w, 200)
		if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil || updated.SafeURL != "" {
			t.Fatalf("safe URL could not be cleared: %v", err)
		}
		body["safe_url"] = "javascript:alert(1)"
		requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf), 400)
		var saved Cloak
		if err := s.DB.First(&saved, "id = ?", c.ID).Error; err != nil || saved.SafeURL != "" {
			t.Fatal("invalid update changed the saved safe URL")
		}
	}
	body := cloakFixture()
	delete(body, "safe_url")
	if c := createTestCloak(t, s, owner, body); c.SafeURL != "" {
		t.Fatal("omitted optional URL did not default to blank")
	}
}

func TestCloakDefault404Routing(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	body := cloakFixture()
	body["safe_url"] = ""
	body["allowed_platforms"] = []string{"tiktok"}
	c := createTestCloak(t, s, owner, body)
	for _, tc := range []struct {
		name, method, language, query string
		code                          int
	}{
		{"allowed visitor", "GET", "en", "ttclid=click", 302},
		{"language blocked", "GET", "zh-CN", "ttclid=click", 404},
		{"platform not allowed", "GET", "en", "", 404},
		{"head blocked", "HEAD", "zh", "ttclid=click", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, c.EntryURL+"?"+tc.query, nil)
			r.Header.Set("Accept-Language", tc.language)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			requireCode(t, w, tc.code)
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cloak response can be cached")
			}
			if tc.code == 302 {
				if !strings.HasPrefix(w.Header().Get("Location"), "https://target.example/path?") {
					t.Fatal("allowed request did not reach the target")
				}
				return
			}
			if w.Header().Get("Location") != "" || w.Header().Get("Content-Type") != "text/html; charset=utf-8" || w.Header().Get("X-Robots-Tag") != "noindex, nofollow" {
				t.Fatal("default 404 response has incorrect headers")
			}
			if tc.method == "HEAD" {
				if w.Body.Len() != 0 {
					t.Fatal("HEAD included a response body")
				}
			} else if !strings.Contains(w.Body.String(), "<h1>Account not found</h1>") || !strings.Contains(w.Body.String(), "Please contact customer support for assistance.") || !strings.Contains(w.Body.String(), `<html lang="en">`) {
				t.Fatal("default page is missing the requested English copy")
			}
		})
	}
	if err := s.DB.Model(&Cloak{}).Where("id = ?", c.ID).Updates(map[string]any{"enabled": false, "status": "off"}).Error; err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", c.EntryURL, nil)
	r.Header.Set("Accept-Language", "zh-CN")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 302)
	if !strings.HasPrefix(w.Header().Get("Location"), "https://target.example/path?") {
		t.Fatal("disabled filtering did not bypass the default 404")
	}
	if err := s.DB.Model(&Cloak{}).Where("id = ?", c.ID).Updates(map[string]any{"enabled": true, "status": "rules", "safe_url": "https://safe.example/help?fixed=yes"}).Error; err != nil {
		t.Fatal(err)
	}
	w = call(t, s, "GET", "/c/"+c.Slug+"?private=never-forward", nil, nil, "")
	requireCode(t, w, 302)
	if w.Header().Get("Location") != "https://safe.example/help?fixed=yes" {
		t.Fatal("configured safe URL was not preserved")
	}
	if err := s.DB.Model(&Cloak{}).Where("id = ?", c.ID).Update("safe_url", "javascript:alert(1)").Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, call(t, s, "GET", "/c/"+c.Slug, nil, nil, ""), 503)
}

func TestCloakDefault404UnavailableTraffic(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	pool := TrafficPool{Slug: randomHex(4), UserID: owner.ID, Enabled: true, Mode: "sequential"}
	if err := s.DB.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	body := cloakFixture()
	body["target_type"], body["traffic_pool_slug"], body["safe_url"] = "traffic_pool", pool.Slug, ""
	c := createTestCloak(t, s, owner, body)
	for _, state := range []string{"empty", "disabled", "deleted"} {
		t.Run(state, func(t *testing.T) {
			if state == "disabled" {
				if err := s.DB.Model(&pool).Update("enabled", false).Error; err != nil {
					t.Fatal(err)
				}
			}
			if state == "deleted" {
				if err := s.DB.Delete(&pool).Error; err != nil {
					t.Fatal(err)
				}
			}
			w := call(t, s, "GET", "/c/"+c.Slug, nil, nil, "")
			requireCode(t, w, 404)
			if !strings.Contains(w.Body.String(), "<h1>Account not found</h1>") || w.Header().Get("Location") != "" {
				t.Fatal("unavailable traffic did not use the default 404")
			}
		})
	}
}

func TestCloakDefault404PrivacyAndSupport(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	body := cloakFixture()
	body["name"], body["safe_url"] = "<script>private-rule</script>", ""
	body["target_url"] = "https://private-target.example/confidential?token=destination-secret"
	body["blocked_uas"] = []string{"private-rule-agent"}
	c := createTestCloak(t, s, owner, body)
	for _, tc := range []struct {
		name, email string
		link        bool
	}{
		{"not configured", "", false},
		{"valid mailbox", " Help@Example.com ", true},
		{"valid unusual mailbox", "help?subject=forged#fragment@example.com", true},
		{"invalid HTML mailbox", `<img src=x onerror="alert(1)">`, false},
		{"invalid protocol", "javascript:alert(1)", false},
		{"invalid header injection", "help@example.com\r\nBcc: attacker@example.com", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.DB.Model(&Settings{}).Where("id = 1").Update("support_email", tc.email).Error; err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", c.EntryURL+"?visitor-token=private-visitor-secret&payload=%3Cscript%3Ealert(1)%3C/script%3E", nil)
			r.Header.Set("User-Agent", "private-rule-agent")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			requireCode(t, w, 404)
			html := w.Body.String()
			if got := strings.Contains(html, `class="support"`); got != tc.link {
				t.Fatalf("support link=%v want %v", got, tc.link)
			}
			if tc.link {
				email, _ := normalizeEmail(tc.email)
				if !strings.Contains(html, `href="mailto:`+url.PathEscape(email)+`"`) || !strings.Contains(html, "Contact support</a>") {
					t.Fatal("support link was not safely encoded")
				}
			}
			for _, private := range []string{c.Name, c.TargetURL, c.Slug, owner.Email, "private-visitor-secret", "private-rule-agent", "<script>", "<img", "onerror=", "?subject=forged", "#fragment@example.com"} {
				if private != "" && strings.Contains(html, private) {
					t.Fatalf("default 404 exposed private or unsafe data: %q", private)
				}
			}
		})
	}
}

func TestCloakRuleMatching(t *testing.T) {
	for _, tc := range []struct {
		name, language, ua, query, country string
		c                                  Cloak
		blocked                            bool
	}{
		{name: "language base", c: Cloak{BlockedLanguages: []string{"zh"}}, language: "en-US,zh-CN;q=0.6", blocked: true},
		{name: "language case", c: Cloak{BlockedLanguages: []string{"zh-cn"}}, language: "ZH-CN", blocked: true},
		{name: "language q zero", c: Cloak{BlockedLanguages: []string{"zh"}}, language: "zh-CN;q=0,en;q=1", blocked: false},
		{name: "language malformed quality", c: Cloak{BlockedLanguages: []string{"zh"}}, language: "zh;q=bad", blocked: false},
		{name: "language NaN quality", c: Cloak{BlockedLanguages: []string{"zh"}}, language: "zh;q=NaN", blocked: false},
		{name: "language positive infinite quality", c: Cloak{BlockedLanguages: []string{"zh"}}, language: "zh;q=+Inf", blocked: false},
		{name: "language child boundary", c: Cloak{BlockedLanguages: []string{"zh-cn"}}, language: "zh-TW", blocked: false},
		{name: "language prefix boundary", c: Cloak{BlockedLanguages: []string{"en"}}, language: "eng", blocked: false},
		{name: "ua case substring", c: Cloak{BlockedUAs: []string{"GoogleBot"}}, ua: "Mozilla googlebot/2", blocked: true},
		{name: "ua no implicit bots", c: Cloak{}, ua: "Googlebot", blocked: false},
		{name: "country selected", c: Cloak{BlockedCountries: []string{"CN"}}, country: "CN", blocked: true},
		{name: "country unknown not selected", c: Cloak{BlockedCountries: []string{"CN"}}, country: "XX", blocked: false},
		{name: "country unknown selected", c: Cloak{BlockedCountries: []string{"XX"}}, country: "XX", blocked: true},
		{name: "platform no gate", c: Cloak{}, blocked: false},
		{name: "platform needs value", c: Cloak{AllowedPlatforms: []string{"tiktok"}}, query: "ttclid=", blocked: true},
		{name: "platform bare key", c: Cloak{AllowedPlatforms: []string{"tiktok"}}, query: "ttclid", blocked: true},
		{name: "platform whitespace", c: Cloak{AllowedPlatforms: []string{"tiktok"}}, query: "ttclid=+", blocked: true},
		{name: "platform nested blocked", c: Cloak{AllowedPlatforms: []string{"tiktok"}}, query: "redirect=" + url.QueryEscape("https://example.com/?ttclid=id"), blocked: true},
		{name: "platform case sensitive", c: Cloak{AllowedPlatforms: []string{"tiktok"}}, query: "TTCLID=id", blocked: true},
		{name: "platform any match", c: Cloak{AllowedPlatforms: []string{"tiktok", "facebook"}}, query: "fbclid=id", blocked: false},
		{name: "platform google", c: Cloak{AllowedPlatforms: []string{"google"}}, query: "gclid=id", blocked: false},
		{name: "platform bigo", c: Cloak{AllowedPlatforms: []string{"bigo"}}, query: "bbg=id", blocked: false},
		{name: "platform tiktok", c: Cloak{AllowedPlatforms: []string{"tiktok"}}, query: "ttclid=id", blocked: false},
		{name: "language still blocks allowed platform", c: Cloak{BlockedLanguages: []string{"zh"}, AllowedPlatforms: []string{"tiktok"}}, language: "zh", query: "ttclid=id", blocked: true},
		{name: "desktop all", c: Cloak{BlockedDevices: []CloakDeviceRule{{Device: "desktop", Systems: []string{"all"}}}}, ua: "Mozilla (Windows NT 10.0)", blocked: true},
		{name: "desktop mac only allows windows", c: Cloak{BlockedDevices: []CloakDeviceRule{{Device: "desktop", Systems: []string{"macos"}}}}, ua: "Mozilla (Windows NT 10.0)", blocked: false},
		{name: "tablet does not block phone", c: Cloak{BlockedDevices: []CloakDeviceRule{{Device: "tablet", Systems: []string{"android"}}}}, ua: "Mozilla (Linux; Android 14; Mobile)", blocked: false},
		{name: "android tablet", c: Cloak{BlockedDevices: []CloakDeviceRule{{Device: "tablet", Systems: []string{"android"}}}}, ua: "Mozilla (Linux; Android 14) Chrome/123", blocked: true},
		{name: "iphone mobile ios", c: Cloak{BlockedDevices: []CloakDeviceRule{{Device: "mobile", Systems: []string{"ios"}}}}, ua: "Mozilla (iPhone; CPU iPhone OS 18_0 like Mac OS X) Mobile/15", blocked: true},
		{name: "ipad tablet ios", c: Cloak{BlockedDevices: []CloakDeviceRule{{Device: "tablet", Systems: []string{"ios"}}}}, ua: "Mozilla (iPad; CPU OS 18_0 like Mac OS X) Mobile/15", blocked: true},
		{name: "ipad desktop mode", c: Cloak{BlockedDevices: []CloakDeviceRule{{Device: "tablet", Systems: []string{"ios"}}}}, ua: "Mozilla (Macintosh; Intel Mac OS X 10_15) Mobile/15 Safari/604", blocked: true},
		{name: "harmony mobile", c: Cloak{BlockedDevices: []CloakDeviceRule{{Device: "mobile", Systems: []string{"harmonyos"}}}}, ua: "Mozilla (Linux; Android 12; HarmonyOS 4; Mobile)", blocked: true},
		{name: "chromeos desktop", c: Cloak{BlockedDevices: []CloakDeviceRule{{Device: "desktop", Systems: []string{"chromeos"}}}}, ua: "Mozilla (X11; CrOS x86_64 15917)", blocked: true},
		{name: "unknown desktop system all", c: Cloak{BlockedDevices: []CloakDeviceRule{{Device: "desktop", Systems: []string{"all"}}}}, blocked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "https://d1apk.com/c/example?"+tc.query, nil)
			r.Header.Set("Accept-Language", tc.language)
			r.Header.Set("User-Agent", tc.ua)
			if got := cloakBlocked(tc.c, r, tc.country); got != tc.blocked {
				t.Fatalf("blocked=%v want %v", got, tc.blocked)
			}
		})
	}
}

func TestCloakDirectRedirectQueryBypassAndNoFetch(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	var fetched atomic.Int64
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fetched.Add(1) }))
	defer targetServer.Close()
	body := cloakFixture()
	body["target_url"] = targetServer.URL + "/target?campaign=old&keep=yes#section"
	c := createTestCloak(t, s, owner, body)
	for _, tc := range []struct {
		method, language, query string
		blocked                 bool
	}{
		{"GET", "en-US", "campaign=new&campaign=other&fbclid=click", false},
		{"HEAD", "en-US", "campaign=new&campaign=other&fbclid=click", false},
		{"GET", "zh-CN", "campaign=new&campaign=other&fbclid=click", true},
	} {
		r := httptest.NewRequest(tc.method, c.EntryURL+"?"+tc.query, nil)
		r.Header.Set("Accept-Language", tc.language)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		requireCode(t, w, 302)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("public rule redirect can be cached")
		}
		location := w.Header().Get("Location")
		if tc.blocked {
			if location != c.SafeURL {
				t.Fatal("safe page received visitor query parameters")
			}
		} else {
			u, err := url.Parse(location)
			if err != nil || len(u.Query()["campaign"]) != 2 || u.Query().Get("campaign") != "new" || u.Query().Get("keep") != "yes" || u.Query().Get("fbclid") != "click" || u.Fragment != "section" {
				t.Fatalf("target query merge failed: %s", location)
			}
		}
	}
	if fetched.Load() != 0 {
		t.Fatal("redirect handler fetched destination on the server")
	}
	if err := s.DB.Model(&Cloak{}).Where("id = ?", c.ID).Updates(map[string]any{"enabled": false, "status": "off"}).Error; err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", c.EntryURL, nil)
	r.Header.Set("Accept-Language", "zh-CN")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 302)
	if !strings.HasPrefix(w.Header().Get("Location"), targetServer.URL) {
		t.Fatal("disabled filtering did not bypass selected language")
	}
	if err := s.DB.Model(&owner).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, call(t, s, "GET", "/c/"+c.Slug, nil, nil, ""), 404)
}

func TestCloakCountryOnlyTrustedEdge(t *testing.T) {
	_, local, _ := net.ParseCIDR("127.0.0.0/8")
	_, internal, _ := net.ParseCIDR("10.0.0.0/8")
	s := &Server{Config: Config{TrustedProxies: []*net.IPNet{local, internal}}}
	for _, tc := range []struct {
		name, peer, forwarded, country, want string
	}{
		{"direct Cloudflare", "173.245.48.5:443", "203.0.113.8", "CN", "CN"},
		{"Cloudflare IPv6", "[2606:4700::1]:443", "", "US", "US"},
		{"direct forged header", "203.0.113.8:443", "", "CN", "XX"},
		{"direct forged chain", "203.0.113.8:443", "173.245.48.5", "CN", "XX"},
		{"nginx behind Cloudflare", "127.0.0.1:1234", "203.0.113.8, 173.245.48.5", "cn", "CN"},
		{"multiple internal proxies", "127.0.0.1:1234", "203.0.113.8, 173.245.48.5, 10.0.0.2", "US", "US"},
		{"nginx direct attack", "127.0.0.1:1234", "173.245.48.5, 203.0.113.8", "CN", "XX"},
		{"untrusted intermediate", "127.0.0.1:1234", "173.245.48.5, 192.0.2.1", "CN", "XX"},
		{"missing edge", "127.0.0.1:1234", "203.0.113.8", "CN", "XX"},
		{"only internal", "127.0.0.1:1234", "10.0.0.2", "CN", "XX"},
		{"missing country", "173.245.48.5:443", "", "", "XX"},
		{"Tor country", "173.245.48.5:443", "", "T1", "T1"},
		{"malformed country", "173.245.48.5:443", "", "China", "XX"},
		{"malformed forwarded hop", "127.0.0.1:1234", "173.245.48.5, invalid", "CN", "XX"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "https://d1apk.com/c/test", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-For", tc.forwarded)
			r.Header.Set("CF-IPCountry", tc.country)
			if got := s.cloakRequestCountry(r); got != tc.want {
				t.Fatalf("country=%s want %s", got, tc.want)
			}
		})
	}
	s.Config.CloakCountryProxies = []*net.IPNet{}
	r := httptest.NewRequest("GET", "https://d1apk.com/c/test", nil)
	r.RemoteAddr = "173.245.48.5:443"
	r.Header.Set("CF-IPCountry", "CN")
	if s.cloakRequestCountry(r) != "XX" {
		t.Fatal("disabled country detection still trusted an edge")
	}
	if networks, err := parseCloakCountryNetworks(""); err != nil || len(networks) != 22 {
		t.Fatal("Cloudflare defaults missing")
	}
	if networks, err := parseCloakCountryNetworks("none"); err != nil || len(networks) != 0 {
		t.Fatal("explicit country detection disable failed")
	}
	if _, err := parseCloakCountryNetworks("not-a-cidr"); err == nil {
		t.Fatal("invalid country edge configuration accepted")
	}
}

func TestCloakCountryRoutingKeepsUnknownAndTorIndependent(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	body := cloakFixture()
	body["blocked_countries"] = []string{"CN", "T1"}
	c := createTestCloak(t, s, owner, body)
	for _, tc := range []struct {
		peer, country string
		blocked       bool
	}{
		{"173.245.48.5:443", "CN", true},
		{"173.245.48.5:443", "T1", true},
		{"173.245.48.5:443", "US", false},
		{"173.245.48.5:443", "", false},
		{"203.0.113.8:443", "CN", false},
	} {
		r := httptest.NewRequest("GET", c.EntryURL, nil)
		r.RemoteAddr = tc.peer
		r.Header.Set("CF-IPCountry", tc.country)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		requireCode(t, w, 302)
		if got := w.Header().Get("Location") == c.SafeURL; got != tc.blocked {
			t.Fatalf("peer=%s country=%s blocked=%v want %v", tc.peer, tc.country, got, tc.blocked)
		}
	}
	cookie, csrf := sessionFor(t, s, owner)
	body["blocked_countries"] = []string{"XX"}
	requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf), 200)
	w := call(t, s, "GET", "/c/"+c.Slug, nil, nil, "")
	requireCode(t, w, 302)
	if w.Header().Get("Location") != c.SafeURL {
		t.Fatal("explicit unknown region rule did not block untrusted/missing country")
	}
}

func TestCloakTrafficSharesCursorRiskAndSafeFallback(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	proUntil := s.Now().Add(time.Hour)
	if err := s.DB.Model(&owner).Update("pro_until", proUntil).Error; err != nil {
		t.Fatal(err)
	}
	pool := TrafficPool{Slug: randomHex(4), UserID: owner.ID, Enabled: true, RiskEnabled: true, Mode: "sequential"}
	if err := s.DB.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	now := s.Now()
	for position, status := range []string{"clean", "unsafe", "clean"} {
		link := TrafficLink{ID: randomHex(16), UserID: owner.ID, PoolSlug: pool.Slug, Position: position, URL: "https://target" + string(rune('a'+position)) + ".example/download", Status: status, CheckedAt: &now}
		if err := s.DB.Create(&link).Error; err != nil {
			t.Fatal(err)
		}
	}
	body := cloakFixture()
	body["target_type"], body["traffic_pool_slug"] = "traffic_pool", pool.Slug
	c := createTestCloak(t, s, owner, body)
	if c.TargetURL != "" {
		t.Fatal("traffic target retained an inactive raw URL")
	}
	for _, tc := range []struct{ path, want string }{
		{"/c/" + c.Slug, "https://targeta.example/download"},
		{"/r/" + pool.Slug, "https://targetc.example/download"},
		{"/c/" + c.Slug + "?fbclid=click", "https://targeta.example/download?fbclid=click"},
	} {
		w := call(t, s, "GET", tc.path, nil, nil, "")
		requireCode(t, w, 302)
		if w.Header().Get("Location") != tc.want {
			t.Fatalf("wrong shared traffic choice: %s, want %s", w.Header().Get("Location"), tc.want)
		}
	}
	if err := s.DB.Model(&pool).Update("mode", "random").Error; err != nil {
		t.Fatal(err)
	}
	for range 15 {
		w := call(t, s, "GET", "/c/"+c.Slug, nil, nil, "")
		requireCode(t, w, 302)
		if got := w.Header().Get("Location"); got != "https://targeta.example/download" && got != "https://targetc.example/download" {
			t.Fatal("random cloaked traffic bypassed risk filter")
		}
	}
	if err := s.DB.Model(&TrafficLink{}).Where("pool_slug = ?", pool.Slug).Update("status", "unsafe").Error; err != nil {
		t.Fatal(err)
	}
	w := call(t, s, "GET", "/c/"+c.Slug+"?fbclid=not-forwarded", nil, nil, "")
	requireCode(t, w, 302)
	if w.Header().Get("Location") != "https://targeta.example/download?fbclid=not-forwarded" {
		t.Fatal("cloaked traffic did not retain the configured first fallback")
	}
	if err := s.DB.Model(&pool).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	w = call(t, s, "GET", "/c/"+c.Slug, nil, nil, "")
	requireCode(t, w, 302)
	if w.Header().Get("Location") != c.SafeURL {
		t.Fatal("disabled traffic did not use safe page")
	}
	if err := s.DB.Delete(&pool).Error; err != nil {
		t.Fatal(err)
	}
	w = call(t, s, "GET", "/c/"+c.Slug, nil, nil, "")
	requireCode(t, w, 302)
	if w.Header().Get("Location") != c.SafeURL {
		t.Fatal("deleted traffic did not use safe page")
	}
}

func TestCloakDomainIsolationAndLoopDefense(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, 0), seedUser(t, s, 0)
	now := s.Now()
	domain := Domain{ID: randomHex(16), UserID: owner.ID, Hostname: "customer.example.com", VerifiedAt: &now, SSLStatus: "ready"}
	if err := s.DB.Create(&domain).Error; err != nil {
		t.Fatal(err)
	}
	owned := createTestCloak(t, s, owner, cloakFixture())
	foreign := createTestCloak(t, s, other, cloakFixture())
	for _, tc := range []struct {
		slug string
		code int
	}{{owned.Slug, 302}, {foreign.Slug, 404}} {
		r := httptest.NewRequest("GET", "https://customer.example.com/c/"+tc.slug, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		requireCode(t, w, tc.code)
	}
	cookie, csrf := sessionFor(t, s, owner)
	body := cloakFixture()
	body["safe_url"] = "https://customer.example.com/c/" + owned.Slug
	requireCode(t, call(t, s, "POST", "/api/cloaks", body, cookie, csrf), 400)
	// Old data and subsequently added custom domains cannot create a loop.
	if err := s.DB.Model(&Cloak{}).Where("id = ?", owned.ID).Update("target_url", "https://customer.example.com/c/"+owned.Slug).Error; err != nil {
		t.Fatal(err)
	}
	w := call(t, s, "GET", "/c/"+owned.Slug, nil, nil, "")
	requireCode(t, w, 302)
	if w.Header().Get("Location") != owned.SafeURL {
		t.Fatal("looping target did not fall back to safe URL")
	}
	if err := s.DB.Model(&Cloak{}).Where("id = ?", owned.ID).Update("safe_url", "https://customer.example.com/c/"+owned.Slug).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, call(t, s, "GET", "/c/"+owned.Slug, nil, nil, ""), 503)
}

func TestCloakManagedDomainAudience(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	if err := s.DB.Model(&Settings{}).Where("id = 1").Updates(map[string]any{"download_domain_mode": "separate", "free_download_base_url": "https://free.example.com", "paid_download_base_url": "https://paid.example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	c := createTestCloak(t, s, owner, cloakFixture())
	for _, tc := range []struct {
		host string
		code int
	}{{"free.example.com", 302}, {"paid.example.com", 404}} {
		r := httptest.NewRequest("GET", "https://"+tc.host+"/c/"+c.Slug, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		requireCode(t, w, tc.code)
	}
}

func TestCloakCanonicalWWWAliasLoopDefense(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	for _, siteHost := range []string{"d1apk.com", "www.d1apk.com"} {
		t.Run(siteHost, func(t *testing.T) {
			s.Config.PublicURL = "https://" + siteHost
			alias := "www.d1apk.com"
			if siteHost == alias {
				alias = "d1apk.com"
			}
			for _, field := range []string{"target_url", "safe_url"} {
				for _, path := range []string{"/c/self", "/section/../c/self", "/%63/self"} {
					body := cloakFixture()
					body[field] = "https://" + alias + path
					requireCode(t, call(t, s, "POST", "/api/cloaks", body, cookie, csrf), 400)
				}
			}
			// Alias protection is restricted to cloak entry paths.
			body := cloakFixture()
			body["target_url"] = "https://" + alias + "/help"
			c := createTestCloak(t, s, owner, body)
			if err := s.DB.Model(&Cloak{}).Where("id = ?", c.ID).Update("target_url", "https://"+alias+"/c/"+c.Slug).Error; err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", c.EntryURL, nil)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			requireCode(t, w, 302)
			if w.Header().Get("Location") != c.SafeURL {
				t.Fatal("existing alias loop was not redirected to safe page")
			}
			if err := s.DB.Model(&Cloak{}).Where("id = ?", c.ID).Update("safe_url", "https://"+alias+"/c/"+c.Slug).Error; err != nil {
				t.Fatal(err)
			}
			w = httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			requireCode(t, w, 503)
		})
	}
}

func TestCloakBrowserEquivalentURLLoopDefense(t *testing.T) {
	s := testServer(t)
	s.Config.PublicURL = "https://d1apk.com"
	owner := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	if err := s.DB.Create(&Domain{ID: randomHex(16), UserID: owner.ID, Hostname: "customer.example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&Settings{}).Where("id = 1").Update("download_base_url", "https://ｄownload.example.com").Error; err != nil {
		t.Fatal(err)
	}
	c := createTestCloak(t, s, owner, cloakFixture())
	for _, raw := range []string{
		"https://d1apk.com/c\\" + c.Slug,
		"https://d1apk.com/section\\..\\c\\" + c.Slug,
		"https://d1apk。com/c/" + c.Slug,
		"https://ｄ１ａｐｋ.com/c/" + c.Slug,
		"https://ｗｗｗ.d1apk.com/c/" + c.Slug,
		"https://customer。example.com/c/" + c.Slug,
		"https://download.example.com/c/" + c.Slug,
	} {
		t.Run(raw, func(t *testing.T) {
			for _, field := range []string{"target_url", "safe_url"} {
				body := cloakFixture()
				body[field] = raw
				requireCode(t, call(t, s, "POST", "/api/cloaks", body, cookie, csrf), 400)
			}
			// Existing stored data is rechecked before returning any redirect.
			if err := s.DB.Model(&Cloak{}).Where("id = ?", c.ID).Updates(map[string]any{"target_url": raw, "safe_url": c.SafeURL}).Error; err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", c.EntryURL, nil)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			requireCode(t, w, 302)
			if w.Header().Get("Location") != c.SafeURL {
				t.Fatal("browser-equivalent recursive target did not use safe URL")
			}
			if err := s.DB.Model(&Cloak{}).Where("id = ?", c.ID).Update("safe_url", raw).Error; err != nil {
				t.Fatal(err)
			}
			w = httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			requireCode(t, w, 503)
		})
	}
	if !validCloakURL("https://例子.测试/help") {
		t.Fatal("valid international hostname was rejected")
	}
	// Canonical comparison also applies to Config.Host and request hostname.
	s.Config.PublicURL = "https://ｄ１ａｐｋ.com"
	if loop, err := s.cloakLoop(s.DB, "https://d1apk.com/c/entry", ""); err != nil || !loop {
		t.Fatal("configuration hostname was not canonicalized")
	}
	if loop, err := s.cloakLoop(s.DB, "https://unrelated。example/c/entry", "ｕｎｒｅｌａｔｅｄ.example"); err != nil || !loop {
		t.Fatal("request hostname was not canonicalized")
	}
}
