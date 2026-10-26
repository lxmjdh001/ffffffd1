package platform

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSupportContactSettings(t *testing.T) {
	s := testServer(t)
	admin := seedUser(t, s, 0)
	if err := s.DB.Model(&admin).Update("role", "admin").Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := sessionFor(t, s, admin)
	original, _ := getSettings(s.DB)
	draft := original
	draft.SupportURL = "  https://t.me/example_support?start=business#contact  "
	requireCode(t, call(t, s, "PUT", "/api/admin/settings", map[string]any{"section": "basic", "settings": draft}, cookie, csrf), 200)
	const want = "https://t.me/example_support?start=business#contact"
	saved, _ := getSettings(s.DB)
	if saved.SupportURL != want || saved.DownloadPriceMicros != original.DownloadPriceMicros {
		t.Fatal("support URL did not persist independently of billing settings")
	}
	public := call(t, s, "GET", "/api/config", nil, nil, "")
	requireCode(t, public, 200)
	var config map[string]any
	if err := json.Unmarshal(public.Body.Bytes(), &config); err != nil || config["support_url"] != want {
		t.Fatalf("public homepage cannot read configured support URL: %s", public.Body.String())
	}
	member := seedUser(t, s, 0)
	memberCookie, memberCSRF := sessionFor(t, s, member)
	requireCode(t, call(t, s, "PUT", "/api/admin/settings", map[string]any{"section": "basic", "settings": draft}, memberCookie, memberCSRF), 403)
	for _, invalid := range []string{"javascript:alert(1)", "data:text/html,hello", "/relative", "https://", "https://user:password@example.com", "https://example.com/a\nb", "https://example.com\\other", "https://example.com/" + strings.Repeat("x", 2048)} {
		draft.SupportURL = invalid
		requireCode(t, call(t, s, "PUT", "/api/admin/settings", map[string]any{"section": "basic", "settings": draft}, cookie, csrf), 400)
	}
	// Older clients omit the new field when saving the basic section.
	raw, _ := json.Marshal(saved)
	var legacy map[string]any
	_ = json.Unmarshal(raw, &legacy)
	delete(legacy, "support_url")
	requireCode(t, call(t, s, "PUT", "/api/admin/settings", map[string]any{"section": "basic", "settings": legacy}, cookie, csrf), 200)
	saved, _ = getSettings(s.DB)
	if saved.SupportURL != want {
		t.Fatal("older client cleared the support URL")
	}
	draft = saved
	draft.SupportURL = ""
	requireCode(t, call(t, s, "PUT", "/api/admin/settings", map[string]any{"section": "basic", "settings": draft}, cookie, csrf), 200)
	saved, _ = getSettings(s.DB)
	if saved.SupportURL != "" {
		t.Fatal("administrator could not clear the support URL")
	}
}

func TestSupportContactMigrationPreservesSettings(t *testing.T) {
	s := testServer(t)
	before, _ := getSettings(s.DB)
	if err := s.DB.Migrator().DropColumn(&Settings{}, "support_url"); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Delete(&Migration{}, "version = ?", homeContactMigrationVersion).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	after, _ := getSettings(s.DB)
	if after.SupportURL != "" || after.SiteName != before.SiteName || after.DownloadPriceMicros != before.DownloadPriceMicros || after.RegistrationGiftMicros != before.RegistrationGiftMicros {
		t.Fatal("support contact migration changed existing settings")
	}
	const want = "https://support.example.com/chat"
	if err := s.DB.Model(&Settings{}).Where("id = 1").Update("support_url", want).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	after, _ = getSettings(s.DB)
	if after.SupportURL != want {
		t.Fatal("repeated migration cleared configured support URL")
	}
}
