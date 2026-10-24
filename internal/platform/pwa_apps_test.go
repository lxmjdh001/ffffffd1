package platform

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createPWAFixture(t *testing.T, s *Server, owner User, name string) PWAApp {
	t.Helper()
	cookie, csrf := sessionFor(t, s, owner)
	w := call(t, s, "POST", "/api/pwas", map[string]any{"name": name, "target_url": "https://example.com/app?source=d1", "description": "A useful website"}, cookie, csrf)
	requireCode(t, w, 201)
	var view pwaAppView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	var app PWAApp
	if err := s.DB.First(&app, "id = ?", view.ID).Error; err != nil {
		t.Fatal(err)
	}
	return app
}
func pwaKeyCall(t *testing.T, s *Server, body any, cookie *http.Cookie, csrf, key string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", s.Config.PublicURL+"/api/pwas", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)
	r.Header.Set("X-CSRF-Token", csrf)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestPWAOwnedIconRemainsPrivateWhenPaused(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	other := seedUser(t, s, 0)
	app := createPWAFixture(t, s, owner, "Paused icon")
	if err := s.DB.Model(&app).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := sessionFor(t, s, owner)
	path := "/api/pwas/" + app.ID + "/icon"
	w := call(t, s, "GET", path, nil, cookie, csrf)
	requireCode(t, w, 200)
	if w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("owner icon must remain a private PNG: %v", w.Header())
	}
	if _, err := png.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
		t.Fatal(err)
	}
	otherCookie, otherCSRF := sessionFor(t, s, other)
	requireCode(t, call(t, s, "GET", path, nil, otherCookie, otherCSRF), 404)
	requireCode(t, call(t, s, "GET", path, nil, nil, ""), 401)
	requireCode(t, pwaPublicCall(t, s, "/p/"+app.Slug+"/icon/512.png", "", ""), 404)
}
func pwaIconCall(t *testing.T, s *Server, id string, raw []byte, cookie *http.Cookie, csrf string, duplicate bool) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "logo.png")
	_, _ = part.Write(raw)
	if duplicate {
		part, _ = writer.CreateFormFile("file", "again.png")
		_, _ = part.Write(raw)
	}
	writer.Close()
	r := httptest.NewRequest("POST", s.Config.PublicURL+"/api/pwas/"+id+"/icon", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("X-CSRF-Token", csrf)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func pwaPublicCall(t *testing.T, s *Server, path, host, language string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", s.Config.PublicURL+path, nil)
	if host != "" {
		r.Host = host
	}
	if language != "" {
		r.Header.Set("Accept-Language", language)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestPWAAppsCRUDIsolationIdempotencyAndSecrets(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	other := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	oc, ocsrf := sessionFor(t, s, other)
	requireCode(t, call(t, s, "GET", "/api/pwas", nil, nil, ""), 401)
	body := map[string]any{"name": "My Web App", "target_url": "https://example.com/app", "description": "Welcome"}
	requireCode(t, call(t, s, "POST", "/api/pwas", body, cookie, ""), 403)
	first := pwaKeyCall(t, s, body, cookie, csrf, "same-create-request")
	requireCode(t, first, 201)
	var view pwaAppView
	json.Unmarshal(first.Body.Bytes(), &view)
	if len(view.ID) != 32 || len(view.Slug) != 16 || view.Name != "My Web App" || !view.Enabled || !view.PushEnabled || view.ThemeColor != "#6373e8" || view.SubscriptionCount != 0 || !strings.HasSuffix(view.EntryURL, "/p/"+view.Slug+"/") || !strings.HasSuffix(view.LaunchURL, "/launch") {
		t.Fatalf("unexpected view: %+v", view)
	}
	again := pwaKeyCall(t, s, body, cookie, csrf, "same-create-request")
	requireCode(t, again, 201)
	var replay pwaAppView
	json.Unmarshal(again.Body.Bytes(), &replay)
	if replay.ID != view.ID {
		t.Fatal("create retry duplicated app")
	}
	body["name"] = "Different"
	requireCode(t, pwaKeyCall(t, s, body, cookie, csrf, "same-create-request"), 409)
	requireCode(t, pwaKeyCall(t, s, body, cookie, csrf, "short"), 400)
	// The same client key belongs to its owner, not the entire platform.
	requireCode(t, pwaKeyCall(t, s, body, oc, ocsrf, "same-create-request"), 201)
	var app PWAApp
	if err := s.DB.First(&app, "id = ?", view.ID).Error; err != nil {
		t.Fatal(err)
	}
	private, err := s.decryptSecret(app.VAPIDPrivateKey)
	if err != nil || private == "" || private == app.VAPIDPrivateKey || app.VAPIDPublicKey == "" {
		t.Fatal("app VAPID identity is missing or not encrypted")
	}
	for _, token := range []string{private, app.VAPIDPrivateKey, app.VAPIDPublicKey, "user_id", "CreateKey", "CreateFingerprint", "icon_png"} {
		if strings.Contains(first.Body.String(), token) {
			t.Fatal("private app field leaked", token)
		}
	}
	marshaled, _ := json.Marshal(app)
	if bytes.Contains(marshaled, []byte(private)) || bytes.Contains(marshaled, []byte(app.VAPIDPublicKey)) {
		t.Fatal("model serialization leaked credentials")
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		requireCode(t, call(t, s, method, "/api/pwas/"+app.ID, map[string]any{"name": "Hijacked"}, oc, ocsrf), 404)
	}
	requireCode(t, pwaIconCall(t, s, app.ID, []byte("bad"), oc, ocsrf, false), 404)
	list := call(t, s, "GET", "/api/pwas", nil, cookie, "")
	requireCode(t, list, 200)
	var listed struct {
		Data []pwaAppView `json:"data"`
	}
	json.Unmarshal(list.Body.Bytes(), &listed)
	if len(listed.Data) != 1 || listed.Data[0].ID != app.ID {
		t.Fatal("list crossed tenant boundary")
	}
	requireCode(t, call(t, s, "PUT", "/api/pwas/"+app.ID, map[string]any{"name": "Updated", "enabled": false, "push_enabled": false, "theme_color": "#ABCDEF"}, cookie, csrf), 200)
	s.DB.First(&app, "id = ?", app.ID)
	if app.Name != "Updated" || app.Enabled || app.PushEnabled || app.ThemeColor != "#abcdef" {
		t.Fatal("update did not persist fields")
	}
	requireCode(t, call(t, s, "DELETE", "/api/pwas/"+app.ID, nil, cookie, csrf), 200)
	requireCode(t, call(t, s, "GET", "/api/pwas/"+app.ID, nil, cookie, ""), 404)
	body["name"] = "My Web App"
	requireCode(t, pwaKeyCall(t, s, body, cookie, csrf, "same-create-request"), 409)
	var count int64
	s.DB.Unscoped().Model(&PWAApp{}).Where("id = ?", app.ID).Count(&count)
	if count != 1 {
		t.Fatal("delete unexpectedly destroyed push identity")
	}
}
func TestPWAAppsFieldsTargetsAndPlanQuota(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	for _, raw := range []string{"javascript:alert(1)", "data:text/html,hello", "ftp://example.com", "http://example.com", "https://user:password@example.com", "https://127.0.0.1", "https://10.0.0.1/a", "https://169.254.169.254/latest", "https://[::1]/", "https://[fc00::1]/", "https://localhost/a", "https://example.local/a", "https://intranet/a", "https://127.1/a", "https://2130706433/a", "https://0x7f000001/a", "https://example.com:444/a", "https://localhost:8088/a", "https://example.com/" + strings.Repeat("x", 2050)} {
		s.Config.Env = "production"
		if _, err := s.normalizePWATarget(raw); err == nil {
			t.Errorf("accepted unsafe/nonpublic target %q", raw)
		}
		s.Config.Env = "test"
	}
	for _, raw := range []string{"https://example.com/path?q=1#section", "https://bücher.de/", "https://8.8.8.8/", "http://127.0.0.1:9999/test"} {
		if _, err := s.normalizePWATarget(raw); err != nil {
			t.Errorf("rejected valid target %s: %v", raw, err)
		}
	}
	invalid := []map[string]any{{"name": "", "target_url": "https://example.com"}, {"name": strings.Repeat("字", 101), "target_url": "https://example.com"}, {"name": "App", "target_url": "https://example.com", "description": strings.Repeat("字", 501)}, {"name": "App", "target_url": "https://example.com", "theme_color": "red"}, {"name": "App", "target_url": "https://example.com", "theme_color": "#fff"}, {"name": "App\nInjected", "target_url": "https://example.com"}, {"name": "App", "target_url": "https://example.com", "secret": "injected"}}
	for _, body := range invalid {
		requireCode(t, call(t, s, "POST", "/api/pwas", body, cookie, csrf), 400)
	}
	if err := s.DB.Model(&Plan{}).Where("id = ?", "free").Update("app_limit", 1).Error; err != nil {
		t.Fatal(err)
	}
	app := createPWAFixture(t, s, owner, "Quota One")
	requireCode(t, call(t, s, "POST", "/api/pwas", map[string]string{"name": "Too many", "target_url": "https://example.com"}, cookie, csrf), 403)
	requireCode(t, call(t, s, "DELETE", "/api/pwas/"+app.ID, nil, cookie, csrf), 200)
	createPWAFixture(t, s, owner, "Quota Reused")
}
func TestPWAPublicPagesManifestScopeAndTenantAvailability(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	app := createPWAFixture(t, s, owner, `A </title><script>alert("x")</script>`)
	base := "/p/" + app.Slug + "/"
	page := pwaPublicCall(t, s, base, "", "en-US")
	requireCode(t, page, 200)
	if strings.Contains(page.Body.String(), `<script>alert`) || !strings.Contains(page.Body.String(), "&lt;/title&gt;") || !strings.Contains(page.Body.String(), "Install app") {
		t.Fatal("public name not safely escaped")
	}
	csp := page.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-src https:;") || !strings.Contains(csp, "script-src 'self';") || !strings.Contains(csp, "frame-ancestors 'none';") || strings.Contains(csp, "unsafe-inline") {
		t.Fatal("PWA page compatibility or document isolation policy changed", csp)
	}
	zh := pwaPublicCall(t, s, base, "", "zh-CN")
	requireCode(t, zh, 200)
	if !strings.Contains(zh.Body.String(), "安装应用") {
		t.Fatal("Chinese installation copy missing")
	}
	launch := pwaPublicCall(t, s, base+"launch", "", "")
	requireCode(t, launch, 200)
	if !strings.Contains(launch.Body.String(), "sandbox=") || !strings.Contains(launch.Body.String(), "allow-top-navigation-by-user-activation") || strings.Contains(launch.Body.String(), "allow-top-navigation\"") || !strings.Contains(launch.Body.String(), `allow="fullscreen; autoplay; encrypted-media; picture-in-picture; clipboard-write"`) {
		t.Fatal("runtime iframe compatibility permissions changed")
	}
	manifest := pwaPublicCall(t, s, base+"manifest.webmanifest", "", "")
	requireCode(t, manifest, 200)
	if !strings.HasPrefix(manifest.Header().Get("Content-Type"), "application/manifest+json") {
		t.Fatal("manifest MIME type")
	}
	var data map[string]any
	if err := json.Unmarshal(manifest.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data["scope"] != base || data["start_url"] != base+"launch" || data["id"] != base || data["display"] != "standalone" {
		t.Fatal("manifest escapes app scope")
	}
	sw := pwaPublicCall(t, s, base+"sw.js", "", "")
	requireCode(t, sw, 200)
	if sw.Header().Get("Service-Worker-Allowed") != "" || strings.Contains(sw.Body.String(), "cache.put(event.request") || !strings.Contains(sw.Body.String(), "event_token") || !strings.Contains(sw.Body.String(), "showNotification") || !strings.Contains(sw.Body.String(), "displayed") || !strings.Contains(sw.Body.String(), "notificationclick") {
		t.Fatal("SW scope, caching or event contract broken")
	}
	config := pwaPublicCall(t, s, base+"push-config", "", "")
	requireCode(t, config, 200)
	if !strings.Contains(config.Body.String(), app.VAPIDPublicKey) || strings.Contains(config.Body.String(), app.VAPIDPrivateKey) {
		t.Fatal("push public config key exposure wrong")
	}
	root := pwaPublicCall(t, s, "/", "", "")
	if !strings.Contains(root.Header().Get("Content-Security-Policy"), "frame-src "+s.Config.PublicURL+"/api/pwa-landing/preview/;") {
		t.Fatal("console must only embed its scoped authenticated template preview")
	}
	suffixes := []string{"", "launch", "manifest.webmanifest", "sw.js", "client.js", "style.css", "icon/192.png", "push-config"}
	for _, suffix := range suffixes {
		requireCode(t, pwaPublicCall(t, s, base+suffix, "customer.example.com", ""), 404)
	}
	if err := s.DB.Model(&app).Update("push_enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	config = pwaPublicCall(t, s, base+"push-config", "", "")
	requireCode(t, config, 200)
	if strings.Contains(config.Body.String(), app.VAPIDPublicKey) || !strings.Contains(config.Body.String(), `"enabled":false`) {
		t.Fatal("disabled push exposed active key")
	}
	if err := s.DB.Model(&owner).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	for _, suffix := range suffixes {
		requireCode(t, pwaPublicCall(t, s, base+suffix, "", ""), 404)
	}
	s.DB.Model(&owner).Update("disabled", false)
	s.DB.Model(&app).Update("enabled", false)
	for _, suffix := range suffixes {
		requireCode(t, pwaPublicCall(t, s, base+suffix, "", ""), 404)
	}
}

func TestPWARuntimeCrossOriginCompatibilityKeepsD1Unframeable(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	app := createPWAFixture(t, s, owner, "H5 runtime")
	s.Config.WebDir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(s.Config.WebDir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{"index.html": "<!doctype html><title>Console</title>", "assets/runtime-test.js": "console.log('static');"} {
		if err := os.WriteFile(filepath.Join(s.Config.WebDir, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	base := "/p/" + app.Slug + "/"
	// External H5 redirects may reach any HTTPS origin. A redirect back to D1
	// must still be rejected as a frame before it can access its same-origin
	// parent. Keep both protections on successful, private and error responses.
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/", 200}, {"/index.html", 301}, {"/assets/runtime-test.js", 200},
		{"/api/pwas", 401}, {"/api/does-not-exist", 404},
		{base, 200}, {base + "launch", 200}, {base + "push-config", 200},
		{base + "sw.js", 200}, {base + "icon/192.png", 200}, {"/p/invalid/launch", 404},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := httptest.NewRequest("GET", s.Config.PublicURL+tc.path, nil)
			r.Header.Set("Sec-Fetch-Dest", "iframe")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			requireCode(t, w, tc.status)
			if w.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
				t.Fatalf("D1 document became frameable: %v", w.Header())
			}
		})
	}
	for _, tc := range []struct {
		name, env, target, frameSources string
		status                          int
	}{
		{"production HTTPS redirects", "production", "https://example.com/app", "https:", 200},
		{"development HTTPS redirects", "test", "https://example.com/app", "https:", 200},
		{"development IPv4 loopback", "test", "http://127.0.0.1:9999/app", "https: http://127.0.0.1:9999", 200},
		{"unaccepted IPv6 target stays unavailable", "test", "http://[::1]:9999/app", "'none'", 404},
		{"production rejects loopback", "production", "http://127.0.0.1:9999/app", "'none'", 404},
		{"development rejects external HTTP", "test", "http://example.com/app", "'none'", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.Config.Env = tc.env
			if err := s.DB.Model(&app).Update("target_url", tc.target).Error; err != nil {
				t.Fatal(err)
			}
			w := pwaPublicCall(t, s, base+"launch", "", "")
			requireCode(t, w, tc.status)
			if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-src "+tc.frameSources+";") || strings.Contains(csp, "frame-src https: http:;") {
				t.Fatalf("unexpected frame source policy: %s", csp)
			}
		})
	}
}

func TestPWAIconNormalizationAndLimits(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	app := createPWAFixture(t, s, owner, "Great App")
	cookie, csrf := sessionFor(t, s, owner)
	base := "/p/" + app.Slug + "/"
	for _, size := range []string{"192", "512"} {
		w := pwaPublicCall(t, s, base+"icon/"+size+".png", "", "")
		requireCode(t, w, 200)
		cfg, err := png.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
		if err != nil || cfg.Width != cfg.Height || cfg.Width != map[string]int{"192": 192, "512": 512}[size] {
			t.Fatal("invalid generated default PNG", err)
		}
	}
	original := image.NewRGBA(image.Rect(0, 0, 80, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 80; x++ {
			original.Set(x, y, color.RGBA{220, 40, 60, 255})
		}
	}
	for _, format := range []string{"png", "jpeg"} {
		var buf bytes.Buffer
		if format == "png" {
			png.Encode(&buf, original)
		} else {
			jpeg.Encode(&buf, original, nil)
		}
		w := pwaIconCall(t, s, app.ID, buf.Bytes(), cookie, csrf, false)
		requireCode(t, w, 200)
		var fresh PWAApp
		s.DB.First(&fresh, "id = ?", app.ID)
		cfg, err := png.DecodeConfig(bytes.NewReader(fresh.IconPNG))
		if err != nil || cfg.Width != 512 || cfg.Height != 512 {
			t.Fatal("uploaded icon not normalized PNG512")
		}
		public := pwaPublicCall(t, s, base+"icon/192.png", "", "")
		requireCode(t, public, 200)
		if public.Header().Get("ETag") == "" || public.Header().Get("Content-Type") != "image/png" {
			t.Fatal("public icon headers missing")
		}
	}
	var good bytes.Buffer
	png.Encode(&good, original)
	requireCode(t, pwaIconCall(t, s, app.ID, good.Bytes(), cookie, "", false), 403)
	requireCode(t, pwaIconCall(t, s, app.ID, good.Bytes(), cookie, csrf, true), 400)
	requireCode(t, pwaIconCall(t, s, app.ID, []byte(`<svg onload="alert(1)"></svg>`), cookie, csrf, false), 400)
	requireCode(t, pwaIconCall(t, s, app.ID, nil, cookie, csrf, false), 400)
	requireCode(t, pwaIconCall(t, s, app.ID, bytes.Repeat([]byte("x"), pwaMaxIconBytes+1), cookie, csrf, false), 413)
	var oversized bytes.Buffer
	png.Encode(&oversized, image.NewGray(image.Rect(0, 0, 4097, 1)))
	requireCode(t, pwaIconCall(t, s, app.ID, oversized.Bytes(), cookie, csrf, false), 400)
	requireCode(t, pwaPublicCall(t, s, base+"icon/1024.png", "", ""), 404)
}
