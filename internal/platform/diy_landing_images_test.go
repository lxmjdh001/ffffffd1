package platform

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/gorm"
)

type diyImageR2Fixture struct {
	mu            sync.Mutex
	objects       map[string][]byte
	puts, deletes int
	failPut       bool
}

func attachDIYImageR2(t *testing.T, s *Server, pageImages bool) *diyImageR2Fixture {
	t.Helper()
	f := &diyImageR2Fixture{objects: map[string][]byte{}}
	service := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256") {
			t.Error("R2 operation was not signed")
		}
		key := strings.TrimPrefix(r.URL.Path, "/images/")
		store := s.diyLandingImageUploadStore()
		if store == nil || !strings.HasPrefix(key, store.Prefix+"return-flow/") {
			t.Errorf("R2 image request escaped its namespace: %s", key)
		}
		switch r.Method {
		case "PUT":
			f.puts++
			if f.failPut {
				w.WriteHeader(403)
				return
			}
			data, _ := io.ReadAll(r.Body)
			if !diyLandingImageType(r.Header.Get("Content-Type")) || r.Header.Get("X-Amz-Meta-Sha256") != hashString(string(data)) {
				t.Error("R2 image metadata did not describe its unchanged bytes")
			}
			f.objects[key] = data
			w.Header().Set("ETag", `"0123456789abcdef0123456789abcdef"`)
		case "GET", "HEAD":
			data, found := f.objects[key]
			if !found {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("ETag", `"0123456789abcdef0123456789abcdef"`)
			http.ServeContent(w, r, "image", time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), bytes.NewReader(data))
		case "DELETE":
			f.deletes++
			delete(f.objects, key)
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected R2 operation %s (must not list bucket)", r.Method)
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(service.Close)
	client, err := minio.New(strings.TrimPrefix(service.URL, "https://"), &minio.Options{Creds: credentials.NewStaticV4("test-access", "test-secret", ""), Secure: true, Region: "auto", BucketLookup: minio.BucketLookupPath, Transport: service.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	if pageImages {
		s.Config.PageImages = PageImageConfig{Endpoint: service.URL, AccessKey: "test-access", SecretKey: "test-secret", Bucket: "images", PublicURL: "https://images.example"}
		s.PageImages, err = NewPageImageStore(s.Config)
		if err != nil {
			t.Fatal(err)
		}
		s.PageImages.Client = client
	} else {
		s.Config.R2 = R2Config{Endpoint: service.URL, AccessKey: "test-access", SecretKey: "test-secret", Bucket: "images"}
		s.Media, err = NewR2MediaStore(s.Config)
		if err != nil {
			t.Fatal(err)
		}
		s.Media.Client = client
	}
	return f
}

func diyImageUploadCall(t *testing.T, s *Server, owner User, data []byte, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", "client-filename.html")
	_, _ = part.Write(data)
	_ = form.Close()
	r := httptest.NewRequest("POST", s.Config.PublicURL+"/api/diy-landings/images", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	if owner.ID != "" {
		cookie, token := sessionFor(t, s, owner)
		r.AddCookie(cookie)
		if csrf == "correct" {
			csrf = token
		}
	}
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

type diyImageUploadResponse struct {
	ID, URL, Reference string
	PreviewURL         string `json:"preview_url"`
	ContentType        string `json:"content_type"`
	Size               int64
}

func diyImageUploaded(t *testing.T, w *httptest.ResponseRecorder) diyImageUploadResponse {
	t.Helper()
	requireCode(t, w, 201)
	var out diyImageUploadResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ID == "" || diyManagedImagePath(out.Reference) == "" || out.PreviewURL != out.Reference || !strings.HasSuffix(out.URL, out.Reference) {
		t.Fatalf("invalid R2 image response: %s", w.Body.String())
	}
	return out
}

func diyImageAnimatedGIF(t *testing.T) []byte {
	t.Helper()
	palette := color.Palette{color.Black, color.White}
	first, second := image.NewPaletted(image.Rect(0, 0, 2, 2), palette), image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	second.SetColorIndex(0, 0, 1)
	var out bytes.Buffer
	if err := gif.EncodeAll(&out, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{4, 4}, LoopCount: 0}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestDIYLandingImageR2UploadPreservesAnimationDeduplicatesAndNeverBills(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, Dollar), seedUser(t, s, Dollar)
	f := attachDIYImageR2(t, s, false)
	data := diyImageAnimatedGIF(t)
	requireCode(t, diyImageUploadCall(t, s, User{}, data, "correct"), 401)
	requireCode(t, diyImageUploadCall(t, s, owner, data, ""), 403)
	first := diyImageUploaded(t, diyImageUploadCall(t, s, owner, data, "correct"))
	if first.ContentType != "image/gif" || first.Size != int64(len(data)) {
		t.Fatal("animation upload was converted")
	}
	var asset ReturnFlowAsset
	if err := s.DB.First(&asset, "id = ?", first.ID).Error; err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	stored := append([]byte(nil), f.objects[asset.ObjectKey]...)
	f.mu.Unlock()
	if !bytes.Equal(stored, data) || asset.ObjectKey == "" || countRows(t, s, &R2MediaObject{}) != 1 {
		t.Fatal("image was not stored as unchanged registered R2 bytes")
	}
	if _, err := os.Stat(filepath.Join(s.returnFlowAssetDirectory(), filepath.Base(first.Reference))); !os.IsNotExist(err) {
		t.Fatal("DIY image silently persisted as a local asset")
	}
	again := diyImageUploaded(t, diyImageUploadCall(t, s, owner, data, "correct"))
	if again.Reference != first.Reference || again.ID != first.ID {
		t.Fatal("owner SHA dedup changed its stable URL")
	}
	f.mu.Lock()
	puts := f.puts
	f.mu.Unlock()
	if puts != 1 {
		t.Fatal("duplicate image was uploaded to R2 again")
	}
	foreign := diyImageUploaded(t, diyImageUploadCall(t, s, other, data, "correct"))
	if foreign.Reference == first.Reference {
		t.Fatal("different owners shared an editable image asset")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "https://download.example/opaque-asset", nil)
	s.serveDIYManagedImage(w, r, first.Reference)
	requireCode(t, w, 200)
	if !bytes.Equal(w.Body.Bytes(), data) || w.Header().Get("Access-Control-Allow-Origin") != "*" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Type") != "image/gif" {
		t.Fatal("download-domain image read changed bytes or isolation headers")
	}
	etag := w.Header().Get("ETag")
	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "https://download.example/opaque-asset", nil)
	r.Header.Set("If-None-Match", etag)
	s.serveDIYManagedImage(w, r, first.Reference)
	requireCode(t, w, 304)
	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", s.Config.PublicURL+first.Reference, nil)
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 200)
	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "https://foreign.example"+first.Reference, nil)
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 404)
	for _, model := range []any{&AccessVisit{}, &MeteredEvent{}, &Ledger{}, &Ticket{}, &Download{}} {
		if countRows(t, s, model) != 0 {
			t.Fatal("R2 image upload/read entered billing or download business flow")
		}
	}
	if user := usageOwner(t, s, owner.ID); user.BalanceMicros != Dollar || user.HeldMicros != 0 {
		t.Fatal("R2 image upload changed customer funds")
	}
	if err := s.DB.Model(&owner).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "https://download.example/opaque-asset", nil)
	r.Header.Set("If-None-Match", etag)
	s.serveDIYManagedImage(w, r, first.Reference)
	requireCode(t, w, 404)
}

func TestDIYLandingImagePageR2RemainsIndependentOfAPKStorage(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	s.Config.S3Endpoint = "https://private-apk.example"
	s.Config.S3Bucket = "private-packages"
	originalBlobs := s.Blobs
	f := attachDIYImageR2(t, s, true)
	result := diyImageUploaded(t, diyImageUploadCall(t, s, owner, returnImage(t), "correct"))
	var asset ReturnFlowAsset
	if err := s.DB.First(&asset, "id = ?", result.ID).Error; err != nil {
		t.Fatal(err)
	}
	if s.Media != nil || s.Blobs != originalBlobs || s.Config.S3Bucket != "private-packages" || !strings.HasPrefix(asset.ObjectKey, s.PageImages.Prefix+"diy/return-flow/") {
		t.Fatal("page-image R2 was coupled to APK package storage")
	}
	f.mu.Lock()
	stored := len(f.objects)
	f.mu.Unlock()
	if stored != 1 {
		t.Fatal("page-image adapter did not upload its object")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", s.Config.PublicURL+result.Reference, nil)
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 200)
	if !bytes.Equal(w.Body.Bytes(), returnImage(t)) {
		t.Fatal("existing public media route cannot read the page-image R2 subnamespace")
	}
	if s.diyLandingImageReadStore(s.PageImages.Prefix+owner.ID+"/"+randomHex(16)+".jpg") != nil {
		t.Fatal("ordinary page-image keys were exposed through DIY media")
	}
	// New images continue to prefer the configured image bucket even when the
	// application also has a separate media/package bucket.
	s.Media = &R2MediaStore{Client: s.PageImages.Client, Bucket: "unrelated-private-bucket", Prefix: "unrelated-private-namespace/"}
	if store := s.diyLandingImageUploadStore(); store.Bucket != s.PageImages.Bucket || store.Prefix != s.PageImages.Prefix+"diy/" {
		t.Fatal("dedicated image R2 was not preferred")
	}
}

func TestDIYLandingImageRequiresR2AndRejectsInvalidFiles(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	requireCode(t, diyImageUploadCall(t, s, owner, returnImage(t), "correct"), 503)
	if countRows(t, s, &ReturnFlowAsset{}) != 0 {
		t.Fatal("missing R2 silently saved a local asset")
	}
	f := attachDIYImageR2(t, s, false)
	largeDimensions := append([]byte(nil), returnImage(t)...)
	binary.BigEndian.PutUint32(largeDimensions[16:20], 4096)
	binary.BigEndian.PutUint32(largeDimensions[20:24], 4096)
	binary.BigEndian.PutUint32(largeDimensions[29:33], crc32.ChecksumIEEE(largeDimensions[12:29]))
	invalid := [][]byte{[]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), []byte("not a picture"), append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, make([]byte, 96)...), returnImage(t)[:20], make([]byte, maxDIYContentImageBytes+1), largeDimensions}
	for _, data := range invalid {
		requireCode(t, diyImageUploadCall(t, s, owner, data, "correct"), 400)
	}
	f.mu.Lock()
	puts := f.puts
	f.mu.Unlock()
	if puts != 0 || countRows(t, s, &ReturnFlowAsset{}) != 0 {
		t.Fatal("invalid image reached R2 or metadata storage")
	}
}

func TestDIYLandingImageR2FailureAndMetadataRollbackLeaveNoAsset(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	f := attachDIYImageR2(t, s, false)
	f.mu.Lock()
	f.failPut = true
	f.mu.Unlock()
	requireCode(t, diyImageUploadCall(t, s, owner, returnImage(t), "correct"), 502)
	f.mu.Lock()
	f.failPut = false
	f.mu.Unlock()
	callback := "test:reject_diy_image_metadata"
	if err := s.DB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "return_flow_assets" {
			tx.AddError(errors.New("injected image metadata failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Callback().Create().Remove(callback) })
	requireCode(t, diyImageUploadCall(t, s, owner, returnImage(t), "correct"), 500)
	f.mu.Lock()
	objects, deletes := len(f.objects), f.deletes
	f.mu.Unlock()
	if objects != 0 || deletes == 0 || countRows(t, s, &ReturnFlowAsset{}) != 0 || countRows(t, s, &R2MediaObject{}) != 0 {
		t.Fatal("failed image transaction left an object or metadata behind")
	}
}

func TestDIYLandingImageExistingLocalDuplicateIsPromotedToR2(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	data := returnImage(t)
	w := returnAssetCall(t, s, data, owner, "")
	requireCode(t, w, 201)
	var before ReturnFlowAsset
	if err := s.DB.First(&before, "user_id = ?", owner.ID).Error; err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(s.returnFlowAssetDirectory(), filepath.Base(before.PublicPath))
	if _, err := os.Stat(backup); err != nil {
		t.Fatal(err)
	}
	f := attachDIYImageR2(t, s, false)
	result := diyImageUploaded(t, diyImageUploadCall(t, s, owner, data, "correct"))
	var after ReturnFlowAsset
	if err := s.DB.First(&after, "id = ?", before.ID).Error; err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	stored := append([]byte(nil), f.objects[after.ObjectKey]...)
	f.mu.Unlock()
	if result.ID != before.ID || result.Reference != before.PublicPath || after.ObjectKey == "" || !bytes.Equal(stored, data) || countRows(t, s, &ReturnFlowAsset{}) != 1 {
		t.Fatal("local duplicate was not promoted using its original identity")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatal("promotion removed the local backup")
	}
}

func TestDIYManagedImageReferencesAreCanonicalOwnedAndImageOnly(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, 0), seedUser(t, s, 0)
	makeAsset := func(user User, ctype string, size int64) ReturnFlowAsset {
		id := randomHex(16)
		row := ReturnFlowAsset{ID: id, UserID: user.ID, SHA256: randomHex(32), PublicPath: "/return-flow-assets/" + id + ".png", ContentType: ctype, Size: size, CreatedAt: s.Now()}
		if err := s.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	owned, foreign := makeAsset(owner, "image/png", 42), makeAsset(other, "image/png", 42)
	for _, path := range []string{owned.PublicPath, "/return-flow-assets/" + randomHex(16) + ".jpeg", "/return-flow-assets/" + randomHex(16) + ".webp"} {
		if diyManagedImagePath(path) != path {
			t.Fatal("canonical image path rejected")
		}
	}
	for _, path := range []string{s.Config.PublicURL + owned.PublicPath, owned.PublicPath + "?token=1", owned.PublicPath + "#fragment", strings.ToUpper(owned.PublicPath), strings.Replace(owned.PublicPath, "/", "\\", -1), "/return-flow-assets/../" + filepath.Base(owned.PublicPath), "//return-flow-assets/" + filepath.Base(owned.PublicPath), strings.Replace(owned.PublicPath, ".png", ".mp4", 1)} {
		if diyManagedImagePath(path) != "" {
			t.Fatalf("noncanonical managed path accepted: %q", path)
		}
	}
	if err := validateDIYManagedImageReferences(s.DB, owner.ID, DIYLandingContent{"image": owned.PublicPath, "external": "https://images.example/logo.png", "text": "Ordinary text"}); err != nil {
		t.Fatal(err)
	}
	for _, asset := range []ReturnFlowAsset{foreign, makeAsset(owner, "video/mp4", 42), makeAsset(owner, "image/svg+xml", 42), makeAsset(owner, "image/png", maxDIYContentImageBytes+1)} {
		if err := validateDIYManagedImageReferences(s.DB, owner.ID, DIYLandingContent{"image": asset.PublicPath}); err == nil {
			t.Fatal("foreign, nonimage or oversize managed asset accepted")
		}
	}
	base := "/api/landing-template-assets/" + randomHex(16)
	signed := s.diyOwnedImageAssetURL(base, owned.PublicPath, "image", s.Now().Add(time.Minute), owner.ID)
	parsed, err := url.Parse(signed)
	if err != nil || parsed.Query().Get("kind") != "managed-image" || parsed.Query().Get("url") != owned.PublicPath {
		t.Fatal("owned image did not receive a managed-image capability")
	}
	if s.diyOwnedImageAssetURL(base, foreign.PublicPath, "image", s.Now().Add(time.Minute), owner.ID) != "" || s.diyOwnedImageAssetURL(base, owned.PublicPath, "script", s.Now().Add(time.Minute), owner.ID) != "" {
		t.Fatal("foreign or script-context image received a signature")
	}
	if err := s.DB.Model(&owner).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := validateDIYManagedImageReferences(s.DB, owner.ID, DIYLandingContent{"image": owned.PublicPath}); err == nil {
		t.Fatal("disabled owner asset remained usable")
	}
}

func TestDIYLandingUploadedAnimatedFormatsAreFullyValidatedWithoutReencoding(t *testing.T) {
	gifData := diyImageAnimatedGIF(t)
	if ctype, ext, valid := diyLandingUploadedImage(gifData); !valid || ctype != "image/gif" || ext != "gif" {
		t.Fatal("animated GIF was rejected")
	}
	if _, _, valid := diyLandingUploadedImage(gifData[:len(gifData)-2]); valid {
		t.Fatal("truncated later GIF frame was accepted")
	}
	static, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, valid := diyLandingUploadedImage(static); !valid {
		t.Fatal("valid tiny WebP fixture was rejected")
	}
	chunk := func(name string, data []byte) []byte {
		out := make([]byte, 8+len(data)+(len(data)&1))
		copy(out, name)
		binary.LittleEndian.PutUint32(out[4:8], uint32(len(data)))
		copy(out[8:], data)
		return out
	}
	frame := append(make([]byte, 16), static[12:]...)
	flags := make([]byte, 10)
	flags[0] = 2
	payload := append(chunk("VP8X", flags), chunk("ANIM", make([]byte, 6))...)
	payload = append(payload, chunk("ANMF", frame)...)
	payload = append(payload, chunk("ANMF", frame)...)
	animated := make([]byte, 12+len(payload))
	copy(animated, "RIFF")
	binary.LittleEndian.PutUint32(animated[4:8], uint32(len(animated)-8))
	copy(animated[8:12], "WEBP")
	copy(animated[12:], payload)
	if ctype, ext, valid := diyLandingUploadedImage(animated); !valid || ctype != "image/webp" || ext != "webp" {
		t.Fatal("animated WebP frames were not decoded")
	}
	if _, _, valid := diyLandingUploadedImage(animated[:len(animated)-1]); valid {
		t.Fatal("truncated animated WebP accepted")
	}
}
