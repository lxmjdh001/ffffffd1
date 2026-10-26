package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func diyTestInput(name string) map[string]any {
	return map[string]any{"name": name, "html": "<!doctype html><html><head><title>Private page</title></head><body><h1>Original source</h1><button data-d1-action=\"primary\">Install</button></body></html>", "config": DIYLandingOptions{Targets: []string{"apk", "pwa"}, PlatformButton: true}}
}

func diyTestDetail(t *testing.T, w *httptest.ResponseRecorder) diyLandingDetail {
	t.Helper()
	var out diyLandingDetail
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func createDIYFixture(t *testing.T, s *Server, owner User, name string) diyLandingDetail {
	t.Helper()
	w := pushTaskCall(t, s, "POST", "/api/diy-landings", diyTestInput(name), owner, "", "correct")
	requireCode(t, w, 201)
	return diyTestDetail(t, w)
}

func publishDIYFixture(t *testing.T, s *Server, owner User, page diyLandingDetail) diyLandingDetail {
	t.Helper()
	w := pushTaskCall(t, s, "POST", "/api/diy-landings/"+page.ID+"/publish", map[string]any{"draft_version": page.DraftVersion}, owner, "", "correct")
	requireCode(t, w, 201)
	return diyTestDetail(t, w)
}

func TestDIYLandingCRUDOwnershipAndSummary(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, 0), seedUser(t, s, 0)
	requireCode(t, call(t, s, "GET", "/api/diy-landings", nil, nil, ""), 401)
	requireCode(t, pushTaskCall(t, s, "POST", "/api/diy-landings", diyTestInput("Denied"), owner, "", ""), 403)
	page := createDIYFixture(t, s, owner, "  Summer launch  ")
	if page.Name != "Summer launch" || page.DraftVersion != 1 || page.DraftHTML == "" || len(page.Revisions) != 0 {
		t.Fatalf("incomplete page: %+v", page)
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		in := diyTestInput("Other")
		in["draft_version"] = page.DraftVersion
		requireCode(t, pushTaskCall(t, s, method, "/api/diy-landings/"+page.ID, in, other, "", "correct"), 404)
	}
	w := pushTaskCall(t, s, "GET", "/api/diy-landings?q=summer&page=1&per_page=1", nil, owner, "", "correct")
	requireCode(t, w, 200)
	if strings.Contains(w.Body.String(), "draft_html") || strings.Contains(w.Body.String(), "Original source") || strings.Contains(w.Body.String(), owner.ID) {
		t.Fatalf("summary leaked private source: %s", w.Body.String())
	}
	var list struct {
		Data  []diyLandingSummary `json:"data"`
		Total int                 `json:"total"`
		Page  int                 `json:"page"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || len(list.Data) != 1 || list.Page != 1 {
		t.Fatalf("unexpected list %+v", list)
	}
	createDIYFixture(t, s, owner, "Second")
	w = pushTaskCall(t, s, "GET", "/api/diy-landings?page=2&per_page=1", nil, owner, "", "correct")
	requireCode(t, w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Page != 2 || len(list.Data) != 1 || list.Total != 2 {
		t.Fatalf("pagination failed %+v", list)
	}
	in := diyTestInput("Updated")
	in["draft_version"] = page.DraftVersion
	w = pushTaskCall(t, s, "PUT", "/api/diy-landings/"+page.ID, in, owner, "", "correct")
	requireCode(t, w, 200)
	updated := diyTestDetail(t, w)
	if updated.DraftVersion != 2 || updated.Name != "Updated" {
		t.Fatal("draft not saved")
	}
	requireCode(t, pushTaskCall(t, s, "PUT", "/api/diy-landings/"+page.ID, in, owner, "", "correct"), 409)
	requireCode(t, pushTaskCall(t, s, "DELETE", "/api/diy-landings/"+page.ID, nil, owner, "", "correct"), 200)
	requireCode(t, pushTaskCall(t, s, "GET", "/api/diy-landings/"+page.ID, nil, owner, "", "correct"), 404)
}

func TestDIYLandingPublishImmutableAndIdempotent(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	page := createDIYFixture(t, s, owner, "Page")
	path := "/api/diy-landings/" + page.ID
	key := "publish-test-key-immutable"
	w := pushTaskCall(t, s, "POST", path+"/publish", map[string]any{"draft_version": page.DraftVersion}, owner, key, "correct")
	requireCode(t, w, 201)
	first := diyTestDetail(t, w)
	if len(first.Revisions) != 1 || first.LatestPublishedRevisionID == "" {
		t.Fatal("publication missing")
	}
	if first.Revisions[0].RawHTML != "" || first.Revisions[0].RenderHTML != "" {
		t.Fatal("detail should contain version summaries")
	}
	requireCode(t, pushTaskCall(t, s, "POST", path+"/publish", map[string]any{"draft_version": page.DraftVersion}, owner, key, "correct"), 200)
	requireCode(t, pushTaskCall(t, s, "POST", path+"/publish", map[string]any{"draft_version": page.DraftVersion}, owner, "different-publication-key", "correct"), 200)
	if countRows(t, s, &DIYLandingRevision{}) != 1 {
		t.Fatal("retry created another revision")
	}
	w = pushTaskCall(t, s, "GET", path+"/revisions/"+first.LatestPublishedRevisionID, nil, owner, "", "correct")
	requireCode(t, w, 200)
	var original DIYLandingRevision
	if err := json.Unmarshal(w.Body.Bytes(), &original); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(original.RawHTML, "Original source") || original.RenderHTML == "" || original.RuntimeContractVersion != 1 {
		t.Fatal("revision did not snapshot processing contract")
	}
	in := diyTestInput("New draft")
	in["draft_version"] = first.DraftVersion
	in["html"] = "<button data-d1-action=\"primary\">New source</button>"
	w = pushTaskCall(t, s, "PUT", path, in, owner, "", "correct")
	requireCode(t, w, 200)
	second := diyTestDetail(t, w)
	if second.LatestPublishedRevisionID != original.ID {
		t.Fatal("draft change modified publication")
	}
	requireCode(t, pushTaskCall(t, s, "POST", path+"/publish", map[string]any{"draft_version": second.DraftVersion}, owner, key, "correct"), 409)
	second = publishDIYFixture(t, s, owner, second)
	var retained DIYLandingRevision
	if err := s.DB.First(&retained, "id = ?", original.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retained.RawHTML != original.RawHTML || retained.ContentHash != original.ContentHash || retained.RenderHTML != original.RenderHTML {
		t.Fatal("published source mutated")
	}
	if second.Revisions[0].RevisionNo != 2 {
		t.Fatal("revision sequence incorrect")
	}
}

func TestDIYBindingVersionsArchiveAndDeletionProtection(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, 0), seedUser(t, s, 0)
	page := publishDIYFixture(t, s, owner, createDIYFixture(t, s, owner, "Shared"))
	app := seedApp(t, s, owner, 10)
	secondApp := seedApp(t, s, owner, 10)
	foreign := seedApp(t, s, other, 10)
	pwa := pushTestApp(t, s, owner)
	ref := &DIYLandingReference{PageID: page.ID, RevisionID: page.LatestPublishedRevisionID}
	if err := s.DB.Transaction(func(tx *gorm.DB) error { return setDIYBinding(tx, owner.ID, "apk", app.ID, ref) }); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Transaction(func(tx *gorm.DB) error { return setDIYBinding(tx, owner.ID, "pwa", pwa.ID, ref) }); err != nil {
		t.Fatal(err)
	}
	if err := setDIYBinding(s.DB, owner.ID, "apk", foreign.ID, ref); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign resource accepted: %v", err)
	}
	if _, err := loadDIYRevision(s.DB, other.ID, "apk", ref); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign revision accepted: %v", err)
	}
	path := "/api/diy-landings/" + page.ID
	requireCode(t, pushTaskCall(t, s, "DELETE", path, nil, owner, "", "correct"), 409)
	w := pushTaskCall(t, s, "GET", path+"/bindings", nil, owner, "", "correct")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), app.Name) || !strings.Contains(w.Body.String(), pwa.Name) {
		t.Fatal("binding names missing")
	}
	requireCode(t, pushTaskCall(t, s, "PATCH", path+"/archive", map[string]bool{"archived": true}, owner, "", "correct"), 200)
	if err := setDIYBinding(s.DB, owner.ID, "apk", app.ID, ref); err != nil {
		t.Fatal("archiving invalidated existing binding", err)
	}
	if err := setDIYBinding(s.DB, owner.ID, "apk", secondApp.ID, ref); err == nil {
		t.Fatal("archived page accepted new binding")
	}
	if _, err := loadDIYRevision(s.DB, owner.ID, "apk", ref); err != nil {
		t.Fatal("archived bound revision cannot render", err)
	}
	w = pushTaskCall(t, s, "GET", "/api/diy-landings?published=1&target=apk", nil, owner, "", "correct")
	requireCode(t, w, 200)
	if strings.Contains(w.Body.String(), page.ID) {
		t.Fatal("archived page offered to new bindings")
	}
	requireCode(t, pushTaskCall(t, s, "PATCH", path+"/archive", map[string]bool{"archived": false}, owner, "", "correct"), 200)
	in := diyTestInput("Shared v2")
	in["draft_version"] = page.DraftVersion
	w = pushTaskCall(t, s, "PUT", path, in, owner, "", "correct")
	requireCode(t, w, 200)
	page = publishDIYFixture(t, s, owner, diyTestDetail(t, w))
	got, err := diyBindingReference(s.DB, "apk", app.ID)
	if err != nil || got == nil || !diySameReference(got, ref) {
		t.Fatal("publication silently upgraded app", err)
	}
	requireCode(t, pushTaskCall(t, s, "DELETE", path+"/revisions/"+ref.RevisionID, nil, owner, "", "correct"), 409)
	if err := setDIYBinding(s.DB, owner.ID, "apk", app.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := setDIYBinding(s.DB, owner.ID, "pwa", pwa.ID, nil); err != nil {
		t.Fatal(err)
	}
	requireCode(t, pushTaskCall(t, s, "DELETE", path+"/revisions/"+ref.RevisionID, nil, owner, "", "correct"), 200)
	requireCode(t, pushTaskCall(t, s, "DELETE", path+"/revisions/"+page.LatestPublishedRevisionID, nil, owner, "", "correct"), 409)
	requireCode(t, pushTaskCall(t, s, "DELETE", path, nil, owner, "", "correct"), 200)
}

func TestDIYLandingCopyTargetsAndQuota(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	in := diyTestInput("APK only")
	in["config"] = DIYLandingOptions{Targets: []string{"apk"}, PlatformButton: true}
	w := pushTaskCall(t, s, "POST", "/api/diy-landings", in, owner, "", "correct")
	requireCode(t, w, 201)
	page := publishDIYFixture(t, s, owner, diyTestDetail(t, w))
	pwa := pushTestApp(t, s, owner)
	if err := setDIYBinding(s.DB, owner.ID, "pwa", pwa.ID, &DIYLandingReference{PageID: page.ID, RevisionID: page.LatestPublishedRevisionID}); err == nil {
		t.Fatal("APK page accepted for PWA")
	}
	w = pushTaskCall(t, s, "POST", "/api/diy-landings/"+page.ID+"/copy", nil, owner, "", "correct")
	requireCode(t, w, 201)
	copy := diyTestDetail(t, w)
	if copy.ID == page.ID || copy.LatestPublishedRevisionID != "" || copy.DraftVersion != 1 || copy.DraftHTML != page.DraftHTML || len(copy.Revisions) != 0 {
		t.Fatal("copy retained publication state")
	}
	if err := s.DB.Model(&DIYLandingPage{}).Where("id = ?", copy.ID).Update("draft_bytes", maxDIYStorageBytes).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, pushTaskCall(t, s, "POST", "/api/diy-landings", diyTestInput("Over quota"), owner, "", "correct"), 409)
	tooBig := diyTestInput("Large")
	tooBig["html"] = strings.Repeat("A", maxDIYHTMLBytes+1)
	requireCode(t, pushTaskCall(t, s, "POST", "/api/diy-landings", tooBig, owner, "", "correct"), 400)
}

func TestDIYLandingMigrationPreservesBusinessRecords(t *testing.T) {
	s := meteredServer(t, 0)
	owner := seedUser(t, s, 123456)
	app := seedApp(t, s, owner, 10)
	page := publishDIYFixture(t, s, owner, createDIYFixture(t, s, owner, "Retained"))
	ref := &DIYLandingReference{PageID: page.ID, RevisionID: page.LatestPublishedRevisionID}
	if err := setDIYBinding(s.DB, owner.ID, "apk", app.ID, ref); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(usageOwner(t, s, owner.ID))
	for range 2 {
		if err := Migrate(s.DB); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := json.Marshal(usageOwner(t, s, owner.ID))
	if string(before) != string(after) {
		t.Fatal("migration rewrote user or funds")
	}
	var n int64
	if err := s.DB.Model(&Migration{}).Where("version = ?", diyLandingMigrationVersion).Count(&n).Error; err != nil || n != 1 {
		t.Fatal("migration not recorded once", err)
	}
	got, err := diyBindingReference(s.DB, "apk", app.ID)
	if err != nil || got == nil || !diySameReference(got, ref) {
		t.Fatal("repeat migration changed binding", err)
	}
	for _, model := range []any{&Ledger{}, &MeteredEvent{}, &Ticket{}, &Download{}} {
		if countRows(t, s, model) != 0 {
			t.Fatalf("DIY created business events in %T", model)
		}
	}
}

func TestDIYLandingPageCountQuotaAndInvalidDraft(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	rows := make([]DIYLandingPage, 0, maxDIYPageCount)
	for i := 0; i < maxDIYPageCount; i++ {
		rows = append(rows, DIYLandingPage{ID: randomHex(16), UserID: owner.ID, Name: fmt.Sprintf("Page %d", i), DraftVersion: 1, DraftConfig: DIYLandingOptions{Targets: []string{"apk", "pwa"}, PlatformButton: true}})
	}
	if err := s.DB.CreateInBatches(&rows, 20).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, pushTaskCall(t, s, "POST", "/api/diy-landings", diyTestInput("Excess"), owner, "", "correct"), 409)
	other := seedUser(t, s, 0)
	in := diyTestInput("Incomplete")
	in["html"] = ""
	w := pushTaskCall(t, s, "POST", "/api/diy-landings", in, other, "", "correct")
	requireCode(t, w, 201)
	page := diyTestDetail(t, w)
	requireCode(t, pushTaskCall(t, s, "POST", "/api/diy-landings/"+page.ID+"/publish", map[string]int64{"draft_version": page.DraftVersion}, other, "", "correct"), 400)
	in["name"] = "bad\nname"
	requireCode(t, pushTaskCall(t, s, "POST", "/api/diy-landings", in, other, "", "correct"), 400)
}
