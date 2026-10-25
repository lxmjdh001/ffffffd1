package platform

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestDownloadDomainUsageFiltersListsAndKeepsOldLinks(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, Dollar)
	cookie, csrf := sessionFor(t, s, u)
	rules := []DownloadDomainRule{
		{URL: "https://s.d1url.com", Audience: "all", Usage: "traffic"},
		{URL: "https://www.d1url.com", Audience: "all", Usage: "traffic"},
		{URL: "https://facebook-google-down.godownd1.com", Audience: "all"},
		{URL: "https://tiktok-google-down.godownd1.com", Audience: "all", Usage: "all"},
	}
	x, _ := getSettings(s.DB)
	x.DownloadDomainRules = rules
	if e := s.DB.Model(&x).Select("DownloadDomainRules").Updates(&x).Error; e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		path string
		want int
	}{{"/api/download-domains", 2}, {"/api/download-domains?usage=all", 2}, {"/api/download-domains?usage=traffic", 4}} {
		w := call(t, s, "GET", tc.path, nil, cookie, "")
		requireCode(t, w, 200)
		var out struct {
			Data []availableDomain `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		if len(out.Data) != tc.want {
			t.Fatalf("%s: got %d domains", tc.path, len(out.Data))
		}
		if tc.want == 2 && out.Data[0].URL != rules[2].URL {
			t.Fatal("restricted short domain leaked")
		}
	}
	requireCode(t, call(t, s, "GET", "/api/download-domains?usage=invalid", nil, cookie, ""), 400)
	if x.assignedDownloadURL("free") != rules[2].URL {
		t.Fatal("default application domain used traffic-only host")
	}
	requireCode(t, call(t, s, "PATCH", "/api/download-domains/note", map[string]string{"url": rules[0].URL, "remark": "分流短链"}, cookie, csrf), 200)
	// Scope changes affect discovery only, never existing host authorization.
	s.Config.Env = "production"
	s.Config.PublicURL = "https://control.example.com"
	app := seedApp(t, s, u, 128)
	for _, rule := range rules[:2] {
		r := httptest.NewRequest("GET", rule.URL+"/a/"+app.Slug+"/info", nil)
		r.RemoteAddr = "203.0.113.45:1234"
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		requireCode(t, w, 200)
	}
}

func TestDownloadDomainUsageMigrationIsOneTimeAndAdminEditable(t *testing.T) {
	s := meteredServer(t, 0)
	x, _ := getSettings(s.DB)
	x.DownloadDomainRules = []DownloadDomainRule{{URL: "https://s.d1url.com", Audience: "all"}, {URL: "https://www.d1url.com", Audience: "all"}, {URL: "https://other.example", Audience: "all"}}
	s.DB.Model(&x).Select("DownloadDomainRules").Updates(&x)
	s.DB.Where("version = ?", 22).Delete(&Migration{})
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	x, _ = getSettings(s.DB)
	if x.DownloadDomainRules[0].Usage != "traffic" || x.DownloadDomainRules[1].Usage != "traffic" || x.DownloadDomainRules[2].Usage != "" {
		t.Fatal("incorrect migration classification")
	}
	admin := seedUser(t, s, 0)
	s.DB.Model(&admin).Update("role", "admin")
	cookie, csrf := sessionFor(t, s, admin)
	x.DownloadDomainRules[0].Usage = "all"
	requireCode(t, call(t, s, "PUT", "/api/admin/settings", map[string]any{"section": "domains", "settings": x}, cookie, csrf), 200)
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	after, _ := getSettings(s.DB)
	if after.DownloadDomainRules[0].Usage != "all" {
		t.Fatal("restart overwrote administrator scope")
	}
	x.DownloadDomainRules[0].Usage = "not-valid"
	requireCode(t, call(t, s, "PUT", "/api/admin/settings", map[string]any{"section": "domains", "settings": x}, cookie, csrf), 400)
}

func TestCustomerDomainsStayGlobalWithTrafficOnlyPublicDomains(t *testing.T) {
	s := meteredServer(t, 0)
	u, other := seedUser(t, s, 0), seedUser(t, s, 0)
	x, _ := getSettings(s.DB)
	x.DownloadDomainRules = []DownloadDomainRule{{URL: "https://s.d1url.com", Audience: "all", Usage: "traffic"}}
	if e := s.DB.Model(&x).Select("DownloadDomainRules").Updates(&x).Error; e != nil {
		t.Fatal(e)
	}
	now := s.Now()
	for _, d := range []Domain{
		{ID: randomHex(16), UserID: u.ID, Hostname: "customer.example.com", VerifiedAt: &now},
		{ID: randomHex(16), UserID: other.ID, Hostname: "other.example.com", VerifiedAt: &now},
	} {
		if e := s.DB.Create(&d).Error; e != nil {
			t.Fatal(e)
		}
	}
	cookie, _ := sessionFor(t, s, u)
	for _, usage := range []string{"all", "traffic"} {
		w := call(t, s, "GET", "/api/download-domains?usage="+usage, nil, cookie, "")
		requireCode(t, w, 200)
		var out struct {
			Data []availableDomain `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		found := false
		for _, d := range out.Data {
			if d.URL == "https://customer.example.com" && d.Source == "custom" {
				found = true
			}
			if d.URL == "https://other.example.com" {
				t.Fatal("another customer's domain leaked")
			}
			if usage == "all" && d.URL == "https://s.d1url.com" {
				t.Fatal("traffic-only public domain leaked")
			}
		}
		if !found {
			t.Fatalf("customer-owned domain missing for %s", usage)
		}
	}
}
