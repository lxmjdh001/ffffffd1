package platform

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Integer basis points keep fractional percentages out of money arithmetic.
type CommissionSetting struct {
	ID                uint      `gorm:"primaryKey"`
	Level1BasisPoints int64     `gorm:"not null;check:level1_basis_points >= 0 AND level1_basis_points <= 10000"`
	Level2BasisPoints int64     `gorm:"not null;check:level2_basis_points >= 0 AND level2_basis_points <= 10000"`
	Level3BasisPoints int64     `gorm:"not null;check:level3_basis_points >= 0 AND level3_basis_points <= 10000"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type ReferralRateOverride struct {
	UserID            string    `gorm:"primaryKey;size:32"`
	Level1BasisPoints int64     `gorm:"not null;check:level1_basis_points >= 0 AND level1_basis_points <= 10000"`
	Level2BasisPoints int64     `gorm:"not null;check:level2_basis_points >= 0 AND level2_basis_points <= 10000"`
	Level3BasisPoints int64     `gorm:"not null;check:level3_basis_points >= 0 AND level3_basis_points <= 10000"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func defaultReferralBasisPoints() [3]int64 { return [3]int64{1000, 200, 200} }

func migrateCommissionSettings(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = 12").Count(&count).Error; err != nil || count > 0 {
		return err
	}
	if err := tx.AutoMigrate(&CommissionSetting{}, &ReferralRateOverride{}); err != nil {
		return err
	}
	config := CommissionSetting{ID: 1, Level1BasisPoints: 1000, Level2BasisPoints: 200, Level3BasisPoints: 200, UpdatedAt: time.Now().UTC()}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&config).Error; err != nil {
		return err
	}
	return tx.Create(&Migration{Version: 12, AppliedAt: time.Now().UTC()}).Error
}

func commissionBasisPoints(config CommissionSetting) [3]int64 {
	return [3]int64{config.Level1BasisPoints, config.Level2BasisPoints, config.Level3BasisPoints}
}

func commissionPercentages(rates [3]int64) [3]float64 {
	return [3]float64{float64(rates[0]) / 100, float64(rates[1]) / 100, float64(rates[2]) / 100}
}

func globalCommissionSettings(tx *gorm.DB) (CommissionSetting, error) {
	var config CommissionSetting
	err := tx.First(&config, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		config = CommissionSetting{ID: 1, Level1BasisPoints: 1000, Level2BasisPoints: 200, Level3BasisPoints: 200}
		return config, nil
	}
	return config, err
}

// Read global settings once per settlement, then each beneficiary's own
// override. Their User rows are already locked by lockRechargeUsers.
func effectiveReferralBasisPoints(tx *gorm.DB, ids []string) (map[string][3]int64, map[string]bool, error) {
	config, err := globalCommissionSettings(tx)
	if err != nil {
		return nil, nil, err
	}
	rates := make(map[string][3]int64, len(ids))
	overridden := make(map[string]bool, len(ids))
	for _, id := range ids {
		rates[id] = commissionBasisPoints(config)
	}
	if len(ids) == 0 {
		return rates, overridden, nil
	}
	var overrides []ReferralRateOverride
	if err := tx.Where("user_id IN ?", ids).Find(&overrides).Error; err != nil {
		return nil, nil, err
	}
	for _, row := range overrides {
		rates[row.UserID] = [3]int64{row.Level1BasisPoints, row.Level2BasisPoints, row.Level3BasisPoints}
		overridden[row.UserID] = true
	}
	return rates, overridden, nil
}

var commissionRatePattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,2})(\.[0-9]{1,2})?$`)

func parseCommissionRates(values []string) ([3]int64, error) {
	var rates [3]int64
	if len(values) != 3 {
		return rates, problem(400, "请分别填写一级、二级和三级佣金比例")
	}
	for i, value := range values {
		if !commissionRatePattern.MatchString(value) {
			return rates, problem(400, "佣金比例须为 0–100 的数字字符串，最多两位小数")
		}
		parts := strings.SplitN(value, ".", 2)
		whole, _ := strconv.ParseInt(parts[0], 10, 64)
		fraction := "00"
		if len(parts) == 2 {
			fraction = parts[1] + strings.Repeat("0", 2-len(parts[1]))
		}
		decimal, _ := strconv.ParseInt(fraction, 10, 64)
		rates[i] = whole*100 + decimal
		if rates[i] > 10000 {
			return rates, problem(400, "每一级佣金比例必须在 0–100% 之间")
		}
	}
	return rates, nil
}

type commissionSettingsView struct {
	Rates     [3]float64 `json:"rates"`
	UpdatedAt time.Time  `json:"updated_at,omitempty"`
}

func commissionSettingsOutput(config CommissionSetting) commissionSettingsView {
	return commissionSettingsView{Rates: commissionPercentages(commissionBasisPoints(config)), UpdatedAt: config.UpdatedAt}
}

func (s *Server) getCommissionSettings(w http.ResponseWriter, r *http.Request) {
	config, err := globalCommissionSettings(s.DB)
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, commissionSettingsOutput(config))
}

func (s *Server) saveCommissionSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Rates []string `json:"rates"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	rates, err := parseCommissionRates(in.Rates)
	if err != nil {
		fail(w, err)
		return
	}
	var out CommissionSetting
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		initial := CommissionSetting{ID: 1, Level1BasisPoints: 1000, Level2BasisPoints: 200, Level3BasisPoints: 200, UpdatedAt: s.Now()}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&out, 1).Error; err != nil {
			return err
		}
		out.Level1BasisPoints, out.Level2BasisPoints, out.Level3BasisPoints = rates[0], rates[1], rates[2]
		out.UpdatedAt = s.Now()
		if err := tx.Save(&out).Error; err != nil {
			return err
		}
		raw, _ := json.Marshal(commissionSettingsOutput(out))
		return recordAudit(tx, userOf(r).ID, "commission.settings", "global", string(raw))
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, commissionSettingsOutput(out))
}

type referralRatesView struct {
	Rates      [3]float64 `json:"rates"`
	Overridden bool       `json:"overridden"`
}

func (s *Server) saveUserReferralRates(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Rates []string `json:"rates"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	rates, err := parseCommissionRates(in.Rates)
	if err != nil {
		fail(w, err)
		return
	}
	out := referralRatesView{Rates: commissionPercentages(rates), Overridden: true}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		user, err := lockActiveUser(tx, r.PathValue("id"))
		if err != nil {
			return err
		}
		override := ReferralRateOverride{UserID: user.ID, Level1BasisPoints: rates[0], Level2BasisPoints: rates[1], Level3BasisPoints: rates[2], UpdatedAt: s.Now()}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, DoUpdates: clause.AssignmentColumns([]string{"level1_basis_points", "level2_basis_points", "level3_basis_points", "updated_at"})}).Create(&override).Error; err != nil {
			return err
		}
		raw, _ := json.Marshal(out)
		return recordAudit(tx, userOf(r).ID, "commission.user_rates", user.ID, string(raw))
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, out)
}

func (s *Server) clearUserReferralRates(w http.ResponseWriter, r *http.Request) {
	var out referralRatesView
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		user, err := lockActiveUser(tx, r.PathValue("id"))
		if err != nil {
			return err
		}
		result := tx.Delete(&ReferralRateOverride{}, "user_id = ?", user.ID)
		if result.Error != nil {
			return result.Error
		}
		config, err := globalCommissionSettings(tx)
		if err != nil {
			return err
		}
		out.Rates = commissionPercentages(commissionBasisPoints(config))
		if result.RowsAffected == 0 {
			return nil
		}
		raw, _ := json.Marshal(out)
		return recordAudit(tx, userOf(r).ID, "commission.user_rates.clear", user.ID, string(raw))
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, out)
}

type adminUserCommissionView struct {
	User
	DirectInviteCount       int64      `json:"direct_invite_count"`
	TeamInviteCount         int64      `json:"team_invite_count"`
	TotalCommissionMicros   int64      `json:"total_commission_micros"`
	ReferralRates           [3]float64 `json:"referral_rates"`
	ReferralRatesOverridden bool       `json:"referral_rates_overridden"`
}

func (s *Server) adminUsersWithCommission(w http.ResponseWriter, r *http.Request) {
	q := addQuery(s.DB.Model(&User{}).Where("deleted_at IS NULL"), r, "name", "email")
	var total int64
	if err := q.Count(&total).Error; err != nil {
		fail(w, err)
		return
	}
	offset, limit := pageParams(r)
	users := []User{}
	if err := q.Order("created_at DESC, id DESC").Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		fail(w, err)
		return
	}
	items := make([]adminUserCommissionView, 0, len(users))
	ids := make([]string, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.ID)
	}
	rates, overrides, err := effectiveReferralBasisPoints(s.DB, ids)
	if err != nil {
		fail(w, err)
		return
	}
	teamCounts := map[string][2]int64{}
	commissions := map[string]int64{}
	if len(ids) > 0 {
		teamSQL := `SELECT r.inviter_id AS owner_id, u.id, 1 AS level FROM referral_accounts r JOIN users u ON u.id = r.user_id WHERE r.inviter_id IN ? AND u.id <> r.inviter_id
		UNION SELECT p.inviter_id AS owner_id, u.id, 2 AS level FROM referral_accounts r JOIN referral_accounts p ON p.user_id = r.inviter_id JOIN users u ON u.id = r.user_id WHERE p.inviter_id IN ? AND u.id <> p.inviter_id AND r.user_id <> p.user_id
		UNION SELECT g.inviter_id AS owner_id, u.id, 3 AS level FROM referral_accounts r JOIN referral_accounts p ON p.user_id = r.inviter_id JOIN referral_accounts g ON g.user_id = p.inviter_id JOIN users u ON u.id = r.user_id WHERE g.inviter_id IN ? AND u.id <> g.inviter_id AND r.user_id <> p.user_id AND r.user_id <> g.user_id AND p.user_id <> g.user_id`
		var counts []struct {
			OwnerID string
			Direct  int64
			Team    int64
		}
		if err := s.DB.Raw("SELECT owner_id, SUM(CASE WHEN level = 1 THEN 1 ELSE 0 END) AS direct, COUNT(*) AS team FROM ("+teamSQL+") AS teams GROUP BY owner_id", ids, ids, ids).Scan(&counts).Error; err != nil {
			fail(w, err)
			return
		}
		for _, count := range counts {
			teamCounts[count.OwnerID] = [2]int64{count.Direct, count.Team}
		}
		var rewards []struct {
			UserID string
			Total  int64
		}
		if err := s.DB.Model(&ReferralReward{}).Select("user_id, COALESCE(SUM(amount_micros), 0) AS total").Where("user_id IN ?", ids).Group("user_id").Scan(&rewards).Error; err != nil {
			fail(w, err)
			return
		}
		for _, reward := range rewards {
			commissions[reward.UserID] = reward.Total
		}
	}
	for _, user := range users {
		counts := teamCounts[user.ID]
		items = append(items, adminUserCommissionView{User: user, DirectInviteCount: counts[0], TeamInviteCount: counts[1], TotalCommissionMicros: commissions[user.ID], ReferralRates: commissionPercentages(rates[user.ID]), ReferralRatesOverridden: overrides[user.ID]})
	}
	jsonOut(w, 200, referralPage[adminUserCommissionView]{Data: items, Total: total})
}
