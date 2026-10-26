package platform

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Referral accounts are created once. An inviter can only be chosen while a new
// user registers; profile updates never change this permanent relationship.
type ReferralAccount struct {
	UserID     string  `gorm:"primaryKey;size:32"`
	InviteCode string  `gorm:"uniqueIndex;size:16;not null"`
	InviterID  *string `gorm:"index;size:32"`
	CreatedAt  time.Time
}

type ReferralReward struct {
	ID             string    `json:"id" gorm:"primaryKey;size:32"`
	UserID         string    `json:"-" gorm:"index:idx_referral_reward_owner_time,priority:1;size:32;not null"`
	SourceUserID   string    `json:"-" gorm:"size:32;not null"`
	OrderID        string    `json:"-" gorm:"uniqueIndex:idx_referral_reward_order_level,priority:1;size:32;not null"`
	Level          int       `json:"level" gorm:"uniqueIndex:idx_referral_reward_order_level,priority:2;not null;check:level >= 1 AND level <= 3"`
	AmountMicros   int64     `json:"amount_micros" gorm:"not null;check:amount_micros > 0"`
	RechargeMicros int64     `json:"recharge_micros" gorm:"not null;check:recharge_micros > 0"`
	CreatedAt      time.Time `json:"created_at" gorm:"index:idx_referral_reward_owner_time,priority:2"`
}

func referralRates() [3]int64 { return [3]int64{10, 2, 2} }

func migrateReferrals(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = 9").Count(&count).Error; err != nil || count > 0 {
		return err
	}
	if err := tx.AutoMigrate(&ReferralAccount{}, &ReferralReward{}); err != nil {
		return err
	}
	// Preserve all existing balances and paid orders. No historical recharge is
	// retroactively rewarded, and old accounts start without an inviter.
	for {
		var users []User
		if err := tx.Where("NOT EXISTS (SELECT 1 FROM referral_accounts WHERE referral_accounts.user_id = users.id)").Limit(200).Find(&users).Error; err != nil {
			return err
		}
		if len(users) == 0 {
			break
		}
		for _, user := range users {
			if _, err := createReferralAccount(tx, user.ID, nil, user.CreatedAt); err != nil {
				return err
			}
		}
	}
	return tx.Create(&Migration{Version: 9, AppliedAt: time.Now().UTC()}).Error
}

func createReferralAccount(tx *gorm.DB, userID string, inviterID *string, now time.Time) (ReferralAccount, error) {
	for range 5 {
		account := ReferralAccount{UserID: userID, InviteCode: randomHex(8), InviterID: inviterID, CreatedAt: now}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&account)
		if result.Error != nil {
			return ReferralAccount{}, result.Error
		}
		if result.RowsAffected == 1 {
			return account, nil
		}
		// Concurrent first visits reuse one account. A rare code collision tries
		// a fresh random code without aborting a PostgreSQL transaction.
		var existing ReferralAccount
		err := tx.First(&existing, "user_id = ?", userID).Error
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return ReferralAccount{}, err
		}
	}
	return ReferralAccount{}, problem(503, "邀请码生成失败，请重试")
}

func registrationInviter(tx *gorm.DB, code, userID string) (*string, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return nil, nil
	}
	if _, err := hex.DecodeString(code); err != nil || len(code) != 16 {
		return nil, problem(400, "邀请码无效，请检查后重试")
	}
	var account ReferralAccount
	err := tx.Joins("JOIN users ON users.id = referral_accounts.user_id").Where("referral_accounts.invite_code = ? AND users.disabled = ?", code, false).First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, problem(400, "邀请码无效，请检查后重试")
	}
	if err != nil {
		return nil, err
	}
	if account.UserID == userID {
		return nil, problem(400, "不能使用自己的邀请码")
	}
	return &account.UserID, nil
}

// A new user cannot already occur in its inviter's chain. At settlement we
// additionally reject repeated users defensively, so malformed stored data can
// never reward the payer or pay an ancestor twice.
func referralAncestors(tx *gorm.DB, userID string) ([]string, error) {
	ancestors := []string{}
	seen := map[string]bool{userID: true}
	current := userID
	for range referralRates() {
		var account ReferralAccount
		err := tx.First(&account, "user_id = ?", current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && account.InviterID == nil) {
			break
		}
		if err != nil {
			return nil, err
		}
		current = *account.InviterID
		if current == "" || seen[current] {
			return nil, problem(409, "邀请关系异常，请联系管理员")
		}
		seen[current] = true
		ancestors = append(ancestors, current)
	}
	return ancestors, nil
}

func lockRechargeUsers(tx *gorm.DB, userID string, ancestors []string) (map[string]*User, error) {
	ids := append([]string{userID}, ancestors...)
	sort.Strings(ids)
	var users []User
	// All settlements lock every affected balance in the same global order,
	// before locking the order, including simultaneous ancestor recharges.
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id").Find(&users).Error; err != nil {
		return nil, err
	}
	if len(users) != len(ids) {
		return nil, gorm.ErrRecordNotFound
	}
	locked := make(map[string]*User, len(users))
	for i := range users {
		locked[users[i].ID] = &users[i]
	}
	return locked, nil
}

func creditReferralRewards(tx *gorm.DB, order Order, ancestors []string, users map[string]*User, now time.Time) error {
	rates, _, err := effectiveReferralBasisPoints(tx, ancestors)
	if err != nil {
		return err
	}
	for i, id := range ancestors {
		if users[id].DeletedAt != nil {
			continue
		}
		// Calculate from paid principal only, excluding recharge gifts and the
		// payment provider's matching suffix. Integer division rounds down.
		amount := order.AmountMicros * rates[id][i] / 10000
		if amount <= 0 {
			continue
		}
		level := i + 1
		reward := ReferralReward{ID: randomHex(16), UserID: id, SourceUserID: order.UserID, OrderID: order.ID, Level: level, AmountMicros: amount, RechargeMicros: order.AmountMicros, CreatedAt: now}
		if err := tx.Create(&reward).Error; err != nil {
			return err
		}
		if err := changeBalance(tx, users[id], amount, fmt.Sprintf("referral:%s:%d", order.ID, level), "referral_reward", fmt.Sprintf("%d 级邀请返利 · 好友充值 %s USD", level, formatMoney(order.AmountMicros))); err != nil {
			return err
		}
	}
	return nil
}

type referralMember struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Level     int       `json:"level"`
	CreatedAt time.Time `json:"created_at"`
}

type referralRewardView struct {
	ReferralReward
	SourceName string `json:"source_name"`
}

type referralPage[T any] struct {
	Data  []T   `json:"data"`
	Total int64 `json:"total"`
}

type referralResponse struct {
	InviteCode                       string                           `json:"invite_code"`
	InviteURL                        string                           `json:"invite_url"`
	Rates                            [3]float64                       `json:"rates"`
	TotalRewardMicros                int64                            `json:"total_reward_micros"`
	TeamCount                        int64                            `json:"team_count"`
	DirectCount                      int64                            `json:"direct_count"`
	LevelCounts                      [3]int64                         `json:"level_counts"`
	WithdrawalAvailableMicros        int64                            `json:"withdrawal_available_micros"`
	WithdrawalPendingMicros          int64                            `json:"withdrawal_pending_micros"`
	WithdrawalPaidMicros             int64                            `json:"withdrawal_paid_micros"`
	WithdrawalMinimumMicros          int64                            `json:"withdrawal_minimum_micros"`
	WithdrawalReconciliationRequired bool                             `json:"withdrawal_reconciliation_required"`
	Rewards                          referralPage[referralRewardView] `json:"rewards"`
	Members                          referralPage[referralMember]     `json:"members"`
}

func referralPageParams(r *http.Request, key string) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get(key))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		page = 100000
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if limit < 1 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	return (page - 1) * limit, limit
}

func (s *Server) referrals(w http.ResponseWriter, r *http.Request) {
	user := userOf(r)
	var response referralResponse
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		locked, err := lockUser(tx, user.ID)
		if err != nil {
			return err
		}
		available, required, err := referralWithdrawalAvailable(tx, locked)
		if err != nil {
			return err
		}
		response.WithdrawalAvailableMicros = available
		response.WithdrawalReconciliationRequired = required
		response.WithdrawalMinimumMicros = referralWithdrawalMinimum
		if err := tx.Model(&ReferralWithdrawal{}).Where("user_id = ? AND status IN ?", user.ID, []string{"pending", "approved"}).Select("COALESCE(SUM(amount_micros), 0)").Scan(&response.WithdrawalPendingMicros).Error; err != nil {
			return err
		}
		if err := tx.Model(&ReferralWithdrawal{}).Where("user_id = ? AND status = ?", user.ID, "paid").Select("COALESCE(SUM(amount_micros), 0)").Scan(&response.WithdrawalPaidMicros).Error; err != nil {
			return err
		}
		var account ReferralAccount
		err = tx.First(&account, "user_id = ?", user.ID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			account, err = createReferralAccount(tx, user.ID, nil, user.CreatedAt)
		}
		if err != nil {
			return err
		}
		response.InviteCode = account.InviteCode
		response.InviteURL = strings.TrimRight(s.Config.PublicURL, "/") + "/#/register?invite=" + url.QueryEscape(account.InviteCode)
		rates, _, err := effectiveReferralBasisPoints(tx, []string{user.ID})
		if err != nil {
			return err
		}
		response.Rates = commissionPercentages(rates[user.ID])
		response.Rewards.Data = []referralRewardView{}
		response.Members.Data = []referralMember{}
		rewards := tx.Model(&ReferralReward{}).Where("user_id = ?", user.ID)
		if err = rewards.Count(&response.Rewards.Total).Error; err != nil {
			return err
		}
		if err = tx.Model(&ReferralReward{}).Where("user_id = ?", user.ID).Select("COALESCE(SUM(amount_micros), 0)").Scan(&response.TotalRewardMicros).Error; err != nil {
			return err
		}
		offset, limit := referralPageParams(r, "rewards_page")
		if err = tx.Model(&ReferralReward{}).Select("referral_rewards.*, users.name AS source_name").Joins("JOIN users ON users.id = referral_rewards.source_user_id").Where("referral_rewards.user_id = ?", user.ID).Order("referral_rewards.created_at DESC, referral_rewards.id DESC").Offset(offset).Limit(limit).Scan(&response.Rewards.Data).Error; err != nil {
			return err
		}
		// Three fixed joins keep the team query bounded regardless of depth.
		// Every branch excludes the owner and repeated users along its path.
		teamSQL := `SELECT u.id, u.name, u.created_at, 1 AS level FROM referral_accounts r JOIN users u ON u.id = r.user_id WHERE r.inviter_id = ? AND u.id <> ?
		UNION SELECT u.id, u.name, u.created_at, 2 AS level FROM referral_accounts r JOIN referral_accounts p ON p.user_id = r.inviter_id JOIN users u ON u.id = r.user_id WHERE p.inviter_id = ? AND u.id <> ? AND r.user_id <> p.user_id
		UNION SELECT u.id, u.name, u.created_at, 3 AS level FROM referral_accounts r JOIN referral_accounts p ON p.user_id = r.inviter_id JOIN referral_accounts g ON g.user_id = p.inviter_id JOIN users u ON u.id = r.user_id WHERE g.inviter_id = ? AND u.id <> ? AND r.user_id <> p.user_id AND r.user_id <> g.user_id AND p.user_id <> g.user_id`
		args := []any{user.ID, user.ID, user.ID, user.ID, user.ID, user.ID}
		var counts []struct {
			Level int
			Count int64
		}
		if err = tx.Raw("SELECT level, COUNT(*) AS count FROM ("+teamSQL+") AS team GROUP BY level", args...).Scan(&counts).Error; err != nil {
			return err
		}
		for _, count := range counts {
			response.LevelCounts[count.Level-1] = count.Count
			response.TeamCount += count.Count
		}
		response.DirectCount = response.LevelCounts[0]
		response.Members.Total = response.TeamCount
		offset, limit = referralPageParams(r, "members_page")
		args = append(args, limit, offset)
		return tx.Raw("SELECT * FROM ("+teamSQL+") AS team ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?", args...).Scan(&response.Members.Data).Error
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, http.StatusOK, response)
}
