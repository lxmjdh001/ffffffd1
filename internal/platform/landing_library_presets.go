package platform

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// These are editable catalog sources, not additional application runtimes.
// An explicit operator command installs them; normal startup never seeds or
// overwrites an administrator's catalog, collected snapshot or application.
//
//go:embed landing-presets/*.html
var landingPresetFiles embed.FS

type landingPreset struct{ Key, Name, Description string }

var landingPresets = []landingPreset{
	{"daylight", "雾蓝 · 效率工具", "雾蓝白底、日程面板与清晰的下载入口，适合效率、工具和日常服务应用。支持修改图标、标题、介绍及按钮文案。"},
	{"mingle", "莓粉 · 社交生活", "莓粉与浅紫搭配，聊天卡片与生活话题组成温暖的首屏，适合社交、社区和生活应用。支持 APK 与 PWA。"},
	{"orbit", "曜紫 · 游戏展示", "深紫星空、行星与场景插画，搭配醒目的杏色安装按钮，适合游戏、娱乐和探索主题应用。支持 APK 与 PWA。"},
}

type LandingPresetInstallResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Created bool   `json:"created"`
}

// InstallLandingPresets adds this named bundle atomically and idempotently.
// Stable IDs preserve subsequent administrator edits, unlisting and deletions.
// Callers must supply an existing enabled administrator's exact login account.
// It does not migrate the database, bootstrap accounts or run background jobs.
func InstallLandingPresets(ctx context.Context, db *gorm.DB, adminAccount string) ([]LandingPresetInstallResult, error) {
	if strings.TrimSpace(adminAccount) == "" {
		return nil, errors.New("an existing administrator account is required")
	}
	inputs := make([]landingLibraryInput, len(landingPresets))
	renders := make([]string, len(landingPresets))
	for index, preset := range landingPresets {
		raw, err := landingPresetFiles.ReadFile("landing-presets/" + preset.Key + ".html")
		if err != nil {
			return nil, err
		}
		enabled := true
		inputs[index] = landingLibraryInput{Name: preset.Name, Description: preset.Description, HTML: string(raw), Enabled: &enabled, Config: DIYLandingOptions{Targets: []string{"apk", "pwa"}, PlatformButton: false}}
		renders[index], err = validateLandingLibraryInput(&inputs[index])
		if err != nil {
			return nil, fmt.Errorf("preset %s: %w", preset.Key, err)
		}
	}
	results := make([]LandingPresetInstallResult, 0, len(inputs))
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var admin User
		if err := tx.Where("email = ?", adminAccount).First(&admin).Error; err != nil {
			return errors.New("administrator account does not exist")
		}
		locked, err := lockUser(tx, admin.ID)
		if err != nil {
			return err
		}
		if locked.Role != "admin" || locked.Disabled {
			return errors.New("an enabled administrator account is required")
		}
		var settings Settings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, "id = 1").Error; err != nil {
			return err
		}
		var count, total int64
		if err := tx.Model(&LandingLibraryTemplate{}).Count(&count).Error; err != nil {
			return err
		}
		if err := tx.Model(&LandingLibraryTemplate{}).Select("COALESCE(SUM(storage_bytes), 0)").Scan(&total).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		for index, preset := range landingPresets {
			id := hashString("d1apk:landing-preset:20261029:" + preset.Key)[:32]
			var existing int64
			if err := tx.Unscoped().Model(&LandingLibraryTemplate{}).Where("id = ?", id).Count(&existing).Error; err != nil {
				return err
			}
			if existing != 0 {
				results = append(results, LandingPresetInstallResult{ID: id, Name: preset.Name})
				continue
			}
			in := inputs[index]
			config, _ := json.Marshal(in.Config)
			size := int64(len(in.HTML) + len(renders[index]) + len(config))
			if count >= 500 || total+size > 500<<20 {
				return errors.New("landing template catalog quota exceeded")
			}
			cover, err := ensureLandingCover(tx, landingCoverInput(in.HTML, in.Config, in.Name), now)
			if err != nil {
				return err
			}
			row := LandingLibraryTemplate{ID: id, AdminID: locked.ID, Name: in.Name, Description: in.Description, HTML: in.HTML, RenderHTML: renders[index], Config: in.Config, Enabled: true, Version: 1, StorageBytes: size, CoverHash: cover, CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			if err := recordAudit(tx, locked.ID, "landing_library.save", id, "安装预设模版："+in.Name); err != nil {
				return err
			}
			count++
			total += size
			results = append(results, LandingPresetInstallResult{ID: id, Name: in.Name, Created: true})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}
