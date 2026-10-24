package platform

import (
	"time"

	"gorm.io/gorm"
)

// PWA applications and push state live in independent tables. Existing package,
// billing, referral, and authentication records are not rewritten.
func migratePWA(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = ?", 13).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		if err := tx.AutoMigrate(&PWAApp{}, &PWAPushSubscription{}, &PWAPushTask{}, &PWAPushDelivery{}); err != nil {
			return err
		}
		if err := tx.Create(&Migration{Version: 13, AppliedAt: time.Now().UTC()}).Error; err != nil {
			return err
		}
	}
	if err := migratePWAPushRichContent(tx); err != nil {
		return err
	}
	return migrateReturnFlow(tx)
}

func migratePWAPushRichContent(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = ?", 14).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	// Extend tasks in place: old tasks retain their content, scheduling and
	// delivery IDs. New actions default to an empty JSON array.
	if err := tx.AutoMigrate(&PWAPushTask{}, &PWAPushTemplate{}, &PWAPushAsset{}); err != nil {
		return err
	}
	return tx.Create(&Migration{Version: 14, AppliedAt: time.Now().UTC()}).Error
}

func migrateReturnFlow(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = ?", 15).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	if err := tx.AutoMigrate(&PWAApp{}, &ReturnFlowAsset{}); err != nil {
		return err
	}
	return tx.Create(&Migration{Version: 15, AppliedAt: time.Now().UTC()}).Error
}
