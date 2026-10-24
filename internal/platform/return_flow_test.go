package platform

import (
	"bytes"
	"encoding/json"
	"html"
	"image"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func returnAssetCall(t *testing.T, s *Server, raw []byte, owner User, csrfMode string) *httptest.ResponseRecorder {
	t.Helper()
	cookie, csrf := sessionFor(t, s, owner)
	if csrfMode == "wrong" {
		csrf = "wrong"
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "creative.mp4")
	_, _ = part.Write(raw)
	_ = writer.Close()
	r := httptest.NewRequest("POST", s.Config.PublicURL+"/api/return-flow/assets", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("X-CSRF-Token", csrf)
	if csrfMode != "anonymous" {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func returnImage(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	_ = png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 12, 20)))
	return out.Bytes()
}
func returnAssetURL(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var value struct {
		URL string `json:"url"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &value); e != nil {
		t.Fatal(e)
	}
	return value.URL
}
func TestReturnFlowAssetsIsolationNormalizationAndRange(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	other := seedUser(t, s, 0)
	data := returnImage(t)
	requireCode(t, returnAssetCall(t, s, data, owner, "anonymous"), 401)
	requireCode(t, returnAssetCall(t, s, data, owner, "wrong"), 403)
	w := returnAssetCall(t, s, data, owner, "")
	requireCode(t, w, 201)
	first := returnAssetURL(t, w)
	w = returnAssetCall(t, s, data, owner, "")
	requireCode(t, w, 201)
	if returnAssetURL(t, w) != first {
		t.Fatal("same owner asset not deduplicated")
	}
	w = returnAssetCall(t, s, data, other, "")
	requireCode(t, w, 201)
	if returnAssetURL(t, w) == first {
		t.Fatal("asset deduplicated across users")
	}
	cookie, csrf := sessionFor(t, s, owner)
	app := createPWAFixture(t, s, owner, "Return demo")
	c := defaultReturnFlowConfig()
	c.Enabled = true
	c.MediaURL = first
	c.ButtonAction = "url"
	c.ButtonURL = "https://example.com/offer"
	requireCode(t, call(t, s, "PUT", "/api/pwas/"+app.ID, map[string]any{"return_flow_config": c}, cookie, csrf), 200)
	c.MediaURL = returnAssetURL(t, w)
	requireCode(t, call(t, s, "PUT", "/api/pwas/"+app.ID, map[string]any{"return_flow_config": c}, cookie, csrf), 400)
	r := httptest.NewRequest("GET", first, nil)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 200)
	if w.Header().Get("Content-Type") != "image/png" {
		t.Fatal("unsafe image MIME")
	}
	if _, err := png.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
		t.Fatal(err)
	}
	movie := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, bytes.Repeat([]byte{0}, 96)...)
	w = returnAssetCall(t, s, movie, owner, "")
	requireCode(t, w, 201)
	video := returnAssetURL(t, w)
	r = httptest.NewRequest("GET", video, nil)
	r.Header.Set("Range", "bytes=0-11")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 206)
	if w.Body.Len() != 12 || w.Header().Get("Content-Type") != "video/mp4" {
		t.Fatal("video range support missing")
	}
	r = httptest.NewRequest("HEAD", video, nil)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 200)
	if w.Body.Len() != 0 {
		t.Fatal("HEAD returned body")
	}
	r = httptest.NewRequest("GET", video, nil)
	r.Host = "other.example"
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 404)
	_ = s.DB.Model(&owner).Update("disabled", true).Error
	r = httptest.NewRequest("GET", video, nil)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 404)
	if countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &Ticket{}) != 0 {
		t.Fatal("media upload changed billing")
	}
}
func TestReturnFlowAssetsValidationAndRollback(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	for _, raw := range [][]byte{[]byte("<svg onload=alert(1)></svg>"), []byte("<html>unsafe</html>"), {}, bytes.Repeat([]byte("x"), int(maxReturnFlowAssetBytes)+1)} {
		requireCode(t, returnAssetCall(t, s, raw, owner, ""), 400)
	}
	for _, f := range []struct {
		data      []byte
		mime, ext string
	}{{[]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'q', 't', ' ', ' '}, "video/quicktime", "mov"}, {[]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'M', '4', 'V', ' '}, "video/mp4", "m4v"}, {[]byte{0x1a, 0x45, 0xdf, 0xa3, 0x42, 0x82, 0x84, 'w', 'e', 'b', 'm'}, "video/webm", "webm"}} {
		m, e := returnFlowVideoType(f.data)
		if m != f.mime || e != f.ext {
			t.Fatal("video format mismatch")
		}
	}
	if err := s.DB.Migrator().DropTable(&Audit{}); err != nil {
		t.Fatal(err)
	}
	requireCode(t, returnAssetCall(t, s, returnImage(t), owner, ""), 500)
	entries, e := os.ReadDir(s.returnFlowAssetDirectory())
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 || countRows(t, s, &ReturnFlowAsset{}) != 0 {
		t.Fatal("failed transaction retained media")
	}
}
func TestReturnFlowConfigPublicRenderingAndAppDownload(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 3*Dollar)
	app := createPWAFixture(t, s, owner, "Return app")
	cookie, csrf := sessionFor(t, s, owner)
	c := defaultReturnFlowConfig()
	c.Enabled = true
	c.TriggerMode = "both"
	c.ShowOnce = false
	c.MediaURL = "https://media.example/banner.mp4"
	c.Title = `"><script>alert(1)</script>`
	c.ButtonAction = "download"
	c.MediaAreaPercent = 100
	requireCode(t, call(t, s, "PUT", "/api/pwas/"+app.ID, map[string]any{"return_flow_config": c}, cookie, csrf), 200)
	saved := call(t, s, "GET", "/api/pwas/"+app.ID, nil, cookie, "")
	requireCode(t, saved, 200)
	if strings.Contains(saved.Body.String(), "vapid_private_key") || !strings.Contains(saved.Body.String(), `"showOnce":false`) {
		t.Fatal("config view mismatch")
	}
	public := pwaPublicCall(t, s, "/p/"+app.Slug+"/", "", "")
	requireCode(t, public, 200)
	if strings.Contains(public.Body.String(), c.Title) || !strings.Contains(html.UnescapeString(public.Body.String()), `"triggerMode":"both"`) || !strings.Contains(public.Header().Get("Content-Security-Policy"), "media-src 'self' https:") {
		t.Fatal("public config unsafe or missing")
	}
	launch := pwaPublicCall(t, s, "/p/"+app.Slug+"/launch", "", "")
	if strings.Contains(launch.Body.String(), "data-return-flow=") {
		t.Fatal("return modal must only run on install entry")
	}
	a := seedApp(t, s, owner, 12)
	d := defaultPageDesign(a)
	d.ReturnFlowConfig = &c
	path := "/api/apps/" + a.ID + "/page-design"
	requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 200)
	for _, layout := range []string{"google_play", "store", "minimal"} {
		d.Layout = layout
		requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 200)
		w := call(t, s, "GET", "/a/"+a.Slug+"/info", nil, nil, "")
		requireCode(t, w, 200)
		if !strings.Contains(html.UnescapeString(w.Body.String()), `"buttonAction":"download"`) {
			t.Fatal("download page config missing")
		}
	}
	w := call(t, s, "GET", "/a/"+a.Slug+"/client.js", nil, nil, "")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), "d1-apk-download-start") || !strings.Contains(w.Body.String(), "D1ReturnFlowInstall") {
		t.Fatal("APK adapter missing")
	}
	if countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("return flow settings changed billing")
	}
}
func TestReturnFlowConfigValidationAndLegacyCreationReplay(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	body := map[string]any{"name": "Legacy request", "target_url": "https://example.com"}
	key := randomHex(16)
	first := pwaKeyCall(t, s, body, cookie, csrf, key)
	requireCode(t, first, 201)
	requireCode(t, pwaKeyCall(t, s, body, cookie, csrf, key), 201)
	var view pwaAppView
	_ = json.Unmarshal(first.Body.Bytes(), &view)
	if view.ReturnFlowConfig.Enabled || !view.ReturnFlowConfig.ShowOnce || view.ReturnFlowConfig.MediaAreaPercent != 50 {
		t.Fatal("old app not disabled by default")
	}
	for _, field := range []struct {
		k string
		v any
	}{{"triggerMode", "bad"}, {"buttonAction", "bad"}, {"mediaAreaPercent", 49}, {"mediaAreaPercent", 101}, {"mediaUrl", "javascript:alert(1)"}, {"buttonUrl", "https://user:pass@example.com"}, {"mediaUrl", "http://insecure.example/banner.jpg"}, {"title", strings.Repeat("x", 101)}} {
		c := map[string]any{"enabled": true, "triggerMode": "back", "buttonAction": "continue"}
		c[field.k] = field.v
		requireCode(t, call(t, s, "PUT", "/api/pwas/"+view.ID, map[string]any{"return_flow_config": c}, cookie, csrf), 400)
	}
	requireCode(t, call(t, s, "PUT", "/api/pwas/"+view.ID, map[string]any{"return_flow_config": map[string]any{"enabled": true, "buttonAction": "url"}}, cookie, csrf), 400)
	c := defaultReturnFlowConfig()
	c.Enabled = true
	body["return_flow_config"] = c
	requireCode(t, pwaKeyCall(t, s, body, cookie, csrf, key), 409)
	requireCode(t, call(t, s, "PUT", "/api/pwas/"+view.ID, map[string]any{"return_flow_config": c}, cookie, csrf), 200)
	delete(body, "return_flow_config")
	requireCode(t, pwaKeyCall(t, s, body, cookie, csrf, key), 201)
}
func TestReturnFlowMigrationPreservesExistingPWAAndLedger(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 7*Dollar)
	app := createPWAFixture(t, s, owner, "Existing app")
	ledger := Ledger{ID: randomHex(16), UserID: owner.ID, Reference: randomHex(16), Kind: "test", AmountMicros: Dollar, BalanceMicros: 7 * Dollar}
	if e := s.DB.Create(&ledger).Error; e != nil {
		t.Fatal(e)
	}
	if e := s.DB.Migrator().DropColumn(&PWAApp{}, "return_flow_json"); e != nil {
		t.Fatal(e)
	}
	if e := s.DB.Migrator().DropTable(&ReturnFlowAsset{}); e != nil {
		t.Fatal(e)
	}
	if e := s.DB.Delete(&Migration{}, "version = ?", 15).Error; e != nil {
		t.Fatal(e)
	}
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	var got PWAApp
	// Production starts a fresh process after DDL. Avoid the cached SELECT *
	// shape from before this test deliberately dropped and re-added the column.
	if e := s.DB.Select("pwa_apps.*").First(&got, "id = ?", app.ID).Error; e != nil {
		t.Fatal(e)
	}
	if got.ReturnFlowJSON != "{}" || got.TargetURL != app.TargetURL || got.VAPIDPrivateKey != app.VAPIDPrivateKey || got.CreateFingerprint != app.CreateFingerprint || got.DeletedAt != app.DeletedAt {
		t.Fatal("migration changed existing PWA")
	}
	var balance User
	_ = s.DB.First(&balance, "id = ?", owner.ID).Error
	if balance.BalanceMicros != owner.BalanceMicros || countRows(t, s, &Ledger{}) != 1 || countRows(t, s, &ReturnFlowAsset{}) != 0 {
		t.Fatal("migration changed ledger")
	}
	var count int64
	_ = s.DB.Model(&Migration{}).Where("version = ?", 15).Count(&count).Error
	if count != 1 {
		t.Fatal("migration not idempotent")
	}
	_, e := os.Stat(filepath.Join(s.Config.DataDir, "return-flow-assets"))
	if !os.IsNotExist(e) {
		t.Fatal("migration created asset files")
	}
}
