package platform

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestMeteredRegistrationGiftIsMoneyNotWithdrawableCash(t *testing.T) {
	s := meteredServer(t, 0)
	in := authInput{Email: "gift-money@example.com", Name: "Gift money", Password: "test123456", CaptchaCode: "12345", CaptchaID: randomHex(16)}
	if e := s.DB.Create(&Challenge{ID: in.CaptchaID, Hash: s.mac(in.CaptchaID + ":" + in.CaptchaCode), ExpiresAt: s.Now().Add(time.Minute)}).Error; e != nil {
		t.Fatal(e)
	}
	w := call(t, s, "POST", "/api/auth/register", in, nil, "")
	requireCode(t, w, 200)
	var out struct {
		User User `json:"user"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	u := usageOwner(t, s, out.User.ID)
	if u.BalanceMicros != 0 || u.HeldMicros != 0 || u.GiftBalanceMicros != 100*Dollar || u.GiftHeldMicros != 0 {
		t.Fatalf("unexpected welcome balances: %+v", u)
	}
	if countRows(t, s, &DownloadGrant{}) != 0 {
		t.Fatal("registration minted count gifts")
	}
	// A new account has no monthly usage row yet, but its dashboard must load.
	cookie, _ := sessionFor(t, s, u)
	dashboard := call(t, s, "GET", "/api/dashboard", nil, cookie, "")
	requireCode(t, dashboard, 200)
	var summary struct {
		User           User  `json:"user"`
		QuotaRemaining int64 `json:"quota_remaining"`
	}
	if e := json.Unmarshal(dashboard.Body.Bytes(), &summary); e != nil {
		t.Fatal(e)
	}
	if summary.User.GiftBalanceMicros != 100*Dollar || summary.QuotaRemaining != 0 {
		t.Fatal("dashboard did not expose money credit separately from retired count gifts")
	}
	if countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("welcome gift ledger missing")
	}
	var entry Ledger
	s.DB.First(&entry, "user_id = ?", u.ID)
	if entry.BalanceType != "gift" || entry.AmountMicros != 100*Dollar || entry.Kind != "registration_gift" {
		t.Fatal("gift misclassified as cash")
	}
	// Neither registration credit nor its consumption enters the referral wallet.
	wallet, e := referralWallet(s.DB, u.ID)
	if e != nil {
		t.Fatal(e)
	}
	if wallet.AvailableMicros != 0 {
		t.Fatal("welcome credit became withdrawable")
	}
	r := usageRequest(s, "/", "203.0.113.101")
	if e = s.chargeUsageUV(r, u.ID, "apk_uv", "one"); e != nil {
		t.Fatal(e)
	}
	u = usageOwner(t, s, u.ID)
	if u.GiftBalanceMicros != 100*Dollar-50000 || u.BalanceMicros != 0 {
		t.Fatal("APK did not use 0.05 welcome credit")
	}
	if e = s.DB.Transaction(func(tx *gorm.DB) error {
		owner, e := lockUser(tx, u.ID)
		if e != nil {
			return e
		}
		return changeBalance(tx, &owner, 200*Dollar, "test-referral", "referral_reward", "test")
	}); e != nil {
		t.Fatal(e)
	}
	if e = s.chargeUsageUV(r, u.ID, "apk_uv", "two"); e != nil {
		t.Fatal(e)
	}
	u = usageOwner(t, s, u.ID)
	available, reconcile, e := referralWithdrawalAvailable(s.DB, u)
	if e != nil || reconcile || available != 200*Dollar {
		t.Fatal("gift ledger invalidated or consumed real referral withdrawal rights")
	}
}

func TestMeteredGiftAndCashSplitCancelAndRetry(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, 30000)
	if e := s.DB.Model(&u).Update("gift_balance_micros", 20000).Error; e != nil {
		t.Fatal(e)
	}
	app := createPWAFixture(t, s, u, "Split payment")
	r := usageRequest(s, "/", "203.0.113.102")
	base := "/p/" + app.Slug + "/install/"
	body := installBody(s, app, r, randomHex(16))
	requireCode(t, usagePost(s, base+"start", body, r), 200)
	got := usageOwner(t, s, u.ID)
	if got.HeldMicros != 30000 || got.GiftHeldMicros != 20000 || got.BalanceMicros != 30000 || got.GiftBalanceMicros != 20000 {
		t.Fatal("split preparation did not hold both balances")
	}
	requireCode(t, usagePost(s, base+"cancel", body, r), 200)
	got = usageOwner(t, s, u.ID)
	if got.HeldMicros != 0 || got.GiftHeldMicros != 0 || got.GiftBalanceMicros != 20000 || got.BalanceMicros != 30000 {
		t.Fatal("cancel changed money")
	}
	body = installBody(s, app, r, randomHex(16))
	requireCode(t, usagePost(s, base+"start", body, r), 200)
	for range 3 {
		requireCode(t, usagePost(s, base+"complete", body, r), 200)
	}
	got = usageOwner(t, s, u.ID)
	if got.BalanceMicros != 0 || got.GiftBalanceMicros != 0 || got.HeldMicros != 0 || got.GiftHeldMicros != 0 {
		t.Fatal("split confirmation not settled once")
	}
	var gift, cash Ledger
	s.DB.First(&gift, "user_id = ? AND balance_type = ?", u.ID, "gift")
	s.DB.First(&cash, "user_id = ? AND balance_type = ?", u.ID, "balance")
	if gift.AmountMicros != -20000 || cash.AmountMicros != -30000 || countRows(t, s, &Ledger{}) != 2 {
		t.Fatal("split ledger incorrect")
	}
}

func TestMeteredGiftConcurrentReservationsAndExpiry(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, 0)
	s.DB.Model(&u).Update("gift_balance_micros", 50000)
	app := createPWAFixture(t, s, u, "Concurrent gift")
	r := usageRequest(s, "/", "203.0.113.103")
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := usagePost(s, "/p/"+app.Slug+"/install/start", installBody(s, app, r, randomHex(16)), r)
			mu.Lock()
			defer mu.Unlock()
			if w.Code == 200 {
				allowed++
			} else if w.Code != 402 {
				t.Errorf("unexpected %d", w.Code)
			}
		}()
	}
	wg.Wait()
	got := usageOwner(t, s, u.ID)
	if allowed != 1 || got.GiftHeldMicros != 50000 || got.HeldMicros != 0 {
		t.Fatal("gift reservations overdrawn")
	}
	s.DB.Model(&MeteredEvent{}).Where("user_id = ?", u.ID).Update("expires_at", s.Now().Add(-time.Second))
	for range 2 {
		if e := s.cleanupMeteredUsage(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	got = usageOwner(t, s, u.ID)
	if got.GiftBalanceMicros != 50000 || got.GiftHeldMicros != 0 || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("expired gift lost money")
	}
}

func TestMeteredFreeFeaturesWithoutAnyCredit(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, 0)
	r := usageRequest(s, "/", "203.0.113.104")
	// Saved stale rates must never re-enable billing for these features.
	s.DB.Model(&Settings{}).Where("id = 1").Updates(map[string]any{"cloak_uv_price_micros": 999999, "return_flow_price_micros": 999999, "pwa_push_price_micros": 999999})
	for range 3 {
		if e := s.chargeUsageUV(r, u.ID, "cloak_uv", "one"); e != nil {
			t.Fatal(e)
		}
	}
	cloak := createTestCloak(t, s, u, map[string]any{"name": "Free cloak", "enabled": true, "target_type": "url", "target_url": "https://target.example/app", "safe_url": "https://safe.example/"})
	requireCode(t, call(t, s, "GET", "/c/"+cloak.Slug, nil, nil, ""), 302)
	for _, kind := range []string{"apk", "pwa"} {
		id, page := billedReturnApp(t, s, u, kind)
		if kind == "apk" {
			var a Application
			s.DB.First(&a, "slug = ?", strings.Split(page, "/")[2])
			id = a.ID
		} else {
			var a PWAApp
			s.DB.First(&a, "slug = ?", strings.Split(page, "/")[2])
			id = a.ID
		}
		endpoint := strings.TrimRight(strings.TrimSuffix(page, "/info"), "/") + "/return-flow/view"
		body := map[string]string{"token": s.returnFlowViewToken(kind, id, r)}
		for _, action := range []string{"", "reserve", "complete", "cancel"} {
			body["action"] = action
			requireCode(t, usagePost(s, endpoint, body, r), 200)
		}
	}
	d := PWAPushDelivery{ID: randomHex(16), UserID: u.ID, PWAID: randomHex(16), ExpiresAt: s.Now().Add(time.Hour)}
	if e := s.reservePushUsage(context.Background(), d); e != nil {
		t.Fatal(e)
	}
	if e := s.DB.Transaction(func(tx *gorm.DB) error { return s.settlePushUsage(tx, d, true, false) }); e != nil {
		t.Fatal(e)
	}
	got := usageOwner(t, s, u.ID)
	if got.BalanceMicros != 0 || got.GiftBalanceMicros != 0 || got.HeldMicros != 0 || got.GiftHeldMicros != 0 || countRows(t, s, &MeteredEvent{}) != 0 || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("free feature changed balances or recorded charge")
	}
}

func TestMeteredPricingMigrationReleasesFreeHoldsAndRetiresCountGifts(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, Dollar)
	if e := s.DB.Model(&u).Update("held_micros", 33000).Error; e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"cloak_uv", "return_flow", "pwa_push", "pwa_install"} {
		price := int64(1000)
		if kind == "pwa_install" {
			price = 30000
		}
		if e := s.DB.Create(&MeteredEvent{ID: randomHex(32), UserID: u.ID, Kind: kind, State: "pending", ResourceID: randomHex(16), PriceMicros: price, ExpiresAt: s.Now().Add(time.Hour)}).Error; e != nil {
			t.Fatal(e)
		}
	}
	grant := DownloadGrant{ID: "free:" + u.ID, UserID: u.ID, Limit: 1000, Used: 10, Reserved: 1, StartsAt: u.CreatedAt}
	if e := s.DB.Create(&grant).Error; e != nil {
		t.Fatal(e)
	}
	s.DB.Where("version = ?", usagePricingVersion).Delete(&Migration{})
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	got := usageOwner(t, s, u.ID)
	if got.BalanceMicros != Dollar || got.HeldMicros != 30000 || got.GiftBalanceMicros != 0 {
		t.Fatal("cutover changed funds or kept free holds")
	}
	s.DB.First(&grant, "id = ?", grant.ID)
	if grant.Limit != 11 || grant.Used != 10 || grant.Reserved != 1 {
		t.Fatal("unused counts not retired or existing reservation erased")
	}
	var paid MeteredEvent
	s.DB.First(&paid, "user_id = ? AND kind = ?", u.ID, "pwa_install")
	if paid.State != "pending" || paid.PriceMicros != 30000 {
		t.Fatal("in-flight quoted install changed")
	}
	var released int64
	s.DB.Model(&MeteredEvent{}).Where("user_id = ? AND state = ?", u.ID, "released").Count(&released)
	if released != 3 || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("legacy free holds were not released once")
	}
	s.DB.Model(&Settings{}).Where("id = 1").Updates(map[string]any{"download_price_micros": 60000, "registration_gift_micros": 80 * Dollar})
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	x, _ := getSettings(s.DB)
	if x.DownloadPriceMicros != 60000 || x.RegistrationGiftMicros != 80*Dollar {
		t.Fatal("restart reset administrator prices")
	}
}
