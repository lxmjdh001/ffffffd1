package platform

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func seedReferralAccount(t *testing.T, s *Server, user User, inviter *User) ReferralAccount {
	t.Helper()
	var inviterID *string
	if inviter != nil {
		inviterID = &inviter.ID
	}
	account, err := createReferralAccount(s.DB, user.ID, inviterID, user.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func seedReferralOrder(t *testing.T, s *Server, user User, method string, amount, bonus int64) Order {
	t.Helper()
	order := Order{ID: randomHex(16), UserID: user.ID, IdempotencyKey: randomHex(16), Method: method, AmountMicros: amount, BonusMicros: bonus, Status: "pending", CreatedAt: s.Now(), ExpiresAt: s.Now().Add(time.Hour)}
	if err := s.DB.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	return order
}

func referralOverview(t *testing.T, s *Server, user User, query string) (referralResponse, string) {
	t.Helper()
	cookie, _ := sessionFor(t, s, user)
	w := call(t, s, "GET", "/api/referrals"+query, nil, cookie, "")
	requireCode(t, w, 200)
	var response referralResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response, w.Body.String()
}

func TestReferralRegistrationPermanentBindingAndInvalidCodes(t *testing.T) {
	s := testServer(t)
	inviter := seedUser(t, s, 0)
	account := seedReferralAccount(t, s, inviter, nil)
	register := func(code string, want int) User {
		t.Helper()
		input := authInput{Email: randomHex(8) + "@example.com", Name: "受邀用户", Password: "123456", InviteCode: code, CaptchaID: randomHex(16), CaptchaCode: "12345"}
		if err := s.DB.Create(&Challenge{ID: input.CaptchaID, Hash: s.mac(input.CaptchaID + ":" + input.CaptchaCode), ExpiresAt: s.Now().Add(time.Minute)}).Error; err != nil {
			t.Fatal(err)
		}
		w := call(t, s, "POST", "/api/auth/register", input, nil, "")
		requireCode(t, w, want)
		var response struct{ User User }
		if want == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
		}
		return response.User
	}
	registered := register("  "+strings.ToUpper(account.InviteCode)+"  ", 200)
	var bound ReferralAccount
	if err := s.DB.First(&bound, "user_id = ?", registered.ID).Error; err != nil || bound.InviterID == nil || *bound.InviterID != inviter.ID || bound.InviteCode == account.InviteCode {
		t.Fatal("registration did not create an independent code and permanent inviter")
	}
	// Existing users have no API that can replace a registration's inviter.
	cookie, csrf := sessionFor(t, s, registered)
	requireCode(t, call(t, s, "PATCH", "/api/me", map[string]string{"name": "新名称", "invite_code": randomHex(8)}, cookie, csrf), 400)
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.First(&bound, "user_id = ?", registered.ID).Error; err != nil || bound.InviterID == nil || *bound.InviterID != inviter.ID {
		t.Fatal("profile update or repeated migration changed the inviter")
	}
	uninvited := register(" ", 200)
	var unbound ReferralAccount
	if err := s.DB.First(&unbound, "user_id = ?", uninvited.ID).Error; err != nil || unbound.InviterID != nil {
		t.Fatal("registration without an invitation acquired an inviter")
	}
	before := countRows(t, s, &User{})
	for _, code := range []string{"invalid", randomHex(8)} {
		register(code, 400)
	}
	if err := s.DB.Model(&inviter).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	register(account.InviteCode, 400)
	if countRows(t, s, &User{}) != before || countRows(t, s, &ReferralAccount{}) != before {
		t.Fatal("invalid invitations left partially registered accounts")
	}
	if err := s.DB.Model(&inviter).Update("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := registrationInviter(s.DB, account.InviteCode, inviter.ID); err == nil {
		t.Fatal("self invitation accepted")
	}
}

func TestReferralThreeLevelsPrincipalOnlyAndPermanentRewards(t *testing.T) {
	for _, method := range []string{"manual", "usdt", "alipay", "wechat"} {
		t.Run(method, func(t *testing.T) {
			s := testServer(t)
			chain := make([]User, 5)
			for i := range chain {
				chain[i] = seedUser(t, s, 0)
				var inviter *User
				if i > 0 {
					inviter = &chain[i-1]
				}
				seedReferralAccount(t, s, chain[i], inviter)
			}
			payer := chain[4]
			// Disabling an existing ancestor does not expire the permanent
			// relationship or change subsequent recharge entitlements.
			if err := s.DB.Model(&chain[2]).Update("disabled", true).Error; err != nil {
				t.Fatal(err)
			}
			order := seedReferralOrder(t, s, payer, method, 100*Dollar, 50*Dollar)
			confirm := func() error { return s.settle(order.ID, "test:"+order.ID, method, order.AmountMicros) }
			if method == "manual" {
				admin := seedUser(t, s, 0)
				if err := s.DB.Model(&admin).Update("role", "admin").Error; err != nil {
					t.Fatal(err)
				}
				if err := s.DB.Model(&order).Update("status", "expired").Error; err != nil {
					t.Fatal(err)
				}
				cookie, csrf := sessionFor(t, s, admin)
				confirm = func() error {
					w := call(t, s, "POST", "/api/admin/orders/"+order.ID+"/settle", map[string]string{}, cookie, csrf)
					if w.Code != 200 {
						return fmt.Errorf("admin settle status=%d body=%s", w.Code, w.Body.String())
					}
					return nil
				}
			}
			// Repeated concurrent confirmations all join one atomic settlement.
			var wg sync.WaitGroup
			for range 16 {
				wg.Go(func() {
					if err := confirm(); err != nil {
						t.Errorf("settlement: %v", err)
					}
				})
			}
			wg.Wait()
			for i, want := range []int64{0, 2 * Dollar, 2 * Dollar, 10 * Dollar, 150 * Dollar} {
				var saved User
				if err := s.DB.First(&saved, "id = ?", chain[i].ID).Error; err != nil || saved.BalanceMicros != want {
					t.Fatalf("level %d balance = %d, want %d", 4-i, saved.BalanceMicros, want)
				}
			}
			if countRows(t, s, &ReferralReward{}) != 3 || countRows(t, s, &Ledger{}) != 5 {
				t.Fatal("retries duplicated rewards or principal/gift entered reward basis")
			}
			var reward ReferralReward
			if err := s.DB.First(&reward, "order_id = ? AND level = 1", order.ID).Error; err != nil || reward.UserID != chain[3].ID || reward.SourceUserID != payer.ID || reward.RechargeMicros != 100*Dollar || reward.AmountMicros != 10*Dollar {
				t.Fatal("reward does not trace the principal, payer and beneficiary")
			}
			var ledger Ledger
			if err := s.DB.First(&ledger, "reference = ?", "referral:"+order.ID+":1").Error; err != nil || ledger.Kind != "referral_reward" || ledger.UserID != chain[3].ID {
				t.Fatal("reward did not credit spendable balance with a unique ledger reference")
			}
			// PostgreSQL stores microsecond timestamps, unlike SQLite's text.
			originalNow := s.Now().Truncate(time.Microsecond)
			s.Now = func() time.Time { return originalNow.AddDate(5, 0, 0) }
			next := seedReferralOrder(t, s, payer, method, 1234567, 0)
			if err := s.settle(next.ID, "test:"+next.ID, method, next.AmountMicros); err != nil {
				t.Fatal(err)
			}
			var later ReferralReward
			if err := s.DB.First(&later, "order_id = ? AND level = 1", next.ID).Error; err != nil || later.AmountMicros != 123456 || !later.CreatedAt.Equal(s.Now()) {
				t.Fatal("invitation expired or fractional credit did not round down")
			}
		})
	}
}

func TestReferralConcurrentBranchesAndAncestorRecharges(t *testing.T) {
	s := testServer(t)
	ancestor := seedUser(t, s, 0)
	seedReferralAccount(t, s, ancestor, nil)
	var orders []Order
	for range 12 {
		child := seedUser(t, s, 0)
		seedReferralAccount(t, s, child, &ancestor)
		orders = append(orders, seedReferralOrder(t, s, child, "usdt", 100*Dollar, 0))
	}
	orders = append(orders, seedReferralOrder(t, s, ancestor, "manual", 100*Dollar, 0))
	var wg sync.WaitGroup
	for _, order := range orders {
		for range 3 {
			wg.Go(func() {
				if err := s.settle(order.ID, "test:"+order.ID, order.Method, order.AmountMicros); err != nil {
					t.Errorf("settlement: %v", err)
				}
			})
		}
	}
	wg.Wait()
	if err := s.DB.First(&ancestor, "id = ?", ancestor.ID).Error; err != nil || ancestor.BalanceMicros != 220*Dollar || countRows(t, s, &ReferralReward{}) != 12 {
		t.Fatal("concurrent branch rewards lost or duplicated an ancestor balance update")
	}
}

func TestReferralFailureRollsBackEntireRecharge(t *testing.T) {
	s := testServer(t)
	ancestor := seedUser(t, s, 100000000*Dollar)
	middle := seedUser(t, s, 0)
	payer := seedUser(t, s, 0)
	seedReferralAccount(t, s, ancestor, nil)
	seedReferralAccount(t, s, middle, &ancestor)
	seedReferralAccount(t, s, payer, &middle)
	order := seedReferralOrder(t, s, payer, "manual", 100*Dollar, 50*Dollar)
	if err := s.settle(order.ID, "test:"+order.ID, order.Method, order.AmountMicros); err == nil {
		t.Fatal("over-cap ancestor reward succeeded")
	}
	for _, user := range []User{middle, payer} {
		var saved User
		if err := s.DB.First(&saved, "id = ?", user.ID).Error; err != nil || saved.BalanceMicros != 0 {
			t.Fatal("failed reward committed some of the credited balances")
		}
	}
	if err := s.DB.First(&order, "id = ?", order.ID).Error; err != nil || order.Status != "pending" || order.TradeID != nil || order.PaidAt != nil || countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &ReferralReward{}) != 0 {
		t.Fatal("failed reward committed paid status or partial ledger/reward history")
	}
}

func TestReferralRejectsCorruptCyclesWithoutCreditingPayer(t *testing.T) {
	for _, self := range []bool{false, true} {
		t.Run(fmt.Sprint(self), func(t *testing.T) {
			s := testServer(t)
			first := seedUser(t, s, 0)
			second := seedUser(t, s, 0)
			seedReferralAccount(t, s, second, &first)
			inviter := &second
			if self {
				inviter = &first
			}
			seedReferralAccount(t, s, first, inviter)
			order := seedReferralOrder(t, s, first, "manual", 100*Dollar, 0)
			if err := s.settle(order.ID, "test:"+order.ID, order.Method, order.AmountMicros); err == nil {
				t.Fatal("cyclic invitation was settled")
			}
			if countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &ReferralReward{}) != 0 {
				t.Fatal("cyclic invitation credited a balance")
			}
		})
	}
}

func TestReferralOverviewCountsPaginationAndPrivacy(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	seedReferralAccount(t, s, owner, nil)
	chain := []User{owner}
	for range 4 {
		user := seedUser(t, s, 0)
		user.Name = fmt.Sprintf("成员%d", len(chain))
		if err := s.DB.Model(&user).Update("name", user.Name).Error; err != nil {
			t.Fatal(err)
		}
		seedReferralAccount(t, s, user, &chain[len(chain)-1])
		chain = append(chain, user)
		order := seedReferralOrder(t, s, user, "manual", 100*Dollar, 0)
		if err := s.settle(order.ID, "test:"+order.ID, order.Method, order.AmountMicros); err != nil {
			t.Fatal(err)
		}
	}
	response, body := referralOverview(t, s, owner, "?per_page=1&members_page=2&rewards_page=2")
	if response.InviteCode == "" || response.InviteURL != s.Config.PublicURL+"/#/register?invite="+response.InviteCode || response.Rates != [3]int64{10, 2, 2} || response.LevelCounts != [3]int64{1, 1, 1} || response.TeamCount != 3 || response.DirectCount != 1 || response.TotalRewardMicros != 14*Dollar {
		t.Fatalf("incorrect referral overview: %+v", response)
	}
	if response.Rewards.Total != 3 || response.Members.Total != 3 || len(response.Rewards.Data) != 1 || len(response.Members.Data) != 1 || response.Rewards.Data[0].SourceName == "" {
		t.Fatal("independent rewards/member pagination lost totals or display names")
	}
	for _, field := range []string{"email", "password", "source_user_id", "order_id", "inviter_id", "balance_micros"} {
		if strings.Contains(body, `"`+field+`"`) {
			t.Fatalf("referral response exposes private field %s", field)
		}
	}
	outOfRange, _ := referralOverview(t, s, owner, "?members_page=999999999&rewards_page=999999999")
	if len(outOfRange.Rewards.Data) != 0 || len(outOfRange.Members.Data) != 0 {
		t.Fatal("out-of-range pages did not return empty arrays")
	}
	requireCode(t, call(t, s, "GET", "/api/referrals", nil, nil, ""), 401)
}

func TestReferralMigrationPreservesLegacyFundsAndStableCodes(t *testing.T) {
	s := testServer(t)
	legacy := seedUser(t, s, 123*Dollar)
	paid := seedReferralOrder(t, s, legacy, "manual", 100*Dollar, 0)
	if err := s.settle(paid.ID, "test:"+paid.ID, paid.Method, paid.AmountMicros); err != nil {
		t.Fatal(err)
	}
	for _, table := range []any{&ReferralReward{}, &ReferralAccount{}} {
		if err := s.DB.Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DB.Delete(&Migration{}, "version = 9").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	var account ReferralAccount
	if err := s.DB.First(&account, "user_id = ?", legacy.ID).Error; err != nil || account.InviteCode == "" || account.InviterID != nil {
		t.Fatal("legacy account did not receive a unique code without an inviter")
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	response, _ := referralOverview(t, s, legacy, "")
	if response.InviteCode != account.InviteCode || response.TotalRewardMicros != 0 || response.TeamCount != 0 {
		t.Fatal("repeat migration rewrote the code or rewarded historical payments")
	}
	if err := s.DB.First(&legacy, "id = ?", legacy.ID).Error; err != nil || legacy.BalanceMicros != 223*Dollar || countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("migration rewrote existing funds or ledger")
	}
	// Users created through trusted CLI/bootstrap paths also obtain a stable
	// code automatically, without adding nullable unique fields to User.
	newUser := seedUser(t, s, 0)
	first, _ := referralOverview(t, s, newUser, "")
	second, _ := referralOverview(t, s, newUser, "")
	if first.InviteCode == "" || first.InviteCode != second.InviteCode || first.InviteCode == account.InviteCode {
		t.Fatal("lazy referral account creation did not keep one independent code")
	}
	var migrations int64
	if err := s.DB.Model(&Migration{}).Where("version = 9").Count(&migrations).Error; err != nil || migrations != 1 {
		t.Fatal("referral migration was not recorded exactly once")
	}
}
