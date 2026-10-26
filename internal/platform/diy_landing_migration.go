package platform

import (
	"gorm.io/gorm"
	"time"
)

const diyLandingMigrationVersion = 24

func migrateDIYLandings(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = ?", diyLandingMigrationVersion).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if err := tx.AutoMigrate(&DIYLandingPage{}, &DIYLandingRevision{}, &DIYLandingBinding{}, &DIYLandingPreview{}); err != nil {
		return err
	}
	return tx.Create(&Migration{Version: diyLandingMigrationVersion, AppliedAt: time.Now().UTC()}).Error
}
