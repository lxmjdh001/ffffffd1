package platform

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func geoIPFixtureConfig() Config {
	return Config{GeoIPCountryDB: "testdata/geoip/GeoIP2-Country-Test.mmdb", GeoIPASNDB: "testdata/geoip/GeoLite2-ASN-Test.mmdb"}
}

func TestGeoIPFilesCountryASNAndIPv6(t *testing.T) {
	db, err := OpenGeoIP(geoIPFixtureConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, tc := range []struct{ ip, country string }{{"81.2.69.160", "GB"}, {"2001:218::", "JP"}, {"127.0.0.1", "XX"}, {"192.168.1.1", "XX"}, {"::1", "XX"}, {"not-an-ip", "XX"}} {
		if got := db.Country(net.ParseIP(tc.ip)); got != tc.country {
			t.Errorf("%s country=%s want %s", tc.ip, got, tc.country)
		}
	}
	if number, organization := db.ASN(net.ParseIP("1.0.0.1")); number == 0 || organization == "" {
		t.Fatal("ASN database did not return a network organization")
	}
	if info := db.Info(); info.Country == nil || info.ASN == nil || info.Country.Type != "GeoIP2-Country" || info.ASN.Type != "GeoLite2-ASN" {
		t.Fatal("missing validated database metadata")
	}
}

func TestGeoIPMissingDisabledAndWrongType(t *testing.T) {
	for _, c := range []Config{{DataDir: t.TempDir()}, {GeoIPCountryDB: "none", GeoIPASNDB: "none"}} {
		db, err := OpenGeoIP(c)
		if err != nil || db.Info().Country != nil || db.Country(net.ParseIP("81.2.69.160")) != "XX" {
			t.Fatal("optional databases must preserve header-only deployments")
		}
		_ = db.Close()
	}
	c := geoIPFixtureConfig()
	for _, invalid := range []Config{{GeoIPCountryDB: filepath.Join(t.TempDir(), "missing.mmdb")}, {GeoIPCountryDB: c.GeoIPASNDB}, {GeoIPCountryDB: c.GeoIPCountryDB, GeoIPASNDB: c.GeoIPCountryDB}} {
		if db, err := OpenGeoIP(invalid); err == nil {
			_ = db.Close()
			t.Fatal("missing or incorrectly typed explicit database was accepted")
		}
	}
}

func TestGeoIPCountryFallbackAuthenticatesVisitor(t *testing.T) {
	db, err := OpenGeoIP(geoIPFixtureConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, local, _ := net.ParseCIDR("127.0.0.1/32")
	s := &Server{GeoIP: db, Config: Config{TrustedProxies: []*net.IPNet{local}}}
	for _, tc := range []struct{ name, peer, xff, visitor, country, want string }{
		{"gray-cloud direct", "81.2.69.160:1234", "", "", "", "GB"},
		{"gray-cloud through nginx", "127.0.0.1:1234", "81.2.69.160", "", "", "GB"},
		{"forged country and CF visitor", "81.2.69.160:1234", "173.245.48.5", "2001:218::", "US", "GB"},
		{"nginx does not trust browser CF headers", "127.0.0.1:1234", "173.245.48.5, 81.2.69.160", "2001:218::", "US", "GB"},
		{"trusted CF country wins", "127.0.0.1:1234", "81.2.69.160, 173.245.48.5", "81.2.69.160", "US", "US"},
		{"CF missing country uses visitor", "127.0.0.1:1234", "81.2.69.160, 173.245.48.5", "81.2.69.160", "", "GB"},
		{"CF unknown country uses visitor", "173.245.48.5:1234", "", "81.2.69.160", "XX", "GB"},
		{"CF invalid country uses visitor", "173.245.48.5:1234", "", "81.2.69.160", "China", "GB"},
		{"CF Tor classification preserved", "173.245.48.5:1234", "", "81.2.69.160", "T1", "T1"},
		{"CF edge is not the visitor", "127.0.0.1:1234", "173.245.48.5", "", "", "XX"},
		{"broken forwarding chain", "127.0.0.1:1234", "173.245.48.5,invalid", "81.2.69.160", "US", "XX"},
		{"direct IPv6", "[2001:218::]:1234", "", "", "", "JP"},
		{"CF IPv6 visitor", "[2606:4700::1]:1234", "", "2001:218::", "", "JP"},
		{"private visitor", "127.0.0.1:1234", "10.0.0.1", "", "", "XX"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "https://example.com/api/config", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-For", tc.xff)
			r.Header.Set("CF-Connecting-IP", tc.visitor)
			r.Header.Set("CF-IPCountry", tc.country)
			if got := s.cloakRequestCountry(r); got != tc.want {
				t.Fatalf("country=%s want %s", got, tc.want)
			}
		})
	}
}

func TestGeoIPReloadRejectsCorruptionAndKeepsPrevious(t *testing.T) {
	c := geoIPFixtureConfig()
	data, err := os.ReadFile(c.GeoIPCountryDB)
	if err != nil {
		t.Fatal(err)
	}
	c.GeoIPCountryDB = filepath.Join(t.TempDir(), "Country.mmdb")
	if err := os.WriteFile(c.GeoIPCountryDB, data, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := OpenGeoIP(c)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	candidate := c.GeoIPCountryDB + ".new"
	if err := os.WriteFile(candidate, []byte("broken candidate database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(candidate, c.GeoIPCountryDB); err != nil {
		t.Fatal(err)
	}
	if err := db.Reload(c); err == nil || db.Country(net.ParseIP("81.2.69.160")) != "GB" {
		t.Fatal("corrupt update replaced the working database")
	}
	if err := db.Reload(Config{DataDir: t.TempDir()}); err == nil || db.Info().Country == nil {
		t.Fatal("missing update discarded the working database")
	}
}

func TestGeoIPConcurrentReloadAndClose(t *testing.T) {
	c := geoIPFixtureConfig()
	db, err := OpenGeoIP(c)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 500 {
				code := db.Country(net.ParseIP("81.2.69.160"))
				if code != "GB" && code != "XX" {
					t.Errorf("unexpected concurrent lookup result %q", code)
				}
			}
		})
	}
	for range 3 {
		if err := db.Reload(c); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if db.Country(net.ParseIP("81.2.69.160")) != "XX" || db.Reload(c) == nil {
		t.Fatal("closed database reopened or returned a stale result")
	}
	_ = db.Close()
}

func TestGeoIPCheckCommand(t *testing.T) {
	c := geoIPFixtureConfig()
	var out bytes.Buffer
	if err := CheckGeoIP([]string{"-country", c.GeoIPCountryDB, "-asn", c.GeoIPASNDB, "-iterations", "100", "81.2.69.160"}, &out); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Samples []struct{ Country string } `json:"samples"`
		Mean    int64                      `json:"country_lookup_mean_ns"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || len(result.Samples) != 1 || result.Samples[0].Country != "GB" || result.Mean < 1 {
		t.Fatal("diagnostic failed to verify and query the local database")
	}
}

func TestGeoIPFallbackControlsCountryRuleAndVisitRecord(t *testing.T) {
	s := testServer(t)
	db, err := OpenGeoIP(geoIPFixtureConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s.GeoIP = db
	owner := seedUser(t, s, 0)
	body := cloakFixture()
	body["allowed_countries"] = []string{"GB"}
	c := createTestCloak(t, s, owner, body)
	for _, tc := range []struct {
		peer, country string
		allowed       bool
	}{
		{"81.2.69.160:1234", "GB", true},
		{"[2001:218::]:1234", "JP", false},
	} {
		r := httptest.NewRequest("GET", c.EntryURL, nil)
		r.RemoteAddr = tc.peer
		r.Header.Set("CF-IPCountry", "US") // A direct browser cannot override its GeoIP result.
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		requireCode(t, w, 302)
		if blocked := w.Header().Get("Location") == c.SafeURL; blocked == tc.allowed {
			t.Fatal("country allowlist did not use the local fallback")
		}
		var visit AccessVisit
		if err := s.DB.Where("kind = ? AND resource_id = ? AND country = ?", "cloaks", c.ID, tc.country).First(&visit).Error; err != nil {
			t.Fatal("resolved country was not persisted in the visit record", err)
		}
	}
}

func BenchmarkGeoIPCountry(b *testing.B) {
	db, err := OpenGeoIP(geoIPFixtureConfig())
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	ip := net.ParseIP("81.2.69.160")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = db.Country(ip)
	}
}
