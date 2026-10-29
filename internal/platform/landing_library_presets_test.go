package platform

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestLandingPresetsUseExistingRenderingAndEditableContent(t *testing.T) {
	s := testServer(t)
	admin := landingLibraryAdmin(t, s)
	result, err := InstallLandingPresets(context.Background(), s.DB, admin.Email)
	if err != nil || len(result) != 3 {
		t.Fatalf("install: %v, %v", result, err)
	}
	member := seedUser(t, s, 0)
	for _, item := range result {
		if !item.Created {
			t.Fatal("new preset was not installed")
		}
		var row LandingLibraryTemplate
		if err := s.DB.First(&row, "id = ?", item.ID).Error; err != nil {
			t.Fatal(err)
		}
		if !row.Enabled || row.Version != 1 || row.Config.PlatformButton || !diySupports(row.Config, "apk") || !diySupports(row.Config, "pwa") {
			t.Fatalf("invalid preset configuration: %s", row.Name)
		}
		prepared, issues, err := prepareDIYHTML(row.HTML, row.Config)
		if err != nil || len(issues) != 0 || strings.Contains(prepared, "{{LOGO}}") {
			t.Fatalf("render %s: issues=%v error=%v", row.Name, issues, err)
		}
		fields, err := diyContentFields(prepared, row.Config)
		if err != nil {
			t.Fatal(err)
		}
		image, title, button := false, false, false
		for _, field := range fields {
			image = image || field.Kind == "image" && validDIYContentImage(field.Value)
			title = title || field.Kind == "title"
			button = button || field.ID == "primary-label"
		}
		if !image || !title || !button {
			t.Fatalf("missing editable logo, heading or action: %s", row.Name)
		}
		ref := favoriteLandingLibraryFixture(t, s, member, row.ID)
		for _, kind := range []string{"apk", "pwa"} {
			if _, err := loadDIYRevision(s.DB, member.ID, kind, &ref); err != nil {
				t.Fatalf("preset cannot bind to %s: %v", kind, err)
			}
		}
		var cover LandingCover
		if err := s.DB.First(&cover, "hash = ?", row.CoverHash).Error; err != nil || cover.State != "pending" {
			t.Fatalf("cover not queued: %v", err)
		}
	}
}

func TestLandingPresetReinstallPreservesCatalogEditsAndDeletedEntries(t *testing.T) {
	s := testServer(t)
	admin := landingLibraryAdmin(t, s)
	items, err := InstallLandingPresets(context.Background(), s.DB, admin.Email)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&LandingLibraryTemplate{}).Where("id = ?", items[0].ID).Updates(map[string]any{"name": "管理员编辑", "html": "<h1>Edited</h1>", "enabled": false, "version": 8}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Delete(&LandingLibraryTemplate{}, "id = ?", items[1].ID).Error; err != nil {
		t.Fatal(err)
	}
	again, err := InstallLandingPresets(context.Background(), s.DB, admin.Email)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range again {
		if result.Created {
			t.Fatal("reinstall recreated an existing preset")
		}
	}
	var edited LandingLibraryTemplate
	s.DB.First(&edited, "id = ?", items[0].ID)
	if edited.Name != "管理员编辑" || edited.HTML != "<h1>Edited</h1>" || edited.Enabled || edited.Version != 8 {
		t.Fatal("reinstall overwrote administrator changes")
	}
	var count int64
	s.DB.Model(&LandingLibraryTemplate{}).Count(&count)
	if count != 2 {
		t.Fatal("reinstall resurrected deleted template")
	}
}

func TestLandingPresetInstallerRejectsNonAdministrators(t *testing.T) {
	s := testServer(t)
	member := seedUser(t, s, 0)
	for _, account := range []string{"", "missing@example.test", member.Email} {
		if _, err := InstallLandingPresets(context.Background(), s.DB, account); err == nil {
			t.Fatal("installer accepted a missing or unprivileged administrator")
		}
	}
	admin := landingLibraryAdmin(t, s)
	s.DB.Model(&admin).Update("disabled", true)
	if _, err := InstallLandingPresets(context.Background(), s.DB, admin.Email); err == nil {
		t.Fatal("installer accepted a disabled administrator")
	}
	var count int64
	s.DB.Model(&LandingLibraryTemplate{}).Count(&count)
	if count != 0 {
		t.Fatal("rejected installs changed the catalog")
	}
}

func TestLandingPresetQuotaFailureRollsBackTheEntireBundle(t *testing.T) {
	s := testServer(t)
	admin := landingLibraryAdmin(t, s)
	rows := make([]LandingLibraryTemplate, 499)
	for index := range rows {
		rows[index] = LandingLibraryTemplate{ID: hashString(fmt.Sprint("existing-", index))[:32], AdminID: admin.ID, Name: "Existing", Config: DIYLandingOptions{Targets: []string{"apk"}}, Version: 1}
	}
	if err := s.DB.CreateInBatches(rows, 50).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := InstallLandingPresets(context.Background(), s.DB, admin.Email); err == nil {
		t.Fatal("installer ignored the shared catalog quota")
	}
	var templates, covers int64
	s.DB.Model(&LandingLibraryTemplate{}).Count(&templates)
	s.DB.Model(&LandingCover{}).Count(&covers)
	if templates != 499 || covers != 0 {
		t.Fatal("quota failure left partially installed presets or covers")
	}
}

func legacyPresetFixture(t *testing.T, s *Server, admin User) []landingPreset {
	t.Helper()
	presets := append([]landingPreset(nil), landingPresets...)
	for index := range presets {
		preset := &presets[index]
		raw := "<h1>Original " + preset.Key + "</h1><button data-d1-action=\"primary\">旧安装按钮</button>"
		config := DIYLandingOptions{Targets: []string{"apk", "pwa"}}
		render, _, err := prepareDIYHTML(raw, config)
		if err != nil {
			t.Fatal(err)
		}
		cover, err := ensureLandingCover(s.DB, landingCoverInput(raw, config, preset.Name), s.Now())
		if err != nil {
			t.Fatal(err)
		}
		preset.PreviousHTMLHash = hashString(raw)
		row := LandingLibraryTemplate{ID: hashString("d1apk:landing-preset:20261029:" + preset.Key)[:32], AdminID: admin.ID, Name: preset.Name, HTML: raw, RenderHTML: render, Config: config, Version: 1, Enabled: true, StorageBytes: int64(len(raw) + len(render)), CoverHash: cover}
		if err := s.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return presets
}

func TestLandingPresetEnglishUpgradePreservesCollectedVersionsAndIsIdempotent(t *testing.T) {
	s := testServer(t)
	admin, member := landingLibraryAdmin(t, s), seedUser(t, s, 0)
	presets := legacyPresetFixture(t, s, admin)
	id := hashString("d1apk:landing-preset:20261029:" + presets[0].Key)[:32]
	ref := favoriteLandingLibraryFixture(t, s, member, id)
	result, err := installLandingPresets(context.Background(), s.DB, admin.Email, true, presets)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range result {
		var row LandingLibraryTemplate
		if err := s.DB.First(&row, "id = ?", item.ID).Error; err != nil || !item.Updated || item.Created || row.Version != 2 || !strings.Contains(row.HTML, `lang="en"`) {
			t.Fatalf("English upgrade failed: %v", err)
		}
	}
	old, err := loadDIYRevision(s.DB, member.ID, "pwa", &ref)
	if err != nil || !strings.Contains(old.RenderHTML, "旧安装按钮") {
		t.Fatalf("upgrade changed a customer's immutable snapshot: %v", err)
	}
	audits := countRows(t, s, &Audit{})
	again, err := installLandingPresets(context.Background(), s.DB, admin.Email, true, presets)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range again {
		if item.Updated || item.Created {
			t.Fatal("second upgrade changed the catalog")
		}
	}
	if countRows(t, s, &Audit{}) != audits {
		t.Fatal("idempotent upgrade appended duplicate audit records")
	}
}

func TestLandingPresetUpgradeConflictRollsBackAllTemplates(t *testing.T) {
	s := testServer(t)
	admin := landingLibraryAdmin(t, s)
	presets := legacyPresetFixture(t, s, admin)
	second := hashString("d1apk:landing-preset:20261029:" + presets[1].Key)[:32]
	if err := s.DB.Model(&LandingLibraryTemplate{}).Where("id = ?", second).Updates(map[string]any{"version": 2, "name": "My custom template"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := installLandingPresets(context.Background(), s.DB, admin.Email, true, presets); err == nil {
		t.Fatal("upgrade overwrote an administrator's edits")
	}
	var first LandingLibraryTemplate
	id := hashString("d1apk:landing-preset:20261029:" + presets[0].Key)[:32]
	if err := s.DB.First(&first, "id = ?", id).Error; err != nil || first.Version != 1 || hashString(first.HTML) != presets[0].PreviousHTMLHash {
		t.Fatal("conflicted upgrade partially committed")
	}
	var coverCount int64
	s.DB.Model(&LandingCover{}).Count(&coverCount)
	if coverCount != 3 {
		t.Fatal("conflicted upgrade left new cover jobs")
	}
	if err := s.DB.Model(&LandingLibraryTemplate{}).Where("id = ?", second).Updates(map[string]any{"version": 1, "deleted_at": gorm.DeletedAt{Time: s.Now(), Valid: true}}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := installLandingPresets(context.Background(), s.DB, admin.Email, true, presets); err == nil {
		t.Fatal("upgrade restored a deleted preset")
	}
}
