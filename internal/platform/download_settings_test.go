package platform

import (
	"strings"
	"testing"
)

func TestDownloadSettingsKeepShareLinkAndDownloadFlow(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	a := seedApp(t, s, u, 10)
	cookie, csrf := sessionFor(t, s, u)
	path := "/api/apps/" + a.ID + "/download-settings"
	requireCode(t, call(t, s, "PATCH", path, map[string]string{"download_mode": "invalid"}, cookie, csrf), 400)
	requireCode(t, call(t, s, "PATCH", path, map[string]string{"download_mode": "info"}, cookie, csrf), 200)
	var saved Application
	if err := s.DB.First(&saved, "id = ?", a.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.DownloadMode != "info" || saved.Name != a.Name || saved.Slug != a.Slug || saved.FileKey != a.FileKey {
		t.Fatal("settings overwrote unrelated app fields")
	}
	w := call(t, s, "GET", "/a/"+a.Slug+"?utm_source=test&fbclid=example", nil, nil, "")
	requireCode(t, w, 302)
	if w.Header().Get("Location") != "/a/"+a.Slug+"/info?utm_source=test&fbclid=example" {
		t.Fatal("detail redirect lost attribution query")
	}
	requireCode(t, call(t, s, "HEAD", "/a/"+a.Slug, nil, nil, ""), 302)
	var n int64
	s.DB.Model(&Ticket{}).Count(&n)
	if n != 0 {
		t.Fatal("detail redirect reserved download quota")
	}
	requireCode(t, call(t, s, "GET", "/a/"+a.Slug+"/info", nil, nil, ""), 200)
	requireCode(t, call(t, s, "PATCH", path, map[string]string{"download_mode": "direct"}, cookie, csrf), 200)
	w = call(t, s, "GET", "/a/"+a.Slug, nil, nil, "")
	requireCode(t, w, 307)
	if !strings.HasPrefix(w.Header().Get("Location"), "/d/") {
		t.Fatal("direct download did not create download ticket")
	}
	other := seedUser(t, s, 0)
	otherCookie, otherCSRF := sessionFor(t, s, other)
	requireCode(t, call(t, s, "PATCH", path, map[string]string{"download_mode": "info"}, otherCookie, otherCSRF), 404)
}
