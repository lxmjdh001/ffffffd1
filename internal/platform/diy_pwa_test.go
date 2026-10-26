package platform

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func diyPWAConfig(page diyLandingDetail) PWALandingConfig {
	return PWALandingConfig{Template: "diy", DIYLanding: &DIYLandingReference{PageID: page.ID, RevisionID: page.LatestPublishedRevisionID}, StartupBgColor: "#ffffff", StartupThemeColor: "#167d9a", Settings: PWALandingSettings{PackagePriority: "pwa"}, StoreInfos: []PWALandingLanguage{{Language: "not a language", AppName: "Hidden old store title", AppIcon: "not an image URL"}}, LanguagePriority: []string{"unused-hidden-locale"}}
}

func diyPWAView(t *testing.T, w *httptest.ResponseRecorder) pwaAppView {
	t.Helper()
	var view pwaAppView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func TestDIYPWACreateEditBindingsAreAtomicAndIdempotent(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, 0), seedUser(t, s, 0)
	page := publishDIYFixture(t, s, owner, createDIYFixture(t, s, owner, "Reusable"))
	foreign := publishDIYFixture(t, s, other, createDIYFixture(t, s, other, "Foreign"))
	cookie, csrf := sessionFor(t, s, owner)
	config := diyPWAConfig(page)
	config.Settings.ExternalBrowser = &ExternalBrowserConfig{Enabled: true, URL: "https://example.com/browser"}
	ret := defaultReturnFlowConfig()
	ret.Enabled = true
	ret.Title = "Come back"
	ret.ButtonText = "Continue"
	body := map[string]any{"name": "DIY PWA", "target_url": "https://example.com/app", "landing_config": config, "return_flow_config": ret}
	w := pwaKeyCall(t, s, body, cookie, csrf, "diy-pwa-create-request")
	requireCode(t, w, 201)
	view := diyPWAView(t, w)
	if view.LandingConfig.DIYLanding == nil || !diySameReference(view.LandingConfig.DIYLanding, config.DIYLanding) || !view.ReturnFlowConfig.Enabled || view.LandingConfig.Settings.ExternalBrowser == nil {
		t.Fatalf("creation lost configuration: %+v", view)
	}
	var stored PWAApp
	if err := s.DB.First(&stored, "id = ?", view.ID).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored.LandingJSON, "diy_landing") {
		t.Fatal("binding stored twice in landing JSON")
	}
	w = pwaKeyCall(t, s, body, cookie, csrf, "diy-pwa-create-request")
	requireCode(t, w, 201)
	if diyPWAView(t, w).ID != view.ID || countRows(t, s, &PWAApp{}) != 1 || countRows(t, s, &DIYLandingBinding{}) != 1 {
		t.Fatal("create retry duplicated app or binding")
	}
	body["landing_config"] = diyPWAConfig(foreign)
	requireCode(t, pwaKeyCall(t, s, body, cookie, csrf, "diy-pwa-create-request"), 409)
	requireCode(t, pwaKeyCall(t, s, body, cookie, csrf, "foreign-binding-create"), 404)
	if countRows(t, s, &PWAApp{}) != 1 {
		t.Fatal("invalid binding left a partial PWA")
	}
	path := "/api/pwas/" + view.ID
	requireCode(t, call(t, s, "PUT", path, map[string]any{"name": "Must roll back", "landing_config": diyPWAConfig(foreign)}, cookie, csrf), 404)
	w = call(t, s, "GET", path, nil, cookie, csrf)
	requireCode(t, w, 200)
	view = diyPWAView(t, w)
	if view.Name != "DIY PWA" || view.LandingConfig.DIYLanding == nil || view.LandingConfig.DIYLanding.PageID != page.ID {
		t.Fatal("invalid binding partially changed application")
	}
	requireCode(t, call(t, s, "PUT", path, map[string]any{"name": "Basic edit"}, cookie, csrf), 200)
	ref, err := diyBindingReference(s.DB, "pwa", view.ID)
	if err != nil || ref == nil || !diySameReference(ref, config.DIYLanding) {
		t.Fatal("basic edit cleared binding", err)
	}
	legacy := PWALandingConfig{Template: "legacy", Settings: config.Settings}
	w = call(t, s, "PUT", path, map[string]any{"landing_config": legacy}, cookie, csrf)
	requireCode(t, w, 200)
	if got, err := diyBindingReference(s.DB, "pwa", view.ID); err != nil || got != nil {
		t.Fatal("system template retained binding", err)
	}
	w = call(t, s, "PUT", path, map[string]any{"landing_config": config}, cookie, csrf)
	requireCode(t, w, 200)
	w = call(t, s, "DELETE", path, nil, cookie, csrf)
	requireCode(t, w, 200)
	if countRows(t, s, &DIYLandingBinding{}) != 0 {
		t.Fatal("deleting PWA left a binding")
	}
}

func TestDIYPWAImportPreservesPublishedVersionAndExplicitOverride(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	page := publishDIYFixture(t, s, owner, createDIYFixture(t, s, owner, "Reusable"))
	cookie, csrf := sessionFor(t, s, owner)
	base := map[string]any{"name": "Source", "target_url": "https://example.com/source", "landing_config": diyPWAConfig(page)}
	w := call(t, s, "POST", "/api/pwas", base, cookie, csrf)
	requireCode(t, w, 201)
	source := diyPWAView(t, w)
	w = call(t, s, "POST", "/api/pwas", map[string]any{"name": "Imported", "target_url": "https://example.com/import", "import_from": source.ID}, cookie, csrf)
	requireCode(t, w, 201)
	imported := diyPWAView(t, w)
	if imported.LandingConfig.Template != "diy" || imported.LandingConfig.DIYLanding == nil || !diySameReference(imported.LandingConfig.DIYLanding, source.LandingConfig.DIYLanding) {
		t.Fatal("import lost fixed publication reference")
	}
	w = call(t, s, "POST", "/api/pwas", map[string]any{"name": "Override", "target_url": "https://example.com/override", "import_from": source.ID, "landing_config": PWALandingConfig{Template: "legacy"}}, cookie, csrf)
	requireCode(t, w, 201)
	if got := diyPWAView(t, w); got.LandingConfig.Template != "legacy" || got.LandingConfig.DIYLanding != nil {
		t.Fatal("explicit template did not override import")
	}
}

func TestDIYPWAInstallManifestLaunchAndOriginalMetering(t *testing.T) {
	s := meteredServer(t, 0)
	owner := seedUser(t, s, Dollar)
	page := publishDIYFixture(t, s, owner, createDIYFixture(t, s, owner, "Install page"))
	cookie, csrf := sessionFor(t, s, owner)
	config := diyPWAConfig(page)
	w := call(t, s, "POST", "/api/pwas", map[string]any{"name": "Application name", "target_url": "https://example.com/target", "landing_config": config}, cookie, csrf)
	requireCode(t, w, 201)
	view := diyPWAView(t, w)
	var app PWAApp
	if err := s.DB.First(&app, "id = ?", view.ID).Error; err != nil {
		t.Fatal(err)
	}
	base := "/p/" + app.Slug + "/"
	w = pwaPublicCall(t, s, base, "", "zh-CN")
	requireCode(t, w, 200)
	for _, required := range []string{"data-d1-diy", "data-kind=\"pwa\"", "d1-diy-content", base + "manifest.webmanifest", base + "diy-parent.js", base + "landing-config.js"} {
		if !strings.Contains(w.Body.String(), required) {
			t.Fatalf("missing %s", required)
		}
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-src http://localhost:8088"+base+"diy-content") || !strings.Contains(w.Header().Get("Vary"), "Accept-Language") {
		t.Fatal("DIY shell CSP/language headers missing")
	}
	if countRows(t, s, &MeteredEvent{}) != 0 || usageOwner(t, s, owner.ID).BalanceMicros != Dollar {
		t.Fatal("PWA landing charged an install")
	}
	w = pwaPublicCall(t, s, base+"manifest.webmanifest", "", "")
	requireCode(t, w, 200)
	var manifest struct {
		Name     string              `json:"name"`
		ID       string              `json:"id"`
		Scope    string              `json:"scope"`
		StartURL string              `json:"start_url"`
		Icons    []map[string]string `json:"icons"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != app.Name || manifest.ID != base || manifest.Scope != base || manifest.StartURL != base+"launch" || manifest.Icons[0]["src"] != base+"icon/192.png" {
		t.Fatalf("DIY changed installation identity %+v", manifest)
	}
	w = pwaPublicCall(t, s, base+"launch", "", "")
	requireCode(t, w, 200)
	if strings.Contains(w.Body.String(), "data-d1-diy") || !strings.Contains(w.Body.String(), "https://example.com/target") {
		t.Fatal("launch was replaced by DIY landing")
	}
	runtime := s.pwaLandingRuntime(&app, httptest.NewRequest(http.MethodGet, s.Config.PublicURL+base, nil))
	info := runtime["app"].(map[string]any)["storeInfo"].(PWALandingLanguage)
	if info.AppName != app.Name || strings.Contains(info.AppIcon, "not an image") {
		t.Fatal("hidden store fields leaked into DIY runtime")
	}
	request := usageRequest(s, base, "203.0.113.23")
	event := installBody(s, app, request, randomHex(16))
	requireCode(t, usagePost(s, base+"install/start", event, request), 200)
	if got := usageOwner(t, s, owner.ID); got.BalanceMicros != Dollar || got.HeldMicros != 50000 {
		t.Fatal("install reservation changed")
	}
	requireCode(t, usagePost(s, base+"install/complete", event, request), 200)
	requireCode(t, usagePost(s, base+"install/complete", event, request), 200)
	if got := usageOwner(t, s, owner.ID); got.BalanceMicros != Dollar-50000 || got.HeldMicros != 0 {
		t.Fatal("install confirmation not charged exactly once")
	}
	event = installBody(s, app, request, randomHex(16))
	requireCode(t, usagePost(s, base+"install/start", event, request), 200)
	requireCode(t, usagePost(s, base+"install/cancel", event, request), 200)
	if got := usageOwner(t, s, owner.ID); got.BalanceMicros != Dollar-50000 || got.HeldMicros != 0 {
		t.Fatal("cancelled install charged")
	}
}

func TestDIYPWABusinessSettingsAndCloakEntry(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 10*Dollar)
	page := publishDIYFixture(t, s, owner, createDIYFixture(t, s, owner, "Cloaked PWA"))
	cookie, csrf := sessionFor(t, s, owner)
	config := diyPWAConfig(page)
	config.Settings.PackagePriority = "w2a_only"
	body := map[string]any{"name": "Configured PWA", "target_url": "https://example.com/app", "landing_config": config}
	requireCode(t, call(t, s, "POST", "/api/pwas", body, cookie, csrf), 400)
	config.Settings.APKURL = "https://example.com/package.apk"
	config.Settings.ExternalBrowser = &ExternalBrowserConfig{Enabled: true, URL: "https://example.com/browser"}
	config.Settings.W2AAutoDownload = true
	body["landing_config"] = config
	w := call(t, s, "POST", "/api/pwas", body, cookie, csrf)
	requireCode(t, w, 201)
	view := diyPWAView(t, w)
	var app PWAApp
	if err := s.DB.First(&app, "id = ?", view.ID).Error; err != nil {
		t.Fatal(err)
	}
	runtime := s.pwaLandingRuntime(&app, httptest.NewRequest(http.MethodGet, s.Config.PublicURL+"/p/"+app.Slug+"/", nil))
	settings := runtime["app"].(map[string]any)["settings"].(map[string]any)
	if settings["packagePriority"] != "w2a_only" || settings["apkUrl"] != config.Settings.APKURL || settings["w2aAutoDownload"] != false {
		t.Fatal("DIY lost package priority or external override")
	}
	addChosenDomain(t, s, owner, "chosen.example")
	cloakBody := cloakFixture()
	cloakBody["target_type"], cloakBody["target_id"], cloakBody["safe_url"] = "pwa", app.ID, ""
	cloak := createTestCloak(t, s, owner, cloakBody)
	w = chosenCall(s, "GET", "chosen.example", "/c/"+cloak.Slug+"?campaign=diy")
	requireCode(t, w, 200)
	for _, part := range []string{"data-d1-diy", "/p/" + app.Slug + "/manifest.webmanifest", "/p/" + app.Slug + "/diy-parent.js", "data-base=\"/p/" + app.Slug + "/\""} {
		if !strings.Contains(w.Body.String(), part) {
			t.Fatalf("cloak relative path error: %s", part)
		}
	}
}

func TestDIYPWAPreviewDoesNotCreateVisitsOrBilling(t *testing.T) {
	s := meteredServer(t, 0)
	owner, other := seedUser(t, s, 0), seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	input := diyTestInput("Preview")
	w := call(t, s, "POST", "/api/diy-landings/preview", map[string]any{"html": input["html"], "config": input["config"], "mode": "pwa"}, cookie, csrf)
	requireCode(t, w, 200)
	var out struct {
		URL string `json:"preview_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.URL, "/api/diy-landing-preview/") {
		t.Fatalf("unexpected preview URL %s", out.URL)
	}
	w = call(t, s, "GET", out.URL, nil, cookie, csrf)
	requireCode(t, w, 200)
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox allow-scripts") || strings.Contains(w.Header().Get("Content-Security-Policy"), "allow-same-origin") {
		t.Fatal("preview isolation missing")
	}
	if !strings.Contains(w.Body.String(), `"preview":true`) || strings.Contains(w.Body.String(), "data-return-flow-token") || strings.Contains(w.Body.String(), "install/start") {
		t.Fatal("preview received production capabilities")
	}
	otherCookie, otherCSRF := sessionFor(t, s, other)
	requireCode(t, call(t, s, "GET", out.URL, nil, otherCookie, otherCSRF), 404)
	for _, model := range []any{&Ledger{}, &MeteredEvent{}, &Ticket{}, &Download{}, &AccessVisit{}} {
		if countRows(t, s, model) != 0 {
			t.Fatalf("preview created production records in %T", model)
		}
	}
	if got := usageOwner(t, s, owner.ID); got.BalanceMicros != 0 || got.HeldMicros != 0 || got.GiftHeldMicros != 0 {
		t.Fatal("preview required or reserved funds")
	}
}

func TestDIYPWAConcurrentModeUpdatesKeepBindingConsistent(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	page := publishDIYFixture(t, s, owner, createDIYFixture(t, s, owner, "Concurrent"))
	cookie, csrf := sessionFor(t, s, owner)
	w := call(t, s, "POST", "/api/pwas", map[string]any{"name": "Concurrent PWA", "target_url": "https://example.com/app", "landing_config": diyPWAConfig(page)}, cookie, csrf)
	requireCode(t, w, 201)
	app := diyPWAView(t, w)
	responses := make(chan *httptest.ResponseRecorder, 8)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			config := diyPWAConfig(page)
			if index%2 == 0 {
				config = PWALandingConfig{Template: "legacy"}
			}
			responses <- call(t, s, "PUT", "/api/pwas/"+app.ID, map[string]any{"landing_config": config}, cookie, csrf)
		}(i)
	}
	group.Wait()
	close(responses)
	for result := range responses {
		requireCode(t, result, 200)
	}
	var stored PWAApp
	if err := s.DB.First(&stored, "id = ?", app.ID).Error; err != nil {
		t.Fatal(err)
	}
	config := storedPWALanding(stored.LandingJSON)
	ref, err := diyBindingReference(s.DB, "pwa", app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if config.Template == "diy" {
		if ref == nil || ref.PageID != page.ID || ref.RevisionID != page.LatestPublishedRevisionID {
			t.Fatal("DIY mode committed without matching binding")
		}
	} else if config.Template != "legacy" || ref != nil {
		t.Fatal("system mode committed with stale binding")
	}
}
