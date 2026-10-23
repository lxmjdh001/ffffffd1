package platform

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func activeDownloadGrant(db *gorm.DB, u User, p Plan, now time.Time) (*DownloadGrant, error) {
	if p.ID == "free" {
		if now.Before(u.CreatedAt) {
			return nil, nil
		}
		grant := DownloadGrant{ID: "free:" + u.ID, UserID: u.ID, PlanID: "free", Limit: p.DownloadLimit, StartsAt: u.CreatedAt}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&grant).Error; err != nil {
			return nil, err
		}
		if err := db.First(&grant, "id = ?", grant.ID).Error; err != nil {
			return nil, err
		}
		return &grant, nil
	}
	var grant DownloadGrant
	err := db.Where("user_id = ? AND plan_id = ? AND starts_at <= ? AND expires_at > ?", u.ID, p.ID, now, now).Order("starts_at DESC").First(&grant).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &grant, err
}

func settleGiftGrant(tx *gorm.DB, ticket Ticket, completed bool) error {
	if ticket.GrantID == "" { // Pre-migration tickets remain settleable.
		return nil
	}
	updates := map[string]any{"reserved": gorm.Expr("reserved - 1")}
	if completed {
		updates["used"] = gorm.Expr("used + 1")
	}
	result := tx.Model(&DownloadGrant{}).Where("id = ? AND user_id = ? AND reserved > 0", ticket.GrantID, ticket.UserID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("gift reservation unavailable")
	}
	return nil
}

func migrateMemberships(tx *gorm.DB, now time.Time) error {
	if err := tx.AutoMigrate(&DownloadGrant{}, &Ticket{}); err != nil {
		return err
	}
	for _, plan := range []Plan{
		{ID: "free", Name: "免费套餐", AppLimit: 10, DownloadLimit: 1000, StorageBytes: 10 << 30},
		{ID: "pro", Name: "尊享套餐", PriceMicros: 99 * Dollar, AppLimit: 500, DownloadLimit: 10000, StorageBytes: 30 << 30},
	} {
		if err := tx.Save(&plan).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(&Settings{}).Where("id = 1").Update("download_price_micros", 3000).Error; err != nil {
		return err
	}
	// Import prior gift usage and attach reservations to the same grant. Neither
	// an upgrade nor a restart may silently give an existing customer fresh gifts.
	var users []User
	if err := tx.Find(&users).Error; err != nil {
		return err
	}
	for _, u := range users {
		free := DownloadGrant{ID: "free:" + u.ID, UserID: u.ID, PlanID: "free", Limit: 1000, StartsAt: u.CreatedAt}
		if err := tx.Model(&Usage{}).Where("user_id = ? AND period = ?", u.ID, period(u.CreatedAt)).Select("COALESCE(SUM(free_used),0)").Scan(&free.Used).Error; err != nil {
			return err
		}
		active := free
		if u.PlanID(now) == "pro" {
			active = DownloadGrant{ID: "legacy:" + u.ID, UserID: u.ID, PlanID: "pro", Limit: 10000, StartsAt: now, ExpiresAt: minTime(nextMonth(now), *u.ProUntil)}
			if err := tx.Model(&Usage{}).Where("user_id = ? AND period = ?", u.ID, period(now)).Select("COALESCE(SUM(free_used),0)").Scan(&active.Used).Error; err != nil {
				return err
			}
			if err := tx.Create(&free).Error; err != nil {
				return err
			}
			for start := active.ExpiresAt; start.Before(*u.ProUntil); {
				next := DownloadGrant{ID: hashString("legacy:" + u.ID + ":" + start.Format(time.RFC3339Nano)), UserID: u.ID, PlanID: "pro", Limit: 10000, StartsAt: start, ExpiresAt: minTime(nextMonth(start), *u.ProUntil)}
				if err := tx.Create(&next).Error; err != nil {
					return err
				}
				start = next.ExpiresAt
			}
		}
		const pending = "user_id = ? AND free = ? AND reserved = ? AND charged = ? AND released = ?"
		if err := tx.Model(&Ticket{}).Where(pending, u.ID, true, true, false, false).Count(&active.Reserved).Error; err != nil {
			return err
		}
		if err := tx.Model(&Ticket{}).Where(pending, u.ID, true, true, false, false).Update("grant_id", active.ID).Error; err != nil {
			return err
		}
		if err := tx.Create(&active).Error; err != nil {
			return err
		}
	}
	return nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func migrateMembershipVersion(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = 4").Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return migrateDownloadDomains(tx)
	}
	now := time.Now().UTC()
	if err := migrateMemberships(tx, now); err != nil {
		return err
	}
	if err := tx.Create(&Migration{Version: 4, AppliedAt: now}).Error; err != nil {
		return err
	}
	return migrateDownloadDomains(tx)
}
