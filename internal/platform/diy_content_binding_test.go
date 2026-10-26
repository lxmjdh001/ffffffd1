package platform

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"strings"
	"testing"
)

func diyContentBindingCode(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		body := w.Body.String()
		if len(body) > 300 {
			body = body[:300] + "…"
		}
		t.Fatalf("status=%d want=%d body=%s", w.Code, want, body)
	}
}

func diyContentBindingFixture(t *testing.T, s *Server, owner User) diyLandingDetail {
	t.Helper()
	input := diyTestInput("Shared content source")
	input["html"] = `<!doctype html><html><head><title>Original title</title></head><body><h1>Original heading</h1><p>Original text</p><img src="https://example.com/picture.png" alt="Page image"><button data-d1-action="primary"><span>Install now</span></button></body></html>`
	w := pushTaskCall(t, s, "POST", "/api/diy-landings", input, owner, "", "correct")
	diyContentBindingCode(t, w, 201)
	return publishDIYFixture(t, s, owner, diyTestDetail(t, w))
}

func diyContentBindingField(t *testing.T, s *Server, page diyLandingDetail, kind string) string {
	t.Helper()
	var revision DIYLandingRevision
	if err := s.DB.First(&revision, "id = ?", page.LatestPublishedRevisionID).Error; err != nil {
		t.Fatal(err)
	}
	fields, err := diyContentFields(revision.RenderHTML, revision.Config)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		if field.Kind == kind {
			return field.ID
		}
	}
	t.Fatalf("fixture has no %s field", kind)
	return ""
}

func diyContentLargePNG(t *testing.T) string {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, 650, 650))
	var n uint32 = 17
	for y := 0; y < 650; y++ {
		for x := 0; x < 650; x++ {
			n ^= n << 13
			n ^= n >> 17
			n ^= n << 5
			picture.SetNRGBA(x, y, color.NRGBA{R: uint8(n), G: uint8(n >> 8), B: uint8(n >> 16), A: 255})
		}
	}
	var data bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err := encoder.Encode(&data, picture); err != nil {
		t.Fatal(err)
	}
	value := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes())
	if len(value) <= 1<<20 || data.Len() > 2<<20 {
		t.Fatal("fixture image does not exercise the request-size boundary")
	}
	return value
}

func TestDIYContentBindingIsPerApplicationAndDoesNotChangeSource(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	page := diyContentBindingFixture(t, s, owner)
	first, second := seedApp(t, s, owner, 8), seedApp(t, s, owner, 8)
	title := diyContentBindingField(t, s, page, "title")
	ref := &DIYLandingReference{PageID: page.ID, RevisionID: page.LatestPublishedRevisionID, Content: DIYLandingContent{title: "Only first", "primary-label": "Get first app"}}
	if err := setDIYBinding(s.DB, owner.ID, "apk", first.ID, ref); err != nil {
		t.Fatal(err)
	}
	other := &DIYLandingReference{PageID: page.ID, RevisionID: page.LatestPublishedRevisionID, Content: DIYLandingContent{title: "Only second", "primary-label": "Get second app"}}
	if err := setDIYBinding(s.DB, owner.ID, "apk", second.ID, other); err != nil {
		t.Fatal(err)
	}
	for _, application := range []struct {
		id       string
		expected *DIYLandingReference
	}{{first.ID, ref}, {second.ID, other}} {
		actual, err := diyBindingReference(s.DB, "apk", application.id)
		if err != nil || !diySameReference(actual, application.expected) {
			t.Fatalf("application content was not isolated: %v", err)
		}
	}
	var original DIYLandingRevision
	if err := s.DB.First(&original, "id = ?", ref.RevisionID).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(original.RenderHTML, "Only first") || strings.Contains(original.RawHTML, "Only second") || countRows(t, s, &DIYLandingPage{}) != 1 || countRows(t, s, &DIYLandingRevision{}) != 1 {
		t.Fatal("per-app edits changed or duplicated the shared source")
	}
	if countRows(t, s, &MeteredEvent{}) != 0 || countRows(t, s, &Ledger{}) != 0 || usageOwner(t, s, owner.ID).BalanceMicros != Dollar {
		t.Fatal("content editing affected accounting")
	}
}

func TestDIYContentAPKCreatePatchDesignImportAndFileReplacement(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	page := diyContentBindingFixture(t, s, owner)
	content := DIYLandingContent{
		diyContentBindingField(t, s, page, "text"):  strings.Repeat("正文", 3000),
		diyContentBindingField(t, s, page, "image"): diyContentLargePNG(t),
		"primary-label": "Install my APK",
	}
	fields := diyAPKFields(page)
	fields["diy_content"] = diyJSON(content)
	w := diyAPKUpload(t, s, owner, "/api/apps", fields, diyAPKBytes(t, "content multipart"))
	diyContentBindingCode(t, w, 201)
	app := diyAPKFromResponse(t, w)
	expected := &DIYLandingReference{PageID: page.ID, RevisionID: page.LatestPublishedRevisionID, Content: content}
	if !diySameReference(app.DIYLanding, expected) {
		t.Fatal("APK multipart did not preserve content larger than 1 MiB")
	}
	cookie, csrf := sessionFor(t, s, owner)
	if err := s.DB.Model(&Application{}).Where("id = ?", app.ID).Update("download_mode", "direct").Error; err != nil {
		t.Fatal(err)
	}
	content["primary-label"] = "Updated APK label"
	w = call(t, s, "PATCH", "/api/apps/"+app.ID, map[string]any{"name": app.Name, "description": app.Description, "version": app.Version, "enabled": true, "landing_source": "diy", "diy_landing": expected}, cookie, csrf)
	diyContentBindingCode(t, w, 200)
	if !diySameReference(diyAPKFromResponse(t, w).DIYLanding, expected) {
		t.Fatal("APK basic edit lost its large inline image or text")
	}
	if diyAPKFromResponse(t, w).DownloadMode != "direct" {
		t.Fatal("content-only edit reset the configured direct-download mode")
	}
	w = call(t, s, "GET", "/api/apps/"+app.ID+"/page-design", nil, cookie, csrf)
	diyContentBindingCode(t, w, 200)
	var design struct {
		Design PageDesign `json:"design"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &design); err != nil {
		t.Fatal(err)
	}
	if !diySameReference(design.Design.DIYLanding, expected) {
		t.Fatal("APK design did not project content from binding")
	}
	content["primary-label"] = "Designed APK label"
	design.Design.DIYLanding.Content = content
	w = call(t, s, "PUT", "/api/apps/"+app.ID+"/page-design", design.Design, cookie, csrf)
	diyContentBindingCode(t, w, 200)
	stored, err := diyBindingReference(s.DB, "apk", app.ID)
	if err != nil || !diySameReference(stored, expected) {
		t.Fatal("APK designer did not save content", err)
	}
	var afterDesign Application
	if err := s.DB.First(&afterDesign, "id = ?", app.ID).Error; err != nil || afterDesign.DownloadMode != "direct" {
		t.Fatal("designer content edit reset the download mode", err)
	}
	w = diyAPKUpload(t, s, owner, "/api/apps/"+app.ID+"/file", map[string]string{"name": app.Name, "description": app.Description, "version": "2.0"}, diyAPKBytes(t, "replacement with preserved content"))
	diyContentBindingCode(t, w, 201)
	if !diySameReference(diyAPKFromResponse(t, w).DIYLanding, expected) {
		t.Fatal("APK file replacement discarded content")
	}
	w = diyAPKUpload(t, s, owner, "/api/apps", map[string]string{"name": "Imported copy", "version": "1", "import_from": app.ID}, diyAPKBytes(t, "imported content"))
	diyContentBindingCode(t, w, 201)
	if !diySameReference(diyAPKFromResponse(t, w).DIYLanding, expected) {
		t.Fatal("APK configuration import discarded content")
	}
}

func TestDIYContentPWACreateUpdateImportAndAtomicValidation(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	page := diyContentBindingFixture(t, s, owner)
	config := diyPWAConfig(page)
	config.DIYLanding.Content = DIYLandingContent{"primary-label": "Install PWA", diyContentBindingField(t, s, page, "image"): diyContentLargePNG(t)}
	cookie, csrf := sessionFor(t, s, owner)
	w := call(t, s, "POST", "/api/pwas", map[string]any{"name": "Customized PWA", "target_url": "https://example.com/app", "landing_config": config}, cookie, csrf)
	diyContentBindingCode(t, w, 201)
	view := diyPWAView(t, w)
	if !diySameReference(view.LandingConfig.DIYLanding, config.DIYLanding) {
		t.Fatal("PWA create lost content larger than 1 MiB")
	}
	config.DIYLanding.Content["primary-label"] = "Updated PWA"
	w = call(t, s, "PUT", "/api/pwas/"+view.ID, map[string]any{"landing_config": config}, cookie, csrf)
	diyContentBindingCode(t, w, 200)
	if !diySameReference(diyPWAView(t, w).LandingConfig.DIYLanding, config.DIYLanding) {
		t.Fatal("PWA update lost content")
	}
	w = call(t, s, "PUT", "/api/pwas/"+view.ID, map[string]any{"name": "Basic PWA edit"}, cookie, csrf)
	diyContentBindingCode(t, w, 200)
	if !diySameReference(diyPWAView(t, w).LandingConfig.DIYLanding, config.DIYLanding) {
		t.Fatal("PWA basic edit discarded content")
	}
	w = call(t, s, "POST", "/api/pwas", map[string]any{"name": "Imported PWA", "target_url": "https://example.com/import", "import_from": view.ID}, cookie, csrf)
	diyContentBindingCode(t, w, 201)
	if !diySameReference(diyPWAView(t, w).LandingConfig.DIYLanding, config.DIYLanding) {
		t.Fatal("PWA import discarded content")
	}
	w = call(t, s, "GET", "/api/pwas", nil, cookie, csrf)
	diyContentBindingCode(t, w, 200)
	var listing struct {
		Data []pwaAppView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Data) != 2 || w.Body.Len() > 32<<10 {
		t.Fatal("PWA list eagerly returned inline content images")
	}
	for _, summary := range listing.Data {
		if !diySameRevision(summary.LandingConfig.DIYLanding, config.DIYLanding) || len(summary.LandingConfig.DIYLanding.Content) != 0 {
			t.Fatal("PWA summary lost source identity or included editable content")
		}
	}
	w = call(t, s, "GET", "/api/pwas/"+view.ID, nil, cookie, csrf)
	diyContentBindingCode(t, w, 200)
	if !diySameReference(diyPWAView(t, w).LandingConfig.DIYLanding, config.DIYLanding) {
		t.Fatal("lightweight list projection removed content from the detail editor")
	}
	invalid := diyPWAConfig(page)
	invalid.DIYLanding.Content = DIYLandingContent{"action": "https://elsewhere.example/other-app"}
	w = call(t, s, "PUT", "/api/pwas/"+view.ID, map[string]any{"name": "Must roll back", "landing_config": invalid}, cookie, csrf)
	diyContentBindingCode(t, w, 400)
	w = call(t, s, "GET", "/api/pwas/"+view.ID, nil, cookie, csrf)
	diyContentBindingCode(t, w, 200)
	unchanged := diyPWAView(t, w)
	if unchanged.Name != "Basic PWA edit" || !diySameReference(unchanged.LandingConfig.DIYLanding, config.DIYLanding) {
		t.Fatal("invalid content partially updated the application")
	}
	legacy := PWALandingConfig{Template: "google_play", Settings: config.Settings, StoreInfos: []PWALandingLanguage{{Language: "en", AppName: "System store name"}}}
	w = call(t, s, "PUT", "/api/pwas/"+view.ID, map[string]any{"landing_config": legacy}, cookie, csrf)
	diyContentBindingCode(t, w, 200)
	if ref, err := diyBindingReference(s.DB, "pwa", view.ID); err != nil || ref != nil {
		t.Fatal("switching template retained application content", err)
	}
}

func TestDIYContentArchivedSameBindingCanEditButCannotAddReference(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, 0), seedUser(t, s, 0)
	page := diyContentBindingFixture(t, s, owner)
	app, another := seedApp(t, s, owner, 8), seedApp(t, s, owner, 8)
	ref := &DIYLandingReference{PageID: page.ID, RevisionID: page.LatestPublishedRevisionID}
	if err := setDIYBinding(s.DB, owner.ID, "apk", app.ID, ref); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&DIYLandingPage{}).Where("id = ?", page.ID).Update("archived_at", s.Now()).Error; err != nil {
		t.Fatal(err)
	}
	ref.Content = DIYLandingContent{"primary-label": "Updated archived snapshot"}
	if err := setDIYBinding(s.DB, owner.ID, "apk", app.ID, ref); err != nil {
		t.Fatal("existing app cannot customize its archived snapshot", err)
	}
	if err := setDIYBinding(s.DB, owner.ID, "apk", another.ID, ref); err == nil {
		t.Fatal("archived page gained a new application binding")
	}
	foreign := seedApp(t, s, other, 8)
	if err := setDIYBinding(s.DB, other.ID, "apk", foreign.ID, ref); err == nil {
		t.Fatal("another owner could customize this page")
	}
}

func TestDIYContentMigrationPreservesOldBindingsAndAccounting(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	page := diyContentBindingFixture(t, s, owner)
	app := seedApp(t, s, owner, 8)
	ref := &DIYLandingReference{PageID: page.ID, RevisionID: page.LatestPublishedRevisionID}
	if err := setDIYBinding(s.DB, owner.ID, "apk", app.ID, ref); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Delete(&Migration{}, "version = ?", diyContentMigrationVersion).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Migrator().DropColumn(&DIYLandingBinding{}, "Content"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(s.DB); err != nil {
			t.Fatal(err)
		}
	}
	actual, err := diyBindingReference(s.DB, "apk", app.ID)
	if err != nil || !diySameReference(actual, ref) || len(actual.Content) != 0 {
		t.Fatal("v27 did not preserve a pre-content binding", err)
	}
	var count int64
	if err := s.DB.Model(&Migration{}).Where("version = ?", diyContentMigrationVersion).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("v27 is not idempotent", err)
	}
	if countRows(t, s, &DIYLandingBinding{}) != 1 || countRows(t, s, &DIYLandingRevision{}) != 1 || countRows(t, s, &Ledger{}) != 0 || usageOwner(t, s, owner.ID).BalanceMicros != Dollar {
		t.Fatal("migration modified source, bindings or accounting")
	}
}

func TestDIYContentReferenceEqualityTreatsNilAndEmptyAsEquivalent(t *testing.T) {
	a := &DIYLandingReference{PageID: "page", RevisionID: "rev"}
	b := &DIYLandingReference{PageID: "page", RevisionID: "rev", Content: DIYLandingContent{}}
	if !diySameReference(a, b) || !diySameReference(nil, nil) || diySameReference(a, nil) {
		t.Fatal("empty overrides changed reference identity")
	}
	b.Content["primary-label"] = "Customized"
	if diySameReference(a, b) || !diySameRevision(a, b) {
		t.Fatal("content equality does not distinguish revision identity from edits")
	}
}

func TestDIYContentAPKRejectsMalformedContentAndKeepsOtherFormLimits(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	page := diyContentBindingFixture(t, s, owner)
	file := diyAPKBytes(t, "invalid content must not save an app")
	for _, raw := range []string{`[]`, `null`, `{"primary-label":true}`, `{"action":"https://example.com/another-app"}`} {
		fields := diyAPKFields(page)
		fields["diy_content"] = raw
		w := diyAPKUpload(t, s, owner, "/api/apps", fields, file)
		diyContentBindingCode(t, w, 400)
	}
	fields := diyAPKFields(page)
	fields["description"] = strings.Repeat("x", 16385)
	w := diyAPKUpload(t, s, owner, "/api/apps", fields, file)
	diyContentBindingCode(t, w, 400)
	if !strings.Contains(w.Body.String(), "表单字段过长") || countRows(t, s, &Application{}) != 0 || countRows(t, s, &DIYLandingBinding{}) != 0 {
		t.Fatal("content-specific allowance bypassed another field's limit or left a partial app")
	}
}

func TestDIYContentDisabledModeRejectsChangesButAllowsUnchangedBinding(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	page := diyContentBindingFixture(t, s, owner)
	app := seedApp(t, s, owner, 8)
	ref := &DIYLandingReference{PageID: page.ID, RevisionID: page.LatestPublishedRevisionID, Content: DIYLandingContent{"primary-label": "Original bound label"}}
	if err := s.saveAPKLandingSource(s.DB, &app, "diy", ref); err != nil {
		t.Fatal(err)
	}
	s.Config.DisableDIYLanding = true
	unchanged := &DIYLandingReference{PageID: ref.PageID, RevisionID: ref.RevisionID, Content: DIYLandingContent{"primary-label": "Original bound label"}}
	if err := s.saveAPKLandingSource(s.DB, &app, "diy", unchanged); err != nil {
		t.Fatal("unchanged map value was rejected by disabled-mode comparison", err)
	}
	changed := &DIYLandingReference{PageID: ref.PageID, RevisionID: ref.RevisionID, Content: DIYLandingContent{"primary-label": "Disallowed edit"}}
	if err := s.saveAPKLandingSource(s.DB, &app, "diy", changed); err == nil {
		t.Fatal("disabled mode accepted new content on the same revision")
	}
	stored, err := diyBindingReference(s.DB, "apk", app.ID)
	if err != nil || !diySameReference(stored, ref) {
		t.Fatal("rejected disabled-mode edit changed stored content", err)
	}
}
