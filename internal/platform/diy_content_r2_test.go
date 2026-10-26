package platform

import (
	"bytes"
	"encoding/json"
	"html"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func runtimeDIYR2Upload(t *testing.T, s *Server, owner User, data []byte) string {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "campaign.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := sessionFor(t, s, owner)
	r := httptest.NewRequest(http.MethodPost, s.Config.PublicURL+"/api/diy-landings/images", &body)
	r.AddCookie(cookie)
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 201)
	var out struct {
		Reference string `json:"reference"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil || diyManagedImagePath(out.Reference) == "" {
		t.Fatal("upload did not return a managed R2 image reference", err)
	}
	return out.Reference
}

func runtimeDIYFirstImageURL(t *testing.T, body string) string {
	t.Helper()
	match := regexp.MustCompile(`<img[^>]+src="([^"]+)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatal("rendered image has no source")
	}
	return html.UnescapeString(match[1])
}

func TestDIYR2ImagesPreviewAndPublicPagesKeepOwnershipAndBilling(t *testing.T) {
	s := meteredServer(t, 0)
	owner, other := seedUser(t, s, Dollar), seedUser(t, s, Dollar)
	attachPublicR2(t, s)
	picture := returnImage(t)
	image := runtimeDIYR2Upload(t, s, owner, picture)
	foreignImage := runtimeDIYR2Upload(t, s, other, picture)
	page, content := runtimeDIYContentPage(t, s, owner)
	var imageID string
	for key := range content {
		if strings.HasPrefix(key, "image-") {
			content[key] = image
			imageID = key
		}
	}
	ref := &DIYLandingReference{PageID: page.ID, RevisionID: page.LatestPublishedRevisionID, Content: content}
	w := pushTaskCall(t, s, "POST", "/api/diy-landings/preview", map[string]any{"reference": ref, "mode": "pwa"}, owner, "", "correct")
	requireCode(t, w, 200)
	var preview struct {
		URL string `json:"preview_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	frame := pushTaskCall(t, s, "GET", preview.URL, nil, owner, "", "correct")
	requireCode(t, frame, 200)
	imageURL := runtimeDIYFirstImageURL(t, frame.Body.String())
	parsed, err := url.Parse(imageURL)
	if err != nil || parsed.Query().Get("kind") != "managed-image" || parsed.Query().Get("url") != image {
		t.Fatal("R2 image was not rewritten through a signed managed resource")
	}
	got := pwaPublicCall(t, s, imageURL, "", "")
	requireCode(t, got, 200)
	if !bytes.Equal(got.Body.Bytes(), picture) || got.Header().Get("Content-Type") != "image/png" {
		t.Fatal("preview did not read the original bytes from R2")
	}
	query := parsed.Query()
	query.Set("url", foreignImage)
	parsed.RawQuery = query.Encode()
	requireCode(t, pwaPublicCall(t, s, parsed.String(), "", ""), 404)
	invalid := *ref
	invalid.Content = DIYLandingContent{imageID: foreignImage}
	requireCode(t, pushTaskCall(t, s, "POST", "/api/diy-landings/preview", map[string]any{"reference": invalid, "mode": "pwa"}, owner, "", "correct"), 400)
	for _, model := range []any{&AccessVisit{}, &MeteredEvent{}, &Ledger{}, &Ticket{}, &Download{}} {
		if countRows(t, s, model) != 0 {
			t.Fatal("image upload, reading or editing preview entered billing")
		}
	}
	config := diyPWAConfig(page)
	config.DIYLanding = ref
	w = pushTaskCall(t, s, "POST", "/api/pwas", map[string]any{"name": "R2 image PWA", "target_url": "https://example.com/target", "landing_config": config}, owner, "", "correct")
	requireCode(t, w, 201)
	pwa := diyPWAView(t, w)
	checkPublic := func(path string) {
		shell := pwaPublicCall(t, s, path, "", "zh-CN")
		requireCode(t, shell, 200)
		frame := pwaPublicCall(t, s, runtimeDIYFrameURL(t, shell.Body.String()), "", "zh-CN")
		requireCode(t, frame, 200)
		assetURL := runtimeDIYFirstImageURL(t, frame.Body.String())
		read := pwaPublicCall(t, s, assetURL, "", "")
		requireCode(t, read, 200)
		if !bytes.Equal(read.Body.Bytes(), picture) {
			t.Fatal("public page lost its R2 image")
		}
	}
	checkPublic("/p/" + pwa.Slug + "/")
	fields := diyAPKFields(page)
	fields["diy_content"] = diyJSON(content)
	w = diyAPKUpload(t, s, owner, "/api/apps", fields, diyAPKBytes(t, "R2 image app"))
	requireCode(t, w, 201)
	app := diyAPKFromResponse(t, w)
	checkPublic("/a/" + app.Slug + "/info")
	before := countRows(t, s, &MeteredEvent{})
	read := pwaPublicCall(t, s, imageURL, "", "")
	requireCode(t, read, 200)
	if countRows(t, s, &MeteredEvent{}) != before {
		t.Fatal("reading an image created a second business event")
	}
	w = pushTaskCall(t, s, "PATCH", "/api/apps/"+app.ID, map[string]any{"name": "Rejected image edit", "landing_source": "diy", "diy_landing": invalid}, owner, "", "correct")
	requireCode(t, w, 400)
	fresh, err := diyBindingReference(s.DB, "apk", app.ID)
	if err != nil || !diySameReference(fresh, ref) {
		t.Fatal("foreign image partially replaced application content", err)
	}
	var stored ReturnFlowAsset
	if err = s.DB.First(&stored, "user_id = ? AND public_path = ?", owner.ID, image).Error; err != nil || stored.ObjectKey == "" {
		t.Fatal("uploaded image was not persisted to R2", err)
	}
	if strings.Contains(diyJSON(fresh), "data:image/") {
		t.Fatal("new R2 image was saved as base64 in the binding")
	}
}
