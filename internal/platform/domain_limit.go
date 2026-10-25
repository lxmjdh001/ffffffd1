package platform

import (
	"time"

	"gorm.io/gorm"
)

const standardDomainLimit int64 = 20

func migrateStandardDomainLimit(tx *gorm.DB) error {
	var n int64
	if e := tx.Model(&Migration{}).Where("version = ?", 23).Count(&n).Error; e != nil {
		return e
	}
	if n > 0 {
		return nil
	}
	if e := tx.Model(&Settings{}).Where("id = ?", 1).Update("domain_limit", standardDomainLimit).Error; e != nil {
		return e
	}
	return tx.Create(&Migration{Version: 23, AppliedAt: time.Now().UTC()}).Error
}
