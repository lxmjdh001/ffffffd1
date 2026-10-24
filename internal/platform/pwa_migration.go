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
	if count != 0 {
		return nil
	}
	if err := tx.AutoMigrate(&PWAApp{}, &PWAPushSubscription{}, &PWAPushTask{}, &PWAPushDelivery{}); err != nil {
		return err
	}
	return tx.Create(&Migration{Version: 13, AppliedAt: time.Now().UTC()}).Error
}
