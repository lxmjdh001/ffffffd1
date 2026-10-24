package platform

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const meteredBillingVersion = 20
const usageUVWindow = 24 * time.Hour

// A unique business event, not a unique visitor. A retry of the same event is
// idempotent; a fresh installation/view/delivery has its own event ID.
type MeteredEvent struct {
	ID          string `gorm:"primaryKey;size:64"`
	UserID      string `gorm:"size:32;not null;index:idx_meter_owner_time,priority:1"`
	Kind        string `gorm:"size:24;not null;index"`
	ResourceID  string `gorm:"size:32;not null;index"`
	SourceID    string `gorm:"size:64;index"`
	State       string `gorm:"size:16;not null;index"`
	PriceMicros int64
	GrantID     string    `gorm:"size:64"`
	Period      string    `gorm:"size:7"`
	ExpiresAt   time.Time `gorm:"index"`
	CreatedAt   time.Time `gorm:"index:idx_meter_owner_time,priority:2"`
	SettledAt   *time.Time
}

type MeteredUV struct {
	Key        string    `gorm:"primaryKey;size:64"`
	UserID     string    `gorm:"size:32;index"`
	Kind       string    `gorm:"size:24"`
	ResourceID string    `gorm:"size:32"`
	ChargedAt  time.Time `gorm:"index"`
}

func migrateUsageBilling(tx *gorm.DB) error {
	var n int64
	if e := tx.Model(&Migration{}).Where("version = ?", meteredBillingVersion).Count(&n).Error; e != nil {
		return e
	}
	if n != 0 {
		return nil
	}
	if e := tx.AutoMigrate(&Settings{}, &Ticket{}, &Download{}, &MeteredEvent{}, &MeteredUV{}); e != nil {
		return e
	}
	var plans []Plan
	if e := tx.Find(&plans).Error; e != nil {
		return e
	}
	apps, storage := int64(500), int64(30<<30)
	for _, p := range plans {
		apps = max(apps, p.AppLimit)
		storage = max(storage, p.StorageBytes)
	}
	x, e := getSettings(tx)
	if e != nil {
		return e
	}
	// Preserve balances, orders, held funds, existing grants, and the operator's
	// APK price. No retroactive usage charges or automatic subscription refunds.
	if e = tx.Model(&Settings{}).Where("id = 1").Updates(map[string]any{
		"metered_billing": true, "pwa_install_price_micros": 30000, "cloak_uv_price_micros": 1000,
		"return_flow_price_micros": 1000, "pwa_push_price_micros": 1000,
		"registration_gift_limit": 1000, "app_limit": apps, "storage_limit_bytes": storage,
	}).Error; e != nil {
		return e
	}
	for i := range x.DownloadDomainRules {
		x.DownloadDomainRules[i].Audience = "all"
	}
	if e = tx.Model(&x).Select("DownloadDomainRules").Updates(&x).Error; e != nil {
		return e
	}
	return tx.Create(&Migration{Version: meteredBillingVersion, AppliedAt: time.Now().UTC()}).Error
}

func meteredEnabled(db *gorm.DB) bool { x, e := getSettings(db); return e == nil && x.MeteredBilling }
func usagePrice(x Settings, kind string) int64 {
	switch kind {
	case "apk_uv":
		return x.DownloadPriceMicros
	case "pwa_install":
		return x.PWAInstallPriceMicros
	case "cloak_uv":
		return x.CloakUVPriceMicros
	case "return_flow":
		return x.ReturnFlowPriceMicros
	case "pwa_push":
		return x.PWAPushPriceMicros
	}
	return -1
}

// Registration gifts form one shared pool for APK UVs and PWA installs. Honor
// previously purchased/granted, still-valid gifts without minting monthly gifts.
func activeMeteredGrant(db *gorm.DB, u User, x Settings, now time.Time) (*DownloadGrant, error) {
	grant := DownloadGrant{ID: "free:" + u.ID, UserID: u.ID, PlanID: "free", Limit: x.RegistrationGiftLimit, StartsAt: u.CreatedAt}
	if e := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&grant).Error; e != nil {
		return nil, e
	}
	var found DownloadGrant
	e := db.Where("user_id = ? AND starts_at <= ? AND (expires_at = ? OR expires_at > ?) AND used + reserved < \"limit\"", u.ID, now, time.Time{}, now).Clauses(clause.OrderBy{Expression: clause.Expr{SQL: "CASE WHEN expires_at = ? THEN 1 ELSE 0 END, expires_at ASC, starts_at ASC", Vars: []any{time.Time{}}}}).First(&found).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &found, e
}

func meteredGiftTotals(db *gorm.DB, u User, x Settings, now time.Time) (limit, used, reserved int64, err error) {
	if _, err = activeMeteredGrant(db, u, x, now); err != nil {
		return
	}
	var totals struct{ Limit, Used, Reserved int64 }
	err = db.Model(&DownloadGrant{}).Where("user_id = ? AND starts_at <= ? AND (expires_at = ? OR expires_at > ?)", u.ID, now, time.Time{}, now).Select("COALESCE(SUM(\"limit\"),0) AS \"limit\", COALESCE(SUM(used),0) AS used, COALESCE(SUM(reserved),0) AS reserved").Scan(&totals).Error
	return totals.Limit, totals.Used, totals.Reserved, err
}

func reserveMetered(tx *gorm.DB, u *User, x Settings, now time.Time, id, kind, resource, source string, expires time.Time) (MeteredEvent, error) {
	var event MeteredEvent
	e := tx.First(&event, "id = ?", id).Error
	if e == nil {
		if event.UserID != u.ID || event.Kind != kind || event.ResourceID != resource {
			return event, problem(409, "用量记录不匹配")
		}
		return event, nil
	}
	if !errors.Is(e, gorm.ErrRecordNotFound) {
		return event, e
	}
	if u.Disabled {
		return event, problem(403, "账户已停用")
	}
	price := usagePrice(x, kind)
	if price < 0 {
		return event, problem(400, "计费类型无效")
	}
	event = MeteredEvent{ID: id, UserID: u.ID, Kind: kind, ResourceID: resource, SourceID: source, State: "pending", PriceMicros: price, Period: period(now), ExpiresAt: expires, CreatedAt: now}
	if (kind == "apk_uv" || kind == "pwa_install") && price > 0 {
		grant, e := activeMeteredGrant(tx, *u, x, now)
		if e != nil {
			return event, e
		}
		if grant != nil {
			event.GrantID = grant.ID
			event.PriceMicros = 0
			if e = tx.Model(grant).UpdateColumn("reserved", gorm.Expr("reserved + 1")).Error; e != nil {
				return event, e
			}
		}
	}
	if event.GrantID == "" && event.PriceMicros > 0 {
		if u.BalanceMicros-u.HeldMicros < event.PriceMicros {
			return event, problem(402, "可用余额不足，请先充值")
		}
		u.HeldMicros += event.PriceMicros
		if e = tx.Model(u).Update("held_micros", u.HeldMicros).Error; e != nil {
			return event, e
		}
	}
	return event, tx.Create(&event).Error
}

func settleMetered(tx *gorm.DB, u *User, event *MeteredEvent, now time.Time, complete bool) error {
	if event.UserID != u.ID {
		return problem(409, "用量所属账户不匹配")
	}
	if event.State != "pending" {
		return nil
	}
	if event.GrantID != "" {
		if e := settleGiftGrant(tx, Ticket{GrantID: event.GrantID, UserID: u.ID}, complete); e != nil {
			return e
		}
	} else if event.PriceMicros > 0 {
		if u.HeldMicros < event.PriceMicros {
			return errors.New("usage hold invariant failed")
		}
		u.HeldMicros -= event.PriceMicros
		if e := tx.Model(u).Update("held_micros", u.HeldMicros).Error; e != nil {
			return e
		}
	}
	if complete {
		if event.PriceMicros > 0 {
			if e := changeBalance(tx, u, -event.PriceMicros, "usage:"+event.ID, event.Kind, "按量计费 · "+event.Kind); e != nil {
				return e
			}
		}
		usage := Usage{UserID: u.ID, Period: event.Period}
		if e := tx.Where("user_id = ? AND period = ?", u.ID, event.Period).FirstOrCreate(&usage).Error; e != nil {
			return e
		}
		if event.GrantID != "" {
			usage.FreeUsed++
		} else {
			usage.PaidUsed++
			usage.SpentMicros += event.PriceMicros
		}
		if e := tx.Save(&usage).Error; e != nil {
			return e
		}
		event.State = "charged"
	} else {
		event.State = "released"
	}
	event.SettledAt = &now
	return tx.Save(event).Error
}

func (s *Server) chargeUsageUV(r *http.Request, owner, kind, resource string) error {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		return nil
	}
	settings, e := getSettings(s.DB)
	if e != nil {
		return e
	}
	if !settings.MeteredBilling {
		return nil
	}
	ip := net.ParseIP(s.clientIP(r))
	if ip == nil {
		return problem(400, "无法识别访问来源")
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		u, e := lockUser(tx, owner)
		if e != nil {
			return e
		}
		x, e := getSettings(tx)
		if e != nil {
			return e
		}
		if !x.MeteredBilling {
			return nil
		}
		if u.Disabled {
			return problem(404, "应用不可用")
		}
		key := s.mac("usage-uv:" + owner + ":" + kind + ":" + resource + ":" + ip.String() + ":" + cleanText(r.UserAgent(), 256))
		var guard MeteredUV
		e = tx.First(&guard, "key = ?", key).Error
		now := s.Now().UTC()
		if e == nil && guard.ChargedAt.After(now.Add(-usageUVWindow)) {
			return nil
		}
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		event, e := reserveMetered(tx, &u, x, now, randomHex(32), kind, resource, "", now)
		if e != nil {
			return e
		}
		if e = settleMetered(tx, &u, &event, now, true); e != nil {
			return e
		}
		return tx.Save(&MeteredUV{Key: key, UserID: owner, Kind: kind, ResourceID: resource, ChargedAt: now}).Error
	})
}

func (s *Server) riskEligible(db *gorm.DB, u User) (bool, error) {
	x, e := getSettings(db)
	if e != nil {
		return false, e
	}
	if u.Disabled {
		return false, nil
	}
	if !x.MeteredBilling {
		return u.PlanID(s.Now()) == "pro", nil
	}
	if u.BalanceMicros-u.HeldMicros > 0 {
		return true, nil
	}
	limit, used, reserved, e := meteredGiftTotals(db, u, x, s.Now())
	return limit-used-reserved > 0, e
}

func (s *Server) cleanupMeteredUsage(ctx context.Context) error {
	var rows []MeteredEvent
	if e := s.DB.WithContext(ctx).Where("state = ? AND (expires_at <= ? OR (kind = ? AND source_id IN (SELECT id FROM pwa_push_deliveries WHERE status IN ?)))", "pending", s.Now(), "pwa_push", []string{"failed", "cancelled"}).Limit(1000).Find(&rows).Error; e != nil {
		return e
	}
	for _, row := range rows {
		if e := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			u, e := lockUser(tx, row.UserID)
			if e != nil {
				return e
			}
			var current MeteredEvent
			if e = tx.First(&current, "id = ?", row.ID).Error; e != nil {
				return e
			}
			if current.State != "pending" {
				return nil
			}
			return settleMetered(tx, &u, &current, s.Now(), false)
		}); e != nil {
			return e
		}
	}
	return nil
}

func sameUsageOrigin(s *Server, r *http.Request) bool {
	expected := "https://" + r.Host
	if !s.Config.Production() {
		expected = "http://" + r.Host
	}
	return r.Header.Get("Origin") == expected && r.Header.Get("Sec-Fetch-Site") != "cross-site"
}

func (s *Server) pwaInstallUsage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !sameUsageOrigin(s, r) {
		fail(w, problem(403, "请求来源不受信任"))
		return
	}
	app, e := s.loadPublicPWA(r)
	if e != nil {
		pwaPublicFailure(w, r, e)
		return
	}
	if s.rejectBlockedUA(w, r, app.UserID, r.UserAgent()) {
		return
	}
	var in struct {
		Key   string `json:"key"`
		Token string `json:"token"`
	}
	if e = decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if !hexKey.MatchString(in.Key) || !s.validReturnFlowViewToken("pwa", app.ID, in.Token, r) {
		fail(w, problem(403, "安装凭证无效，请刷新页面"))
		return
	}
	if e = s.rate("pwa-install:"+s.clientIP(r), 120, time.Minute); e != nil {
		fail(w, e)
		return
	}
	action := r.PathValue("action")
	if action != "start" && action != "complete" && action != "cancel" {
		http.NotFound(w, r)
		return
	}
	var out MeteredEvent
	e = s.DB.Transaction(func(tx *gorm.DB) error {
		u, e := lockUser(tx, app.UserID)
		if e != nil {
			return e
		}
		if u.Disabled {
			return problem(404, "应用不可用")
		}
		var current PWAApp
		if e = tx.First(&current, "id = ? AND user_id = ? AND enabled = ?", app.ID, u.ID, true).Error; e != nil {
			return problem(404, "应用不可用")
		}
		x, e := getSettings(tx)
		if e != nil {
			return e
		}
		if !x.MeteredBilling {
			return nil
		}
		id := s.mac("pwa-install:" + app.ID + ":" + in.Key)
		if action == "start" {
			out, e = reserveMetered(tx, &u, x, s.Now(), id, "pwa_install", app.ID, "", s.Now().Add(time.Hour))
			return e
		}
		if e = tx.First(&out, "id = ? AND user_id = ? AND resource_id = ? AND kind = ?", id, u.ID, app.ID, "pwa_install").Error; e != nil {
			return problem(404, "安装记录不存在")
		}
		if action == "complete" && out.State == "released" {
			return problem(410, "安装凭证已释放，请刷新页面")
		}
		if action == "complete" && out.ExpiresAt.Before(s.Now()) && out.State == "pending" {
			return problem(410, "安装凭证已过期")
		}
		return settleMetered(tx, &u, &out, s.Now(), action == "complete")
	})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]any{"allowed": true, "state": out.State})
}

func (s *Server) reservePushUsage(ctx context.Context, d PWAPushDelivery) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		u, e := lockUser(tx, d.UserID)
		if e != nil {
			return e
		}
		x, e := getSettings(tx)
		if e != nil || !x.MeteredBilling {
			return e
		}
		event, e := reserveMetered(tx, &u, x, s.Now(), hashString("pwa-push:"+d.ID), "pwa_push", d.PWAID, d.ID, d.ExpiresAt)
		if e == nil && event.State == "released" {
			return problem(409, "推送计费预留已释放")
		}
		return e
	})
}

func (s *Server) settlePushUsage(tx *gorm.DB, d PWAPushDelivery, accepted, terminal bool) error {
	if !accepted && !terminal {
		return nil
	}
	u, e := lockUser(tx, d.UserID)
	if e != nil {
		return e
	}
	var event MeteredEvent
	e = tx.First(&event, "id = ?", hashString("pwa-push:"+d.ID)).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	return settleMetered(tx, &u, &event, s.Now(), accepted)
}

func publicMeteredPlans(x Settings) []Plan {
	return []Plan{{ID: "free", Name: "按量计费", AppLimit: x.AppLimit, DownloadLimit: x.RegistrationGiftLimit, StorageBytes: x.StorageLimitBytes}}
}
