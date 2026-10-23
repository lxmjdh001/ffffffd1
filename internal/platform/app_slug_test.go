package platform

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCustomAppSlugCreateRenameAndConflicts(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	w := uploadFixture(t, s, cookie, csrf, "test.exe", []byte("MZ1234567890"), " My-App_1 ")
	requireCode(t, w, 201)
	var a Application
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if a.Slug != "my-app_1" {
		t.Fatalf("slug not normalized: %s", a.Slug)
	}
	requireCode(t, call(t, s, "HEAD", "/a/my-app_1", nil, nil, ""), 200)
	requireCode(t, uploadFixture(t, s, cookie, csrf, "test.exe", []byte("MZ1234567890"), "MY-APP_1"), 409)
	other := seedUser(t, s, 0)
	otherCookie, otherCSRF := sessionFor(t, s, other)
	requireCode(t, uploadFixture(t, s, otherCookie, otherCSRF, "test.exe", []byte("MZ1234567890"), "my-app_1"), 409)
	for _, bad := range []string{"../secret", "has space", "-bad", strings.Repeat("a", 33)} {
		requireCode(t, uploadFixture(t, s, cookie, csrf, "test.exe", []byte("MZ1234567890"), bad), 400)
	}
	body := map[string]any{"name": a.Name, "description": a.Description, "version": a.Version, "enabled": true, "slug": "new-brand"}
	requireCode(t, call(t, s, "PATCH", "/api/apps/"+a.ID, body, cookie, csrf), 200)
	requireCode(t, call(t, s, "HEAD", "/a/my-app_1", nil, nil, ""), 404)
	requireCode(t, call(t, s, "HEAD", "/a/new-brand", nil, nil, ""), 200)
	requireCode(t, call(t, s, "PATCH", "/api/apps/"+a.ID, body, otherCookie, otherCSRF), 404)
	b := seedApp(t, s, u, 10)
	body["slug"] = b.Slug
	requireCode(t, call(t, s, "PATCH", "/api/apps/"+a.ID, body, cookie, csrf), 409)
	requireCode(t, call(t, s, "HEAD", "/a/new-brand", nil, nil, ""), 200)
	delete(body, "slug")
	requireCode(t, call(t, s, "PATCH", "/api/apps/"+a.ID, body, cookie, csrf), 200)
	var saved Application
	s.DB.First(&saved, "id = ?", a.ID)
	if saved.Slug != "new-brand" {
		t.Fatal("legacy metadata save changed share slug")
	}
}
