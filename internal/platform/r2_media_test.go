package platform

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type publicR2Fixture struct {
	mu                   sync.Mutex
	objects              map[string][]byte
	failPut, corruptRead bool
}

func attachPublicR2(t *testing.T, s *Server) *publicR2Fixture {
	t.Helper()
	f := &publicR2Fixture{objects: map[string][]byte{}}
	service := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256") {
			t.Error("backend R2 request was not signed")
		}
		if r.URL.Path == "/private-bucket/" || r.URL.Path == "/private-bucket" {
			w.WriteHeader(200)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/private-bucket/")
		if !strings.HasPrefix(key, s.Media.Prefix) {
			t.Errorf("request escaped private environment: %s", key)
		}
		switch r.Method {
		case "PUT":
			if f.failPut {
				w.WriteHeader(403)
				return
			}
			f.objects[key], _ = io.ReadAll(r.Body)
			w.Header().Set("ETag", `"0123456789abcdef0123456789abcdef"`)
		case "GET", "HEAD":
			data, ok := f.objects[key]
			if !ok {
				w.WriteHeader(404)
				return
			}
			if f.corruptRead {
				data = bytes.Repeat([]byte{'x'}, len(data))
			}
			w.Header().Set("ETag", `"0123456789abcdef0123456789abcdef"`)
			http.ServeContent(w, r, "private", time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC), bytes.NewReader(data))
		case "DELETE":
			delete(f.objects, key)
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected bucket/list operation: %s %s", r.Method, r.URL)
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(service.Close)
	s.Config.R2 = R2Config{Endpoint: service.URL, AccessKey: "test-access", SecretKey: "test-secret", Bucket: "private-bucket"}
	var err error
	s.Media, err = NewR2MediaStore(s.Config)
	if err != nil {
		t.Fatal(err)
	}
	s.Media.Client, err = minio.New(strings.TrimPrefix(service.URL, "https://"), &minio.Options{Creds: credentials.NewStaticV4("test-access", "test-secret", ""), Secure: true, Region: "auto", BucketLookup: minio.BucketLookupPath, Transport: service.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	s.Blobs = s.Media.packages()
	return f
}

func TestPublicR2UploadsPermissionsAndRange(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	other := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	a := createPWAFixture(t, s, u, "R2 app")
	f := attachPublicR2(t, s)
	requireCode(t, uploadFixture(t, s, cookie, csrf, "test.exe", []byte("MZ1234567890")), 201)
	var app Application
	s.DB.First(&app, "user_id = ?", u.ID)
	if err := verifyStoredFile(context.Background(), s.Blobs, app.FileKey, app.Size, app.SHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Config.DataDir, "files", app.FileKey)); !os.IsNotExist(err) {
		t.Fatal("new package persisted locally")
	}
	_, rawTicket, visitor := seedTicket(t, s, app)
	partial := download(t, s, rawTicket, visitor, "GET", "bytes=0-5")
	requireCode(t, partial, 206)
	if partial.Body.String() != "MZ1234" || countRows(t, s, &Download{}) != 0 {
		t.Fatal("R2 partial download changed content or billed a partial read")
	}
	requireCode(t, pwaIconCall(t, s, a.ID, returnImage(t), cookie, csrf, false), 200)
	var fresh PWAApp
	s.DB.First(&fresh, "id = ?", a.ID)
	if fresh.IconObjectKey == "" || len(fresh.IconPNG) != 0 {
		t.Fatal("PWA icon binary retained in database")
	}
	for _, size := range []string{"192", "512"} {
		w := pwaPublicCall(t, s, "/p/"+a.Slug+"/icon/"+size+".png", "", "")
		requireCode(t, w, 200)
		if _, err := png.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
			t.Fatal(err)
		}
	}
	otherCookie, otherCSRF := sessionFor(t, s, other)
	requireCode(t, call(t, s, "GET", "/api/pwas/"+a.ID+"/icon", nil, otherCookie, otherCSRF), 404)
	w := pushAssetCall(t, s, u, returnImage(t), "image", false, "correct")
	requireCode(t, w, 200)
	var imageURL struct{ URL string }
	json.Unmarshal(w.Body.Bytes(), &imageURL)
	var asset PWAPushAsset
	s.DB.First(&asset, "user_id = ?", u.ID)
	if asset.ObjectKey == "" || len(asset.PNG) != 0 {
		t.Fatal("notification image retained in database")
	}
	requireCode(t, pwaPublicCall(t, s, strings.TrimPrefix(imageURL.URL, s.Config.PublicURL), "", ""), 200)
	before := len(f.objects)
	requireCode(t, pushAssetCall(t, s, u, returnImage(t), "image", false, "correct"), 200)
	if len(f.objects) != before {
		t.Fatal("duplicate push image created a new R2 object")
	}
	movie := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, bytes.Repeat([]byte{0}, 96)...)
	w = returnAssetCall(t, s, movie, u, "")
	requireCode(t, w, 201)
	video := returnAssetURL(t, w)
	var media ReturnFlowAsset
	s.DB.First(&media, "user_id = ?", u.ID)
	if media.ObjectKey == "" {
		t.Fatal("return media missing R2 key")
	}
	if _, err := os.Stat(filepath.Join(s.returnFlowAssetDirectory(), filepath.Base(media.PublicPath))); !os.IsNotExist(err) {
		t.Fatal("return media persisted locally")
	}
	r := httptest.NewRequest("GET", video, nil)
	r.Header.Set("Range", "bytes=0-11")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 206)
	if !bytes.Equal(w.Body.Bytes(), movie[:12]) {
		t.Fatal("R2 video range incorrect")
	}
	f.failPut = true
	requireCode(t, pwaIconCall(t, s, a.ID, returnImage(t), cookie, csrf, false), 502)
	var after PWAApp
	s.DB.First(&after, "id = ?", a.ID)
	if after.IconObjectKey != fresh.IconObjectKey {
		t.Fatal("failed upload replaced previous icon")
	}
	f.failPut = false
	requireCode(t, call(t, s, "DELETE", "/api/pwas/"+a.ID, nil, cookie, csrf), 200)
	s.DB.Model(&R2MediaObject{}).Where("key = ?", fresh.IconObjectKey).Update("created_at", s.Now().Add(-48*time.Hour))
	s.cleanupR2Media(context.Background())
	if _, ok := f.objects[fresh.IconObjectKey]; ok {
		t.Fatal("deleted PWA icon not cleaned")
	}
	if _, ok := f.objects[media.ObjectKey]; !ok {
		t.Fatal("cleanup removed referenced return media")
	}
	if err := s.Media.remove(context.Background(), "pwa/foreign.png"); err == nil {
		t.Fatal("foreign object deletion allowed")
	}
	if err := s.DB.Model(&u).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, pwaPublicCall(t, s, strings.TrimPrefix(imageURL.URL, s.Config.PublicURL), "", ""), 404)
}

func TestPublicR2MigrationVerifiedAndRepeatable(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	requireCode(t, uploadFixture(t, s, cookie, csrf, "test.exe", []byte("MZ1234567890")), 201)
	a := createPWAFixture(t, s, u, "Old icon")
	requireCode(t, pwaIconCall(t, s, a.ID, returnImage(t), cookie, csrf, false), 200)
	requireCode(t, pushAssetCall(t, s, u, returnImage(t), "image", false, "correct"), 200)
	requireCode(t, returnAssetCall(t, s, returnImage(t), u, ""), 201)
	f := attachPublicR2(t, s)
	if err := s.MigrateFilesToR2(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.objects) != 4 {
		t.Fatalf("expected 4 migrated files, got %d", len(f.objects))
	}
	var app Application
	s.DB.First(&app, "user_id = ?", u.ID)
	if _, err := os.Stat(filepath.Join(s.Config.DataDir, "files", app.FileKey)); err != nil {
		t.Fatal("migration removed local backup")
	}
	if err := s.MigrateFilesToR2(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.objects) != 4 {
		t.Fatal("repeated migration duplicated objects")
	}
	var icon PWAApp
	s.DB.First(&icon, "id = ?", a.ID)
	if len(icon.IconPNG) != 0 || icon.IconObjectKey == "" {
		t.Fatal("icon migration incomplete")
	}
	requireCode(t, pwaPublicCall(t, s, "/p/"+a.Slug+"/icon/512.png", "", ""), 200)
}

func TestPublicR2CorruptMigrationRetainsOriginal(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	a := createPWAFixture(t, s, u, "Original")
	requireCode(t, pwaIconCall(t, s, a.ID, returnImage(t), cookie, csrf, false), 200)
	f := attachPublicR2(t, s)
	f.corruptRead = true
	if err := s.MigrateFilesToR2(context.Background()); err == nil {
		t.Fatal("corrupt R2 migration accepted")
	}
	var row PWAApp
	s.DB.First(&row, "id = ?", a.ID)
	if len(row.IconPNG) == 0 || row.IconObjectKey != "" || len(f.objects) != 0 {
		t.Fatal("failed migration lost original or leaked object")
	}
}

func TestPublicR2ConfigurationIsolation(t *testing.T) {
	valid := R2Config{Endpoint: "https://account.r2.cloudflarestorage.com", AccessKey: "access", SecretKey: "secret", Bucket: "private"}
	c := Config{R2: valid, PublicURL: "https://one.example"}
	if err := valid.validate(c); err != nil {
		t.Fatal(err)
	}
	first, _ := NewR2MediaStore(c)
	c.PublicURL = "https://two.example"
	second, _ := NewR2MediaStore(c)
	if first.Prefix == second.Prefix {
		t.Fatal("environments share private object namespace")
	}
	c.PageImages = PageImageConfig{Endpoint: valid.Endpoint, Bucket: valid.Bucket}
	if err := valid.validate(c); err != nil {
		t.Fatal("shared public bucket must be accepted for APK", err)
	}
	c.R2 = R2Config{}
	c.PageImages.AccessKey, c.PageImages.SecretKey = "access", "secret"
	if got := c.r2Config(); got != valid {
		t.Fatal("page-image bucket was not reused")
	}
	c.R2 = valid
	c.PageImages = PageImageConfig{}
	c.S3Endpoint = "legacy.example"
	if err := valid.validate(c); err == nil {
		t.Fatal("ambiguous storage configuration accepted")
	}
}

// Explicit opt-in: isolated temporary DB, only this test's random objects are
// created/deleted. Never enumerates or changes permissions on the shared bucket.
func TestPublicR2Live(t *testing.T) {
	if os.Getenv("D1_R2_FILES_LIVE_TEST") != "1" {
		t.Skip("set D1_R2_FILES_LIVE_TEST=1 to verify real public R2 uploads")
	}
	c, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	s := testServer(t)
	s.Media, err = NewR2MediaStore(c)
	if err != nil || s.Media == nil {
		t.Fatal("R2 files configuration unavailable", err)
	}
	if err = s.Media.checkBucket(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Blobs = s.Media.packages()
	t.Cleanup(func() {
		var apps []Application
		s.DB.Find(&apps)
		for _, a := range apps {
			if e := s.Blobs.Delete(context.Background(), a.FileKey); e != nil {
				t.Error("live package cleanup failed", a.FileKey, e)
			}
		}
		var objects []R2MediaObject
		s.DB.Find(&objects)
		for _, o := range objects {
			if e := s.Media.remove(context.Background(), o.Key); e != nil {
				t.Error("live media cleanup failed", o.Key, e)
			}
		}
	})
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	var apk bytes.Buffer
	z := zip.NewWriter(&apk)
	part, _ := z.Create("AndroidManifest.xml")
	part.Write([]byte("<manifest package=\"test.d1.r2\"/>"))
	z.Close()
	requireCode(t, uploadFixture(t, s, cookie, csrf, "r2-test.apk", apk.Bytes()), 201)
	var app Application
	s.DB.First(&app, "user_id = ?", u.ID)
	if err = verifyStoredFile(context.Background(), s.Blobs, app.FileKey, app.Size, app.SHA256); err != nil {
		t.Fatal(err)
	}
	if c.PageImages.PublicURL == "" || c.PageImages.Bucket != s.Media.Bucket {
		t.Fatal("public URL for configured R2 bucket unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", c.PageImages.PublicURL+"/"+s.Media.Prefix+"apps/"+app.FileKey, nil)
	resp, err := s.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode != 200 || readErr != nil || !bytes.Equal(data, apk.Bytes()) {
		t.Fatal("anonymous R2 APK read did not match upload", resp.StatusCode, readErr)
	}
	if key := os.Getenv("D1_R2_VERIFY_KEY"); key != "" {
		if !hexKey.MatchString(key) {
			t.Fatal("invalid migrated APK verification key")
		}
		req, _ = http.NewRequestWithContext(ctx, "GET", c.PageImages.PublicURL+"/"+s.Media.Prefix+"apps/"+key, nil)
		resp, err = s.HTTP.Do(req)
		if err != nil {
			t.Fatal("migrated APK public read failed", err)
		}
		data, readErr = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode != 200 || readErr != nil || hashString(string(data)) != os.Getenv("D1_R2_VERIFY_SHA") {
			t.Fatal("migrated APK public SHA256 verification failed", resp.StatusCode, readErr)
		}
		t.Log("existing migrated APK: anonymous public GET and SHA256 verified")
	}
	a := createPWAFixture(t, s, u, "Public R2 fixture")
	requireCode(t, pwaIconCall(t, s, a.ID, returnImage(t), cookie, csrf, false), 200)
	requireCode(t, pwaPublicCall(t, s, "/p/"+a.Slug+"/icon/192.png", "", ""), 200)
	requireCode(t, pushAssetCall(t, s, u, returnImage(t), "image", false, "correct"), 200)
	requireCode(t, returnAssetCall(t, s, returnImage(t), u, ""), 201)
	t.Log("real R2 APK/PWA/push/return uploads and anonymous APK GET passed; only this run's objects are cleaned")
}
