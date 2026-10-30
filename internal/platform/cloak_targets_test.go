package platform

import (
	"encoding/json"
	"html"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func chosenCall(s *Server, method, host, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+host+path, nil)
	r.RemoteAddr = "203.0.113.90:1234"
	r.Header.Set("Accept-Language", "en")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func addChosenDomain(t *testing.T, s *Server, owner User, host string) {
	t.Helper()
	now := s.Now()
	if err := s.DB.Create(&Domain{ID: randomHex(16), UserID: owner.ID, Hostname: host, Token: randomHex(16), VerifiedAt: &now, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestCloakAppTargetsOwnedChoicesAndValidation(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, Dollar), seedUser(t, s, Dollar)
	app, foreign := seedApp(t, s, owner, 100), seedApp(t, s, other, 100)
	pwa, foreignPWA := createPWAFixture(t, s, owner, "Own PWA"), createPWAFixture(t, s, other, "Foreign PWA")
	cookie, csrf := sessionFor(t, s, owner)
	request := func(kind, id, mode string) map[string]any {
		body := cloakFixture()
		body["target_type"], body["target_id"], body["app_target_mode"] = kind, id, mode
		return body
	}
	for _, body := range []map[string]any{request("app", foreign.ID, "direct"), request("pwa", foreignPWA.ID, "")} {
		requireCode(t, call(t, s, "POST", "/api/cloaks", body, cookie, csrf), 404)
	}
	for _, body := range []map[string]any{request("app", "", "direct"), request("pwa", "../other", ""), request("app", app.ID, "invalid")} {
		requireCode(t, call(t, s, "POST", "/api/cloaks", body, cookie, csrf), 400)
	}
	c := createTestCloak(t, s, owner, request("app", app.ID, "info"))
	if c.TargetURL != "" || c.TrafficPoolSlug != "" || c.TargetID != app.ID || c.AppTargetMode != "info" {
		t.Fatal("inactive target fields were retained")
	}
	createTestCloak(t, s, owner, request("pwa", pwa.ID, "direct"))
	requireCode(t, call(t, s, "GET", "/api/cloak-targets", nil, nil, ""), 401)
	w := call(t, s, "GET", "/api/cloak-targets", nil, cookie, "")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), app.ID) || !strings.Contains(w.Body.String(), pwa.ID) || strings.Contains(w.Body.String(), foreign.ID) || strings.Contains(w.Body.String(), foreignPWA.ID) || strings.Contains(w.Body.String(), "vapid") || strings.Contains(w.Body.String(), "file_key") {
		t.Fatal("target choices crossed tenant boundaries or exposed secrets")
	}
	s.DB.Model(&app).Update("enabled", false)
	s.DB.Delete(&pwa)
	w = call(t, s, "GET", "/api/cloak-targets", nil, cookie, "")
	requireCode(t, w, 200)
	if strings.Contains(w.Body.String(), app.ID) || strings.Contains(w.Body.String(), pwa.ID) {
		t.Fatal("unavailable targets are still selectable")
	}
}

func TestCloakRunsAppAndPoolHandlersWithoutChangingTheirBehavior(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, 10*Dollar), seedUser(t, s, Dollar)
	app := seedApp(t, s, owner, 120)
	s.DB.Model(&app).Update("download_mode", "info")
	app.DownloadMode = "info"
	pwa := createPWAFixture(t, s, owner, "Chosen Domain PWA")
	landing, _ := json.Marshal(landingFixture())
	s.DB.Model(&PWAApp{}).Where("id = ?", pwa.ID).Update("landing_json", string(landing))
	addChosenDomain(t, s, owner, "chosen.example")
	addChosenDomain(t, s, other, "foreign.example")
	for _, kind := range []string{"app", "pwa"} {
		body := cloakFixture()
		body["target_type"], body["safe_url"], body["app_target_mode"] = kind, "", "info"
		body["target_id"] = app.ID
		if kind == "pwa" {
			body["target_id"] = pwa.ID
		}
		c := createTestCloak(t, s, owner, body)
		path := "/c/" + c.Slug
		w := chosenCall(s, "GET", "chosen.example", path+"?campaign=proof")
		requireCode(t, w, 200)
		if w.Header().Get("Location") != "" || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
			t.Fatal("owned page was redirected instead of rendered")
		}
		requireCode(t, chosenCall(s, "GET", "foreign.example", path), 404)
		r := httptest.NewRequest("GET", "http://chosen.example"+path, nil)
		r.Header.Set("Accept-Language", "zh")
		blocked := httptest.NewRecorder()
		s.Handler().ServeHTTP(blocked, r)
		requireCode(t, blocked, 404)
		if blocked.Header().Get("Location") != "" || !strings.Contains(blocked.Body.String(), "Account not found") {
			t.Fatal("default safe page escaped the chosen domain")
		}
		pool := TrafficPool{Slug: randomHex(4), UserID: owner.ID, Enabled: true, Mode: "sequential"}
		s.DB.Create(&pool)
		target := s.Config.PublicURL + "/a/" + app.Slug + "/info"
		if kind == "pwa" {
			target = s.Config.PublicURL + "/p/" + pwa.Slug + "/"
		}
		s.DB.Create(&TrafficLink{ID: randomHex(16), PoolSlug: pool.Slug, UserID: owner.ID, URL: target, Status: "safe"})
		// Standalone traffic retains its original redirect, including the
		// configured destination domain. A cloak selects the same target but
		// renders an owned landing in place on its copied entry domain.
		for _, method := range []string{"GET", "HEAD"} {
			w = chosenCall(s, method, "chosen.example", "/r/"+pool.Slug+"?utm_source=test")
			requireCode(t, w, 302)
			if w.Header().Get("Location") != target {
				t.Fatal("standalone traffic no longer uses its original destination")
			}
		}
		poolBody := cloakFixture()
		poolBody["target_type"], poolBody["traffic_pool_slug"], poolBody["safe_url"] = "traffic_pool", pool.Slug, ""
		poolCloak := createTestCloak(t, s, owner, poolBody)
		w = chosenCall(s, "GET", "chosen.example", "/c/"+poolCloak.Slug+"?utm_source=test")
		requireCode(t, w, 200)
		if w.Header().Get("Location") != "" || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
			t.Fatal("cloak pool landing redirected instead of rendering in place")
		}
		requireCode(t, chosenCall(s, "GET", "foreign.example", "/c/"+poolCloak.Slug), 404)
		if kind == "app" {
			s.DB.Model(&app).Update("enabled", false)
		} else {
			s.DB.Delete(&PWAApp{}, "id = ?", pwa.ID)
		}
		requireCode(t, chosenCall(s, "GET", "chosen.example", path), 404)
		// Availability of the destination is still the pool's own logic. A
		// deleted app must not silently turn a pool redirect into a safe page.
		w = chosenCall(s, "GET", "chosen.example", "/c/"+poolCloak.Slug)
		requireCode(t, w, 302)
		if w.Header().Get("Location") != target {
			t.Fatal("cloak rewrote the pool's behavior after its target changed")
		}
	}
}

func TestPWACustomDomainAssetsOriginAndIsolation(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, Dollar), seedUser(t, s, Dollar)
	pwa := createPWAFixture(t, s, owner, "Origin test")
	landing, _ := json.Marshal(landingFixture())
	s.DB.Model(&PWAApp{}).Where("id = ?", pwa.ID).Update("landing_json", string(landing))
	addChosenDomain(t, s, owner, "chosen.example")
	addChosenDomain(t, s, other, "foreign.example")
	base := "/p/" + pwa.Slug + "/"
	for _, suffix := range []string{"", "launch", "manifest.webmanifest", "sw.js", "client.js", "style.css", "icon/192.png", "push-config", "landing-config.js", "api/promotion-links/by-domain"} {
		w := chosenCall(s, "GET", "chosen.example", base+suffix)
		requireCode(t, w, 200)
		if w.Header().Get("Location") != "" {
			t.Fatal("PWA asset redirected", suffix)
		}
		requireCode(t, chosenCall(s, "GET", "foreign.example", base+suffix), 404)
	}
	w := chosenCall(s, "GET", "chosen.example", base+"landing-config.js")
	if !strings.Contains(w.Body.String(), "http://chosen.example/p/") || strings.Contains(w.Body.String(), s.Config.PublicURL) {
		t.Fatal("runtime API/image origin switched to the system domain")
	}
	for _, path := range []string{"/api/me", "/api/pwas", "/", "/api/pwa-landing/preview/google_play"} {
		requireCode(t, chosenCall(s, "GET", "chosen.example", path), 404)
	}
	for _, origin := range []string{"http://chosen.example", "http://attacker.example"} {
		r := httptest.NewRequest("POST", "http://chosen.example"+base+"subscribe", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		code := 400 // Origin passed; empty push keys are rejected by input validation.
		if origin != "http://chosen.example" {
			code = 403
		}
		requireCode(t, w, code)
	}
}

func TestCloakDirectDownloadKeepsExistingTicketFlowOnEntryDomain(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	app := seedApp(t, s, owner, 120)
	if err := s.DB.Model(&app).Update("download_mode", "info").Error; err != nil {
		t.Fatal(err)
	}
	addChosenDomain(t, s, owner, "chosen.example")
	body := cloakFixture()
	body["target_type"], body["target_id"], body["app_target_mode"], body["safe_url"] = "app", app.ID, "direct", ""
	c := createTestCloak(t, s, owner, body)
	w := chosenCall(s, "HEAD", "chosen.example", "/c/"+c.Slug)
	requireCode(t, w, 200)
	if w.Header().Get("Location") != "" || countRows(t, s, &Ticket{}) != 0 {
		t.Fatal("metadata check changed the download flow or reserved quota")
	}
	w = chosenCall(s, "GET", "chosen.example", "/c/"+c.Slug)
	requireCode(t, w, 307)
	if !strings.HasPrefix(w.Header().Get("Location"), "/d/") {
		t.Fatal("download switched to another domain or ignored direct mode")
	}
	var ticket Ticket
	if err := s.DB.First(&ticket).Error; err != nil || ticket.Host != "chosen.example" || ticket.AppID != app.ID || ticket.Reserved || ticket.Charged {
		t.Fatal("download did not preserve the original uncharged same-host ticket", err)
	}
	// Existing non-cloak share links retain their configured detail-page mode.
	w = chosenCall(s, "GET", "chosen.example", "/a/"+app.Slug)
	requireCode(t, w, 302)
	if w.Header().Get("Location") != "/a/"+app.Slug+"/info" {
		t.Fatal("cloak direct mode changed the ordinary app's behavior")
	}
}

func TestCloakDetailsContinueDownloadingOnChosenDomain(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	app := seedApp(t, s, owner, 120)
	addChosenDomain(t, s, owner, "chosen.example")
	addChosenDomain(t, s, owner, "other.example")
	cookie, csrf := sessionFor(t, s, owner)
	design := defaultPageDesign(app)
	design.Layout = "google_play"
	requireCode(t, call(t, s, "PUT", "/api/apps/"+app.ID+"/page-design", design, cookie, csrf), 200)
	body := cloakFixture()
	body["target_type"], body["target_id"], body["app_target_mode"], body["safe_url"] = "app", app.ID, "info", ""
	c := createTestCloak(t, s, owner, body)
	page := chosenCall(s, "GET", "chosen.example", "/c/"+c.Slug+"?fbclid=chosen-domain")
	requireCode(t, page, 200)
	if page.Header().Get("Location") != "" || !strings.Contains(page.Body.String(), `action="/a/`+app.Slug+`/download"`) {
		t.Fatal("cloak details did not keep native download actions on the entry host")
	}
	config := chosenCall(s, "GET", "chosen.example", "/a/"+app.Slug+"/landing-config.js")
	requireCode(t, config, 200)
	if !strings.Contains(config.Body.String(), `"s3PublicDomain":"http://chosen.example"`) || strings.Contains(config.Body.String(), s.Config.PublicURL) {
		t.Fatal("APK template switched back to the system origin")
	}
	fields := url.Values{}
	for _, name := range []string{"request_key", "attribution_token"} {
		match := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(html.UnescapeString(page.Body.String()))
		if len(match) != 2 {
			t.Fatal("missing native download form field", name)
		}
		fields.Set(name, match[1])
	}
	r := httptest.NewRequest("POST", "http://chosen.example/a/"+app.Slug+"/download", strings.NewReader(fields.Encode()))
	r.RemoteAddr = "203.0.113.90:1234"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://chosen.example")
	r.Header.Set("Accept", "application/json")
	for _, cookie := range page.Result().Cookies() {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 200)
	var answer map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil || !strings.HasPrefix(answer["download_url"], "/d/") {
		t.Fatal("download API did not return a same-host file URL", err)
	}
	for _, host := range []string{"other.example", "chosen.example"} {
		r := httptest.NewRequest("GET", "http://"+host+answer["download_url"], nil)
		r.RemoteAddr = "203.0.113.90:1234"
		for _, cookie := range page.Result().Cookies() {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		code := 200
		if host != "chosen.example" {
			code = 403
		}
		requireCode(t, w, code)
		if w.Header().Get("Location") != "" {
			t.Fatal("file download switched away from the selected host")
		}
	}
	if countRows(t, s, &Download{}) != 1 {
		t.Fatal("same-host download did not use the original completion accounting")
	}
}
