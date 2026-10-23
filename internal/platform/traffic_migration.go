package platform

import (
	"gorm.io/gorm"
	"time"
)

func migrateTrafficGroups(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = 6").Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if tx.Migrator().HasTable("traffic_pools") {
		var legacy []TrafficPool
		if err := tx.Table("traffic_pools").Find(&legacy).Error; err != nil {
			return err
		}
		for _, p := range legacy {
			p.Mode = "random"
			p.Name = "APK 分流"
			if err := tx.Create(&p).Error; err != nil {
				return err
			}
			var links []TrafficLink
			if err := tx.Where("user_id = ?", p.UserID).Order("id").Find(&links).Error; err != nil {
				return err
			}
			for i, l := range links {
				if err := tx.Model(&l).Updates(map[string]any{"pool_slug": p.Slug, "position": i}).Error; err != nil {
					return err
				}
			}
		}
	}
	return tx.Create(&Migration{Version: 6, AppliedAt: time.Now().UTC()}).Error
}
