package platform

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func configureTestAlipay(s *Server) {
	s.Config.FutoonURL = "https://gateway.example/submit.php"
	s.Config.FutoonPID = "test-merchant"
	s.Config.FutoonKey = "test-only-merchant-key"
}

func alipayNotification(t *testing.T, s *Server, o Order, money, trade string, mutate func(map[string]string)) *httptest.ResponseRecorder {
	t.Helper()
	p := map[string]string{"pid": s.Config.FutoonPID, "type": "alipay", "out_trade_no": o.ID, "money": money, "trade_no": trade, "trade_status": "TRADE_SUCCESS", "sign_type": "MD5"}
	if mutate != nil {
		mutate(p)
	}
	p["sign"] = FutoonSign(p, s.Config.FutoonKey)
	q := url.Values{}
	for k, v := range p {
		q.Set(k, v)
	}
	return call(t, s, "GET", "/api/payments/futoon/notify?"+q.Encode(), nil, nil, "")
}

func TestAlipayQuoteAndLockedOrder(t *testing.T) {
	for _, tc := range []struct{ amount, rate, fee, cents int64 }{
		{100 * Dollar, 6650000, 5 * Dollar, 69825},
		{Dollar, 6650000, 50000, 699},
		{1010000, 6650000, 50500, 706},
		{10000 * Dollar, 1000 * Dollar, 500 * Dollar, 1050000000},
	} {
		fee, cents, err := alipayQuote(tc.amount, tc.rate)
		if err != nil || fee != tc.fee || cents != tc.cents {
			t.Fatalf("incorrect quote: fee=%d cents=%d err=%v", fee, cents, err)
		}
	}
	for _, tc := range []struct{ amount, rate int64 }{{0, 6650000}, {Dollar, 0}, {Dollar, 1000*Dollar + 1}, {Dollar + 1, 6650000}} {
		if _, _, err := alipayQuote(tc.amount, tc.rate); err == nil {
			t.Fatal("invalid payment quote accepted")
		}
	}
	s := testServer(t)
	configureTestAlipay(s)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	key := randomHex(16)
	o := createBonusOrder(t, s, cookie, csrf, key, "100", "alipay")
	if o.AmountMicros != 100*Dollar || o.PaymentCurrency != "CNY" || o.AlipayRateMicros != 6650000 || o.PayCNYCents != 69825 || o.FeeMicros != 5*Dollar || o.BonusMicros != 0 {
		t.Fatal("order did not separate USD principal, fee, CNY payment and rate")
	}
	pay, err := url.Parse(o.PayURL)
	if err != nil || pay.Query().Get("type") != "alipay" || pay.Query().Get("money") != "698.25" || pay.Query().Get("notify_url") != s.Config.PublicURL+"/api/payments/futoon/notify" || pay.Query().Get("return_url") != s.Config.PublicURL+"/api/payments/futoon/return" {
		t.Fatal("incorrect merchant order, amount or D1 callbacks")
	}
	params := map[string]string{}
	for k, v := range pay.Query() {
		params[k] = v[0]
	}
	if params["sign"] != FutoonSign(params, s.Config.FutoonKey) || strings.Contains(o.PayURL, s.Config.FutoonKey) {
		t.Fatal("invalid signature or leaked merchant key")
	}
	if err := s.DB.Model(&Settings{}).Where("id = 1").Updates(map[string]any{"alipay_cny_rate_micros": 7 * Dollar, "usdt_rate_micros": 990000}).Error; err != nil {
		t.Fatal(err)
	}
	retry := createBonusOrder(t, s, cookie, csrf, key, "100", "alipay")
	if retry.ID != o.ID || retry.PayURL != o.PayURL || retry.PayCNYCents != 69825 || retry.AlipayRateMicros != 6650000 {
		t.Fatal("retry or exchange-rate change rewrote the existing order")
	}
	newOrder := createBonusOrder(t, s, cookie, csrf, randomHex(16), "100", "alipay")
	if newOrder.PayCNYCents != 73500 {
		t.Fatal("new order did not use updated CNY rate independently of USDT rate")
	}
	if countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("creating an unpaid order credited funds")
	}
	// An existing retry remains available even if the channel is then disabled.
	s.Config.FutoonKey = ""
	retry = createBonusOrder(t, s, cookie, csrf, key, "100", "alipay")
	if retry.ID != o.ID {
		t.Fatal("disabled channel changed an existing idempotent order")
	}
}

func registerFromInvitation(t *testing.T, s *Server, inviter User) User {
	t.Helper()
	view, _ := referralOverview(t, s, inviter, "")
	link, err := url.Parse(view.InviteURL)
	if err != nil {
		t.Fatal(err)
	}
	route, _ := url.Parse(link.Fragment)
	code := route.Query().Get("invite")
	if route.Path != "/register" || code != view.InviteCode {
		t.Fatal("generated invitation link did not target registration")
	}
	input := authInput{Email: randomHex(8) + "@example.com", Name: "支付邀请测试", Password: "test-password-123", InviteCode: code, CaptchaID: randomHex(16), CaptchaCode: "12345"}
	if err := s.DB.Create(&Challenge{ID: input.CaptchaID, Hash: s.mac(input.CaptchaID + ":" + input.CaptchaCode), ExpiresAt: s.Now().Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	w := call(t, s, "POST", "/api/auth/register", input, nil, "")
	requireCode(t, w, 200)
	var response struct{ User User }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.User.ID == "" || len(w.Result().Cookies()) == 0 {
		t.Fatal("invitation registration did not establish the new account/session", err)
	}
	var binding ReferralAccount
	if err := s.DB.First(&binding, "user_id = ?", response.User.ID).Error; err != nil || binding.InviterID == nil || *binding.InviterID != inviter.ID {
		t.Fatal("invitation link did not persist the correct inviter")
	}
	return response.User
}

func TestAlipayInvitationRegistrationThreeLevelSettlementAndReplay(t *testing.T) {
	s := testServer(t)
	configureTestAlipay(s)
	chain := []User{seedUser(t, s, 0)}
	for range 4 {
		chain = append(chain, registerFromInvitation(t, s, chain[len(chain)-1]))
	}
	payer := chain[4]
	// An activity gift must not change the fee or referral base.
	settings, _ := getSettings(s.DB)
	settings.RechargeBonusTiers = []RechargeTier{{100 * Dollar, 50}}
	if err := s.DB.Save(&settings).Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := sessionFor(t, s, payer)
	o := createBonusOrder(t, s, cookie, csrf, randomHex(16), "100", "alipay")
	if o.PayCNYCents != 69825 || o.BonusMicros != 50*Dollar || o.FeeMicros != 5*Dollar {
		t.Fatal("gift or fee was added to the wrong payment base")
	}
	returned := call(t, s, "GET", "/api/payments/futoon/return?out_trade_no="+o.ID+"&trade_status=TRADE_SUCCESS", nil, nil, "")
	requireCode(t, returned, 303)
	if returned.Header().Get("Location") != s.Config.PublicURL+"/#/billing?order="+o.ID || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("browser return credited money or used another project's URL")
	}
	for _, money := range []string{"100.00", "698.24", "698.26"} {
		requireCode(t, alipayNotification(t, s, o, money, "payment-bill", nil), 400)
	}
	for _, mutate := range []func(map[string]string){func(p map[string]string) { p["pid"] = "other-merchant" }, func(p map[string]string) { p["type"] = "wxpay" }, func(p map[string]string) { p["trade_status"] = "WAIT_BUYER_PAY" }} {
		requireCode(t, alipayNotification(t, s, o, "698.25", "payment-bill", mutate), 400)
	}
	if countRows(t, s, &ReferralReward{}) != 0 || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("unverified or mismatched callback credited funds")
	}
	if err := s.DB.Model(&Settings{}).Where("id = 1").Update("alipay_cny_rate_micros", 7*Dollar).Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			w := alipayNotification(t, s, o, "698.25", "payment-bill", nil)
			if w.Code != 200 || w.Body.String() != "success" {
				t.Errorf("repeated callback failed: %d %s", w.Code, w.Body.String())
			}
		})
	}
	wg.Wait()
	for i, want := range []int64{0, 2 * Dollar, 2 * Dollar, 10 * Dollar, 150 * Dollar} {
		var saved User
		if err := s.DB.First(&saved, "id = ?", chain[i].ID).Error; err != nil || saved.BalanceMicros != want {
			t.Fatalf("account %d balance=%d want=%d err=%v", i, saved.BalanceMicros, want, err)
		}
	}
	if countRows(t, s, &ReferralReward{}) != 3 || countRows(t, s, &Ledger{}) != 5 {
		t.Fatal("duplicate callback or a fourth ancestor received extra credits")
	}
	for i := 1; i <= 3; i++ {
		view, _ := referralOverview(t, s, chain[i], "")
		if view.Rewards.Total != 1 || view.Rewards.Data[0].RechargeMicros != 100*Dollar {
			t.Fatal("referral overview did not show one principal-only reward")
		}
	}
	next := createBonusOrder(t, s, cookie, csrf, randomHex(16), "100", "alipay")
	requireCode(t, alipayNotification(t, s, next, "735.00", "payment-bill", nil), 400)
	if countRows(t, s, &ReferralReward{}) != 3 || countRows(t, s, &Ledger{}) != 5 {
		t.Fatal("replayed gateway bill credited another order")
	}
}

func TestAlipaySettingsIsolationAndMigration(t *testing.T) {
	s := testServer(t)
	member := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, member)
	payload := map[string]any{"section": "alipay", "settings": map[string]any{"alipay_cny_rate_micros": 7250000, "usdt_rate_micros": 123, "usdt_wallet": "forged-wallet"}}
	requireCode(t, call(t, s, "PUT", "/api/admin/settings", payload, cookie, csrf), 403)
	if err := s.DB.Model(&member).Update("role", "admin").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&Settings{}).Where("id = 1").Update("usdt_wallet", usdtContract).Error; err != nil {
		t.Fatal(err)
	}
	original, _ := getSettings(s.DB)
	requireCode(t, call(t, s, "PUT", "/api/admin/settings", payload, cookie, "wrong"), 403)
	requireCode(t, call(t, s, "PUT", "/api/admin/settings", payload, cookie, csrf), 200)
	after, _ := getSettings(s.DB)
	if after.AlipayCNYRateMicros != 7250000 || after.USDTRateMicros != original.USDTRateMicros || after.USDTWallet == nil || original.USDTWallet == nil || *after.USDTWallet != *original.USDTWallet {
		t.Fatal("Alipay rate save changed USDT configuration")
	}
	for _, rate := range []int64{0, -1, 9999, 1000*Dollar + 1} {
		requireCode(t, call(t, s, "PUT", "/api/admin/settings", map[string]any{"section": "alipay", "settings": map[string]int64{"alipay_cny_rate_micros": rate}}, cookie, csrf), 400)
	}
	for range 2 {
		if err := Migrate(s.DB); err != nil {
			t.Fatal(err)
		}
	}
	after, _ = getSettings(s.DB)
	if after.AlipayCNYRateMicros != 7250000 {
		t.Fatal("restart migration overwrote the configured rate")
	}
}

func TestAlipayRejectsForgedAndDuplicateFieldsAndAcceptsPOST(t *testing.T) {
	s := testServer(t)
	configureTestAlipay(s)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	o := createBonusOrder(t, s, cookie, csrf, randomHex(16), "100", "alipay")
	params := map[string]string{"pid": s.Config.FutoonPID, "type": "alipay", "out_trade_no": o.ID, "money": "698.25", "trade_no": "post-payment-bill", "trade_status": "TRADE_SUCCESS", "sign_type": "MD5"}
	params["sign"] = FutoonSign(params, s.Config.FutoonKey)
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	for _, invalid := range []string{q.Encode() + "&money=698.25", strings.Replace(q.Encode(), "sign="+params["sign"], "sign="+strings.Repeat("0", 32), 1)} {
		requireCode(t, call(t, s, "GET", "/api/payments/futoon/notify?"+invalid, nil, nil, ""), 400)
	}
	if countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("forged notification credited money")
	}
	r := httptest.NewRequest("POST", "http://localhost:8088/api/payments/futoon/notify", strings.NewReader(q.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://gateway.example")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 200)
	if w.Body.String() != "success" || countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("valid form notification did not credit exactly once")
	}
}

func TestAlipayMigrationPreservesExistingOrdersAndUSDT(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 123*Dollar)
	o := seedReferralOrder(t, s, u, "alipay", 100*Dollar, 50*Dollar)
	if err := s.DB.Model(&o).Updates(map[string]any{"pay_url": "https://old-gateway.example/submit.php?legacy=1", "status": "paid"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&Settings{}).Where("id = 1").Updates(map[string]any{"usdt_rate_micros": 990000, "usdt_wallet": usdtContract}).Error; err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"payment_currency", "alipay_rate_micros", "fee_micros", "pay_cny_cents"} {
		if err := s.DB.Migrator().DropColumn(&Order{}, column); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DB.Migrator().DropColumn(&Settings{}, "alipay_cny_rate_micros"); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Delete(&Migration{}, "version = 10").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	settings, _ := getSettings(s.DB)
	if settings.AlipayCNYRateMicros != 6650000 || settings.USDTRateMicros != 990000 || settings.USDTWallet == nil || *settings.USDTWallet != usdtContract {
		t.Fatal("upgrade changed USDT configuration or omitted default CNY rate")
	}
	var saved Order
	if err := s.DB.First(&saved, "id = ?", o.ID).Error; err != nil || saved.AmountMicros != 100*Dollar || saved.BonusMicros != 50*Dollar || saved.Status != "paid" || saved.PayURL != "https://old-gateway.example/submit.php?legacy=1" || saved.PaymentCurrency != "" || saved.PayCNYCents != 0 {
		t.Fatal("upgrade repriced or rewrote an existing order", err)
	}
	if err := s.DB.First(&u, "id = ?", u.ID).Error; err != nil || u.BalanceMicros != 123*Dollar || countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &ReferralReward{}) != 0 {
		t.Fatal("upgrade changed balances or rewarded historical orders", err)
	}
}
