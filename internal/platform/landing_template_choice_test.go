package platform

import (
	"testing"
)

func TestAPKGoogleTemplateSelectionIsExplicitAndPreservesBusinessSettings(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	source := seedApp(t, s, owner, 128)
	d := defaultPageDesign(source)
	d.Layout = "custom"
	d.SystemLayout = "custom"
	d.ExternalBrowser = &ExternalBrowserConfig{Enabled: true, URL: "https://example.com/offer"}
	d.ReturnFlowConfig = &ReturnFlowConfig{Enabled: true, TriggerMode: "back", ShowOnce: true, MediaAreaPercent: 50, ButtonAction: "download", Title: "Keep this return page"}
	if err := s.DB.Create(&AppPageDesign{AppID: source.ID, Design: d}).Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := sessionFor(t, s, owner)
	payload := map[string]any{"name": source.Name, "description": source.Description, "version": source.Version, "enabled": source.Enabled, "landing_source": "system"}
	w := call(t, s, "PATCH", "/api/apps/"+source.ID, payload, cookie, csrf)
	requireCode(t, w, 200)
	a := diyAPKFromResponse(t, w)
	if a.LandingTemplate != "custom" {
		t.Fatal("saving old metadata silently changed its template")
	}
	payload["landing_template"] = "google_play"
	w = call(t, s, "PATCH", "/api/apps/"+source.ID, payload, cookie, csrf)
	requireCode(t, w, 200)
	a = diyAPKFromResponse(t, w)
	if a.LandingTemplate != "google_play" {
		t.Fatal("explicit Google selection was not projected")
	}
	got, err := s.pageDesign(source)
	if err != nil {
		t.Fatal(err)
	}
	if got.Layout != "google_play" || got.ExternalBrowser == nil || got.ExternalBrowser.URL != d.ExternalBrowser.URL || got.ReturnFlowConfig == nil || got.ReturnFlowConfig.Title != d.ReturnFlowConfig.Title {
		t.Fatal("changing template lost app-wide business configuration")
	}
	fields := map[string]string{"name": "Imported Google", "version": "1", "import_from": source.ID, "landing_source": "system", "landing_template": "google_play"}
	w = diyAPKUpload(t, s, owner, "/api/apps", fields, diyAPKBytes(t, "google template fixture"))
	requireCode(t, w, 201)
	created := diyAPKFromResponse(t, w)
	if created.LandingTemplate != "google_play" || created.LandingSource != "system" {
		t.Fatal("new APK did not select Google template")
	}
}

func TestAPKInvalidTemplateChoiceDoesNotLeaveAnApplication(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	for _, selected := range []string{"custom", "legacy", "unknown"} {
		fields := map[string]string{"name": "Invalid new choice", "version": "1", "landing_source": "system", "landing_template": selected}
		requireCode(t, diyAPKUpload(t, s, owner, "/api/apps", fields, diyAPKBytes(t, "invalid")), 400)
	}
	if countRows(t, s, &Application{}) != 0 || countRows(t, s, &AppPageDesign{}) != 0 || countRows(t, s, &DIYLandingBinding{}) != 0 {
		t.Fatal("failed template choices left a partial application")
	}
}
