package platform

import (
	"context"
	"fmt"
	"strings"
	"testing"
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
