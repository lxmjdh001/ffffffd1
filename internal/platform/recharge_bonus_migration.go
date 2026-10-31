package platform

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

func migrateRechargeTenThousandBonus(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = ?", 33).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	settings, err := getSettings(tx)
	if err != nil {
		return err
	}
	found := false
	for i := range settings.RechargeBonusTiers {
		if settings.RechargeBonusTiers[i].ThresholdMicros == 10000*Dollar {
			settings.RechargeBonusTiers[i].Percent = 80
			found = true
		}
	}
	if !found {
		settings.RechargeBonusTiers = append(settings.RechargeBonusTiers, RechargeTier{10000 * Dollar, 80})
	}
	tiers, err := json.Marshal(settings.RechargeBonusTiers)
	if err != nil {
		return err
	}
	// Update only future orders' promotion. Existing orders retain their saved
	// bonus, and the operator's lower tiers and campaign switch stay unchanged.
	if err := tx.Model(&Settings{}).Where("id = 1").Update("recharge_bonus_tiers", string(tiers)).Error; err != nil {
		return err
	}
	return tx.Create(&Migration{Version: 33, AppliedAt: time.Now().UTC()}).Error
}
