package platform

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloakCountryAllowlistMatching(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cloak   Cloak
		country string
		blocked bool
	}{
		{"selected country", Cloak{AllowedCountries: []string{"US", "CN"}}, "CN", false},
		{"unselected country", Cloak{AllowedCountries: []string{"US"}}, "CN", true},
		{"unknown not selected", Cloak{AllowedCountries: []string{"US"}}, "XX", true},
		{"unknown explicitly allowed", Cloak{AllowedCountries: []string{"US", "XX"}}, "XX", false},
		{"Tor not selected", Cloak{AllowedCountries: []string{"US", "XX"}}, "T1", true},
		{"Tor explicitly allowed", Cloak{AllowedCountries: []string{"T1"}}, "T1", false},
		{"empty list unrestricted", Cloak{AllowedCountries: []string{}}, "CN", false},
		{"other rules still apply", Cloak{AllowedCountries: []string{"US"}, BlockedUAs: []string{"curl"}}, "US", true},
		{"legacy block preserved", Cloak{BlockedCountries: []string{"CN"}}, "CN", true},
		{"legacy nonmatching country", Cloak{BlockedCountries: []string{"CN"}}, "US", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "https://example.com/c/test", nil)
			r.Header.Set("User-Agent", "curl/8")
			if blocked := cloakBlocked(tc.cloak, r, tc.country); blocked != tc.blocked {
				t.Fatalf("blocked=%v want %v", blocked, tc.blocked)
			}
		})
	}
}

func TestCloakCountryAllowlistAPIAndTrustedRouting(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	for _, invalid := range [][]string{{"China"}, {""}, {"U1"}, make([]string, 301)} {
		body := cloakFixture()
		body["allowed_countries"] = invalid
		requireCode(t, call(t, s, "POST", "/api/cloaks", body, cookie, csrf), 400)
	}
	body := cloakFixture()
	body["blocked_languages"] = []string{}
	body["allowed_countries"] = []string{" us ", "US"}
	c := createTestCloak(t, s, owner, body)
	if len(c.AllowedCountries) != 1 || c.AllowedCountries[0] != "US" {
		t.Fatalf("allowlist not normalized: %v", c.AllowedCountries)
	}
	w := call(t, s, "GET", "/api/cloaks", nil, cookie, "")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), `"allowed_countries":["US"]`) {
		t.Fatal("saved allowlist missing from API response")
	}
	assertRouting := func(peer, country string, blocked bool) {
		t.Helper()
		r := httptest.NewRequest("GET", c.EntryURL, nil)
		r.RemoteAddr = peer
		r.Header.Set("CF-IPCountry", country)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		requireCode(t, w, 302)
		if got := w.Header().Get("Location") == c.SafeURL; got != blocked {
			t.Fatalf("peer=%s country=%s blocked=%v want %v", peer, country, got, blocked)
		}
	}
	assertRouting("173.245.48.5:443", "US", false)
	assertRouting("173.245.48.5:443", "CN", true)
	assertRouting("173.245.48.5:443", "", true)
	assertRouting("173.245.48.5:443", "T1", true)
	assertRouting("203.0.113.8:443", "US", true) // Untrusted headers cannot admit a country.
	delete(body, "allowed_countries")
	requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf), 200)
	assertRouting("173.245.48.5:443", "CN", true) // An older client must not clear the allowlist.
	body["allowed_countries"] = []string{"US", "XX", "T1"}
	requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf), 200)
	assertRouting("203.0.113.8:443", "US", false) // Classified as the explicitly allowed XX.
	assertRouting("173.245.48.5:443", "T1", false)
	body["allowed_countries"] = []string{}
	requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf), 200)
	assertRouting("173.245.48.5:443", "CN", false)
}

func TestCloakCountryAllowlistMigrationPreservesLegacyBlocks(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	body := cloakFixture()
	body["blocked_countries"] = []string{"CN"}
	c := createTestCloak(t, s, owner, body)
	if err := s.DB.Migrator().DropColumn(&Cloak{}, "allowed_countries"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(s.DB); err != nil {
			t.Fatal(err)
		}
	}
	var saved Cloak
	if err := s.DB.First(&saved, "id = ?", c.ID).Error; err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", c.EntryURL, nil)
	if len(saved.AllowedCountries) != 0 || !cloakBlocked(saved, r, "CN") || cloakBlocked(saved, r, "US") {
		t.Fatal("migration inverted a legacy country block")
	}
	cookie, csrf := sessionFor(t, s, owner)
	body["blocked_countries"] = []string{}
	body["allowed_countries"] = []string{"CN"}
	requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf), 200)
	if err := s.DB.First(&saved, "id = ?", c.ID).Error; err != nil {
		t.Fatal(err)
	}
	if cloakBlocked(saved, r, "CN") || !cloakBlocked(saved, r, "US") {
		t.Fatal("saving an allowlist did not replace the legacy block")
	}
}
