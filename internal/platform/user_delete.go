package platform

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
)

const userDeletionMigrationVersion = 30

func migrateUserDeletion(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = ?", userDeletionMigrationVersion).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	if !tx.Migrator().HasColumn(&User{}, "DeletedAt") {
		if err := tx.Migrator().AddColumn(&User{}, "DeletedAt"); err != nil {
			return err
		}
	}
	if !tx.Migrator().HasIndex(&User{}, "idx_users_deleted_at") {
		if err := tx.Migrator().CreateIndex(&User{}, "idx_users_deleted_at"); err != nil {
			return err
		}
	}
	return tx.Create(&Migration{Version: userDeletionMigrationVersion, AppliedAt: time.Now().UTC()}).Error
}

// Financial callbacks and reservation cleanup still use lockUser; only
// interactive administrator changes must reject a removed account.
func lockActiveUser(tx *gorm.DB, id string) (User, error) {
	u, err := lockUser(tx, id)
	if err == nil && u.DeletedAt != nil {
		err = gorm.ErrRecordNotFound
	}
	return u, err
}

func (s *Server) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"confirm_email"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		u, err := lockUser(tx, r.PathValue("id"))
		if err != nil {
			return err
		}
		if u.ID == userOf(r).ID || u.Role == "admin" {
			return problem(403, "不可删除管理员账号")
		}
		if strings.TrimSpace(in.Email) == "" || !strings.EqualFold(strings.TrimSpace(in.Email), u.Email) {
			return problem(400, "请输入该用户邮箱以确认删除")
		}
		if u.DeletedAt != nil {
			return nil
		}
		if u.BalanceMicros != 0 || u.HeldMicros != 0 || u.GiftHeldMicros != 0 {
			return problem(409, "账户仍有余额或冻结用量，请处理完成后再删除")
		}
		var pending int64
		if err = tx.Model(&Order{}).Where("user_id = ? AND status = ? AND expires_at > ?", u.ID, "pending", s.Now()).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return problem(409, "用户存在未完成的充值订单，请处理完成后再删除")
		}
		if err = tx.Model(&ReferralWithdrawal{}).Where("user_id = ? AND status IN ?", u.ID, []string{"pending", "approved"}).Count(&pending).Error; err != nil {
			return err
		}
		if pending > 0 {
			return problem(409, "用户存在未完成的提现，请处理完成后再删除")
		}
		now := s.Now()
		if err = tx.Model(&u).Updates(map[string]any{"disabled": true, "deleted_at": now}).Error; err != nil {
			return err
		}
		for _, model := range []any{&Session{}, &DIYLandingPreview{}} {
			if err = tx.Where("user_id = ?", u.ID).Delete(model).Error; err != nil {
				return err
			}
		}
		for _, model := range []any{&Application{}, &PWAApp{}, &TrafficPool{}} {
			if err = tx.Model(model).Where("user_id = ?", u.ID).Update("enabled", false).Error; err != nil {
				return err
			}
		}
		if err = tx.Model(&Cloak{}).Where("user_id = ?", u.ID).Updates(map[string]any{"enabled": false, "status": "blocked"}).Error; err != nil {
			return err
		}
		// Preserve ledger/order rows, invitation history and retained files.
		// Push workers already gate queued deliveries on users.disabled.
		detail, _ := json.Marshal(map[string]any{"email": u.Email, "name": u.Name, "gift_balance_micros": u.GiftBalanceMicros})
		return recordAudit(tx, userOf(r).ID, "user.delete", u.ID, string(detail))
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
