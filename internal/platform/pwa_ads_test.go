package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func adPWARequest(s *Server, method, path string, body any, cookies []*http.Cookie) *httptest.ResponseRecorder {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, "http://localhost:8088"+path, bytes.NewReader(raw))
	r.RemoteAddr = "198.51.100.21:8000"
	r.Header.Set("Origin", "http://localhost:8088")
	r.Header.Set("User-Agent", "PWA test Chrome")
	r.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func adPWAFields(t *testing.T, body string) map[string]string {
	t.Helper()
	section := regexp.MustCompile(`<section id="d1-pwa-ads"[^>]+>`).FindString(body)
	out := map[string]string{}
	for _, key := range []string{"token", "proof", "mode"} {
		m := regexp.MustCompile(`data-` + key + `="([^"]+)"`).FindStringSubmatch(section)
		if len(m) != 2 {
			t.Fatal("missing PWA advertising context", key)
		}
		out[key] = html.UnescapeString(m[1])
	}
	return out
}
func quickAdConfig(a Application) AdConfig {
	c := defaultAds(a)
	c.Mode = "quick"
	c.FacebookEnabled = true
	c.FacebookPixel = "123456789"
	return c
}
func TestPWAQuickClickAndDeepInstallUseSeparateAPKNamespace(t *testing.T) {
	for _, mode := range []string{"quick", "deep"} {
		t.Run(mode, func(t *testing.T) {
			s := testServer(t)
			u := seedUser(t, s, Dollar)
			p := createPWAFixture(t, s, u, "PWA callback")
			a := seedApp(t, s, u, 100)
			// Even deliberately equal public IDs cannot share a config or a capability.
			if e := s.DB.Model(&p).Update("id", a.ID).Error; e != nil {
				t.Fatal(e)
			}
			p.ID = a.ID
			cfg := quickAdConfig(pwaAdApplication(p))
			cfg.Mode = mode
			cfg.FacebookToken, _ = s.encryptSecret("fixture-only")
			if e := s.DB.Create(&cfg).Error; e != nil {
				t.Fatal(e)
			}
			page := adPWARequest(s, "GET", "/p/"+p.Slug+"/?fbclid=pwa-click", nil, nil)
			requireCode(t, page, 200)
			fields := adPWAFields(t, page.Body.String())
			cookies := page.Result().Cookies()
			if mode == "quick" && !strings.Contains(page.Body.String(), `data-mode="quick" hidden`) {
				t.Fatal("quick callback exposed deep consent panel")
			}
			event := "pwa_install"
			consent := true
			if mode == "quick" {
				event = "install_click"
				consent = false
			}
			payload := map[string]any{"event": event, "consent": consent, "attribution_token": fields["token"], "proof": fields["proof"]}
			if _, e := s.readAttribution(fields["token"], a.ID); e == nil {
				t.Fatal("PWA capability accepted for APK")
			}
			if countRows(t, s, &Conversion{}) != 0 {
				t.Fatal("page view generated callback")
			}
			if mode == "deep" {
				bad := map[string]any{"event": "install_click", "attribution_token": fields["token"], "proof": fields["proof"]}
				requireCode(t, adPWARequest(s, "POST", "/p/"+p.Slug+"/ad-event", bad, cookies), 400)
			}
			for range 3 {
				requireCode(t, adPWARequest(s, "POST", "/p/"+p.Slug+"/ad-event", payload, cookies), 202)
			}
			if countRows(t, s, &Conversion{}) != 1 || countRows(t, s, &Delivery{}) != 1 {
				t.Fatal("PWA callback lost or repeated")
			}
			var conversion Conversion
			s.DB.First(&conversion)
			if conversion.AppID != pwaAdID(p.ID) || mode == "quick" && (conversion.Event != "pwa_click" || conversion.Consent) {
				t.Fatal("PWA event identity or consent was altered")
			}
			var calls int
			s.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return jsonResponse(200, `{"events_received":1}`), nil
			})}
			if !s.deliverOne(context.Background()) || calls != 1 {
				t.Fatal("PWA callback not dispatched")
			}
			bad := map[string]any{"event": event, "consent": true, "attribution_token": fields["token"], "proof": "invalid"}
			requireCode(t, adPWARequest(s, "POST", "/p/"+p.Slug+"/ad-event", bad, cookies), 400)
			requireCode(t, adPWARequest(s, "POST", "/p/"+p.Slug+"/ad-event", payload, nil), 403)
		})
	}
}
func TestAdConfigurationListCreatePixelAndToggleOwnership(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	a := seedApp(t, s, u, 100)
	p := createPWAFixture(t, s, u, "Bound PWA")
	foreign := seedUser(t, s, Dollar)
	foreignPWA := createPWAFixture(t, s, foreign, "Private")
	cookie, csrf := sessionFor(t, s, u)
	w := call(t, s, "GET", "/api/ad-configurations", nil, cookie, "")
	requireCode(t, w, 200)
	if strings.Contains(w.Body.String(), a.Name) || strings.Contains(w.Body.String(), p.ID) {
		t.Fatal("unconfigured applications leaked into configuration list")
	}
	c := quickAdConfig(pwaAdApplication(p))
	payload := map[string]any{"resource_kind": "pwas", "resource_id": p.ID, "config": c, "facebook_token": "fixture-only"}
	requireCode(t, call(t, s, "POST", "/api/ad-configurations", payload, cookie, csrf), 200)
	requireCode(t, call(t, s, "POST", "/api/ad-configurations", payload, cookie, csrf), 409)
	payload["resource_id"] = foreignPWA.ID
	requireCode(t, call(t, s, "POST", "/api/ad-configurations", payload, cookie, csrf), 404)
	w = call(t, s, "GET", "/api/ad-configurations", nil, cookie, "")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), `"facebook_pixel":"123456789"`) || strings.Contains(w.Body.String(), "fixture-only") || strings.Contains(w.Body.String(), a.ID) {
		t.Fatal("configuration list fields or credential isolation wrong")
	}
	requireCode(t, call(t, s, "POST", "/api/ad-configurations/pwas/"+p.ID+"/enabled", map[string]any{"enabled": false}, cookie, csrf), 200)
	var cfg AdConfig
	s.DB.First(&cfg, "app_id = ?", pwaAdID(p.ID))
	if !cfg.Disabled || !cfg.FacebookEnabled {
		t.Fatal("disabling lost configured channel")
	}
	otherCookie, otherCSRF := sessionFor(t, s, foreign)
	requireCode(t, call(t, s, "GET", "/api/pwas/"+p.ID+"/ads", nil, otherCookie, ""), 404)
	requireCode(t, call(t, s, "POST", "/api/ad-configurations/pwas/"+p.ID+"/enabled", map[string]any{"enabled": true}, otherCookie, otherCSRF), 404)
	if countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("configuring callbacks charged balance")
	}
}
func TestQuickAPKCallbackStartsAtFirstBytesWithoutInventingConsent(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	a := seedApp(t, s, u, 100)
	cfg := quickAdConfig(a)
	cfg.FacebookToken, _ = s.encryptSecret("fixture-only")
	s.DB.Create(&cfg)
	start := directRequest(t, s, "GET", "/a/"+a.Slug+"/direct?fbclid=quick-click", nil)
	requireCode(t, start, 307)
	if countRows(t, s, &Delivery{}) != 0 {
		t.Fatal("redirect counted as actual download")
	}
	r := httptest.NewRequest("GET", "http://localhost:8088"+start.Header().Get("Location"), nil)
	r.Header.Set("Range", "bytes=0-0")
	for _, c := range start.Result().Cookies() {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 206)
	drainDownloadAds(s)
	if countRows(t, s, &Download{}) != 0 || countRows(t, s, &Delivery{}) != 1 {
		t.Fatal("quick download waited for full file")
	}
	var event Conversion
	s.DB.First(&event)
	if event.Event != "download_start" || event.VerifiedBy != "quick_download_server" || event.Consent {
		t.Fatal("wrong quick callback semantics")
	}
	for range 2 {
		w = directRequest(t, s, "GET", start.Header().Get("Location"), start.Result().Cookies())
		requireCode(t, w, 200)
		drainDownloadAds(s)
	}
	if countRows(t, s, &Delivery{}) != 1 {
		t.Fatal("file retry/completion duplicated quick callback")
	}
}

func TestQuickRoutedAPKUsesSourceWithoutChangingConsent(t *testing.T) {
	f := routedAdFixtureFor(t, true)
	s := f.s
	if e := s.DB.Model(&f.cfg).Update("mode", "quick").Error; e != nil {
		t.Fatal(e)
	}
	s.DB.Model(&f.pool).Update("cursor", 1)
	file, cookies := f.targetTicket(t, f.click(t, false), f.apps[1])
	requireCode(t, routedAdRequest(s, "GET", file, cookies, nil, "bytes=0-0"), 206)
	var event Conversion
	s.DB.Preload("Deliveries").First(&event)
	if event.AppID != f.apps[0].ID || event.DownloadedAppID != f.apps[1].ID || event.VerifiedBy != "quick_download_server" || event.Consent || len(event.Deliveries) != 2 {
		t.Fatal("quick routed callback lost source identity or changed consent")
	}
	requireCode(t, routedAdRequest(s, "GET", file, cookies, nil, ""), 200)
	if countRows(t, s, &Conversion{}) != 1 {
		t.Fatal("completion repeated source callback")
	}
}
func TestPWADepthSDKRegisterSignatureAndLegacyMigration(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	p := createPWAFixture(t, s, u, "Deep PWA")
	cfg := quickAdConfig(pwaAdApplication(p))
	cfg.Mode = "deep"
	cfg.FacebookToken, _ = s.encryptSecret("fixture-only")
	secret := "test-pwa-signature"
	cfg.APISecret, _ = s.encryptSecret(secret)
	s.DB.Create(&cfg)
	page := adPWARequest(s, "GET", "/p/"+p.Slug+"/?fbclid=deep", nil, nil)
	fields := adPWAFields(t, page.Body.String())
	cookies := page.Result().Cookies()
	sdk := func() *httptest.ResponseRecorder {
		in := SDKEvent{AttributionToken: fields["token"], Event: "register", EventID: "registration-unique-id", Consent: true}
		body, _ := json.Marshal(in)
		r := httptest.NewRequest("POST", "http://localhost:8088/api/sdk/pwas/"+p.ID+"/events", bytes.NewReader(body))
		stamp := fmt.Sprint(s.Now().Unix())
		r.Header.Set("X-D1-Time", stamp)
		r.Header.Set("X-D1-Signature", sdkSignature(secret, stamp, body))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	requireCode(t, sdk(), 409)
	requireCode(t, adPWARequest(s, "POST", "/p/"+p.Slug+"/ad-event", map[string]any{"event": "pwa_install", "consent": true, "attribution_token": fields["token"], "proof": fields["proof"]}, cookies), 202)
	requireCode(t, sdk(), 202)
	requireCode(t, sdk(), 202)
	if countRows(t, s, &Conversion{}) != 2 || countRows(t, s, &Delivery{}) != 2 {
		t.Fatal("deep register event missing or repeated")
	}
	var before AdConfig
	s.DB.First(&before, "app_id = ?", pwaAdID(p.ID))
	s.DB.Where("version = ?", 32).Delete(&Migration{})
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	var after AdConfig
	s.DB.First(&after, "app_id = ?", pwaAdID(p.ID))
	if before.FacebookToken != after.FacebookToken || before.APISecret != after.APISecret || before.AppID != after.AppID || countRows(t, s, &Conversion{}) != 2 {
		t.Fatal("idempotent migration altered credentials or events")
	}
}

func TestPWAAdsMigratesLegacyConfigPrimaryKey(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	app := seedApp(t, s, u, 100)
	cfg := quickAdConfig(app)
	cfg.Mode = "deep"
	cfg.FacebookToken, _ = s.encryptSecret("legacy-secret")
	if e := s.DB.Create(&cfg).Error; e != nil {
		t.Fatal(e)
	}
	if s.DB.Dialector.Name() == "postgres" {
		if e := s.DB.Exec("ALTER TABLE ad_configs ALTER COLUMN app_id TYPE varchar(32)").Error; e != nil {
			t.Fatal(e)
		}
	}
	s.DB.Where("version = ?", 32).Delete(&Migration{})
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	var old AdConfig
	s.DB.First(&old, "app_id = ?", app.ID)
	if old.FacebookToken != cfg.FacebookToken || old.Mode != "deep" {
		t.Fatal("legacy credentials or mode changed")
	}
	p := createPWAFixture(t, s, u, "Namespaced PWA")
	wide := quickAdConfig(pwaAdApplication(p))
	if e := s.DB.Create(&wide).Error; e != nil {
		t.Fatal("legacy primary key cannot hold PWA namespace", e)
	}
	if countRows(t, s, &AdConfig{}) != 2 {
		t.Fatal("namespace migration lost a config")
	}
}
