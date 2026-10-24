package platform

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
)

func commissionAdmin(t *testing.T, s *Server) User {
	t.Helper()
	user := seedUser(t, s, 0)
	if err := s.DB.Model(&user).Update("role", "admin").Error; err != nil {
		t.Fatal(err)
	}
	return user
}

func TestReferralCommissionSettingsValidationPermissionsAndMigration(t *testing.T) {
	s := testServer(t)
	admin := commissionAdmin(t, s)
	member := seedUser(t, s, 0)
	ac, csrf := sessionFor(t, s, admin)
	uc, ucCSRF := sessionFor(t, s, member)
	get := call(t, s, "GET", "/api/admin/commission-settings", nil, ac, "")
	requireCode(t, get, 200)
	var config commissionSettingsView
	json.Unmarshal(get.Body.Bytes(), &config)
	if config.Rates != [3]float64{10, 2, 2} {
		t.Fatal("new installation changed default rates")
	}
	for _, method := range []string{"GET", "PUT"} {
		requireCode(t, call(t, s, method, "/api/admin/commission-settings", map[string]any{"rates": []string{"10", "2", "2"}}, uc, ucCSRF), 403)
	}
	requireCode(t, call(t, s, "PUT", "/api/admin/commission-settings", map[string]any{"rates": []string{"10", "2", "2"}}, ac, ""), 403)
	for _, values := range [][]string{nil, {}, {"10", "2"}, {"1", "2", "3", "4"}, {"-1", "2", "2"}, {"100.01", "2", "2"}, {"1.001", "2", "2"}, {"01", "2", "2"}, {" 10", "2", "2"}, {"1e1", "2", "2"}, {"1.", "2", "2"}, {"NaN", "2", "2"}} {
		requireCode(t, call(t, s, "PUT", "/api/admin/commission-settings", map[string]any{"rates": values}, ac, csrf), 400)
	}
	requireCode(t, call(t, s, "PUT", "/api/admin/commission-settings", map[string]any{"rates": []int{10, 2, 2}}, ac, csrf), 400)
	requireCode(t, call(t, s, "PUT", "/api/admin/commission-settings", map[string]any{"rates": []string{"10", "2", "2"}, "unknown": true}, ac, csrf), 400)
	w := call(t, s, "PUT", "/api/admin/commission-settings", map[string]any{"rates": []string{"0", "99.99", "100.00"}}, ac, csrf)
	requireCode(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &config)
	if config.Rates != [3]float64{0, 99.99, 100} || config.UpdatedAt.IsZero() {
		t.Fatal("fractional/zero/max percentages failed")
	}
	overview, _ := referralOverview(t, s, member, "")
	if overview.Rates != config.Rates {
		t.Fatal("non-overridden user did not follow global settings")
	}
	// Missing singleton config has a safe read fallback and can be recreated by
	// a later explicit admin save without affecting existing user money.
	if err := s.DB.Delete(&CommissionSetting{}, 1).Error; err != nil {
		t.Fatal(err)
	}
	w = call(t, s, "GET", "/api/admin/commission-settings", nil, ac, "")
	requireCode(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &config)
	if config.Rates != [3]float64{10, 2, 2} {
		t.Fatal("missing global config lost the default fallback")
	}
	requireCode(t, call(t, s, "PUT", "/api/admin/commission-settings", map[string]any{"rates": []string{"12.34", "5.67", "8.90"}}, ac, csrf), 200)
	var usersBefore []User
	var ledgerBefore []Ledger
	var rewardsBefore []ReferralReward
	s.DB.Order("id").Find(&usersBefore)
	s.DB.Order("id").Find(&ledgerBefore)
	s.DB.Order("id").Find(&rewardsBefore)
	if err := s.DB.Delete(&Migration{}, "version = 12").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Migrator().DropTable(&CommissionSetting{}, &ReferralRateOverride{}); err != nil {
		t.Fatal(err)
	}
	const name = "test:commission-migration-fail"
	s.DB.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*Migration); ok && row.Version == 12 {
			tx.AddError(errors.New("forced commission migration failure"))
		}
	})
	if err := Migrate(s.DB); err == nil {
		t.Fatal("failed migration unexpectedly committed")
	}
	s.DB.Callback().Create().Remove(name)
	if s.DB.Migrator().HasTable(&CommissionSetting{}) || s.DB.Migrator().HasTable(&ReferralRateOverride{}) {
		t.Fatal("failed commission migration left partial new tables")
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	var usersAfter []User
	var ledgerAfter []Ledger
	var rewardsAfter []ReferralReward
	s.DB.Order("id").Find(&usersAfter)
	s.DB.Order("id").Find(&ledgerAfter)
	s.DB.Order("id").Find(&rewardsAfter)
	if !reflect.DeepEqual(usersBefore, usersAfter) || !reflect.DeepEqual(ledgerBefore, ledgerAfter) || !reflect.DeepEqual(rewardsBefore, rewardsAfter) {
		t.Fatal("commission migration changed existing balances or reward/ledger history")
	}
	var count int64
	s.DB.Model(&CommissionSetting{}).Count(&count)
	if count != 1 {
		t.Fatal("migration was not idempotent")
	}
}

func TestReferralCommissionBeneficiaryOverridesFutureOnlyAndTeamStats(t *testing.T) {
	s := testServer(t)
	admin := commissionAdmin(t, s)
	ac, csrf := sessionFor(t, s, admin)
	chain := make([]User, 5)
	for i := range chain {
		chain[i] = seedUser(t, s, 0)
		var inviter *User
		if i > 0 {
			inviter = &chain[i-1]
		}
		seedReferralAccount(t, s, chain[i], inviter)
	}
	branch := seedUser(t, s, 0)
	seedReferralAccount(t, s, branch, &chain[0])
	set := func(user User, rates []string) {
		t.Helper()
		w := call(t, s, "PUT", "/api/admin/users/"+user.ID+"/referral-rates", map[string]any{"rates": rates}, ac, csrf)
		requireCode(t, w, 200)
		var view referralRatesView
		json.Unmarshal(w.Body.Bytes(), &view)
		want, _ := parseCommissionRates(rates)
		if !view.Overridden || view.Rates != commissionPercentages(want) {
			t.Fatal("user override response does not match saved rates")
		}
	}
	for i, rates := range [][]string{{"30", "40", "50"}, {"11", "22", "33"}, {"1", "2", "3"}, {"99", "99", "99"}} {
		set(chain[i], rates)
	}
	order := seedReferralOrder(t, s, chain[3], "manual", 100*Dollar, 50*Dollar)
	settle := func(order Order) {
		t.Helper()
		if err := s.settle(order.ID, "commission-test:"+order.ID, order.Method, order.AmountMicros); err != nil {
			t.Fatal(err)
		}
	}
	settle(order)
	checkRewards := func(order Order, expected map[string]int64) {
		t.Helper()
		var rows []ReferralReward
		if err := s.DB.Where("order_id = ?", order.ID).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) != len(expected) {
			t.Fatalf("reward count %d, want %d", len(rows), len(expected))
		}
		for _, reward := range rows {
			if expected[reward.UserID] != reward.AmountMicros {
				t.Fatalf("wrong beneficiary/level rate: user=%s level=%d amount=%d want=%d", reward.UserID, reward.Level, reward.AmountMicros, expected[reward.UserID])
			}
		}
	}
	firstExpected := map[string]int64{chain[0].ID: 50 * Dollar, chain[1].ID: 22 * Dollar, chain[2].ID: Dollar}
	checkRewards(order, firstExpected)
	// This pending order predates the update, but rates apply when its payment
	// settles. A zero direct rate does not suppress higher-level rewards.
	pending := seedReferralOrder(t, s, chain[3], "manual", 100*Dollar, 0)
	set(chain[2], []string{"0", "2", "3"})
	set(chain[1], []string{"11", "2.25", "33"})
	set(chain[0], []string{"30", "40", "3.33"})
	requireCode(t, call(t, s, "PUT", "/api/admin/commission-settings", map[string]any{"rates": []string{"8.88", "4.44", "1.11"}}, ac, csrf), 200)
	settle(order)
	checkRewards(order, firstExpected)
	settle(pending)
	checkRewards(pending, map[string]int64{chain[0].ID: 3330000, chain[1].ID: 2250000})
	overview, _ := referralOverview(t, s, chain[0], "")
	if overview.Rates != [3]float64{30, 40, 3.33} || overview.WithdrawalReconciliationRequired {
		t.Fatal("beneficiary overview did not use own override or proof drifted")
	}
	w := call(t, s, "DELETE", "/api/admin/users/"+chain[0].ID+"/referral-rates", nil, ac, csrf)
	requireCode(t, w, 200)
	var cleared referralRatesView
	json.Unmarshal(w.Body.Bytes(), &cleared)
	if cleared.Overridden || cleared.Rates != [3]float64{8.88, 4.44, 1.11} {
		t.Fatal("clearing override did not restore latest global settings")
	}
	requireCode(t, call(t, s, "DELETE", "/api/admin/users/"+chain[0].ID+"/referral-rates", nil, ac, csrf), 200)
	next := seedReferralOrder(t, s, chain[3], "manual", 100*Dollar+1, 0)
	settle(next)
	checkRewards(next, map[string]int64{chain[0].ID: 1110000, chain[1].ID: 2250000})
	// Three-level team counts exclude the fourth descendant, while a second
	// direct branch contributes to both direct and team totals.
	w = call(t, s, "GET", "/api/admin/users?q="+chain[0].Email, nil, ac, "")
	requireCode(t, w, 200)
	var listing referralPage[adminUserCommissionView]
	json.Unmarshal(w.Body.Bytes(), &listing)
	if listing.Total != 1 || len(listing.Data) != 1 {
		t.Fatal("admin user search/pagination changed")
	}
	row := listing.Data[0]
	if row.ID != chain[0].ID || row.DirectInviteCount != 2 || row.TeamInviteCount != 4 || row.TotalCommissionMicros != 54440000 || row.ReferralRates != [3]float64{8.88, 4.44, 1.11} || row.ReferralRatesOverridden {
		t.Fatalf("wrong commission admin row: %#v", row)
	}
	if strings.Contains(w.Body.String(), "password_hash") {
		t.Fatal("admin user enrichment exposed password hash")
	}
	w = call(t, s, "GET", "/api/admin/users?q="+chain[1].Email, nil, ac, "")
	requireCode(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &listing)
	row = listing.Data[0]
	if row.DirectInviteCount != 1 || row.TeamInviteCount != 3 || !row.ReferralRatesOverridden || row.ReferralRates != [3]float64{11, 2.25, 33} {
		t.Fatal("individual override/team admin fields wrong")
	}
	uc, ucCSRF := sessionFor(t, s, chain[0])
	requireCode(t, call(t, s, "PUT", "/api/admin/users/"+chain[1].ID+"/referral-rates", map[string]any{"rates": []string{"1", "2", "3"}}, uc, ucCSRF), 403)
	requireCode(t, call(t, s, "DELETE", "/api/admin/users/"+chain[1].ID+"/referral-rates", nil, uc, ucCSRF), 403)
	requireCode(t, call(t, s, "PUT", "/api/admin/users/"+chain[1].ID+"/referral-rates", map[string]any{"rates": []string{"1", "2", "3"}}, ac, ""), 403)
	requireCode(t, call(t, s, "DELETE", "/api/admin/users/"+chain[1].ID+"/referral-rates", nil, ac, ""), 403)
	requireCode(t, call(t, s, "PUT", "/api/admin/users/"+randomHex(16)+"/referral-rates", map[string]any{"rates": []string{"1", "2", "3"}}, ac, csrf), 404)
}

func TestReferralCommissionConcurrentUpdatesAndAtomicAudit(t *testing.T) {
	s := testServer(t)
	admin := commissionAdmin(t, s)
	user := seedUser(t, s, 0)
	ac, csrf := sessionFor(t, s, admin)
	updates := [][]string{{"10.01", "2.02", "3.03"}, {"44.44", "55.55", "66.66"}}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, rates := range updates {
		rates := rates
		wg.Go(func() {
			<-start
			requireCode(t, call(t, s, "PUT", "/api/admin/commission-settings", map[string]any{"rates": rates}, ac, csrf), 200)
		})
	}
	close(start)
	wg.Wait()
	config, err := globalCommissionSettings(s.DB)
	if err != nil {
		t.Fatal(err)
	}
	valid := false
	for _, update := range updates {
		rates, _ := parseCommissionRates(update)
		valid = valid || commissionBasisPoints(config) == rates
	}
	if !valid {
		t.Fatal("concurrent config save mixed multiple updates")
	}
	const name = "test:commission-audit-fail"
	s.DB.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*Audit); ok && strings.HasPrefix(row.Action, "commission.") {
			tx.AddError(errors.New("forced commission audit failure"))
		}
	})
	requireCode(t, call(t, s, "PUT", "/api/admin/commission-settings", map[string]any{"rates": []string{"1", "2", "3"}}, ac, csrf), 500)
	after, err := globalCommissionSettings(s.DB)
	if err != nil || !reflect.DeepEqual(config, after) {
		t.Fatal("failed audit committed a global config change")
	}
	requireCode(t, call(t, s, "PUT", "/api/admin/users/"+user.ID+"/referral-rates", map[string]any{"rates": []string{"1", "2", "3"}}, ac, csrf), 500)
	var count int64
	s.DB.Model(&ReferralRateOverride{}).Where("user_id = ?", user.ID).Count(&count)
	if count != 0 {
		t.Fatal("failed audit committed an individual override")
	}
	s.DB.Callback().Create().Remove(name)
}
