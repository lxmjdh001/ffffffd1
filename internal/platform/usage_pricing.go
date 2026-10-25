package platform

import (
	"time"

	"gorm.io/gorm"
)

const usagePricingVersion = 21

func freeUsageKind(kind string) bool {
	return kind == "cloak_uv" || kind == "return_flow" || kind == "pwa_push"
}

// One-way cutover: no retroactive refunds or top-ups. Existing completed
// charges and cash balances remain audit records; only unused count gifts end.
func migrateUsagePricing(tx *gorm.DB) error {
	var count int64
	if e := tx.Model(&Migration{}).Where("version = ?", usagePricingVersion).Count(&count).Error; e != nil {
		return e
	}
	if count > 0 {
		return nil
	}
	if e := tx.AutoMigrate(&User{}, &Settings{}, &Ledger{}, &MeteredEvent{}); e != nil {
		return e
	}
	if e := tx.Model(&Settings{}).Where("id = 1").Updates(map[string]any{"download_price_micros": 50000, "pwa_install_price_micros": 50000, "cloak_uv_price_micros": 0, "return_flow_price_micros": 0, "pwa_push_price_micros": 0, "registration_gift_limit": 0, "registration_gift_micros": 100 * Dollar}).Error; e != nil {
		return e
	}
	if e := tx.Model(&DownloadGrant{}).Where("\"limit\" > used + reserved").UpdateColumn("limit", gorm.Expr("used + reserved")).Error; e != nil {
		return e
	}
	for {
		var events []MeteredEvent
		if e := tx.Where("state = ? AND kind IN ?", "pending", []string{"cloak_uv", "return_flow", "pwa_push"}).Limit(500).Find(&events).Error; e != nil {
			return e
		}
		if len(events) == 0 {
			break
		}
		for _, event := range events {
			u, e := lockUser(tx, event.UserID)
			if e != nil {
				return e
			}
			if e = tx.First(&event, "id = ?", event.ID).Error; e != nil {
				return e
			}
			if e = settleMetered(tx, &u, &event, time.Now().UTC(), false); e != nil {
				return e
			}
		}
	}
	return tx.Create(&Migration{Version: usagePricingVersion, AppliedAt: time.Now().UTC()}).Error
}

func registrationUsageGift(tx *gorm.DB, u *User, x Settings, now time.Time) error {
	if x.RegistrationGiftMicros <= 0 {
		return nil
	}
	u.GiftBalanceMicros = x.RegistrationGiftMicros
	if e := tx.Model(u).Update("gift_balance_micros", u.GiftBalanceMicros).Error; e != nil {
		return e
	}
	return tx.Create(&Ledger{ID: randomHex(16), UserID: u.ID, Reference: "registration-gift:" + u.ID, Kind: "registration_gift", BalanceType: "gift", AmountMicros: x.RegistrationGiftMicros, BalanceMicros: u.GiftBalanceMicros, Note: "注册赠送用量额度", CreatedAt: now}).Error
}
