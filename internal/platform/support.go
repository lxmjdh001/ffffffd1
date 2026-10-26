package platform

import (
	"net/url"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"
)

const homeContactMigrationVersion = 28

func migrateHomeContact(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = ?", homeContactMigrationVersion).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if err := tx.AutoMigrate(&Settings{}); err != nil {
		return err
	}
	return tx.Create(&Migration{Version: homeContactMigrationVersion, AppliedAt: time.Now().UTC()}).Error
}

func normalizeSupportURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if len(raw) > 2048 || strings.IndexFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 || strings.Contains(raw, "\\") || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return "", problem(400, "请输入有效的客服链接（HTTP/HTTPS）")
	}
	return raw, nil
}
