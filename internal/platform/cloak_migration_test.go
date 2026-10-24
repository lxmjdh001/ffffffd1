package platform

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloakFilterMigrationPreservesLegacyRules(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	old := createTestCloak(t, s, owner, cloakFixture())
	// Reproduce the table before these two optional filters existed.
	for _, column := range []string{"block_empty_language", "blocked_browsers"} {
		if err := s.DB.Migrator().DropColumn(&Cloak{}, column); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := Migrate(s.DB); err != nil {
			t.Fatal(err)
		}
	}
	var migrated Cloak
	if err := s.DB.First(&migrated, "id = ?", old.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migrated.BlockEmptyLanguage || len(migrated.BlockedBrowsers) != 0 || migrated.Slug != old.Slug || migrated.TargetURL != old.TargetURL || migrated.SafeURL != old.SafeURL || len(migrated.BlockedLanguages) != 1 || migrated.BlockedLanguages[0] != "zh" {
		t.Fatalf("migration changed a legacy rule: %+v", migrated)
	}
	for _, language := range []string{"", "en-US", "zh-CN"} {
		r := httptest.NewRequest("GET", old.EntryURL, nil)
		r.Header.Set("Accept-Language", language)
		r.Header.Set("User-Agent", "Mozilla/5.0 Chrome/130.0 Safari/537.36 Edg/130.0")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		requireCode(t, w, 302)
		if language == "zh-CN" {
			if w.Header().Get("Location") != old.SafeURL {
				t.Fatal("migration lost an existing language restriction")
			}
		} else if !strings.HasPrefix(w.Header().Get("Location"), "https://target.example/path?") {
			t.Fatal("migration enabled new filters on a legacy rule")
		}
	}
	input := cloakFixture()
	input["block_empty_language"], input["blocked_browsers"] = true, []string{"edge"}
	current := createTestCloak(t, s, owner, input)
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	var saved Cloak
	if err := s.DB.First(&saved, "id = ?", current.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !saved.BlockEmptyLanguage || len(saved.BlockedBrowsers) != 1 || saved.BlockedBrowsers[0] != "edge" || countRows(t, s, &Cloak{}) != 2 {
		t.Fatal("repeated migration lost new settings or changed the number of rules")
	}
}
