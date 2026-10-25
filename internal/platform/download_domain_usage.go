package platform

import (
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Categorize the operator's existing short domains once. Later administrator
// choices are authoritative; restarting never overwrites a saved category.
func migrateDownloadDomainUsage(tx *gorm.DB) error {
	var count int64
	if e := tx.Model(&Migration{}).Where("version = ?", 22).Count(&count).Error; e != nil {
		return e
	}
	if count > 0 {
		return nil
	}
	x, e := getSettings(tx)
	if e != nil {
		return e
	}
	changed := false
	for i := range x.DownloadDomainRules {
		rule := &x.DownloadDomainRules[i]
		parsed, e := url.Parse(rule.URL)
		if e != nil {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		if rule.Usage == "" && (host == "s.d1url.com" || host == "www.d1url.com") {
			rule.Usage = "traffic"
			changed = true
		}
	}
	if changed {
		if e := tx.Model(&x).Select("DownloadDomainRules").Updates(&x).Error; e != nil {
			return e
		}
	}
	return tx.Create(&Migration{Version: 22, AppliedAt: time.Now().UTC()}).Error
}
