package platform

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAPKReferenceLandingSettingsAndNativeDownloadBoundary(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 5*Dollar)
	app := seedApp(t, s, owner, 128)
	cookie, csrf := sessionFor(t, s, owner)
	d := defaultPageDesign(app)
	d.Title = "My APK"
	d.DisplayRating = "4.8"
	d.DisplayReviewCount = "12K"
	d.Landing = &APKLandingOptions{AgeRating: "18", EnableQRCode: true, StayPromptText: "Ready to install?", Reviews: []PWALandingReview{{Nickname: "Customer", Comment: "Useful app", Rating: 5, CommentLanguage: "en-US"}}, ButtonPosition: "bottom", ButtonBgColor: "#1976D2", ButtonTextColor: "#FFFFFF"}
	path := "/api/apps/" + app.ID + "/page-design"
	base := "/a/" + app.Slug
	requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 200)
	for _, layout := range []string{"google_play", "custom"} {
		d.Layout = layout
		requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 200)
		page := call(t, s, "GET", base+"/info", nil, nil, "")
		requireCode(t, page, 200)
		if !strings.Contains(page.Body.String(), "d1-native-download") || !strings.Contains(page.Body.String(), base+"/download") || !strings.Contains(page.Body.String(), "/pwa-template-assets/") || strings.Contains(page.Body.String(), "sw.js") {
			t.Fatal("APK template must keep native ticket flow and exclude PWA worker")
		}
		config := call(t, s, "GET", base+"/landing-config.js", nil, nil, "")
		requireCode(t, config, 200)
		raw := strings.TrimSuffix(strings.TrimPrefix(config.Body.String(), "window.__APP_CONFIG__ = "), ";")
		var fields map[string]any
		if e := json.Unmarshal([]byte(raw), &fields); e != nil {
			t.Fatal(e)
		}
		settings := fields["app"].(map[string]any)["settings"].(map[string]any)
		if settings["packagePriority"] != "pwa" || settings["apkUrl"] != nil {
			t.Fatal("template attempted a direct package download bypass")
		}
		if fields["d1APK"].(map[string]any)["rating"] != "4.8" || !strings.Contains(config.Body.String(), "Useful app") {
			t.Fatal("new editor fields missing in reference template config")
		}
		custom := call(t, s, "GET", base+"/api/promotion-links/by-domain", nil, nil, "")
		requireCode(t, custom, 200)
		if !strings.Contains(custom.Body.String(), "My APK") {
			t.Fatal("custom image template config missing")
		}
	}
	script := call(t, s, "GET", base+"/client.js", nil, nil, "")
	if !strings.Contains(script.Body.String(), "form.requestSubmit()") || !strings.Contains(script.Body.String(), "options?.source==='return_flow'") || !strings.Contains(script.Body.String(), "new MutationObserver") {
		t.Fatal("source template did not bind to original download handling")
	}
	if countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &Download{}) != 0 {
		t.Fatal("view/config/preview billed a download")
	}
	other := seedUser(t, s, 0)
	asset := returnAssetCall(t, s, returnImage(t), other, "")
	requireCode(t, asset, 201)
	d.Screenshots = []string{returnAssetURL(t, asset)}
	requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 400)
	d.Screenshots = nil
	d.Landing.ButtonBgColor = "red;background:evil"
	requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 400)
}
