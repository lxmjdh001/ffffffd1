package platform

import (
	"time"

	"gorm.io/gorm"
)

const diyContentMigrationVersion = 27

// Content belongs to an application's binding. Existing published revisions,
// shared favorites and all accounting records remain unchanged.
func migrateDIYContent(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = ?", diyContentMigrationVersion).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if err := tx.AutoMigrate(&DIYLandingBinding{}); err != nil {
		return err
	}
	return tx.Create(&Migration{Version: diyContentMigrationVersion, AppliedAt: time.Now().UTC()}).Error
}
