package platform

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLegacyDownloadDomainKeepsLinksWithoutNewAssignments(t *testing.T) {
	s := testServer(t)
	free := seedUser(t, s, 0)
	paid := seedUser(t, s, 0)
	if err := s.DB.Model(&paid).Update("pro_until", s.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	x, err := getSettings(s.DB)
	if err != nil {
		t.Fatal(err)
	}
	x.DownloadBaseURL = "https://legacy.example.com"
	x.DownloadDomainRules = []DownloadDomainRule{
		{URL: "https://legacy.example.com", Audience: "all", LegacyOnly: true},
		{URL: "https://one.example.com", Audience: "all"},
		{URL: "https://two.example.com", Audience: "all"},
		{URL: "https://paid.example.com", Audience: "paid"},
	}
	requireCode(t, saveDownloadDomainSettings(t, s, x), 200)
	saved, err := getSettings(s.DB)
	if err != nil || !saved.DownloadDomainRules[0].LegacyOnly {
		t.Fatal("admin save lost legacy-only state")
	}
	for _, u := range []User{free, paid} {
		if assignedDomainFor(t, s, u) != "https://one.example.com" {
			t.Fatal("legacy domain was reassigned")
		}
		if err := s.DB.First(&u, "id = ?", u.ID).Error; err != nil {
			t.Fatal(err)
		}
		domains, err := s.downloadDomainsFor(u)
		want := 2
		if u.ID == paid.ID {
			want = 3
		}
		if err != nil || len(domains) != want {
			t.Fatalf("unexpected domains: %+v, %v", domains, err)
		}
		for _, d := range domains {
			if d.URL == x.DownloadBaseURL {
				t.Fatal("legacy domain leaked into copy choices")
			}
		}
	}
	config := call(t, s, "GET", "/api/config", nil, nil, "")
	requireCode(t, config, 200)
	var public struct {
		Base string `json:"download_base_url"`
	}
	if err := json.Unmarshal(config.Body.Bytes(), &public); err != nil || public.Base != "https://one.example.com" {
		t.Fatal("public config offers retired domain")
	}
	a := seedApp(t, s, free, 10)
	for _, host := range []string{"legacy.example.com", "one.example.com", "two.example.com"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("HEAD", "https://"+host+"/a/"+a.Slug, nil))
		requireCode(t, w, 200)
	}
	if saved.managedDownloadHosts(true)["legacy.example.com"] != "all" {
		t.Fatal("old links no longer authorized")
	}
	cookie, csrf := sessionFor(t, s, free)
	requireCode(t, call(t, s, "POST", "/api/domains", map[string]string{"hostname": "legacy.example.com"}, cookie, csrf), 400)
	requireCode(t, call(t, s, "PATCH", "/api/download-domains/note", map[string]string{"url": x.DownloadBaseURL, "remark": "no longer selectable"}, cookie, csrf), 403)
	// With no selectable rule, do not resurrect the legacy DownloadBaseURL.
	x.DownloadDomainRules = x.DownloadDomainRules[:1]
	requireCode(t, saveDownloadDomainSettings(t, s, x), 200)
	if assignedDomainFor(t, s, free) != "" {
		t.Fatal("legacy fallback was reassigned")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("HEAD", "https://legacy.example.com/a/"+a.Slug, nil))
	requireCode(t, w, 200)
}

func TestTrafficPreviewUsesTheSameDomainsAsCopyDrawer(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	requireCode(t, call(t, s, "POST", "/api/traffic-pools", map[string]any{"slug": "copy-route", "name": "分流", "enabled": true, "urls": []string{"https://example.com/app.apk"}}, cookie, csrf), 200)
	preview := func() string {
		t.Helper()
		w := call(t, s, "GET", "/api/traffic-pools", nil, cookie, "")
		requireCode(t, w, 200)
		var out struct {
			Data []struct {
				EntryURL string `json:"entry_url"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || len(out.Data) != 1 {
			t.Fatal("invalid traffic response")
		}
		return out.Data[0].EntryURL
	}
	if preview() != "" {
		t.Fatal("unconfigured preview fell back to control website")
	}
	x, _ := getSettings(s.DB)
	x.DownloadDomainRules = []DownloadDomainRule{{URL: "https://old.example.com", Audience: "all", LegacyOnly: true}, {URL: "https://one.example.com", Audience: "all"}, {URL: "https://paid.example.com", Audience: "paid"}}
	if err := s.DB.Save(&x).Error; err != nil {
		t.Fatal(err)
	}
	if preview() != "https://one.example.com/r/copy-route" {
		t.Fatal("traffic preview differs from picker")
	}
	x.DownloadDomainRules = x.DownloadDomainRules[:1]
	if err := s.DB.Save(&x).Error; err != nil {
		t.Fatal(err)
	}
	now := s.Now()
	d := Domain{ID: randomHex(16), UserID: u.ID, Hostname: "mine.example.com", Token: randomHex(24), VerifiedAt: &now, SSLStatus: "ready"}
	if err := s.DB.Create(&d).Error; err != nil {
		t.Fatal(err)
	}
	if preview() != "https://mine.example.com/r/copy-route" {
		t.Fatal("custom domain not selectable")
	}
	requireCode(t, call(t, s, "DELETE", "/api/domains/"+d.ID, nil, cookie, csrf), 200)
	if preview() != "" {
		t.Fatal("removed custom domain still selected")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "https://mine.example.com/r/copy-route", nil))
	requireCode(t, w, 404)
}
