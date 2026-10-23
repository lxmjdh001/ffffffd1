package platform

import "testing"

func TestSettingsSectionSaveIgnoresOtherDrafts(t *testing.T) {
	s := testServer(t)
	admin := seedUser(t, s, 0)
	if err := s.DB.Model(&admin).Update("role", "admin").Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := sessionFor(t, s, admin)
	original, err := getSettings(s.DB)
	if err != nil {
		t.Fatal(err)
	}
	draft := original
	draft.SiteName = "局部保存测试"
	draft.DownloadPriceMicros = -1
	draft.DownloadDomainRules = []DownloadDomainRule{{URL: "https://download.example.com/invalid-path", Audience: "all"}}
	var plans []Plan
	s.DB.Find(&plans)
	for i := range plans {
		plans[i].AppLimit = -1
	}
	requireCode(t, call(t, s, "PUT", "/api/admin/settings", map[string]any{"section": "basic", "settings": draft, "plans": plans}, cookie, csrf), 200)
	saved, err := getSettings(s.DB)
	if err != nil {
		t.Fatal(err)
	}
	if saved.SiteName != draft.SiteName || saved.DownloadPriceMicros != original.DownloadPriceMicros || len(saved.DownloadDomainRules) != len(original.DownloadDomainRules) {
		t.Fatal("section save modified unrelated settings")
	}
	var free Plan
	s.DB.First(&free, "id = ?", "free")
	if free.AppLimit < 1 {
		t.Fatal("section save applied an unrelated plan draft")
	}
	draft = saved
	draft.DownloadPriceMicros = 1234
	draft.SiteName = ""
	requireCode(t, call(t, s, "PUT", "/api/admin/settings", map[string]any{"section": "billing", "settings": draft}, cookie, csrf), 200)
	saved, _ = getSettings(s.DB)
	if saved.DownloadPriceMicros != 1234 || saved.SiteName != "局部保存测试" {
		t.Fatal("billing section overwrote basic settings")
	}
}

func TestDomainRulesUpgradeRepairsExistingDatabase(t *testing.T) {
	s := testServer(t)
	if err := s.DB.Migrator().DropColumn(&Settings{}, "download_domain_rules"); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Delete(&Migration{}, "version = 7").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	if !s.DB.Migrator().HasColumn(&Settings{}, "download_domain_rules") {
		t.Fatal("missing new domain rule column after migration")
	}
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	requireCode(t, call(t, s, "POST", "/api/domains", map[string]string{"hostname": "migration-test.example.com"}, cookie, csrf), 201)
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	var n int64
	s.DB.Model(&Domain{}).Where("hostname = ?", "migration-test.example.com").Count(&n)
	if n != 1 {
		t.Fatal("migration lost domain data")
	}
}
