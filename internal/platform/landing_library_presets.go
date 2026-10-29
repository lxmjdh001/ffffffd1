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

type landingPreset struct{ Key, Name, Description, PreviousHTMLHash string }

var landingPresets = []landingPreset{
	{"daylight", "Daylight · Productivity", "A clear blue and white app page with an editable daily planner. Suitable for productivity, utility and everyday service apps.", "58922191faf561ee560efed07b7dd019d1d286e61c65a1ad6c9a974741dcb4a0"},
	{"mingle", "Mingle · Social", "A warm pink and lilac app page with conversation cards and lifestyle topics. Suitable for social, community and lifestyle apps.", "0c9c209a41e00c352f00604cc684758ba5fb54753a8447ff62229bb181e436c5"},
	{"orbit", "Orbit · Adventure", "A deep purple app page with an illustrated planet, scene previews and apricot install buttons. Suitable for games and entertainment.", "d5b9b3efaca82d015ea74dc6b2417dc151fb2f493fbe7d2e5805f8e9fa31c4a2"},
}

type LandingPresetInstallResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Created bool   `json:"created"`
	Updated bool   `json:"updated,omitempty"`
}

// InstallLandingPresets adds this named bundle atomically and idempotently.
// Stable IDs preserve subsequent administrator edits, unlisting and deletions.
// Callers must supply an existing enabled administrator's exact login account.
// It does not migrate the database, bootstrap accounts or run background jobs.
func InstallLandingPresets(ctx context.Context, db *gorm.DB, adminAccount string) ([]LandingPresetInstallResult, error) {
	return installLandingPresets(ctx, db, adminAccount, false, landingPresets)
}

// UpgradeLandingPresets updates only the original, unchanged v1 bundle. Edited,
// unlisted or deleted catalog entries cause the entire upgrade to be rejected.
// Collected snapshots and application bindings remain immutable.
func UpgradeLandingPresets(ctx context.Context, db *gorm.DB, adminAccount string) ([]LandingPresetInstallResult, error) {
	return installLandingPresets(ctx, db, adminAccount, true, landingPresets)
}

func installLandingPresets(ctx context.Context, db *gorm.DB, adminAccount string, upgrade bool, presets []landingPreset) ([]LandingPresetInstallResult, error) {
	if strings.TrimSpace(adminAccount) == "" {
		return nil, errors.New("an existing administrator account is required")
	}
	inputs := make([]landingLibraryInput, len(presets))
	renders := make([]string, len(presets))
	for index, preset := range presets {
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
		for index, preset := range presets {
			id := hashString("d1apk:landing-preset:20261029:" + preset.Key)[:32]
			var existing int64
			if err := tx.Unscoped().Model(&LandingLibraryTemplate{}).Where("id = ?", id).Count(&existing).Error; err != nil {
				return err
			}
			if existing != 0 {
				if !upgrade {
					results = append(results, LandingPresetInstallResult{ID: id, Name: preset.Name})
					continue
				}
				var row LandingLibraryTemplate
				if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
					return err
				}
				if row.DeletedAt.Valid || !row.Enabled {
					return fmt.Errorf("preset %s was unlisted or deleted; no templates changed", preset.Key)
				}
				in := inputs[index]
				if row.HTML == in.HTML && row.Name == in.Name && row.Description == in.Description {
					results = append(results, LandingPresetInstallResult{ID: id, Name: in.Name})
					continue
				}
				if row.Version != 1 || hashString(row.HTML) != preset.PreviousHTMLHash {
					return fmt.Errorf("preset %s has administrator edits; no templates changed", preset.Key)
				}
				config, _ := json.Marshal(in.Config)
				size := int64(len(in.HTML) + len(renders[index]) + len(config))
				if total-row.StorageBytes+size > 500<<20 {
					return errors.New("landing template catalog quota exceeded")
				}
				cover, err := ensureLandingCover(tx, landingCoverInput(in.HTML, in.Config, in.Name), now)
				if err != nil {
					return err
				}
				total += size - row.StorageBytes
				row.Name, row.Description, row.HTML, row.RenderHTML, row.Config = in.Name, in.Description, in.HTML, renders[index], in.Config
				row.CoverHash, row.CoverURL, row.StorageBytes, row.Version, row.UpdatedAt = cover, "", size, row.Version+1, now
				if err := tx.Save(&row).Error; err != nil {
					return err
				}
				if err := recordAudit(tx, locked.ID, "landing_library.save", id, "更新英文预设模版："+in.Name); err != nil {
					return err
				}
				results = append(results, LandingPresetInstallResult{ID: id, Name: in.Name, Updated: true})
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
