package platform

import (
	"encoding/json"

	"strings"
	"testing"
)

func landingFixture() PWALandingConfig {
	return PWALandingConfig{Template: "google_play", StartupBgColor: "#ffffff", StartupThemeColor: "#01875f", DisplayMode: "fullscreen", PWAOrientation: "portrait", EnableGoogleBadge: true, EnableOfficialVerify: true, LanguagePriority: []string{"en-US", "zh-CN"}, StoreInfos: []PWALandingLanguage{{Language: "en-US", AppName: "North Studio", DeveloperName: "North", AppSummary: "A useful app", DownloadCount: "1M+", ReviewCount: "200", DetailImages: []string{"https://images.example/screen.png"}, Reviews: []PWALandingReview{{Nickname: "Reviewer", Comment: "Great", Rating: 5}}}, {Language: "zh-CN", AppName: "北方工作室", AppSummary: "欢迎使用", CustomImages: []string{"https://images.example/custom.png"}, ShowInstallButton: true, ButtonText: "安装", ButtonPosition: "bottom"}}, Settings: PWALandingSettings{PackagePriority: "pwa", IOSCompatible: true, EnableIframeEmbed: true}}
}
func TestPWALandingHelperLanguageMatchesConfiguredPage(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	p := createPWAFixture(t, s, u, "Localized install page")
	c := landingFixture()
	c.LanguagePriority = []string{"ar-SA"}
	c.StoreInfos = []PWALandingLanguage{{Language: "ar-SA", AppName: "Arabic page"}, {Language: "en-US", AppName: "English page"}}
	raw, _ := json.Marshal(c)
	if err := s.DB.Model(&p).Update("landing_json", string(raw)).Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ suffix, browser, language string }{
		{"", "zh-CN", "ar-SA"},
		{"?language=ar-SA", "en-US", "ar-SA"},
		{"?language=en-US", "zh-CN", "en-US"},
	} {
		w := pwaPublicCall(t, s, "/p/"+p.Slug+"/"+test.suffix, "", test.browser)
		requireCode(t, w, 200)
		if !strings.Contains(w.Body.String(), `lang="`+test.language+`"`) || !strings.Contains(w.Body.String(), `data-locale="en"`) {
			t.Fatal("install helper text follows browser language instead of configured page")
		}
	}
}
func TestPWAInstallAnimationStylesStayOnInstallPages(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	app := createPWAFixture(t, s, owner, "Install animation")
	base := "/p/" + app.Slug + "/"
	for _, layout := range []string{"google_play", "custom", "legacy"} {
		c := landingFixture()
		c.Template = layout
		raw, _ := json.Marshal(c)
		if err := s.DB.Model(&app).Update("landing_json", string(raw)).Error; err != nil {
			t.Fatal(err)
		}
		styles := pwaPublicCall(t, s, base+"style.css", "", "")
		requireCode(t, styles, 200)
		if !strings.Contains(styles.Body.String(), "d1-pwa-install-spin") || !strings.Contains(styles.Body.String(), "prefers-reduced-motion: reduce") {
			t.Fatalf("%s install page is missing icon feedback styles", layout)
		}
		launchStyles := pwaPublicCall(t, s, base+"style.css?launch=1", "", "")
		requireCode(t, launchStyles, 200)
		if strings.Contains(launchStyles.Body.String(), "d1-pwa-install-spin") {
			t.Fatal("installation animation leaked into application runtime")
		}
	}
	if countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("animation assets changed billing")
	}
}
func TestPWALandingConfigRoundTripTemplatesAndLanguage(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	app := createPWAFixture(t, s, owner, "North Studio")
	c := landingFixture()
	c.StoreInfos[1].AppIcon = "https://images.example/zh-icon.webp"
	path := "/api/pwas/" + app.ID
	base := "/p/" + app.Slug + "/"
	requireCode(t, call(t, s, "PUT", path, map[string]any{"landing_config": c}, cookie, csrf), 200)
	var got pwaAppView
	view := call(t, s, "GET", path, nil, cookie, "")
	requireCode(t, view, 200)
	_ = json.Unmarshal(view.Body.Bytes(), &got)
	if got.LandingConfig.StoreInfos[0].Reviews[0].Comment != "Great" || got.LandingConfig.DisplayMode != "fullscreen" {
		t.Fatal("creation settings lost")
	}
	page := pwaPublicCall(t, s, base, "", "en-US")
	requireCode(t, page, 200)
	if !strings.Contains(page.Body.String(), "google_play-") || strings.Contains(page.Body.String(), "class=\"install-shell\"") || !strings.Contains(page.Body.String(), "landing-config.js") {
		t.Fatal("reference template not served")
	}
	if !strings.Contains(page.Header().Get("Content-Security-Policy"), "script-src 'self';") || !strings.Contains(page.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("script/document isolation changed")
	}
	config := pwaPublicCall(t, s, base+"landing-config.js", "", "zh-CN")
	requireCode(t, config, 200)
	if !strings.Contains(config.Body.String(), "北方工作室") || strings.Contains(config.Body.String(), "vapid_private") || strings.Contains(config.Body.String(), "user_id") {
		t.Fatal("localized runtime invalid")
	}
	runtime := pwaPublicCall(t, s, base+"api/promotion-links/by-domain?language=en-US&domain=other.example", "", "")
	requireCode(t, runtime, 200)
	if !strings.Contains(runtime.Body.String(), "North Studio") {
		t.Fatal("custom scoped API mismatch")
	}
	manifest := pwaPublicCall(t, s, base+"manifest.webmanifest", "", "zh-CN")
	requireCode(t, manifest, 200)
	var fields map[string]any
	_ = json.Unmarshal(manifest.Body.Bytes(), &fields)
	if fields["display"] != "fullscreen" || fields["orientation"] != "portrait" || fields["name"] != "北方工作室" || fields["scope"] != base || fields["theme_color"] != "#01875f" {
		t.Fatal("manifest settings not applied", fields)
	}
	icons := fields["icons"].([]any)
	if len(icons) != 1 || icons[0].(map[string]any)["src"] != "https://images.example/zh-icon.webp" {
		t.Fatal("localized install icon not applied")
	}
	launchCSS := pwaPublicCall(t, s, base+"style.css?launch=1", "", "")
	requireCode(t, launchCSS, 200)
	if !strings.Contains(launchCSS.Body.String(), ".runtime-bar") {
		t.Fatal("reference app lost runtime layout")
	}
	installCSS := pwaPublicCall(t, s, base+"style.css", "", "")
	if strings.Contains(installCSS.Body.String(), ".install-page{min-height") {
		t.Fatal("legacy styles leaked into reference template")
	}
	c.Template = "custom"
	requireCode(t, call(t, s, "PUT", path, map[string]any{"landing_config": c}, cookie, csrf), 200)
	page = pwaPublicCall(t, s, base, "", "zh-CN")
	requireCode(t, page, 200)
	if !strings.Contains(page.Body.String(), "/pwa-template-assets/custom-") {
		t.Fatal("custom template not selected")
	}
	other := seedUser(t, s, 0)
	otherCookie, otherCSRF := sessionFor(t, s, other)
	requireCode(t, call(t, s, "GET", path, nil, otherCookie, ""), 404)
	requireCode(t, call(t, s, "PUT", path, map[string]any{"landing_config": c}, otherCookie, otherCSRF), 404)
	requireCode(t, call(t, s, "GET", "/api/pwa-landing/preview/google_play?preview=true", nil, nil, ""), 401)
	preview := call(t, s, "GET", "/api/pwa-landing/preview/google_play?preview=true", nil, cookie, "")
	requireCode(t, preview, 200)
	if preview.Header().Get("X-Frame-Options") != "SAMEORIGIN" || strings.Contains(preview.Body.String(), "landing-config.js") {
		t.Fatal("preview boundary invalid")
	}
	requireCode(t, call(t, s, "PUT", path, map[string]any{"enabled": false}, cookie, csrf), 200)
	for _, suffix := range []string{"", "landing-config.js", "api/promotion-links/by-domain"} {
		requireCode(t, pwaPublicCall(t, s, base+suffix, "", ""), 404)
	}
	if countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &PWAPushDelivery{}) != 0 {
		t.Fatal("creation changed billing or deliveries")
	}
}
func TestPWALandingValidationAssetIsolationAndIdempotency(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	other := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	app := createPWAFixture(t, s, owner, "Existing")
	path := "/api/pwas/" + app.ID
	otherMedia := returnAssetCall(t, s, returnImage(t), other, "")
	requireCode(t, otherMedia, 201)
	c := landingFixture()
	c.StoreInfos[0].AppIcon = returnAssetURL(t, otherMedia)
	requireCode(t, call(t, s, "PUT", path, map[string]any{"landing_config": c}, cookie, csrf), 400)
	c = landingFixture()
	c.StoreInfos[0].DetailImages = []string{"javascript:alert(1)"}
	requireCode(t, call(t, s, "PUT", path, map[string]any{"landing_config": c}, cookie, csrf), 400)
	c = landingFixture()
	c.StoreInfos = append(c.StoreInfos, c.StoreInfos[0])
	requireCode(t, call(t, s, "PUT", path, map[string]any{"landing_config": c}, cookie, csrf), 400)
	c = landingFixture()
	c.Settings.PackagePriority = "w2a_only"
	requireCode(t, call(t, s, "PUT", path, map[string]any{"landing_config": c}, cookie, csrf), 400)
	c = landingFixture()
	body := map[string]any{"name": "Configured", "target_url": "https://example.com", "landing_config": c}
	key := randomHex(16)
	requireCode(t, pwaKeyCall(t, s, body, cookie, csrf, key), 201)
	requireCode(t, pwaKeyCall(t, s, body, cookie, csrf, key), 201)
	c.Template = "custom"
	body["landing_config"] = c
	requireCode(t, pwaKeyCall(t, s, body, cookie, csrf, key), 409)
	requireCode(t, call(t, s, "GET", "/pwa-template-assets/.vite/manifest.json", nil, nil, ""), 404)
}
func TestPWALandingMigrationPreservesExistingConfig(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 8*Dollar)
	app := createPWAFixture(t, s, owner, "Existing")
	returnConfig := defaultReturnFlowConfig()
	returnConfig.Enabled = true
	raw := returnFlowJSON(&returnConfig)
	if e := s.DB.Model(&PWAApp{}).Where("id = ?", app.ID).Update("return_flow_json", raw).Error; e != nil {
		t.Fatal(e)
	}
	if e := s.DB.Migrator().DropColumn(&PWAApp{}, "landing_json"); e != nil {
		t.Fatal(e)
	}
	if e := s.DB.Delete(&Migration{}, "version = ?", 16).Error; e != nil {
		t.Fatal(e)
	}
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	var got PWAApp
	if e := s.DB.Select("pwa_apps.*").First(&got, "id = ?", app.ID).Error; e != nil {
		t.Fatal(e)
	}
	if got.LandingJSON != "{}" || got.ReturnFlowJSON != raw || got.VAPIDPrivateKey != app.VAPIDPrivateKey || got.TargetURL != app.TargetURL || got.CreateFingerprint != app.CreateFingerprint {
		t.Fatal("migration rewrote app state")
	}
	var balance User
	_ = s.DB.First(&balance, "id = ?", owner.ID).Error
	if balance.BalanceMicros != owner.BalanceMicros {
		t.Fatal("wallet changed")
	}
}
