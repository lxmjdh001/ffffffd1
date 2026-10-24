package platform

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func importUpload(t *testing.T, s *Server, cookie *http.Cookie, csrf, source string) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	form := multipart.NewWriter(&b)
	_ = form.WriteField("name", "Imported app")
	_ = form.WriteField("version", "2.0")
	_ = form.WriteField("import_from", source)
	f, _ := form.CreateFormFile("file", "new.exe")
	_, _ = f.Write([]byte("MZnew independently uploaded package"))
	_ = form.Close()
	r := httptest.NewRequest("POST", s.Config.PublicURL+"/api/apps", &b)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestImportAPKConfigurationAndIndependentPictures(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	s.DB.Model(&Plan{}).Where("id = ?", "free").Update("app_limit", 10)
	source := seedApp(t, s, owner, 10)
	s.DB.Model(&source).Update("download_mode", "info")
	fixture := attachPageImageFixture(t, s)
	cookie, csrf := sessionFor(t, s, owner)
	url := uploadedPageImage(t, s, source, cookie, csrf)
	design := defaultPageDesign(source)
	design.Icon = url
	design.Screenshots = []string{url}
	local := design
	local.Language = "en-US"
	local.Title = "English title"
	design.Localizations = []PageDesign{local}
	design.LanguagePriority = []string{"en-US"}
	requireCode(t, call(t, s, "PUT", "/api/apps/"+source.ID+"/page-design", design, cookie, csrf), 200)
	w := importUpload(t, s, cookie, csrf, source.ID)
	requireCode(t, w, 201)
	var target Application
	if err := json.Unmarshal(w.Body.Bytes(), &target); err != nil {
		t.Fatal(err)
	}
	if target.ID == source.ID || target.Slug == source.Slug || target.FileKey == source.FileKey || target.DownloadMode != "info" || target.Downloads != 0 {
		t.Fatalf("not an independent app: %+v", target)
	}
	copied, err := s.pageDesign(target)
	if err != nil {
		t.Fatal(err)
	}
	if copied.Icon == url || copied.Icon != copied.Screenshots[0] || copied.Icon != copied.Localizations[0].Icon || copied.Localizations[0].Title != "English title" {
		t.Fatalf("configuration or pictures not cloned: %+v", copied)
	}
	fixture.mu.Lock()
	n := len(fixture.objects)
	fixture.mu.Unlock()
	if n != 2 {
		t.Fatalf("shared references should be cloned only once, got %d objects", n)
	}
	requireCode(t, call(t, s, "DELETE", "/api/apps/"+source.ID, nil, cookie, csrf), 200)
	requireCode(t, call(t, s, "PUT", "/api/apps/"+target.ID+"/page-design", copied, cookie, csrf), 200)
	fixture.mu.Lock()
	n = len(fixture.objects)
	fixture.mu.Unlock()
	if n != 1 {
		t.Fatal("deleting source removed imported pictures")
	}
}

func TestImportAPKRequiresOwnershipAndRollsBack(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	other := seedUser(t, s, 0)
	source := seedApp(t, s, other, 10)
	cookie, csrf := sessionFor(t, s, owner)
	w := importUpload(t, s, cookie, csrf, source.ID)
	requireCode(t, w, 404)
	if countRows(t, s, &Application{}) != 1 {
		t.Fatal("unauthorized import created an app")
	}
	files, err := os.ReadDir(filepath.Join(s.Config.DataDir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatal("failed import leaked a package")
	}
}

func TestImportAPKPictureFailureRollsBack(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	source := seedApp(t, s, owner, 10)
	s.DB.Model(&Plan{}).Where("id = ?", "free").Update("app_limit", 10)
	fixture := attachPageImageFixture(t, s)
	cookie, csrf := sessionFor(t, s, owner)
	design := defaultPageDesign(source)
	design.Icon = uploadedPageImage(t, s, source, cookie, csrf)
	requireCode(t, call(t, s, "PUT", "/api/apps/"+source.ID+"/page-design", design, cookie, csrf), 200)
	fixture.failPut = true
	w := importUpload(t, s, cookie, csrf, source.ID)
	if w.Code < 400 || countRows(t, s, &Application{}) != 1 || countRows(t, s, &AppPageDesign{}) != 1 || countRows(t, s, &PageImageObject{}) != 1 {
		t.Fatal("partial import was retained")
	}
}

func TestImportPWAIconOwnershipAndFreshIdentity(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	other := seedUser(t, s, 0)
	source := createPWAFixture(t, s, owner, "Source")
	s.DB.Model(&Plan{}).Where("id = ?", "free").Update("app_limit", 10)
	s.DB.Model(&source).Update("icon_png", []byte("copied icon bytes"))
	cookie, csrf := sessionFor(t, s, owner)
	body := map[string]any{"name": "Imported", "target_url": "https://new.example/app", "import_from": source.ID}
	w := pwaKeyCall(t, s, body, cookie, csrf, "import-request-001")
	requireCode(t, w, 201)
	var view pwaAppView
	_ = json.Unmarshal(w.Body.Bytes(), &view)
	var target PWAApp
	s.DB.First(&target, "id = ?", view.ID)
	if target.Slug == source.Slug || target.VAPIDPublicKey == source.VAPIDPublicKey || string(target.IconPNG) != "copied icon bytes" || view.SubscriptionCount != 0 {
		t.Fatal("source identity was reused or icon not copied")
	}
	requireCode(t, call(t, s, "DELETE", "/api/pwas/"+source.ID, nil, cookie, csrf), 200)
	replay := pwaKeyCall(t, s, body, cookie, csrf, "import-request-001")
	requireCode(t, replay, 201)
	if !strings.Contains(replay.Body.String(), target.ID) {
		t.Fatal("idempotency replay failed after source deletion")
	}
	alien := createPWAFixture(t, s, other, "Other account")
	body["import_from"] = alien.ID
	requireCode(t, pwaKeyCall(t, s, body, cookie, csrf, "import-request-002"), 404)
	if countRows(t, s, &PWAApp{}) != 2 {
		t.Fatal("unauthorized PWA import created a record")
	}
}
