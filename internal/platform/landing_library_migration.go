package platform

import (
	"gorm.io/gorm"
	"time"
)

const landingLibraryMigrationVersion = 25

func migrateLandingLibrary(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = ?", landingLibraryMigrationVersion).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if err := tx.AutoMigrate(&LandingLibraryTemplate{}, &LandingLibraryFavorite{}); err != nil {
		return err
	}
	return tx.Create(&Migration{Version: landingLibraryMigrationVersion, AppliedAt: time.Now().UTC()}).Error
}
