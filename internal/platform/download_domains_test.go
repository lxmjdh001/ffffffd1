package platform

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func saveDownloadDomainSettings(t *testing.T, s *Server, x Settings) *httptest.ResponseRecorder {
	t.Helper()
	admin := seedUser(t, s, 0)
	if err := s.DB.Model(&admin).Update("role", "admin").Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := sessionFor(t, s, admin)
	var plans []Plan
	if err := s.DB.Find(&plans).Error; err != nil {
		t.Fatal(err)
	}
	return call(t, s, "PUT", "/api/admin/settings", map[string]any{"settings": x, "plans": plans}, cookie, csrf)
}

func assignedDomainFor(t *testing.T, s *Server, u User) string {
	t.Helper()
	cookie, _ := sessionFor(t, s, u)
	w := call(t, s, "GET", "/api/download-domain", nil, cookie, "")
	requireCode(t, w, 200)
	var out struct {
		Base string `json:"download_base_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Base
}

func TestManagedDownloadDomainAssignmentWithoutControlFallback(t *testing.T) {
	s := testServer(t)
	free := seedUser(t, s, 0)
	paid := seedUser(t, s, 0)
	until := s.Now().Add(time.Hour)
	if err := s.DB.Model(&paid).Update("pro_until", until).Error; err != nil {
		t.Fatal(err)
	}
	if assignedDomainFor(t, s, free) != "" {
		t.Fatal("unconfigured download domain fell back to website")
	}
	w := call(t, s, "GET", "/api/config", nil, nil, "")
	var config struct {
		Base string `json:"download_base_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &config); err != nil || config.Base != "" {
		t.Fatal("public config exposed a control-host fallback")
	}
	x, err := getSettings(s.DB)
	if err != nil {
		t.Fatal(err)
	}
	x.DownloadDomainMode = "separate"
	x.FreeDownloadBaseURL = "FREE.download.example/"
	x.PaidDownloadBaseURL = "https://paid.download.example"
	requireCode(t, saveDownloadDomainSettings(t, s, x), 200)
	if assignedDomainFor(t, s, free) != "https://free.download.example" || assignedDomainFor(t, s, paid) != "https://paid.download.example" {
		t.Fatal("tier-specific domains were not assigned")
	}
	if err := s.DB.Model(&paid).Update("pro_until", s.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if assignedDomainFor(t, s, paid) != "https://free.download.example" {
		t.Fatal("expired member kept the paid assignment")
	}
	x, err = getSettings(s.DB)
	if err != nil {
		t.Fatal(err)
	}
	x.DownloadDomainMode = "unified"
	x.DownloadBaseURL = "https://all.download.example"
	requireCode(t, saveDownloadDomainSettings(t, s, x), 200)
	if assignedDomainFor(t, s, free) != x.DownloadBaseURL || assignedDomainFor(t, s, paid) != x.DownloadBaseURL {
		t.Fatal("unified assignment did not cover every member")
	}
	x.DownloadDomainMode = "separate"
	x.FreeDownloadBaseURL = ""
	requireCode(t, saveDownloadDomainSettings(t, s, x), 200)
	if assignedDomainFor(t, s, free) != "" {
		t.Fatal("empty free domain fell back to an inactive unified domain")
	}
}

func TestManagedDownloadDomainRoutingAndCertificateAuthorization(t *testing.T) {
	s := testServer(t)
	free := seedUser(t, s, 0)
	paid := seedUser(t, s, 0)
	if err := s.DB.Model(&paid).Update("pro_until", s.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	freeApp := seedApp(t, s, free, 10)
	paidApp := seedApp(t, s, paid, 10)
	x, err := getSettings(s.DB)
	if err != nil {
		t.Fatal(err)
	}
	x.DownloadDomainMode = "separate"
	x.DownloadBaseURL = "https://all.download.example"
	x.FreeDownloadBaseURL = "https://free.download.example"
	x.PaidDownloadBaseURL = "https://paid.download.example"
	if err := s.DB.Save(&x).Error; err != nil {
		t.Fatal(err)
	}
	probe := func(host string, a Application, want int) {
		t.Helper()
		r := httptest.NewRequest("HEAD", "https://"+host+"/a/"+a.Slug, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		requireCode(t, w, want)
	}
	probe("free.download.example", freeApp, 200)
	probe("paid.download.example", paidApp, 200)
	probe("free.download.example", paidApp, 404)
	probe("paid.download.example", freeApp, 404)
	probe("all.download.example", freeApp, 404)
	asks := func(host string, want int) {
		t.Helper()
		r := httptest.NewRequest("GET", "http://localhost:8088/internal/tls/ask?domain="+host, nil)
		r.RemoteAddr = "127.0.0.1:10000"
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		requireCode(t, w, want)
	}
	asks("free.download.example", 204)
	asks("paid.download.example", 204)
	asks("all.download.example", 404)
	asks("unknown.download.example", 404)
	if err := s.DB.Model(&paid).Update("pro_until", s.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	probe("paid.download.example", paidApp, 404)
	probe("free.download.example", paidApp, 200)
	x.DownloadDomainMode = "unified"
	if err := s.DB.Save(&x).Error; err != nil {
		t.Fatal(err)
	}
	probe("all.download.example", freeApp, 200)
	probe("all.download.example", paidApp, 200)
	asks("all.download.example", 204)
	asks("free.download.example", 404)
	s.Config.Env = "production"
	s.Config.PublicURL = "https://console.example.com"
	probe("console.example.com", freeApp, 404)
}

func TestManagedDownloadDomainsRejectUnsafeAndClaimedHosts(t *testing.T) {
	s := testServer(t)
	s.Config.PublicURL = "https://console.example.com"
	for _, raw := range []string{"console.example.com", "https://www.console.example.com", "http://download.example", "https://download.example/path", "https://download.example:443", "https://download.example?x=1", "https://download.example/#fragment", "https://user:pass@download.example", "https://127.0.0.1", "localhost"} {
		if _, err := s.normalizeDownloadURL(raw); err == nil {
			t.Errorf("unsafe download origin accepted: %s", raw)
		}
	}
	x, err := getSettings(s.DB)
	if err != nil {
		t.Fatal(err)
	}
	x.DownloadDomainMode = "invalid"
	requireCode(t, saveDownloadDomainSettings(t, s, x), 400)
	x.DownloadDomainMode = "separate"
	x.DownloadBaseURL = "https://shared.download.example"
	x.FreeDownloadBaseURL = "https://free.download.example"
	x.PaidDownloadBaseURL = "https://paid.download.example"
	requireCode(t, saveDownloadDomainSettings(t, s, x), 200)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	for _, host := range []string{"shared.download.example", "free.download.example", "paid.download.example"} {
		requireCode(t, call(t, s, "POST", "/api/domains", map[string]string{"hostname": host}, cookie, csrf), 400)
	}
	if err := s.DB.Create(&Domain{ID: randomHex(16), UserID: u.ID, Hostname: "customer.download.example", Token: randomHex(24)}).Error; err != nil {
		t.Fatal(err)
	}
	x.PaidDownloadBaseURL = "https://customer.download.example"
	requireCode(t, saveDownloadDomainSettings(t, s, x), 409)
	if _, err := s.normalizeDownloadURL("HTTPS://DOWNload.example/"); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadDomainMigrationKeepsExistingUnifiedDomain(t *testing.T) {
	s := testServer(t)
	if err := s.DB.Model(&Settings{}).Where("id = 1").Update("download_base_url", "https://legacy.download.example").Error; err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"download_domain_mode", "free_download_base_url", "paid_download_base_url"} {
		if err := s.DB.Migrator().DropColumn(&Settings{}, column); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DB.Delete(&Migration{}, "version = 5").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	x, err := getSettings(s.DB)
	if err != nil || x.DownloadDomainMode != "unified" || x.DownloadBaseURL != "https://legacy.download.example" {
		t.Fatal("migration lost the existing download domain")
	}
	x.DownloadDomainMode = "separate"
	x.FreeDownloadBaseURL = "https://free.download.example"
	if err := s.DB.Save(&x).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	x, err = getSettings(s.DB)
	if err != nil || x.DownloadDomainMode != "separate" || x.FreeDownloadBaseURL != "https://free.download.example" {
		t.Fatal("restart reset the domain assignment")
	}
}
