package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type pageImageFixture struct {
	mu      sync.Mutex
	objects map[string][]byte
	deleted []string
	failPut bool
	failDel bool
}

// Exercise the real signed S3 SDK over TLS, not an in-memory storage adapter.
func attachPageImageFixture(t *testing.T, s *Server) *pageImageFixture {
	t.Helper()
	f := &pageImageFixture{objects: map[string][]byte{}}
	service := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256") {
			t.Error("R2 image access was not signed")
		}
		key := strings.TrimPrefix(r.URL.Path, "/shared-pwa/")
		if !strings.HasPrefix(key, "d1apk/page-images/") {
			t.Errorf("request touched a non-D1 object: %s", key)
		}
		switch r.Method {
		case "PUT":
			if f.failPut {
				http.Error(w, "forbidden", 403)
				return
			}
			if r.Header.Get("Content-Type") != "image/jpeg" || !strings.Contains(r.Header.Get("Cache-Control"), "immutable") {
				t.Error("incorrect R2 image metadata")
			}
			f.objects[key], _ = io.ReadAll(r.Body)
			w.Header().Set("ETag", `"0123456789abcdef0123456789abcdef"`)
		case "DELETE":
			if f.failDel {
				http.Error(w, "forbidden", 403)
				return
			}
			delete(f.objects, key)
			f.deleted = append(f.deleted, key)
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected R2 request (must never list shared bucket): %s", r.Method)
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(service.Close)
	s.Config.PageImages = PageImageConfig{Endpoint: service.URL, AccessKey: "test-access", SecretKey: "test-secret", Bucket: "shared-pwa", PublicURL: "https://images.example.com"}
	var err error
	s.PageImages, err = NewPageImageStore(s.Config)
	if err != nil {
		t.Fatal(err)
	}
	s.PageImages.Client, err = minio.New(strings.TrimPrefix(service.URL, "https://"), &minio.Options{Creds: credentials.NewStaticV4("test-access", "test-secret", ""), Secure: true, Region: "auto", BucketLookup: minio.BucketLookupPath, Transport: service.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func pageImagePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func uploadPageImageRequest(t *testing.T, s *Server, a Application, data []byte, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	form := multipart.NewWriter(&b)
	part, _ := form.CreateFormFile("file", "untrusted-name.png")
	_, _ = part.Write(data)
	_ = form.Close()
	r := httptest.NewRequest("POST", "http://localhost:8088/api/apps/"+a.ID+"/page-images", &b)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("X-CSRF-Token", csrf)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func uploadedPageImage(t *testing.T, s *Server, a Application, cookie *http.Cookie, csrf string) string {
	t.Helper()
	w := uploadPageImageRequest(t, s, a, pageImagePNG(t, 12, 24), cookie, csrf)
	requireCode(t, w, 201)
	var out struct{ URL string }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.URL == "" {
		t.Fatal("image upload URL missing", err)
	}
	return out.URL
}

func TestPageImageR2UploadPublishAndPermanentDelete(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	a := seedApp(t, s, u, 10)
	other := seedApp(t, s, u, 10)
	f := attachPageImageFixture(t, s)
	cookie, csrf := sessionFor(t, s, u)
	raw := uploadedPageImage(t, s, a, cookie, csrf)
	otherURL := uploadedPageImage(t, s, other, cookie, csrf)
	if !strings.HasPrefix(raw, "https://images.example.com/"+s.PageImages.Prefix+a.ID+"/") {
		t.Fatal("image was not isolated in the app and environment namespace")
	}
	key := strings.TrimPrefix(raw, "https://images.example.com/")
	if _, err := jpeg.Decode(bytes.NewReader(f.objects[key])); err != nil {
		t.Fatal("uploaded bytes were not sanitized JPEG", err)
	}
	design := defaultPageDesign(a)
	design.Icon = raw
	design.Screenshots = []string{"https://images.example.com/pwa/existing.jpg"}
	requireCode(t, call(t, s, "PUT", "/api/apps/"+a.ID+"/page-design", design, cookie, csrf), 200)
	public := call(t, s, "GET", "/a/"+a.Slug+"/info", nil, nil, "")
	requireCode(t, public, 200)
	if !strings.Contains(public.Body.String(), raw) {
		t.Fatal("published page did not use R2 image URL")
	}
	if countRows(t, s, &Download{}) != 0 || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("image upload changed usage or billing")
	}
	if _, ok := s.Blobs.(LocalStore); !ok {
		t.Fatal("public images changed APK storage")
	}
	requireCode(t, call(t, s, "DELETE", "/api/apps/"+a.ID, nil, cookie, csrf), 200)
	if len(f.deleted) != 1 || f.deleted[0] != key {
		t.Fatal("delete touched more than this app's own image", f.deleted)
	}
	if _, ok := f.objects[strings.TrimPrefix(otherURL, "https://images.example.com/")]; !ok {
		t.Fatal("another app image was removed")
	}
	if countRows(t, s, &PageImageObject{}) != 1 {
		t.Fatal("deleted app image record retained")
	}
}

func TestPageImageAuthValidationAndFailures(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	a := seedApp(t, s, u, 10)
	other := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	data := pageImagePNG(t, 12, 24)
	requireCode(t, uploadPageImageRequest(t, s, a, data, nil, ""), 401)
	requireCode(t, uploadPageImageRequest(t, s, a, data, cookie, "wrong"), 403)
	otherCookie, otherCSRF := sessionFor(t, s, other)
	requireCode(t, uploadPageImageRequest(t, s, a, data, otherCookie, otherCSRF), 404)
	requireCode(t, uploadPageImageRequest(t, s, a, data, cookie, csrf), 503)
	f := attachPageImageFixture(t, s)
	for _, invalid := range [][]byte{nil, []byte("<svg onload=alert(1) />"), []byte("not-a-picture"), bytes.Repeat([]byte("A"), maxPageImageBytes+1), pageImagePNG(t, 1401, 1)} {
		requireCode(t, uploadPageImageRequest(t, s, a, invalid, cookie, csrf), 400)
	}
	if len(f.objects) != 0 || countRows(t, s, &PageImageObject{}) != 0 {
		t.Fatal("invalid image reached R2")
	}
	f.failPut = true
	w := uploadPageImageRequest(t, s, a, data, cookie, csrf)
	requireCode(t, w, 502)
	if strings.Contains(w.Body.String(), "test-secret") || countRows(t, s, &PageImageObject{}) != 0 {
		t.Fatal("upload failure leaked credentials or retained an asset")
	}
}

func TestPageImageCleanupPreservesPublishedDraftOtherEnvironmentAndPWA(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	a := seedApp(t, s, u, 10)
	f := attachPageImageFixture(t, s)
	cookie, csrf := sessionFor(t, s, u)
	published := uploadedPageImage(t, s, a, cookie, csrf)
	orphan := uploadedPageImage(t, s, a, cookie, csrf)
	recent := uploadedPageImage(t, s, a, cookie, csrf)
	design := defaultPageDesign(a)
	design.Icon = published
	requireCode(t, call(t, s, "PUT", "/api/apps/"+a.ID+"/page-design", design, cookie, csrf), 200)
	if err := s.DB.Model(&PageImageObject{}).Where("url IN ?", []string{published, orphan}).Update("created_at", s.Now().Add(-48*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	foreign := PageImageObject{Key: "d1apk/page-images/other-environment/" + a.ID + "/" + randomHex(16) + ".jpg", AppID: a.ID, UserID: u.ID, URL: "https://images.example.com/other.jpg", CreatedAt: s.Now().Add(-48 * time.Hour)}
	if err := s.DB.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	f.objects["pwa/icon.png"] = []byte("PWA image")
	f.objects[foreign.Key] = []byte("different D1 environment")
	s.cleanupOrphans(context.Background())
	if len(f.deleted) != 1 || f.deleted[0] != strings.TrimPrefix(orphan, "https://images.example.com/") {
		t.Fatal("cleanup deleted a live/recent/foreign asset", f.deleted)
	}
	for _, raw := range []string{published, recent} {
		if _, ok := f.objects[strings.TrimPrefix(raw, "https://images.example.com/")]; !ok {
			t.Fatal("published or pending draft image was removed")
		}
	}
	if len(f.objects["pwa/icon.png"]) == 0 || len(f.objects[foreign.Key]) == 0 {
		t.Fatal("shared bucket cleanup touched PWA or another environment")
	}
	design.Icon = orphan
	requireCode(t, call(t, s, "PUT", "/api/apps/"+a.ID+"/page-design", design, cookie, csrf), 400)
	other := seedApp(t, s, u, 10)
	design.Icon = recent
	requireCode(t, call(t, s, "PUT", "/api/apps/"+other.ID+"/page-design", design, cookie, csrf), 400)
	for _, key := range []string{"pwa/icon.png", foreign.Key, s.PageImages.Prefix + "../bad.jpg"} {
		if err := s.PageImages.Delete(context.Background(), key); err == nil {
			t.Fatal("storage adapter accepted an unsafe deletion key", key)
		}
	}
}

func TestPageImageDeleteFailureRetriesWithoutSoftDeletedApp(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	a := seedApp(t, s, u, 10)
	f := attachPageImageFixture(t, s)
	cookie, csrf := sessionFor(t, s, u)
	_ = uploadedPageImage(t, s, a, cookie, csrf)
	f.failDel = true
	w := call(t, s, "DELETE", "/api/apps/"+a.ID, nil, cookie, csrf)
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), `"file_cleanup_pending":true`) {
		t.Fatal("R2 cleanup failure was hidden")
	}
	if countRows(t, s, &Application{}) != 0 || countRows(t, s, &PageImageObject{}) != 1 {
		t.Fatal("image cleanup failure retained app or lost retry record")
	}
	f.failDel = false
	s.cleanupOrphans(context.Background())
	if countRows(t, s, &PageImageObject{}) != 0 || len(f.objects) != 0 {
		t.Fatal("deferred image cleanup failed")
	}
}

func TestPageImageConfigIsOptionalAndIsolated(t *testing.T) {
	s := testServer(t)
	if store, err := NewPageImageStore(s.Config); err != nil || store != nil {
		t.Fatal("optional image storage required unexpectedly", err)
	}
	valid := PageImageConfig{Endpoint: "https://account.r2.cloudflarestorage.com", AccessKey: "access", SecretKey: "secret", Bucket: "shared", PublicURL: "https://images.example.com"}
	for _, mutate := range []func(*PageImageConfig){func(c *PageImageConfig) { c.SecretKey = "" }, func(c *PageImageConfig) { c.Endpoint = "http://insecure.example.com" }, func(c *PageImageConfig) { c.PublicURL = "https://user:password@example.com" }, func(c *PageImageConfig) { c.PublicURL = "https://images.example.com/path?key=secret" }, func(c *PageImageConfig) { c.Bucket = "../../pwa" }} {
		c := valid
		mutate(&c)
		if err := c.validate(); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid image config accepted or leaked secret")
		}
	}
	s.Config.PageImages = valid
	first, err := NewPageImageStore(s.Config)
	if err != nil {
		t.Fatal(err)
	}
	s.Config.PublicURL = "https://production.example.com"
	second, err := NewPageImageStore(s.Config)
	if err != nil || first.Prefix == second.Prefix {
		t.Fatal("development and production image paths collide", err)
	}
}

// Explicit opt-in only: normal test runs never write to a real shared bucket.
func TestPageImageR2Live(t *testing.T) {
	if os.Getenv("D1_R2_LIVE_TEST") != "1" {
		t.Skip("set D1_R2_LIVE_TEST=1 with PAGE_IMAGE_R2_* to run isolated vendor verification")
	}
	c, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	s := testServer(t) // A disposable local DB, never the configured live DB.
	s.Config.PageImages = c.PageImages
	s.PageImages, err = NewPageImageStore(s.Config)
	if err != nil || s.PageImages == nil {
		t.Fatal("live R2 image configuration unavailable", err)
	}
	u := seedUser(t, s, 0)
	a := seedApp(t, s, u, 10)
	cookie, csrf := sessionFor(t, s, u)
	defer func() {
		w := call(t, s, "DELETE", "/api/apps/"+a.ID, nil, cookie, csrf)
		if w.Code != 200 || strings.Contains(w.Body.String(), `"file_cleanup_pending":true`) || countRows(t, s, &PageImageObject{}) != 0 {
			t.Error("live fixture cleanup incomplete; inspect this run's D1-only app image prefix", a.ID)
		}
	}()
	raw := uploadedPageImage(t, s, a, cookie, csrf)
	design := defaultPageDesign(a)
	design.Icon = raw
	requireCode(t, call(t, s, "PUT", "/api/apps/"+a.ID+"/page-design", design, cookie, csrf), 200)
	page := call(t, s, "GET", "/a/"+a.Slug+"/info", nil, nil, "")
	requireCode(t, page, 200)
	if !strings.Contains(page.Body.String(), raw) {
		t.Fatal("live R2 URL missing from the public page")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", raw, nil)
	resp, err := s.HTTP.Do(req)
	if err != nil {
		t.Fatal("public image read failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal("R2 public image was not readable", resp.StatusCode)
	}
	cfg, err := jpeg.DecodeConfig(resp.Body)
	if err != nil || cfg.Width != 12 || cfg.Height != 24 {
		t.Fatal("public image content was incorrect", err)
	}
	t.Log("real R2 PUT -> D1 save -> public detail page -> public HTTPS image GET passed; own fixture is removed on exit")
}
