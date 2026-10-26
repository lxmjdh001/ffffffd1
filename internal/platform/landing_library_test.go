package platform

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
)

func landingLibraryAdmin(t *testing.T, s *Server) User {
	t.Helper()
	u := seedUser(t, s, Dollar)
	u.Role = "admin"
	if err := s.DB.Model(&u).Update("role", "admin").Error; err != nil {
		t.Fatal(err)
	}
	return u
}
func landingLibraryTestInput(name string) map[string]any {
	return map[string]any{"name": name, "description": "Ready to install", "html": "<h1>Library original</h1><button data-d1-action=\"primary\">Install</button>", "config": DIYLandingOptions{Targets: []string{"apk", "pwa"}, PlatformButton: true}, "cover_url": "https://assets.example/cover.png", "enabled": true}
}
func createLandingLibraryFixture(t *testing.T, s *Server, admin User, name string) LandingLibraryTemplate {
	t.Helper()
	w := pushTaskCall(t, s, "POST", "/api/admin/landing-templates", landingLibraryTestInput(name), admin, "", "correct")
	requireCode(t, w, 201)
	var row LandingLibraryTemplate
	if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	return row
}
func favoriteLandingLibraryFixture(t *testing.T, s *Server, owner User, id string) DIYLandingReference {
	t.Helper()
	w := pushTaskCall(t, s, "POST", "/api/landing-templates/"+id+"/favorite", map[string]any{}, owner, "", "correct")
	requireCode(t, w, 200)
	var out struct {
		Reference DIYLandingReference `json:"reference"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Reference.PageID == "" || out.Reference.RevisionID == "" {
		t.Fatal("favorite omitted the owned published reference")
	}
	return out.Reference
}

func TestLandingLibraryAdminCRUDAndCustomerVisibility(t *testing.T) {
	s := testServer(t)
	admin, member := landingLibraryAdmin(t, s), seedUser(t, s, 0)
	requireCode(t, call(t, s, "GET", "/api/landing-templates", nil, nil, ""), 401)
	requireCode(t, pushTaskCall(t, s, "POST", "/api/admin/landing-templates", landingLibraryTestInput("Denied"), member, "", "correct"), 403)
	requireCode(t, pushTaskCall(t, s, "POST", "/api/admin/landing-templates", landingLibraryTestInput("CSRF"), admin, "", ""), 403)
	row := createLandingLibraryFixture(t, s, admin, "  Store campaign  ")
	if row.Name != "Store campaign" || row.Version != 1 || row.HTML == "" || !row.Enabled {
		t.Fatalf("incomplete admin template: %+v", row)
	}
	path := "/api/admin/landing-templates/" + row.ID
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		requireCode(t, pushTaskCall(t, s, method, path, landingLibraryTestInput("Denied"), member, "", "correct"), 403)
	}
	w := pushTaskCall(t, s, "GET", "/api/landing-templates?q=campaign&target=apk&page=1&per_page=1", nil, member, "", "correct")
	requireCode(t, w, 200)
	if strings.Contains(w.Body.String(), "Library original") || strings.Contains(w.Body.String(), "\"html\"") || strings.Contains(w.Body.String(), admin.ID) {
		t.Fatal("catalog summary leaked raw content or administrator identity")
	}
	var list struct {
		Data  []landingLibrarySummary `json:"data"`
		Total int                     `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || len(list.Data) != 1 {
		t.Fatal("published library filtering failed")
	}
	requireCode(t, pushTaskCall(t, s, "GET", "/api/landing-templates/"+row.ID, nil, member, "", "correct"), 200)
	input := landingLibraryTestInput("Revised")
	input["enabled"] = false
	input["expected_version"] = int64(1)
	w = pushTaskCall(t, s, "PUT", path, input, admin, "", "correct")
	requireCode(t, w, 200)
	var updated LandingLibraryTemplate
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 || updated.Enabled {
		t.Fatal("admin update lost catalog state")
	}
	requireCode(t, pushTaskCall(t, s, "PUT", path, input, admin, "", "correct"), 409)
	requireCode(t, pushTaskCall(t, s, "GET", "/api/landing-templates/"+row.ID, nil, member, "", "correct"), 404)
	requireCode(t, pushTaskCall(t, s, "GET", path, nil, admin, "", "correct"), 200)
	w = pushTaskCall(t, s, "GET", "/api/landing-templates", nil, member, "", "correct")
	requireCode(t, w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 0 {
		t.Fatal("disabled library template remained public")
	}
	requireCode(t, pushTaskCall(t, s, "GET", "/api/landing-templates?target=other", nil, member, "", "correct"), 400)
	bad := landingLibraryTestInput("Invalid")
	bad["name"] = ""
	requireCode(t, pushTaskCall(t, s, "POST", "/api/admin/landing-templates", bad, admin, "", "correct"), 400)
	requireCode(t, pushTaskCall(t, s, "DELETE", path, nil, admin, "", "correct"), 200)
	requireCode(t, pushTaskCall(t, s, "GET", path, nil, admin, "", "correct"), 404)
}

func TestLandingLibraryFavoriteSnapshotsStayOwnedAndUsableAfterSourceChanges(t *testing.T) {
	s := meteredServer(t, 0)
	admin, owner, other := landingLibraryAdmin(t, s), seedUser(t, s, Dollar), seedUser(t, s, Dollar)
	source := createLandingLibraryFixture(t, s, admin, "Prepared store")
	ref := favoriteLandingLibraryFixture(t, s, owner, source.ID)
	if again := favoriteLandingLibraryFixture(t, s, owner, source.ID); !diySameReference(&again, &ref) {
		t.Fatal("repeated favorite created another page")
	}
	if countRows(t, s, &DIYLandingPage{}) != 1 || countRows(t, s, &DIYLandingRevision{}) != 1 || countRows(t, s, &LandingLibraryFavorite{}) != 1 {
		t.Fatal("favorite snapshot is not idempotent")
	}
	if _, err := loadDIYRevision(s.DB, other.ID, "apk", &ref); err == nil {
		t.Fatal("snapshot crossed tenant ownership")
	}
	app := seedApp(t, s, owner, 128)
	pwa := createPWAFixture(t, s, owner, "Library PWA")
	if err := setDIYBinding(s.DB, owner.ID, "apk", app.ID, &ref); err != nil {
		t.Fatal(err)
	}
	if err := setDIYBinding(s.DB, owner.ID, "pwa", pwa.ID, &ref); err != nil {
		t.Fatal(err)
	}
	before, err := loadDIYRevision(s.DB, owner.ID, "apk", &ref)
	if err != nil {
		t.Fatal(err)
	}
	input := landingLibraryTestInput("Changed admin template")
	input["html"] = "<button data-d1-action=\"primary\">Admin new source</button>"
	input["config"] = DIYLandingOptions{Targets: []string{"apk"}, PlatformButton: true}
	input["enabled"] = false
	requireCode(t, pushTaskCall(t, s, "PUT", "/api/admin/landing-templates/"+source.ID, input, admin, "", "correct"), 200)
	requireCode(t, pushTaskCall(t, s, "POST", "/api/landing-templates/"+source.ID+"/favorite", map[string]any{}, other, "", "correct"), 404)
	if again := favoriteLandingLibraryFixture(t, s, owner, source.ID); !diySameReference(&again, &ref) {
		t.Fatal("existing favorite broke after unpublishing source")
	}
	requireCode(t, pushTaskCall(t, s, "DELETE", "/api/admin/landing-templates/"+source.ID, nil, admin, "", "correct"), 200)
	after, err := loadDIYRevision(s.DB, owner.ID, "pwa", &ref)
	if err != nil {
		t.Fatal(err)
	}
	if after.RawHTML != before.RawHTML || after.RenderHTML != before.RenderHTML || after.ContentHash != before.ContentHash {
		t.Fatal("administrator changes mutated a customer snapshot")
	}
	w := pushTaskCall(t, s, "GET", "/api/landing-favorites?target=pwa", nil, owner, "", "correct")
	requireCode(t, w, 200)
	var list struct {
		Data  []landingFavoriteView `json:"data"`
		Total int                   `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || !diySameReference(&list.Data[0].Reference, &ref) || list.Data[0].SourceAvailable || !list.Data[0].Enabled || !diySupports(list.Data[0].Config, "pwa") {
		t.Fatal("source deletion removed or disabled the saved compatible snapshot")
	}
	w = pushTaskCall(t, s, "GET", "/api/landing-templates?favorites=1&target=pwa", nil, owner, "", "correct")
	requireCode(t, w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || !diySameReference(&list.Data[0].FavoriteReference, &ref) || list.Data[0].Version != 1 {
		t.Fatal("saved-template catalog showed the mutated or removed source instead of the favorite snapshot")
	}
	var removed LandingLibraryTemplate
	if err := s.DB.Unscoped().First(&removed, "id = ?", source.ID).Error; err != nil {
		t.Fatal(err)
	}
	if removed.HTML != "" || removed.RenderHTML != "" || removed.StorageBytes != 0 {
		t.Fatal("deleted source retained large code outside the library quota")
	}
	w = pushTaskCall(t, s, "GET", "/api/landing-favorites", nil, other, "", "correct")
	requireCode(t, w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 0 {
		t.Fatal("favorites crossed accounts")
	}
	if countRows(t, s, &MeteredEvent{}) != 0 || countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &AccessVisit{}) != 0 {
		t.Fatal("collecting catalog templates created business usage")
	}
}

func TestLandingLibraryUnfavoritePreservesBindingsAndCustomerEdits(t *testing.T) {
	s := testServer(t)
	admin, owner := landingLibraryAdmin(t, s), seedUser(t, s, Dollar)
	source := createLandingLibraryFixture(t, s, admin, "Favorite lifecycle")
	ref := favoriteLandingLibraryFixture(t, s, owner, source.ID)
	app := seedApp(t, s, owner, 128)
	if err := setDIYBinding(s.DB, owner.ID, "apk", app.ID, &ref); err != nil {
		t.Fatal(err)
	}
	path := "/api/landing-templates/" + source.ID + "/favorite"
	requireCode(t, pushTaskCall(t, s, "DELETE", path, nil, owner, "", "correct"), 200)
	requireCode(t, pushTaskCall(t, s, "DELETE", path, nil, owner, "", "correct"), 200)
	var page DIYLandingPage
	if err := s.DB.First(&page, "id = ?", ref.PageID).Error; err != nil {
		t.Fatal(err)
	}
	if page.ArchivedAt == nil {
		t.Fatal("unmodified removed favorite remained available for new bindings")
	}
	if _, err := loadDIYRevision(s.DB, owner.ID, "apk", &ref); err != nil {
		t.Fatal("unfavorite invalidated an already bound version", err)
	}
	if saved, err := diyBindingReference(s.DB, "apk", app.ID); err != nil || saved == nil || !diySameReference(saved, &ref) {
		t.Fatal("unfavorite removed an application's binding")
	}
	newRef := favoriteLandingLibraryFixture(t, s, owner, source.ID)
	if diySameReference(&newRef, &ref) {
		t.Fatal("re-favorite silently repointed the existing bound snapshot")
	}
	if err := s.DB.Model(&DIYLandingPage{}).Where("id = ?", newRef.PageID).Updates(map[string]any{"draft_version": 2, "draft_html": "<h1>Customer modifications</h1>"}).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, pushTaskCall(t, s, "DELETE", path, nil, owner, "", "correct"), 200)
	var edited DIYLandingPage
	if err := s.DB.First(&edited, "id = ?", newRef.PageID).Error; err != nil {
		t.Fatal(err)
	}
	if edited.ArchivedAt != nil || !strings.Contains(edited.DraftHTML, "Customer modifications") {
		t.Fatal("unfavorite archived or lost customer-authored changes")
	}
	if countRows(t, s, &LandingLibraryFavorite{}) != 0 {
		t.Fatal("removed favorite still listed")
	}
}

func TestLandingLibraryConcurrentFavoritesAndFailureAtomicity(t *testing.T) {
	s := testServer(t)
	admin, owner := landingLibraryAdmin(t, s), seedUser(t, s, Dollar)
	source := createLandingLibraryFixture(t, s, admin, "Concurrent catalog")
	var wg sync.WaitGroup
	refs := make(chan DIYLandingReference, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); refs <- favoriteLandingLibraryFixture(t, s, owner, source.ID) }()
	}
	wg.Wait()
	close(refs)
	var expected DIYLandingReference
	for ref := range refs {
		if expected.PageID == "" {
			expected = ref
		} else if !diySameReference(&ref, &expected) {
			t.Fatal("concurrent favorite created competing references")
		}
	}
	if countRows(t, s, &LandingLibraryFavorite{}) != 1 || countRows(t, s, &DIYLandingPage{}) != 1 || countRows(t, s, &DIYLandingRevision{}) != 1 {
		t.Fatal("concurrent favorite duplicated snapshots")
	}
	other := seedUser(t, s, 0)
	page := createDIYFixture(t, s, other, "Quota holder")
	if err := s.DB.Model(&DIYLandingPage{}).Where("id = ?", page.ID).Update("draft_bytes", maxDIYStorageBytes).Error; err != nil {
		t.Fatal(err)
	}
	pages, revisions := countRows(t, s, &DIYLandingPage{}), countRows(t, s, &DIYLandingRevision{})
	requireCode(t, pushTaskCall(t, s, "POST", "/api/landing-templates/"+source.ID+"/favorite", map[string]any{}, other, "", "correct"), 409)
	if countRows(t, s, &DIYLandingPage{}) != pages || countRows(t, s, &DIYLandingRevision{}) != revisions || countRows(t, s, &LandingLibraryFavorite{}) != 1 {
		t.Fatal("failed favorite left a partial snapshot")
	}
	s.Config.DisableDIYLanding = true
	requireCode(t, pushTaskCall(t, s, "POST", "/api/landing-templates/"+source.ID+"/favorite", map[string]any{}, other, "", "correct"), 503)
	requireCode(t, pushTaskCall(t, s, "POST", "/api/admin/landing-templates", landingLibraryTestInput("Disabled"), admin, "", "correct"), 503)
}

func TestLandingLibrarySnapshotDeletionAndMigrationPreserveBusinessState(t *testing.T) {
	s := meteredServer(t, 0)
	admin, owner := landingLibraryAdmin(t, s), seedUser(t, s, 123456)
	app := seedApp(t, s, owner, 128)
	source := createLandingLibraryFixture(t, s, admin, "Migration catalog")
	ref := favoriteLandingLibraryFixture(t, s, owner, source.ID)
	if err := setDIYBinding(s.DB, owner.ID, "apk", app.ID, &ref); err != nil {
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
		t.Fatal("library migration changed owner funds")
	}
	var migrations int64
	if err := s.DB.Model(&Migration{}).Where("version = ?", landingLibraryMigrationVersion).Count(&migrations).Error; err != nil || migrations != 1 {
		t.Fatal("library migration is not idempotent")
	}
	if _, err := loadDIYRevision(s.DB, owner.ID, "apk", &ref); err != nil {
		t.Fatal("migration damaged a saved page")
	}
	if err := setDIYBinding(s.DB, owner.ID, "apk", app.ID, nil); err != nil {
		t.Fatal(err)
	}
	requireCode(t, pushTaskCall(t, s, "DELETE", "/api/diy-landings/"+ref.PageID, nil, owner, "", "correct"), 200)
	if countRows(t, s, &LandingLibraryFavorite{}) != 0 {
		t.Fatal("deleting an unbound owned snapshot left a dangling favorite")
	}
	var loaded LandingLibraryTemplate
	if err := s.DB.First(&loaded, "id = ?", source.ID).Error; err != nil {
		t.Fatal("deleting customer's copy deleted source template")
	}
	if countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &MeteredEvent{}) != 0 {
		t.Fatal("catalog lifecycle wrote billing records")
	}
	if err := s.DB.Transaction(func(tx *gorm.DB) error { return migrateLandingLibrary(tx) }); err != nil {
		t.Fatal(err)
	}
}

func TestLandingLibraryLegacyCoverInputsAreIgnored(t *testing.T) {
	s := testServer(t)
	admin := landingLibraryAdmin(t, s)
	input := landingLibraryTestInput("Generated cover")
	input["cover_url"] = "javascript:alert(1)"
	w := pushTaskCall(t, s, "POST", "/api/admin/landing-templates", input, admin, "", "correct")
	requireCode(t, w, 201)
	var source LandingLibraryTemplate
	if err := json.Unmarshal(w.Body.Bytes(), &source); err != nil {
		t.Fatal(err)
	}
	if source.CoverURL != "" || source.CoverStatus != "pending" || source.CoverHash == "" {
		t.Fatalf("legacy manual cover was not replaced with a screenshot task: %+v", source)
	}
	var stored LandingLibraryTemplate
	if err := s.DB.First(&stored, "id = ?", source.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.CoverURL != "" || countRows(t, s, &LandingCover{}) != 1 {
		t.Fatal("a caller-supplied remote cover was retained")
	}
}
