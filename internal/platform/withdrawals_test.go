package platform

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

func withdrawalCall(t *testing.T, s *Server, method, path string, body any, cookie *http.Cookie, csrf, key string) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, "http://localhost:8088"+path, bytes.NewReader(raw))
	if cookie != nil {
		r.AddCookie(cookie)
	}
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func withdrawalCredit(t *testing.T, s *Server, id string, amount int64, kind string) {
	t.Helper()
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		u, err := lockUser(tx, id)
		if err != nil {
			return err
		}
		return changeBalance(tx, &u, amount, "fixture:"+randomHex(16), kind, "测试账本")
	}); err != nil {
		t.Fatal(err)
	}
}

func withdrawalState(t *testing.T, s *Server, id string) (User, ReferralWallet) {
	t.Helper()
	var u User
	var wallet ReferralWallet
	if err := s.DB.First(&u, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.First(&wallet, "user_id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return u, wallet
}

func decodeWithdrawal(t *testing.T, w *httptest.ResponseRecorder) ReferralWithdrawal {
	t.Helper()
	var out ReferralWithdrawal
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestReferralWithdrawalProvenanceAndInputValidation(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	withdrawalCredit(t, s, u.ID, 200*Dollar, "referral_reward")
	withdrawalCredit(t, s, u.ID, 150*Dollar, "recharge_bonus")
	withdrawalCredit(t, s, u.ID, -50*Dollar, "subscription")
	_, wallet := withdrawalState(t, s, u.ID)
	if wallet.AvailableMicros != 150*Dollar {
		t.Fatal("spending did not consume commission first")
	}
	withdrawalCredit(t, s, u.ID, -200*Dollar, "download")
	for _, kind := range []string{"recharge", "recharge_bonus", "adjustment"} {
		withdrawalCredit(t, s, u.ID, 200*Dollar, kind)
	}
	_, wallet = withdrawalState(t, s, u.ID)
	if wallet.AvailableMicros != 0 {
		t.Fatal("recharge/gift/admin credit resurrected spent commission")
	}
	request := func(amount, address, key string) *httptest.ResponseRecorder {
		return withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": amount, "address": address}, cookie, csrf, key)
	}
	requireCode(t, request("100", usdtContract, randomHex(16)), 400)
	withdrawalCredit(t, s, u.ID, 150*Dollar+123456, "referral_reward")
	for _, amount := range []string{"99.999999", "0", "-100", "100.0000001", "1e2", "100000000", " 100", "100."} {
		requireCode(t, request(amount, usdtContract, randomHex(16)), 400)
	}
	for _, address := range []string{"", "0x" + randomHex(20), "T" + strings.Repeat("1", 33), usdtContract[:33] + "1"} {
		requireCode(t, request("100", address, randomHex(16)), 400)
	}
	requireCode(t, request("100", usdtContract, ""), 400)
	requireCode(t, withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]any{"amount": 100, "address": usdtContract}, cookie, csrf, randomHex(16)), 400)
	requireCode(t, withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": "100", "address": usdtContract, "user_id": randomHex(16)}, cookie, csrf, randomHex(16)), 400)
	key := randomHex(16)
	w := request("100.123456", " "+usdtContract+" ", key)
	requireCode(t, w, 201)
	out := decodeWithdrawal(t, w)
	if out.AmountMicros != 100*Dollar+123456 || out.Address != usdtContract || out.Status != "pending" || out.PayoutReference != nil || out.ReviewedAt != nil {
		t.Fatalf("wrong withdrawal: %#v", out)
	}
	replayed := request("100.123456", usdtContract, key)
	requireCode(t, replayed, 200)
	if decodeWithdrawal(t, replayed).ID != out.ID {
		t.Fatal("repeated request created a second withdrawal")
	}
	requireCode(t, request("100", usdtContract, key), 409)
	u, wallet = withdrawalState(t, s, u.ID)
	if u.BalanceMicros != 750*Dollar || wallet.AvailableMicros != 50*Dollar || u.HeldMicros != 0 {
		t.Fatalf("wrong reserved balances user=%#v wallet=%#v", u, wallet)
	}
	var ledger []Ledger
	s.DB.Where("user_id = ? AND kind = ?", u.ID, "withdrawal_hold").Find(&ledger)
	if len(ledger) != 1 || ledger[0].AmountMicros != -out.AmountMicros {
		t.Fatal("request/replay debit was not exactly once")
	}
	var audits int64
	s.DB.Model(&Audit{}).Where("target = ?", out.ID).Count(&audits)
	if audits != 1 {
		t.Fatal("request replay wrote duplicate audit")
	}
	response, _ := referralOverview(t, s, u, "")
	if response.WithdrawalAvailableMicros != 50*Dollar || response.WithdrawalPendingMicros != out.AmountMicros || response.WithdrawalPaidMicros != 0 || response.WithdrawalMinimumMicros != 100*Dollar || response.WithdrawalReconciliationRequired {
		t.Fatalf("wrong withdrawal overview: %#v", response)
	}
}

func TestReferralWithdrawalReviewExactlyOnceAndRecords(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	other := seedUser(t, s, 0)
	admin := seedUser(t, s, 0)
	if err := s.DB.Model(&admin).Update("role", "admin").Error; err != nil {
		t.Fatal(err)
	}
	uc, ucCSRF := sessionFor(t, s, u)
	oc, ocCSRF := sessionFor(t, s, other)
	ac, acCSRF := sessionFor(t, s, admin)
	withdrawalCredit(t, s, u.ID, 600*Dollar, "referral_reward")
	create := func() ReferralWithdrawal {
		w := withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": "100", "address": usdtContract}, uc, ucCSRF, randomHex(16))
		requireCode(t, w, 201)
		return decodeWithdrawal(t, w)
	}
	first, second, third := create(), create(), create()
	review := func(record ReferralWithdrawal, action, note, reference string) *httptest.ResponseRecorder {
		return withdrawalCall(t, s, "POST", "/api/admin/referral-withdrawals/"+record.ID+"/review", map[string]string{"action": action, "note": note, "transaction_id": reference}, ac, acCSRF, "")
	}
	requireCode(t, call(t, s, "GET", "/api/referral-withdrawals", nil, nil, ""), 401)
	requireCode(t, withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": "100", "address": usdtContract}, uc, "", randomHex(16)), 403)
	for _, c := range []*http.Cookie{uc, oc} {
		requireCode(t, call(t, s, "GET", "/api/admin/referral-withdrawals", nil, c, ""), 403)
	}
	requireCode(t, withdrawalCall(t, s, "POST", "/api/admin/referral-withdrawals/"+first.ID+"/review", map[string]string{"action": "reject", "note": "非法权限"}, oc, ocCSRF, ""), 403)
	requireCode(t, withdrawalCall(t, s, "POST", "/api/admin/referral-withdrawals/"+first.ID+"/review", map[string]string{"action": "reject", "note": "缺少令牌"}, ac, "", ""), 403)
	var listing referralPage[ReferralWithdrawal]
	w := call(t, s, "GET", "/api/referral-withdrawals", nil, oc, "")
	requireCode(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &listing)
	if listing.Total != 0 || len(listing.Data) != 0 || strings.Contains(w.Body.String(), first.ID) {
		t.Fatal("owner records leaked to another user")
	}
	w = call(t, s, "GET", "/api/referral-withdrawals?page=2&per_page=2", nil, uc, "")
	requireCode(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &listing)
	if listing.Total != 3 || len(listing.Data) != 1 {
		t.Fatal("withdrawal records pagination failed")
	}
	requireCode(t, review(first, "paid", "", randomHex(32)), 409)
	requireCode(t, review(first, "approve", "审核通过", ""), 200)
	requireCode(t, review(first, "approve", "审核通过", ""), 200)
	requireCode(t, review(first, "approve", "更改审核备注", ""), 409)
	for _, bad := range []string{"", "not-a-chain-id", strings.Repeat("f", 63), strings.Repeat("x", 64)} {
		requireCode(t, review(first, "paid", "已线下打款", bad), 400)
	}
	txid := randomHex(32)
	w = review(first, "paid", "已线下打款", strings.ToUpper(txid))
	requireCode(t, w, 200)
	paid := decodeWithdrawal(t, w)
	if paid.Status != "paid" || paid.PaidAt == nil || paid.ReviewedAt == nil || paid.PayoutReference == nil || *paid.PayoutReference != txid {
		t.Fatal("paid record missing approval/reference/timestamp")
	}
	requireCode(t, review(first, "paid", "已线下打款", txid), 200)
	requireCode(t, review(first, "paid", "已线下打款", randomHex(32)), 409)
	requireCode(t, review(first, "reject", "不应退回", ""), 409)
	requireCode(t, review(first, "approve", "审核通过", ""), 409)
	requireCode(t, review(second, "reject", " ", ""), 400)
	requireCode(t, review(second, "reject", "收款信息需核对", ""), 200)
	requireCode(t, review(second, "reject", "收款信息需核对", ""), 200)
	requireCode(t, review(second, "reject", "不同原因", ""), 409)
	requireCode(t, review(second, "approve", "审核通过", ""), 409)
	requireCode(t, review(third, "approve", "人工核对通过", ""), 200)
	requireCode(t, review(third, "paid", "复用凭证", txid), 409)
	// Disabled users cannot request, but existing requests can still be resolved.
	if err := s.DB.Model(&u).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": "100", "address": usdtContract}, uc, ucCSRF, randomHex(16)), 401)
	requireCode(t, review(third, "reject", "用户已停用，退回余额", ""), 200)
	u, wallet := withdrawalState(t, s, u.ID)
	if u.BalanceMicros != 500*Dollar || wallet.AvailableMicros != 500*Dollar || u.HeldMicros != 0 {
		t.Fatal("approval/payment double debited, or rejection refunded more than once")
	}
	var refunds, holds, audits int64
	s.DB.Model(&Ledger{}).Where("user_id = ? AND kind = ?", u.ID, "withdrawal_refund").Count(&refunds)
	s.DB.Model(&Ledger{}).Where("user_id = ? AND kind = ?", u.ID, "withdrawal_hold").Count(&holds)
	s.DB.Model(&Audit{}).Where("action LIKE ?", "referral.withdrawal.%").Count(&audits)
	if refunds != 2 || holds != 3 || audits != 8 {
		t.Fatalf("wrong financial/audit count: refund=%d hold=%d audit=%d", refunds, holds, audits)
	}
	if err := s.DB.Model(&u).Update("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	overview, _ := referralOverview(t, s, u, "")
	if overview.WithdrawalAvailableMicros != 500*Dollar || overview.WithdrawalPendingMicros != 0 || overview.WithdrawalPaidMicros != 100*Dollar {
		t.Fatalf("wrong final overview: %#v", overview)
	}
	var adminListing referralPage[adminReferralWithdrawal]
	w = call(t, s, "GET", "/api/admin/referral-withdrawals?status=paid&q="+u.Email, nil, ac, "")
	requireCode(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &adminListing)
	if adminListing.Total != 1 || len(adminListing.Data) != 1 || adminListing.Data[0].ID != first.ID || adminListing.Data[0].UserName != u.Name || adminListing.Data[0].UserEmail != u.Email {
		t.Fatal("admin status/search/user fields failed")
	}
	requireCode(t, call(t, s, "GET", "/api/admin/referral-withdrawals?status=unknown", nil, ac, ""), 400)
	requireCode(t, review(ReferralWithdrawal{ID: randomHex(16)}, "approve", "", ""), 404)
}

func TestReferralWithdrawalHeldFundsAndRejectionHeadroom(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	admin := seedUser(t, s, 0)
	s.DB.Model(&admin).Update("role", "admin")
	uc, csrf := sessionFor(t, s, u)
	ac, acCSRF := sessionFor(t, s, admin)
	withdrawalCredit(t, s, u.ID, 150*Dollar, "referral_reward")
	if err := s.DB.Model(&u).Update("held_micros", 60*Dollar).Error; err != nil {
		t.Fatal(err)
	}
	request := func() *httptest.ResponseRecorder {
		return withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": "100", "address": usdtContract}, uc, csrf, randomHex(16))
	}
	requireCode(t, request(), 400)
	overview, _ := referralOverview(t, s, u, "")
	if overview.WithdrawalAvailableMicros != 90*Dollar {
		t.Fatal("overview ignored in-flight download holds")
	}
	if err := s.DB.Model(&u).Update("held_micros", 50*Dollar).Error; err != nil {
		t.Fatal(err)
	}
	w := request()
	requireCode(t, w, 201)
	out := decodeWithdrawal(t, w)
	u, wallet := withdrawalState(t, s, u.ID)
	if u.BalanceMicros != 50*Dollar || u.HeldMicros != 50*Dollar || wallet.AvailableMicros != 50*Dollar {
		t.Fatal("withdrawal altered download hold or overspent it")
	}
	withdrawalCredit(t, s, u.ID, 100000000*Dollar-150*Dollar, "recharge")
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		locked, err := lockUser(tx, u.ID)
		if err != nil {
			return err
		}
		return changeBalance(tx, &locked, 1, "exceed-cap", "adjustment", "too much")
	})
	var e *apiError
	if !errors.As(err, &e) || e.Status != 400 {
		t.Fatal("ordinary credit consumed withdrawal refund headroom")
	}
	w = withdrawalCall(t, s, "POST", "/api/admin/referral-withdrawals/"+out.ID+"/review", map[string]string{"action": "reject", "note": "退回测试"}, ac, acCSRF, "")
	requireCode(t, w, 200)
	u, wallet = withdrawalState(t, s, u.ID)
	if u.BalanceMicros != 100000000*Dollar || u.HeldMicros != 50*Dollar || wallet.AvailableMicros != 150*Dollar {
		t.Fatal("cap-edge rejection did not restore commission once")
	}
}

func TestReferralWithdrawalConcurrencyWithSpendingAndDownloadHold(t *testing.T) {
	for _, race := range []string{"spend", "reserve", "same-key", "different-key"} {
		t.Run(race, func(t *testing.T) {
			s := testServer(t)
			u := seedUser(t, s, 0)
			withdrawalCredit(t, s, u.ID, 150*Dollar, "referral_reward")
			uc, csrf := sessionFor(t, s, u)
			key := randomHex(16)
			start := make(chan struct{})
			var wg sync.WaitGroup
			codes := make(chan int, 2)
			for i := range 2 {
				i := i
				wg.Go(func() {
					<-start
					if i == 0 || race == "same-key" || race == "different-key" {
						requestKey := key
						if race == "different-key" && i == 1 {
							requestKey = randomHex(16)
						}
						w := withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": "100", "address": usdtContract}, uc, csrf, requestKey)
						codes <- w.Code
						return
					}
					err := s.DB.Transaction(func(tx *gorm.DB) error {
						locked, err := lockUser(tx, u.ID)
						if err != nil {
							return err
						}
						if race == "spend" {
							return changeBalance(tx, &locked, -80*Dollar, "race-spend", "subscription", "并发消费")
						}
						if locked.BalanceMicros-locked.HeldMicros < 80*Dollar {
							return problem(402, "insufficient")
						}
						return tx.Model(&locked).Update("held_micros", locked.HeldMicros+80*Dollar).Error
					})
					if err == nil {
						codes <- 200
					} else {
						var e *apiError
						if !errors.As(err, &e) || e.Status != 402 {
							t.Errorf("race failed unexpectedly: %v", err)
						}
						codes <- 402
					}
				})
			}
			close(start)
			wg.Wait()
			close(codes)
			var created, failed, successful int
			for code := range codes {
				if code == 201 {
					created++
				}
				if code >= 400 {
					failed++
				} else {
					successful++
				}
				if code != 200 && code != 201 && code != 400 && code != 402 {
					t.Errorf("unexpected HTTP status %d", code)
				}
			}
			u, wallet := withdrawalState(t, s, u.ID)
			var withdrawals int64
			s.DB.Model(&ReferralWithdrawal{}).Where("user_id = ?", u.ID).Count(&withdrawals)
			if u.BalanceMicros < u.HeldMicros || wallet.AvailableMicros < 0 || withdrawals > 1 || created > 1 {
				t.Fatal("concurrent spending/withdrawal broke fund constraints")
			}
			if race == "same-key" {
				if successful != 2 || created != 1 || withdrawals != 1 || u.BalanceMicros != 50*Dollar || wallet.AvailableMicros != 50*Dollar {
					t.Fatal("same-key concurrent request was not exactly once")
				}
			} else if successful != 1 || failed != 1 {
				t.Fatal("both conflicting operations consumed the same balance")
			}
		})
	}
}

func TestReferralWithdrawalMigrationHistoricalReplayAndRollback(t *testing.T) {
	s := testServer(t)
	if err := s.DB.Delete(&Migration{}, "version = 11").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Migrator().DropTable(&ReferralWithdrawal{}, &ReferralWallet{}); err != nil {
		t.Fatal(err)
	}
	users := []User{seedUser(t, s, 1000*Dollar), seedUser(t, s, 125*Dollar), seedUser(t, s, 150*Dollar), seedUser(t, s, 50*Dollar), seedUser(t, s, 500*Dollar)}
	base := s.Now().Add(-time.Hour).Truncate(time.Second)
	add := func(user User, kind string, amount int64, at time.Time, id string) {
		t.Helper()
		if id == "" {
			id = randomHex(16)
		}
		row := Ledger{ID: id, UserID: user.ID, Kind: kind, Reference: "history:" + id, AmountMicros: amount, BalanceMicros: 0, CreatedAt: at}
		if err := s.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	add(users[0], "referral_reward", 150*Dollar, base, "")
	add(users[0], "subscription", -160*Dollar, base.Add(time.Minute), "")
	add(users[0], "recharge", 1000*Dollar, base.Add(2*time.Minute), "")
	add(users[1], "referral_reward", 200*Dollar, base, "")
	add(users[1], "download", -50*Dollar, base.Add(time.Minute), "")
	add(users[1], "adjustment", 500*Dollar, base.Add(2*time.Minute), "")
	add(users[2], "download", -100*Dollar, base, "")
	add(users[2], "referral_reward", 150*Dollar, base.Add(time.Minute), "")
	add(users[3], "download", -100*Dollar, base, strings.Repeat("0", 32))
	add(users[3], "referral_reward", 150*Dollar, base, strings.Repeat("f", 32))
	seedReferralOrder(t, s, users[0], "manual", 100*Dollar, 0)
	var usersBefore []User
	var ledgerBefore []Ledger
	var ordersBefore []Order
	s.DB.Order("id").Find(&usersBefore)
	s.DB.Order("id").Find(&ledgerBefore)
	s.DB.Order("id").Find(&ordersBefore)
	// A failure after seeding must leave neither partial tables nor version 11.
	const failName = "test:withdrawal-migration-fail"
	s.DB.Callback().Create().Before("gorm:create").Register(failName, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*Migration); ok && row.Version == 11 {
			tx.AddError(errors.New("forced withdrawal migration rollback"))
		}
	})
	if err := Migrate(s.DB); err == nil {
		t.Fatal("forced migration failure was ignored")
	}
	s.DB.Callback().Create().Remove(failName)
	if s.DB.Migrator().HasTable(&ReferralWallet{}) || s.DB.Migrator().HasTable(&ReferralWithdrawal{}) {
		t.Fatal("failed migration left partial new tables")
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	for i, want := range []int64{0, 125 * Dollar, 150 * Dollar, 50 * Dollar} {
		_, wallet := withdrawalState(t, s, users[i].ID)
		if wallet.AvailableMicros != want {
			t.Fatalf("historical wallet %d = %d, want %d", i, wallet.AvailableMicros, want)
		}
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	var usersAfter []User
	var ledgerAfter []Ledger
	var ordersAfter []Order
	s.DB.Order("id").Find(&usersAfter)
	s.DB.Order("id").Find(&ledgerAfter)
	s.DB.Order("id").Find(&ordersAfter)
	if !reflect.DeepEqual(usersBefore, usersAfter) || !reflect.DeepEqual(ledgerBefore, ledgerAfter) || !reflect.DeepEqual(ordersBefore, ordersAfter) {
		t.Fatal("migration changed existing user money/ledger/orders")
	}
	var wallets, migrations int64
	s.DB.Model(&ReferralWallet{}).Count(&wallets)
	s.DB.Model(&Migration{}).Where("version = 11").Count(&migrations)
	if wallets != 4 || migrations != 1 {
		t.Fatal("migration duplicated seed/version records")
	}
	// The ledger can describe more commission than the real balance at the
	// migration cutoff. Recharging later must not revive the clipped portion.
	withdrawalCredit(t, s, users[1].ID, 100*Dollar, "recharge")
	overview, _ := referralOverview(t, s, users[1], "")
	if overview.WithdrawalAvailableMicros != 125*Dollar {
		t.Fatal("reconciliation revived the historical balance-clipped commission")
	}
	uc, csrf := sessionFor(t, s, users[1])
	w := withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": "150", "address": usdtContract}, uc, csrf, randomHex(16))
	requireCode(t, w, 400)
	// Original clipping + old-binary spending + a later commission invalidates
	// min(cached, raw replay). Exact proof mismatch must close withdrawals.
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		locked, err := lockUser(tx, users[1].ID)
		if err != nil {
			return err
		}
		if err := tx.Model(&locked).Update("balance_micros", 200*Dollar).Error; err != nil {
			return err
		}
		at := time.Now().UTC().Add(time.Second)
		return tx.Create(&[]Ledger{
			{ID: randomHex(16), UserID: locked.ID, Reference: "old-clipped-spend", Kind: "download", AmountMicros: -125 * Dollar, BalanceMicros: 100 * Dollar, CreatedAt: at},
			{ID: randomHex(16), UserID: locked.ID, Reference: "old-clipped-reward", Kind: "referral_reward", AmountMicros: 100 * Dollar, BalanceMicros: 200 * Dollar, CreatedAt: at.Add(time.Second)},
		}).Error
	}); err != nil {
		t.Fatal(err)
	}
	w = withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": "100", "address": usdtContract}, uc, csrf, randomHex(16))
	requireCode(t, w, 409)
	withdrawalCredit(t, s, users[1].ID, 100*Dollar, "recharge")
	overview, _ = referralOverview(t, s, users[1], "")
	if overview.WithdrawalAvailableMicros != 0 || !overview.WithdrawalReconciliationRequired {
		t.Fatal("historical clipping/old binary counterexample escaped proof validation")
	}
}

func TestReferralWithdrawalLedgerReconciliationPreventsRollbackDrift(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	withdrawalCredit(t, s, u.ID, 150*Dollar, "referral_reward")
	// Simulate an older binary after a rollback: it writes durable balance and
	// spending ledger, but has no code to update the new commission counter.
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		locked, err := lockUser(tx, u.ID)
		if err != nil {
			return err
		}
		if err := tx.Model(&locked).Update("balance_micros", 1000*Dollar).Error; err != nil {
			return err
		}
		now := time.Now().UTC().Add(time.Second)
		return tx.Create(&[]Ledger{
			{ID: randomHex(16), UserID: u.ID, Reference: "old-spend", Kind: "download", AmountMicros: -150 * Dollar, BalanceMicros: 0, CreatedAt: now},
			{ID: randomHex(16), UserID: u.ID, Reference: "old-recharge", Kind: "recharge", AmountMicros: 1000 * Dollar, BalanceMicros: 1000 * Dollar, CreatedAt: now.Add(time.Second)},
		}).Error
	}); err != nil {
		t.Fatal(err)
	}
	uc, csrf := sessionFor(t, s, u)
	w := withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": "100", "address": usdtContract}, uc, csrf, randomHex(16))
	requireCode(t, w, 409)
	overview, _ := referralOverview(t, s, u, "")
	_, wallet := withdrawalState(t, s, u.ID)
	if overview.WithdrawalAvailableMicros != 0 || !overview.WithdrawalReconciliationRequired || !wallet.ReconciliationRequired {
		t.Fatal("old binary consumption/recharge did not require a cash eligibility audit")
	}
}

func TestReferralWithdrawalProofDetectsChangedLedgerAndKeepsNormalMoneyWorking(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	withdrawalCredit(t, s, u.ID, 200*Dollar, "referral_reward")
	uc, csrf := sessionFor(t, s, u)
	w := withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": "100", "address": usdtContract}, uc, csrf, randomHex(16))
	requireCode(t, w, 201)
	out := decodeWithdrawal(t, w)
	// Same count, altered amount: a count-only checkpoint would miss this.
	if err := s.DB.Model(&Ledger{}).Where("user_id = ? AND kind = ?", u.ID, "referral_reward").Update("amount_micros", 175*Dollar).Error; err != nil {
		t.Fatal(err)
	}
	// New code must advance its existing proof, never reseed from the database
	// and silently absorb an old-binary ledger discrepancy.
	withdrawalCredit(t, s, u.ID, 50*Dollar, "referral_reward")
	withdrawalCredit(t, s, u.ID, 200*Dollar, "recharge")
	withdrawalCredit(t, s, u.ID, -25*Dollar, "download")
	overview, _ := referralOverview(t, s, u, "")
	if overview.WithdrawalAvailableMicros != 0 || !overview.WithdrawalReconciliationRequired {
		t.Fatal("later valid writes washed out an altered ledger amount")
	}
	admin := seedUser(t, s, 0)
	s.DB.Model(&admin).Update("role", "admin")
	ac, acCSRF := sessionFor(t, s, admin)
	w = withdrawalCall(t, s, "POST", "/api/admin/referral-withdrawals/"+out.ID+"/review", map[string]string{"action": "reject", "note": "账务核对期间退回原提现"}, ac, acCSRF, "")
	requireCode(t, w, 200)
	state, wallet := withdrawalState(t, s, u.ID)
	if state.BalanceMicros != 425*Dollar || wallet.AvailableMicros != 225*Dollar || !wallet.ReconciliationRequired {
		t.Fatal("cash-eligibility mismatch blocked or erased ordinary credits/spending/refund")
	}
	overview, _ = referralOverview(t, s, u, "")
	if overview.WithdrawalAvailableMicros != 0 || !overview.WithdrawalReconciliationRequired || overview.WithdrawalPendingMicros != 0 {
		t.Fatal("refunding an existing request silently cleared the audit requirement")
	}
	w = withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": "100", "address": usdtContract}, uc, csrf, randomHex(16))
	requireCode(t, w, 409)
}

func TestReferralWithdrawalProofAllowsNormalIncomeAfterHistoricalClamp(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 125*Dollar)
	if err := s.DB.Create(&Ledger{ID: randomHex(16), UserID: u.ID, Reference: "clipped-history", Kind: "referral_reward", AmountMicros: 150 * Dollar, BalanceMicros: 150 * Dollar, CreatedAt: s.Now().Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Delete(&Migration{}, "version = 11").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Migrator().DropTable(&ReferralWithdrawal{}, &ReferralWallet{}); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	_, wallet := withdrawalState(t, s, u.ID)
	if wallet.AvailableMicros != 125*Dollar {
		t.Fatal("historical commission was not clamped to existing balance")
	}
	withdrawalCredit(t, s, u.ID, -125*Dollar, "download")
	withdrawalCredit(t, s, u.ID, 100*Dollar, "referral_reward")
	overview, _ := referralOverview(t, s, u, "")
	if overview.WithdrawalAvailableMicros != 100*Dollar || overview.WithdrawalReconciliationRequired {
		t.Fatal("normal new-binary income after spending clipped commission was blocked")
	}
	uc, csrf := sessionFor(t, s, u)
	w := withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": "100", "address": usdtContract}, uc, csrf, randomHex(16))
	requireCode(t, w, 201)
}

func TestReferralWithdrawalConcurrentReviewRefundAndPaymentUniqueness(t *testing.T) {
	s := testServer(t)
	admin := seedUser(t, s, 0)
	s.DB.Model(&admin).Update("role", "admin")
	ac, csrf := sessionFor(t, s, admin)
	users := []User{seedUser(t, s, 0), seedUser(t, s, 0)}
	requests := make([]ReferralWithdrawal, 2)
	sharedKey := randomHex(16)
	for i, user := range users {
		withdrawalCredit(t, s, user.ID, 200*Dollar, "referral_reward")
		uc, ucCSRF := sessionFor(t, s, user)
		w := withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": "100", "address": usdtContract}, uc, ucCSRF, sharedKey)
		requireCode(t, w, 201)
		requests[i] = decodeWithdrawal(t, w)
	}
	if requests[0].ID == requests[1].ID {
		t.Fatal("idempotency keys crossed user boundaries")
	}
	review := func(record ReferralWithdrawal, action, note, hash string) *httptest.ResponseRecorder {
		return withdrawalCall(t, s, "POST", "/api/admin/referral-withdrawals/"+record.ID+"/review", map[string]string{"action": action, "note": note, "transaction_id": hash}, ac, csrf, "")
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			<-start
			requireCode(t, review(requests[0], "reject", "重复请求退回测试", ""), 200)
		})
	}
	close(start)
	wg.Wait()
	u, wallet := withdrawalState(t, s, users[0].ID)
	var refunds int64
	s.DB.Model(&Ledger{}).Where("user_id = ? AND kind = ?", u.ID, "withdrawal_refund").Count(&refunds)
	if u.BalanceMicros != 200*Dollar || wallet.AvailableMicros != 200*Dollar || refunds != 1 {
		t.Fatal("concurrent rejection returned funds more than once")
	}
	// Both owners may use the same request key, but a chain transaction can
	// confirm only one payout, including racing administrators.
	uc, ucCSRF := sessionFor(t, s, users[0])
	w := withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": "100", "address": usdtContract}, uc, ucCSRF, randomHex(16))
	requireCode(t, w, 201)
	requests[0] = decodeWithdrawal(t, w)
	for _, record := range requests {
		requireCode(t, review(record, "approve", "审核通过", ""), 200)
	}
	txid := randomHex(32)
	codes := make(chan int, 2)
	start = make(chan struct{})
	for _, record := range requests {
		record := record
		wg.Go(func() {
			<-start
			codes <- review(record, "paid", "线下完成打款", txid).Code
		})
	}
	close(start)
	wg.Wait()
	close(codes)
	var paid, duplicate int
	for code := range codes {
		if code == 200 {
			paid++
		} else if code == 409 {
			duplicate++
		} else {
			t.Fatalf("unexpected concurrent payout response %d", code)
		}
	}
	if paid != 1 || duplicate != 1 {
		t.Fatal("same transaction paid multiple withdrawals")
	}
}

func TestReferralWithdrawalAtomicFailureRollback(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	withdrawalCredit(t, s, u.ID, 200*Dollar, "referral_reward")
	uc, csrf := sessionFor(t, s, u)
	request := func() *httptest.ResponseRecorder {
		return withdrawalCall(t, s, "POST", "/api/referral-withdrawals", map[string]string{"amount": "100", "address": usdtContract}, uc, csrf, randomHex(16))
	}
	const holdFail = "test:withdrawal-hold-fail"
	s.DB.Callback().Create().Before("gorm:create").Register(holdFail, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*Ledger); ok && row.Kind == "withdrawal_hold" {
			tx.AddError(errors.New("forced ledger write failure"))
		}
	})
	requireCode(t, request(), 500)
	s.DB.Callback().Create().Remove(holdFail)
	check := func(balance, available int64, withdrawals, refunds int64) {
		t.Helper()
		state, wallet := withdrawalState(t, s, u.ID)
		var count, refundCount int64
		s.DB.Model(&ReferralWithdrawal{}).Where("user_id = ?", u.ID).Count(&count)
		s.DB.Model(&Ledger{}).Where("user_id = ? AND kind = ?", u.ID, "withdrawal_refund").Count(&refundCount)
		if state.BalanceMicros != balance || wallet.AvailableMicros != available || count != withdrawals || refundCount != refunds {
			t.Fatalf("transaction was not atomic: balance=%d available=%d withdrawals=%d refunds=%d", state.BalanceMicros, wallet.AvailableMicros, count, refundCount)
		}
	}
	check(200*Dollar, 200*Dollar, 0, 0)
	w := request()
	requireCode(t, w, 201)
	out := decodeWithdrawal(t, w)
	admin := seedUser(t, s, 0)
	s.DB.Model(&admin).Update("role", "admin")
	ac, acCSRF := sessionFor(t, s, admin)
	const auditFail = "test:withdrawal-refund-audit-fail"
	s.DB.Callback().Create().Before("gorm:create").Register(auditFail, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*Audit); ok && row.Action == "referral.withdrawal.reject" {
			tx.AddError(errors.New("forced audit write failure"))
		}
	})
	reject := func() *httptest.ResponseRecorder {
		return withdrawalCall(t, s, "POST", "/api/admin/referral-withdrawals/"+out.ID+"/review", map[string]string{"action": "reject", "note": "原子退回测试"}, ac, acCSRF, "")
	}
	requireCode(t, reject(), 500)
	s.DB.Callback().Create().Remove(auditFail)
	check(100*Dollar, 100*Dollar, 1, 0)
	var current ReferralWithdrawal
	s.DB.First(&current, "id = ?", out.ID)
	if current.Status != "pending" {
		t.Fatal("failed audit left rejected request without refund")
	}
	requireCode(t, reject(), 200)
	check(200*Dollar, 200*Dollar, 1, 1)
}
