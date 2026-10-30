package platform

import (
	"errors"
	"net"
	"time"

	"gorm.io/gorm"
)

// Only adds reservation linkage. Previously charged UVs, balances, payment
// settings and in-flight legacy tickets keep their existing rights.
func migrateAPKDownloadBilling(tx *gorm.DB) error {
	var n int64
	if err := tx.Model(&Migration{}).Where("version = ?", 31).Count(&n).Error; err != nil || n > 0 {
		return err
	}
	if err := tx.AutoMigrate(&Ticket{}); err != nil {
		return err
	}
	return tx.Create(&Migration{Version: 31, AppliedAt: time.Now().UTC()}).Error
}

// Called under the owner lock, only for an available file's 200/206 GET.
// Concurrent tickets for the same app/visitor share one pending reservation.
// No debit or UV timestamp is committed until a positive file write occurs.
func (s *Server) reserveAPKDownload(tx *gorm.DB, u *User, ticket *Ticket) error {
	if ticket.BillingPrepaid {
		return nil
	}
	now := s.Now().UTC()
	if ticket.BillingEventID != "" {
		var prior MeteredEvent
		if err := tx.First(&prior, "id = ? AND user_id = ?", ticket.BillingEventID, u.ID).Error; err != nil {
			return err
		}
		if prior.State == "charged" || prior.State == "pending" && prior.ExpiresAt.After(now) {
			return nil
		}
		if prior.State == "pending" {
			if err := settleMetered(tx, u, &prior, now, false); err != nil {
				return err
			}
		}
		ticket.BillingEventID = ""
	}
	ip := net.ParseIP(ticket.IP)
	if ip == nil {
		return problem(400, "无法识别访问来源")
	}
	key := s.mac("usage-uv:" + u.ID + ":apk_uv:" + ticket.AppID + ":" + ip.String() + ":" + cleanText(ticket.UserAgent, 256))
	var guard MeteredUV
	err := tx.First(&guard, "key = ?", key).Error
	if err == nil && guard.ChargedAt.After(now.Add(-usageUVWindow)) {
		return nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var pending MeteredEvent
	err = tx.First(&pending, "user_id = ? AND kind = ? AND resource_id = ? AND source_id = ? AND state = ? AND expires_at > ?", u.ID, "apk_uv", ticket.AppID, key, "pending", now).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		x, err := getSettings(tx)
		if err != nil {
			return err
		}
		pending, err = reserveMetered(tx, u, x, now, randomHex(32), "apk_uv", ticket.AppID, key, now.Add(40*time.Minute))
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	ticket.BillingEventID = pending.ID
	return nil
}

func (s *Server) settleAPKDownloadStart(tx *gorm.DB, u *User, ticket *Ticket, occurred time.Time) error {
	if !ticket.Metered || ticket.BillingPrepaid || ticket.BillingEventID == "" {
		return nil
	}
	var event MeteredEvent
	if err := tx.First(&event, "id = ? AND user_id = ? AND kind = ? AND resource_id = ?", ticket.BillingEventID, u.ID, "apk_uv", ticket.AppID).Error; err != nil {
		return err
	}
	if event.State == "charged" {
		return nil
	}
	if event.State == "released" {
		// A long database outage may outlive the unused-hold timeout. A durable
		// positive-write receipt can recover using the original quoted price,
		// while preserving the nonnegative balance invariant and any newer UV.
		var guard MeteredUV
		err := tx.First(&guard, "key = ?", event.SourceID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && !guard.ChargedAt.Before(occurred.Add(-usageUVWindow)) && guard.ChargedAt.Before(occurred.Add(usageUVWindow)) {
			ticket.BillingEventID = ""
			return tx.Model(ticket).Update("billing_event_id", "").Error
		}
		x, err := getSettings(tx)
		if err != nil {
			return err
		}
		x.DownloadPriceMicros = event.PriceMicros
		replacement, err := reserveMetered(tx, u, x, s.Now(), hashString("apk-start-recovery:"+event.ID), "apk_uv", ticket.AppID, event.SourceID, s.Now().Add(40*time.Minute))
		if err != nil {
			return err
		}
		ticket.BillingEventID = replacement.ID
		if err := tx.Model(ticket).Update("billing_event_id", replacement.ID).Error; err != nil {
			return err
		}
		event = replacement
	}
	if event.State != "pending" {
		return errors.New("download usage reservation unavailable")
	}
	if occurred.IsZero() || occurred.After(s.Now()) {
		occurred = s.Now().UTC()
	}
	if err := settleMetered(tx, u, &event, occurred, true); err != nil {
		return err
	}
	if err := tx.Save(&MeteredUV{Key: event.SourceID, UserID: u.ID, Kind: "apk_uv", ResourceID: ticket.AppID, ChargedAt: occurred}).Error; err != nil {
		return err
	}
	// Only the ticket that settled this shared UV owns the displayed charge.
	ticket.PriceMicros = event.PriceMicros
	return tx.Model(ticket).Update("price_micros", ticket.PriceMicros).Error
}

func (s *Server) confirmAPKDownloadStart(pre Ticket, occurred time.Time) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		u, err := lockUser(tx, pre.UserID)
		if err != nil {
			return err
		}
		var ticket Ticket
		if err = tx.First(&ticket, "hash = ? AND user_id = ?", pre.Hash, pre.UserID).Error; err != nil {
			return err
		}
		if !ticket.Reserved || ticket.StartedAt == nil {
			return errors.New("download has not started")
		}
		return s.settleAPKDownloadStart(tx, &u, &ticket, occurred)
	})
}
