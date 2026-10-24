package platform

import (
	"encoding/json"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func OpenDB(c Config) (*gorm.DB, error) {
	if err := os.MkdirAll(c.DataDir, 0700); err != nil {
		return nil, err
	}
	var d gorm.Dialector
	if c.DatabaseURL != "" {
		d = postgres.Open(c.DatabaseURL)
	} else {
		d = sqlite.Open(filepath.Join(c.DataDir, "appdock.db") + "?_journal_mode=WAL&_busy_timeout=15000&_foreign_keys=on&_txlock=immediate")
	}
	db, err := gorm.Open(d, &gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if c.DatabaseURL == "" {
		sqlDB.SetMaxOpenConns(1)
	} else {
		sqlDB.SetMaxOpenConns(30)
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetConnMaxLifetime(30 * time.Minute)
	}
	return db, nil
}
func Migrate(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(681247003)").Error; err != nil {
				return err
			}
		}
		if err := tx.AutoMigrate(&TrafficPool{}, &TrafficLink{}, &RiskUsage{}, &Cloak{}); err != nil {
			return err
		}
		if err := tx.AutoMigrate(&AppPageDesign{}, &PageImageObject{}); err != nil {
			return err
		}
		if err := tx.AutoMigrate(&Domain{}, &DownloadDomainNote{}); err != nil {
			return err
		}
		if err := tx.AutoMigrate(&Application{}); err != nil {
			return err
		}
		if err := tx.AutoMigrate(&UABlock{}); err != nil {
			return err
		}
		if err := tx.AutoMigrate(&Migration{}); err != nil {
			return err
		}
		if err := migrateDownloadDomainRules(tx); err != nil {
			return err
		}
		if err := migrateUSDTPayment(tx); err != nil {
			return err
		}
		if err := migrateTrafficGroups(tx); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&Migration{}).Where("version = 1").Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			if err := tx.AutoMigrate(&User{}, &Plan{}, &Settings{}, &Session{}, &Challenge{}, &RateBucket{}, &Application{}, &Domain{}, &Usage{}, &Ticket{}, &Download{}, &Ledger{}, &Order{}, &USDTReservation{}, &Audit{}, &AdConfig{}, &Attribution{}, &Conversion{}, &Delivery{}); err != nil {
				return err
			}
			for _, p := range []Plan{{ID: "free", Name: "免费套餐", AppLimit: 10, DownloadLimit: 1000, StorageBytes: 10 << 30}, {ID: "pro", Name: "尊享套餐", PriceMicros: 99 * Dollar, AppLimit: 500, DownloadLimit: 10000, StorageBytes: 30 << 30}} {
				if err := tx.Create(&p).Error; err != nil {
					return err
				}
			}
			if err := tx.Create(&Settings{ID: 1, SiteName: "D1 Apk", RegistrationOpen: true, DownloadPriceMicros: 3000, MaxUploadBytes: 500 << 20, DomainLimit: 10, USDTRateMicros: Dollar}).Error; err != nil {
				return err
			}
			if err := tx.Create(&Migration{Version: 1, AppliedAt: time.Now().UTC()}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&Migration{}).Where("version = 2").Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			// Existing orders retain a zero bonus. New orders snapshot their gift.
			if err := tx.AutoMigrate(&Settings{}, &Order{}); err != nil {
				return err
			}
			if err := tx.Create(&Migration{Version: 2, AppliedAt: time.Now().UTC()}).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&Migration{}).Where("version = 3").Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			if err := migrateMembershipVersion(tx); err != nil {
				return err
			}
			if err := migrateReferrals(tx); err != nil {
				return err
			}
			if err := migrateAlipayPayment(tx); err != nil {
				return err
			}
			if err := migrateReferralWithdrawals(tx); err != nil {
				return err
			}
			return migrateCommissionSettings(tx)
		}
		if err := tx.AutoMigrate(&Settings{}); err != nil {
			return err
		}
		tiers, err := json.Marshal(defaultRechargeTiers())
		if err != nil {
			return err
		}
		// Adopt USD at the existing numeric values. Preserve balances, prices,
		// ledger history, and the payment/bonus snapshots of existing orders.
		if err := tx.Model(&Settings{}).Where("id = 1").Updates(map[string]any{
			"recharge_bonus_tiers": string(tiers), "recharge_bonus_enabled": true, "usdt_rate_micros": Dollar,
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&Migration{Version: 3, AppliedAt: time.Now().UTC()}).Error; err != nil {
			return err
		}
		if err := migrateMembershipVersion(tx); err != nil {
			return err
		}
		if err := migrateReferrals(tx); err != nil {
			return err
		}
		if err := migrateAlipayPayment(tx); err != nil {
			return err
		}
		if err := migrateReferralWithdrawals(tx); err != nil {
			return err
		}
		return migrateCommissionSettings(tx)
	})
}
func BootstrapAdmin(db *gorm.DB, c Config) error {
	if c.AdminEmail == "" && c.AdminPassword == "" {
		return nil
	}
	email, err := normalizeAdminAccount(c.AdminEmail)
	if err != nil || len(c.AdminPassword) < 6 || len(c.AdminPassword) > 72 {
		return errors.New("ADMIN_ACCOUNT (or ADMIN_EMAIL) and a 6–72 byte ADMIN_PASSWORD are required together")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if e := tx.Exec("SELECT pg_advisory_xact_lock(681247004)").Error; e != nil {
				return e
			}
		}
		var n int64
		if err := tx.Model(&User{}).Where("role = ?", "admin").Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		hash, e := bcrypt.GenerateFromPassword([]byte(c.AdminPassword), 12)
		if e != nil {
			return e
		}
		u := User{ID: randomHex(16), Email: email, Name: "平台管理员", Role: "admin", PasswordHash: string(hash)}
		return tx.Create(&u).Error
	})
}

// ResetAdmin is an operator-only CLI action; no unauthenticated HTTP reset exists.
func ResetAdmin(db *gorm.DB, c Config) error {
	email, e := normalizeAdminAccount(c.AdminEmail)
	if e != nil || len(c.AdminPassword) < 6 || len(c.AdminPassword) > 72 {
		return errors.New("ADMIN_ACCOUNT (or ADMIN_EMAIL) and 6–72 byte ADMIN_PASSWORD required")
	}
	previous := email
	if raw := os.Getenv("ADMIN_PREVIOUS_ACCOUNT"); raw != "" {
		previous, e = normalizeAdminAccount(raw)
		if e != nil {
			return e
		}
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(c.AdminPassword), 12)
	if e != nil {
		return e
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var user User
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "email = ? AND role = ?", previous, "admin").Error; e != nil {
			return e
		}
		updates := map[string]any{"email": email, "password_hash": string(hash)}
		if previous != email && !strings.Contains(email, "@") {
			updates["name"] = email
		}
		if e := tx.Model(&user).Updates(updates).Error; e != nil {
			return e
		}
		if e := tx.Delete(&Session{}, "user_id = ?", user.ID).Error; e != nil {
			return e
		}
		return recordAudit(tx, user.ID, "admin.cli_password_reset", user.ID, "管理员 CLI 更新登录账号与密码并撤销所有会话")
	})
}
func lockUser(tx *gorm.DB, id string) (User, error) {
	var u User
	e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, "id = ?", id).Error
	return u, e
}
func getSettings(db *gorm.DB) (Settings, error) {
	var s Settings
	e := db.First(&s, 1).Error
	return s, e
}
func getPlan(db *gorm.DB, u User, now time.Time) (Plan, error) {
	var p Plan
	e := db.First(&p, "id = ?", u.PlanID(now)).Error
	return p, e
}
func recordAudit(tx *gorm.DB, actor, action, target, detail string) error {
	return tx.Create(&Audit{ID: randomHex(16), ActorID: actor, Action: action, Target: target, Detail: detail, CreatedAt: time.Now().UTC()}).Error
}
func changeBalance(tx *gorm.DB, u *User, delta int64, reference, kind, note string) error {
	if delta < 0 && u.BalanceMicros-u.HeldMicros < -delta {
		return problem(402, "余额不足，请先充值")
	}
	if delta > 0 {
		var pending int64
		// Pending withdrawals remain the user's funds and need refund headroom.
		// Ordinary credits cannot occupy that space before an administrator can
		// reject the request. A refund itself exits the pending balance atomically.
		if kind != "withdrawal_refund" {
			if err := tx.Model(&ReferralWithdrawal{}).Where("user_id = ? AND status IN ?", u.ID, []string{"pending", "approved"}).Select("COALESCE(SUM(amount_micros), 0)").Scan(&pending).Error; err != nil {
				return err
			}
		}
		if u.BalanceMicros > 100000000*Dollar-delta-pending {
			return problem(400, "余额超过系统上限")
		}
	}
	u.BalanceMicros += delta
	if err := tx.Model(u).Update("balance_micros", u.BalanceMicros).Error; err != nil {
		return err
	}
	wallet, err := referralWallet(tx, u.ID)
	if err != nil {
		return err
	}
	entry := Ledger{ID: randomHex(16), UserID: u.ID, Reference: reference, Kind: kind, AmountMicros: delta, BalanceMicros: u.BalanceMicros, Note: strings.TrimSpace(note), CreatedAt: time.Now().UTC()}
	if err := tx.Create(&entry).Error; err != nil {
		return err
	}
	return updateReferralWallet(tx, u, wallet, entry)
}
