package platform

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func pushTemplateInput() pwaPushTemplateInput {
	return pwaPushTemplateInput{Name: "Welcome", Title: "Welcome back", Body: "Your new content is ready", URL: "https://example.com/open", Icon: "https://example.com/icon.png", Image: "https://example.com/banner.png", Actions: []PWAPushAction{{Action: "open", Title: "Open now", URL: "https://example.com/open"}}}
}

func pushTemplateFromResponse(t *testing.T, w *httptest.ResponseRecorder) pwaPushTemplateView {
	t.Helper()
	var out pwaPushTemplateView
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPWAPushTemplatesOwnershipCRUDAndIdempotency(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, 0), seedUser(t, s, 0)
	path := "/api/pwa-push/templates"
	in := pushTemplateInput()
	key := "template-create-test-key"
	requireCode(t, call(t, s, "GET", path, nil, nil, ""), 401)
	requireCode(t, pushTaskCall(t, s, "POST", path, in, owner, key, ""), 403)
	requireCode(t, pushTaskCall(t, s, "POST", path, in, owner, "short", "correct"), 400)
	w := pushTaskCall(t, s, "POST", path, in, owner, key, "correct")
	requireCode(t, w, 201)
	view := pushTemplateFromResponse(t, w)
	if len(view.ID) != 32 || len(view.Actions) != 1 || view.Actions[0].Action != "open" || view.Image != in.Image || view.Icon != in.Icon {
		t.Fatalf("incomplete template: %+v", view)
	}
	for _, private := range []string{"user_id", "create_key_hash", "create_fingerprint", "actions_json", "deleted_at", owner.ID} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatalf("response leaked %s", private)
		}
	}
	in.Name = "  Welcome  "
	replay := pushTaskCall(t, s, "POST", path, in, owner, key, "correct")
	requireCode(t, replay, 200)
	if pushTemplateFromResponse(t, replay).ID != view.ID || countRows(t, s, &PWAPushTemplate{}) != 1 {
		t.Fatal("normalized replay created a duplicate template")
	}
	in.Name = "Different"
	requireCode(t, pushTaskCall(t, s, "POST", path, in, owner, key, "correct"), 409)
	requireCode(t, pushTaskCall(t, s, "POST", path, in, other, key, "correct"), 201)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		requireCode(t, pushTaskCall(t, s, method, path+"/"+view.ID, in, other, "", "correct"), 404)
	}
	app := pushTestApp(t, s, owner)
	taskInput := pwaPushTaskInput{PWAID: app.ID, TemplateID: view.ID, ScheduleType: "now"}
	taskKey := "immutable-template-task-key"
	w = pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", taskInput, owner, taskKey, "correct")
	requireCode(t, w, 201)
	var task pwaPushTaskView
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if task.Title != view.Title || task.Body != view.Body || task.Image != view.Image || task.Icon != view.Icon || len(task.Actions) != 1 || task.TemplateID == nil || *task.TemplateID != view.ID {
		t.Fatalf("task did not snapshot template content: %+v", task)
	}
	in.Name, in.Title, in.URL, in.Image, in.Actions = "Updated", "A different title", "", "", []PWAPushAction{}
	w = pushTaskCall(t, s, "PUT", path+"/"+view.ID, in, owner, "", "correct")
	requireCode(t, w, 200)
	updated := pushTemplateFromResponse(t, w)
	if updated.Title != in.Title || updated.URL != "" || updated.Image != "" || len(updated.Actions) != 0 {
		t.Fatalf("template fields were not replaced: %+v", updated)
	}
	w = pushTaskCall(t, s, "GET", path+"/"+view.ID, nil, owner, "", "correct")
	requireCode(t, w, 200)
	if pushTemplateFromResponse(t, w).Name != "Updated" {
		t.Fatal("updated template was not persisted")
	}
	requireCode(t, pushTaskCall(t, s, "DELETE", path+"/"+view.ID, nil, owner, "", "correct"), 200)
	requireCode(t, pushTaskCall(t, s, "GET", path+"/"+view.ID, nil, owner, "", "correct"), 404)
	original := pushTemplateInput()
	requireCode(t, pushTaskCall(t, s, "POST", path, original, owner, key, "correct"), 409)
	var retained PWAPushTemplate
	if err := s.DB.Unscoped().First(&retained, "id = ?", view.ID).Error; err != nil || !retained.DeletedAt.Valid {
		t.Fatal("deleted template lost its idempotency tombstone", err)
	}
	var unchanged PWAPushTask
	if err := s.DB.First(&unchanged, "id = ?", task.ID).Error; err != nil || unchanged.Title != task.Title || unchanged.Body != task.Body || unchanged.URL != task.URL || unchanged.Image != task.Image || unchanged.Icon != task.Icon || len(pwaTaskActions(unchanged)) != 1 {
		t.Fatal("template update/delete mutated an existing task snapshot", err)
	}
	requireCode(t, pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", taskInput, owner, taskKey, "correct"), 200)
	if countRows(t, s, &PWAPushTask{}) != 1 {
		t.Fatal("task retry after source template deletion duplicated delivery task")
	}
}

func TestPWAPushTemplateValidationSearchAndQuota(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	path := "/api/pwa-push/templates"
	invalid := []func(*pwaPushTemplateInput){
		func(in *pwaPushTemplateInput) { in.Name = "" },
		func(in *pwaPushTemplateInput) { in.Name = strings.Repeat("字", 101) },
		func(in *pwaPushTemplateInput) { in.Title = strings.Repeat("x", 101) },
		func(in *pwaPushTemplateInput) { in.URL = "javascript:alert(1)" },
		func(in *pwaPushTemplateInput) { in.Image = "http://example.com/banner.png" },
		func(in *pwaPushTemplateInput) { in.Icon = "https://user:pass@example.com/logo.png" },
		func(in *pwaPushTemplateInput) { in.Actions[0].URL = "data:text/html,test" },
		func(in *pwaPushTemplateInput) { in.Actions[0].Action = "invalid action" },
		func(in *pwaPushTemplateInput) { in.Actions[0].Title = strings.Repeat("x", 41) },
		func(in *pwaPushTemplateInput) { in.Actions = append(in.Actions, in.Actions[0], in.Actions[0]) },
	}
	for _, change := range invalid {
		in := pushTemplateInput()
		change(&in)
		requireCode(t, pushTaskCall(t, s, "POST", path, in, u, randomHex(16), "correct"), 400)
	}
	for _, name := range []string{"Summer 100%", "Summer 100x", "Winter"} {
		in := pushTemplateInput()
		in.Name = name
		requireCode(t, pushTaskCall(t, s, "POST", path, in, u, randomHex(16), "correct"), 201)
	}
	w := pushTaskCall(t, s, "GET", path+"?q=summer&page=2&per_page=1", nil, u, "", "correct")
	requireCode(t, w, 200)
	var response struct {
		Data    []pwaPushTemplateView `json:"data"`
		Total   int64                 `json:"total"`
		Page    int                   `json:"page"`
		PerPage int                   `json:"per_page"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Total != 2 || response.Page != 2 || response.PerPage != 1 || len(response.Data) != 1 {
		t.Fatalf("search pagination incorrect: %s", w.Body.String())
	}
	w = pushTaskCall(t, s, "GET", path+"?q=100%25", nil, u, "", "correct")
	requireCode(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &response)
	if response.Total != 1 || response.Data[0].Name != "Summer 100%" {
		t.Fatal("literal SQL wildcard was not escaped")
	}
	// Name, title and body share one parenthesized search condition. A match in
	// another owner's body must never bypass the user_id predicate.
	other := seedUser(t, s, 0)
	for _, owner := range []User{u, other} {
		in := pushTemplateInput()
		in.Name, in.Title, in.Body = "Fresh content", "Headline-only-needle", "Body-only-needle"
		requireCode(t, pushTaskCall(t, s, "POST", path, in, owner, randomHex(16), "correct"), 201)
	}
	for _, search := range []string{"HEADLINE-only-needle", "body-only-needle"} {
		w = pushTaskCall(t, s, "GET", path+"?q="+search, nil, u, "", "correct")
		requireCode(t, w, 200)
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Total != 1 || len(response.Data) != 1 || response.Data[0].Name != "Fresh content" {
			t.Fatalf("title/body search crossed tenant boundary: %s", w.Body.String())
		}
	}
	var count int64
	s.DB.Model(&PWAPushTemplate{}).Where("user_id = ?", u.ID).Count(&count)
	bulk := make([]PWAPushTemplate, 0, 200-count)
	for i := count; i < 200; i++ {
		row := PWAPushTemplate{ID: randomHex(16), UserID: u.ID, CreateKeyHash: randomHex(32), CreateFingerprint: randomHex(32), CreatedAt: s.Now()}
		applyPWAPushTemplate(&row, pushTemplateInput())
		bulk = append(bulk, row)
	}
	if err := s.DB.Create(&bulk).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, pushTaskCall(t, s, "POST", path, pushTemplateInput(), u, randomHex(16), "correct"), 409)
}

func pushAssetCall(t *testing.T, s *Server, u User, raw []byte, kind string, duplicate bool, csrfOverride string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("kind", kind)
	part, _ := writer.CreateFormFile("file", "source.bin")
	part.Write(raw)
	if duplicate {
		part, _ = writer.CreateFormFile("file", "duplicate.bin")
		part.Write(raw)
	}
	writer.Close()
	cookie, csrf := sessionFor(t, s, u)
	if csrfOverride != "correct" {
		csrf = csrfOverride
	}
	r := httptest.NewRequest("POST", s.Config.PublicURL+"/api/pwa-push/assets", &body)
	r.AddCookie(cookie)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func pushAssetImage(t *testing.T, width, height int, jpegFormat bool) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			im.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: 120, B: uint8(y % 256), A: 255})
		}
	}
	var encoded bytes.Buffer
	if jpegFormat {
		if err := jpeg.Encode(&encoded, im, &jpeg.Options{Quality: 80}); err != nil {
			t.Fatal(err)
		}
	} else if err := png.Encode(&encoded, im); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestPWAPushAssetUploadNormalizationAndTenantDedup(t *testing.T) {
	s := testServer(t)
	u, other := seedUser(t, s, 0), seedUser(t, s, 0)
	raw := pushAssetImage(t, 2000, 1000, false)
	w := pushAssetCall(t, s, u, raw, "image", false, "correct")
	requireCode(t, w, 200)
	var asset struct {
		URL           string `json:"url"`
		Width, Height int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &asset); err != nil || asset.Width != 1600 || asset.Height != 800 {
		t.Fatalf("large image aspect ratio not preserved: %s", w.Body.String())
	}
	for _, private := range []string{"user_id", "sha256", "png", u.ID} {
		if private != "png" && strings.Contains(w.Body.String(), private) {
			t.Fatal("asset response leaked", private)
		}
	}
	replayed := pushAssetCall(t, s, u, raw, "image", false, "correct")
	requireCode(t, replayed, 200)
	var replay struct {
		URL string `json:"url"`
	}
	json.Unmarshal(replayed.Body.Bytes(), &replay)
	if replay.URL != asset.URL || countRows(t, s, &PWAPushAsset{}) != 1 {
		t.Fatal("same owner/content upload was not deduplicated")
	}
	w = pushAssetCall(t, s, other, raw, "image", false, "correct")
	requireCode(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &replay)
	if replay.URL == asset.URL || countRows(t, s, &PWAPushAsset{}) != 2 {
		t.Fatal("asset hash crossed tenant boundary")
	}
	path := strings.TrimPrefix(asset.URL, s.Config.PublicURL)
	w = pwaPublicCall(t, s, path, "", "")
	requireCode(t, w, 200)
	if w.Header().Get("Content-Type") != "image/png" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("public image is not a safe PNG")
	}
	im, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil || im.Bounds().Dx() != 1600 || im.Bounds().Dy() != 800 {
		t.Fatal("public asset bytes malformed", err)
	}
	requireCode(t, pwaPublicCall(t, s, path, "other.example", ""), 404)
	requireCode(t, pwaPublicCall(t, s, strings.TrimSuffix(path, ".png")+".svg", "", ""), 404)
	if err = s.DB.Model(&User{}).Where("id = ?", u.ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, pwaPublicCall(t, s, path, "", ""), 404)
	icon := pushAssetCall(t, s, other, pushAssetImage(t, 320, 160, true), "icon", false, "correct")
	requireCode(t, icon, 200)
	json.Unmarshal(icon.Body.Bytes(), &asset)
	if asset.Width != 512 || asset.Height != 512 {
		t.Fatal("icon was not normalized to 512 square")
	}
	// A valid one-pixel WebP exercises the optional decoder registration.
	webp, _ := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if _, width, height, err := normalizePWAPushAsset(webp, "image"); err != nil || width != 1 || height != 1 {
		t.Fatal("WebP format not accepted", err)
	}
}

func TestPWAPushAssetRejectsInvalidUploadsAndQuota(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	raw := pushAssetImage(t, 20, 10, false)
	requireCode(t, pushAssetCall(t, s, u, raw, "image", false, ""), 403)
	for _, tc := range []struct {
		raw  []byte
		kind string
	}{
		{nil, "image"}, {[]byte("not an image"), "image"}, {[]byte("GIF89a\x01\x00\x01\x00"), "image"}, {raw, "unsupported"}, {pushAssetImage(t, 4097, 1, false), "image"},
	} {
		requireCode(t, pushAssetCall(t, s, u, tc.raw, tc.kind, false, "correct"), 400)
	}
	requireCode(t, pushAssetCall(t, s, u, raw, "image", true, "correct"), 400)
	requireCode(t, pushAssetCall(t, s, u, bytes.Repeat([]byte("x"), pwaMaxPushAssetBytes+1), "image", false, "correct"), 413)
	bulk := make([]PWAPushAsset, 0, 500)
	for range 500 {
		bulk = append(bulk, PWAPushAsset{ID: randomHex(16), UserID: u.ID, SHA256: randomHex(32), PNG: raw, Width: 20, Height: 10, CreatedAt: s.Now()})
	}
	if err := s.DB.CreateInBatches(&bulk, 100).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, pushAssetCall(t, s, u, raw, "image", false, "correct"), 409)
}

func TestPWAPushTemplateAuditFailureRollsBack(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	if err := s.DB.Callback().Create().Before("gorm:create").Register("test:pwa-template-audit-fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "audits" {
			tx.AddError(fmt.Errorf("forced audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Callback().Create().Remove("test:pwa-template-audit-fail") })
	w := pushTaskCall(t, s, "POST", "/api/pwa-push/templates", pushTemplateInput(), u, "rollback-template-create", "correct")
	requireCode(t, w, 500)
	if countRows(t, s, &PWAPushTemplate{}) != 0 {
		t.Fatal("failed audit persisted a template")
	}
	w = pushAssetCall(t, s, u, pushAssetImage(t, 12, 12, false), "image", false, "correct")
	requireCode(t, w, 500)
	if countRows(t, s, &PWAPushAsset{}) != 0 {
		t.Fatal("failed audit persisted an image")
	}
}

// This is the pre-rich-content schema, including its original column/index
// definitions, so the migration test catches accidental changes to old fields.
type legacyPWAPushTask struct {
	ID                 string     `gorm:"primaryKey;size:32"`
	UserID             string     `gorm:"uniqueIndex:idx_pwa_push_request,priority:1;index;size:32;not null"`
	RequestKeyHash     string     `gorm:"uniqueIndex:idx_pwa_push_request,priority:2;size:64;not null"`
	RequestFingerprint string     `gorm:"size:64;not null"`
	PWAID              string     `gorm:"index;size:32;not null"`
	PWAName            string     `gorm:"size:100"`
	Title              string     `gorm:"size:400;not null"`
	Body               string     `gorm:"size:2000;not null"`
	URL                string     `gorm:"size:2048;not null"`
	Icon               string     `gorm:"size:2048"`
	ScheduleType       string     `gorm:"size:20;not null"`
	ScheduledAt        *time.Time `gorm:"index:idx_pwa_push_due,priority:2"`
	DelayMinutes       int
	CountriesJSON      string `gorm:"not null"`
	Status             string `gorm:"index:idx_pwa_push_due,priority:1;size:20;not null"`
	AudienceAt         *time.Time
	AudienceCursor     string `gorm:"size:32"`
	Expanded           bool
	CreatedAt          time.Time
	CompletedAt        *time.Time
}

func (legacyPWAPushTask) TableName() string { return "pwa_push_tasks" }

func TestPWAPushRichMigrationPreservesLegacyTasksAndLedger(t *testing.T) {
	s := testServer(t)
	// Replace only new PWA tables with the exact prior task schema. This also
	// exercises PostgreSQL when TEST_DATABASE_URL is present.
	for _, model := range []any{&PWAPushDelivery{}, &PWAPushTask{}, &PWAPushTemplate{}, &PWAPushAsset{}} {
		if err := s.DB.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DB.AutoMigrate(&legacyPWAPushTask{}); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Where("version = ?", 14).Delete(&Migration{}).Error; err != nil {
		t.Fatal(err)
	}
	now := s.Now().UTC().Truncate(time.Second)
	old := legacyPWAPushTask{ID: randomHex(16), UserID: randomHex(16), RequestKeyHash: randomHex(32), RequestFingerprint: randomHex(32), PWAID: randomHex(16), PWAName: "Old app", Title: "Original title", Body: "Original body", URL: "https://example.com/original", Icon: "https://example.com/icon.png", ScheduleType: "first_subscribe", DelayMinutes: 2880, CountriesJSON: `["US"]`, Status: "active", Expanded: true, CreatedAt: now}
	ledger := Ledger{ID: randomHex(16), UserID: old.UserID, Reference: "migration14-balance", Kind: "recharge", AmountMicros: 100 * Dollar, BalanceMicros: 108 * Dollar, Note: "Preserve this balance", CreatedAt: now}
	if err := s.DB.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Transaction(migratePWA); err != nil {
		t.Fatal(err)
	}
	var migrated PWAPushTask
	if err := s.DB.First(&migrated, "id = ?", old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migrated.TemplateID != nil || migrated.Image != "" || migrated.ActionsJSON != "[]" || migrated.Title != old.Title || migrated.Body != old.Body || migrated.URL != old.URL || migrated.Icon != old.Icon || migrated.DelayMinutes != old.DelayMinutes || migrated.Status != old.Status || migrated.CountriesJSON != old.CountriesJSON || !migrated.CreatedAt.Equal(now) {
		t.Fatalf("migration rewrote existing task: %+v", migrated)
	}
	var unchanged Ledger
	if err := s.DB.First(&unchanged, "id = ?", ledger.ID).Error; err != nil || unchanged.AmountMicros != ledger.AmountMicros || unchanged.BalanceMicros != ledger.BalanceMicros || unchanged.Reference != ledger.Reference || unchanged.Note != ledger.Note {
		t.Fatal("migration touched ledger history", err)
	}
	if !s.DB.Migrator().HasTable(&PWAPushTemplate{}) || !s.DB.Migrator().HasTable(&PWAPushAsset{}) {
		t.Fatal("new rich notification tables are missing")
	}
	if err := s.DB.Transaction(migratePWA); err != nil {
		t.Fatal("migration is not idempotent", err)
	}
	var versions int64
	s.DB.Model(&Migration{}).Where("version IN ?", []int{13, 14}).Count(&versions)
	if versions != 2 {
		t.Fatal("migration versions 13 and 14 were not both retained")
	}
}
