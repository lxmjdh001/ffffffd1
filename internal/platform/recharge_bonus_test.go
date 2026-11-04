package platform

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync"
	"testing"
)

func createBonusOrder(t *testing.T, s *Server, cookie *http.Cookie, csrf, key, amount, method string) Order {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"amount": amount, "method": method})
	r := httptest.NewRequest("POST", "http://localhost:8088/api/orders", bytes.NewReader(body))
	r.AddCookie(cookie)
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 201)
	var order Order
	if err := json.Unmarshal(w.Body.Bytes(), &order); err != nil {
		t.Fatal(err)
	}
	return order
}

func TestRechargeBonusThresholdAndCustomAmount(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	for _, tc := range []struct {
		amount string
		bonus  int64
	}{{"100", 0}, {"200", 0}, {"299.99", 0}, {"300", 30 * Dollar}, {"300.01", 30001000}, {"999.99", 99999000}, {"1000", 300 * Dollar}, {"1000.01", 300003000}, {"2999.99", 899997000}, {"3000", 1500 * Dollar}, {"9999.99", 4999995000}, {"10000", 8000 * Dollar}} {
		t.Run(tc.amount, func(t *testing.T) {
			o := createBonusOrder(t, s, cookie, csrf, randomHex(16), tc.amount, "manual")
			if o.BonusMicros != tc.bonus {
				t.Fatalf("bonus = %d, want %d", o.BonusMicros, tc.bonus)
			}
		})
	}
	if err := s.DB.First(&u, "id = ?", u.ID).Error; err != nil || u.BalanceMicros != 0 || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("creating unpaid orders credited funds")
	}
}

func TestRechargeTenThousandBonusSettlement(t *testing.T) {
	for _, method := range []string{"manual", "usdt"} {
		t.Run(method, func(t *testing.T) {
			s := testServer(t)
			s.Config.USDTWallet = usdtContract
			u := seedUser(t, s, 0)
			cookie, csrf := sessionFor(t, s, u)
			o := createBonusOrder(t, s, cookie, csrf, randomHex(16), "10000", method)
			if o.AmountMicros != 10000*Dollar || o.BonusMicros != 8000*Dollar {
				t.Fatal("10,000 top-up did not snapshot the 80% bonus")
			}
			for range 2 {
				if err := s.settle(o.ID, "fixture:"+o.ID, method, o.AmountMicros); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.DB.First(&u, "id = ?", u.ID).Error; err != nil || u.BalanceMicros != 18000*Dollar || countRows(t, s, &Ledger{}) != 2 {
				t.Fatal("principal and 80% bonus were not credited exactly once")
			}
		})
	}
}

func TestRechargeTenThousandBonusMigration(t *testing.T) {
	for _, existingTopTier := range []bool{false, true} {
		t.Run(strconv.FormatBool(existingTopTier), func(t *testing.T) {
			s := testServer(t)
			u := seedUser(t, s, 50*Dollar)
			old := Order{ID: randomHex(16), UserID: u.ID, IdempotencyKey: randomHex(16), Method: "manual", AmountMicros: 10000 * Dollar, BonusMicros: 5000 * Dollar, Status: "pending"}
			if err := s.DB.Create(&old).Error; err != nil {
				t.Fatal(err)
			}
			settings, _ := getSettings(s.DB)
			settings.RechargeBonusEnabled = false
			settings.RechargeBonusTiers = []RechargeTier{{300 * Dollar, 10}, {1000 * Dollar, 30}, {3000 * Dollar, 50}}
			if existingTopTier {
				settings.RechargeBonusTiers = append(settings.RechargeBonusTiers, RechargeTier{10000 * Dollar, 50})
			}
			if err := s.DB.Save(&settings).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.DB.Delete(&Migration{}, "version = ?", 33).Error; err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := Migrate(s.DB); err != nil {
					t.Fatal(err)
				}
			}
			settings, err := getSettings(s.DB)
			if err != nil || settings.RechargeBonusEnabled || !reflect.DeepEqual(settings.RechargeBonusTiers, defaultRechargeTiers()) {
				t.Fatal("migration lost prior tiers, changed the campaign switch or duplicated the new tier")
			}
			if err := s.DB.First(&old, "id = ?", old.ID).Error; err != nil || old.BonusMicros != 5000*Dollar {
				t.Fatal("migration rewrote a pending order's promised bonus")
			}
			if err := s.DB.First(&u, "id = ?", u.ID).Error; err != nil || u.BalanceMicros != 50*Dollar || countRows(t, s, &Ledger{}) != 0 {
				t.Fatal("migration altered account funds")
			}
			settings.RechargeBonusTiers = []RechargeTier{{500 * Dollar, 20}}
			if err := s.DB.Save(&settings).Error; err != nil {
				t.Fatal(err)
			}
			if err := Migrate(s.DB); err != nil {
				t.Fatal(err)
			}
			after, _ := getSettings(s.DB)
			if !reflect.DeepEqual(settings.RechargeBonusTiers, after.RechargeBonusTiers) {
				t.Fatal("repeated migration overwrote subsequent operator changes")
			}
		})
	}
}

func TestRechargeBonusSettlementAllMethods(t *testing.T) {
	for _, method := range []string{"manual", "usdt"} {
		t.Run(method, func(t *testing.T) {
			s := testServer(t)
			s.Config.FutoonURL = "https://payments.example/submit.php"
			s.Config.FutoonPID = "test-merchant"
			s.Config.FutoonKey = "test-key"
			s.Config.USDTWallet = usdtContract
			u := seedUser(t, s, 0)
			cookie, csrf := sessionFor(t, s, u)
			o := createBonusOrder(t, s, cookie, csrf, randomHex(16), "1000", method)
			if o.AmountMicros != 1000*Dollar || o.BonusMicros != 300*Dollar {
				t.Fatal("order did not snapshot principal and gift separately")
			}
			if method == "usdt" {
				baseCents := (o.AmountMicros*100 + o.RateMicros - 1) / o.RateMicros
				if o.USDTUnits%10000 != 0 || o.USDTUnits < (baseCents-99)*10000 || o.USDTUnits > (baseCents+10)*10000 {
					t.Fatal("USDT payment amount included the gift")
				}
			}
			confirm := func() error { return s.settle(o.ID, "test:"+o.ID, method, o.AmountMicros) }
			if method == "manual" {
				admin := seedUser(t, s, 0)
				if err := s.DB.Model(&admin).Update("role", "admin").Error; err != nil {
					t.Fatal(err)
				}
				adminCookie, adminCSRF := sessionFor(t, s, admin)
				confirm = func() error {
					w := call(t, s, "POST", "/api/admin/orders/"+o.ID+"/settle", map[string]string{}, adminCookie, adminCSRF)
					if w.Code != 200 {
						return problem(w.Code, w.Body.String())
					}
					return nil
				}
			}
			var wg sync.WaitGroup
			for range 24 {
				wg.Go(func() {
					if err := confirm(); err != nil {
						t.Errorf("settlement: %v", err)
					}
				})
			}
			wg.Wait()
			if err := s.DB.First(&u, "id = ?", u.ID).Error; err != nil || u.BalanceMicros != 1300*Dollar {
				t.Fatal("principal and gift were not credited exactly once")
			}
			if countRows(t, s, &Ledger{}) != 2 {
				t.Fatal("expected separate principal and gift entries")
			}
			var gift Ledger
			if err := s.DB.First(&gift, "reference = ?", "recharge-bonus:"+o.ID).Error; err != nil || gift.Kind != "recharge_bonus" || gift.AmountMicros != 300*Dollar || gift.BalanceMicros != 1300*Dollar {
				t.Fatal("gift ledger is not traceable to the order")
			}
		})
	}
}

func TestRechargeBonusLocksOrderTermsAndRollsBackAtomically(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	key := randomHex(16)
	original := createBonusOrder(t, s, cookie, csrf, key, "1000", "manual")
	if err := s.DB.Model(&Settings{}).Where("id = 1").Update("recharge_bonus_enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	retried := createBonusOrder(t, s, cookie, csrf, key, "1000", "manual")
	future := createBonusOrder(t, s, cookie, csrf, randomHex(16), "1000", "manual")
	if retried.ID != original.ID || retried.BonusMicros != 300*Dollar || future.BonusMicros != 0 {
		t.Fatal("campaign changes rewrote an order or a retry's promised gift")
	}
	// The base credit succeeds within the cap, but the gift would exceed it.
	// Both balance entries and the paid status must roll back together.
	before := int64(100000000*Dollar - 1000*Dollar)
	if err := s.DB.Model(&u).Update("balance_micros", before).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.settle(original.ID, "manual:rollback", "manual", 1000*Dollar); err == nil {
		t.Fatal("over-cap credit succeeded")
	}
	if err := s.DB.First(&u, "id = ?", u.ID).Error; err != nil || u.BalanceMicros != before || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("partial gift settlement was committed")
	}
	var saved Order
	if err := s.DB.First(&saved, "id = ?", original.ID).Error; err != nil || saved.Status != "pending" {
		t.Fatal("failed payment marked paid")
	}
}

func TestRechargeBonusMigrationPreservesExistingData(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 123*Dollar)
	old := Order{ID: randomHex(16), UserID: u.ID, IdempotencyKey: randomHex(16), Method: "manual", AmountMicros: 1000 * Dollar, BonusMicros: 300 * Dollar, Status: "pending"}
	if err := s.DB.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&Settings{}).Where("id = 1").Updates(map[string]any{"download_price_micros": 500, "usdt_rate_micros": 7 * Dollar}).Error; err != nil {
		t.Fatal(err)
	}
	// Recreate the v2 settings table; v3 must add tiers without rewriting funds.
	if err := s.DB.Migrator().DropColumn(&Settings{}, "recharge_bonus_tiers"); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Delete(&Migration{}, "version = 3").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	settings, err := getSettings(s.DB)
	if err != nil || settings.DownloadPriceMicros != 500 || settings.USDTRateMicros != Dollar || !settings.RechargeBonusEnabled || !reflect.DeepEqual(settings.RechargeBonusTiers, defaultRechargeTiers()) {
		t.Fatal("upgrade lost settings or dollar campaign defaults")
	}
	if err := s.DB.First(&old, "id = ?", old.ID).Error; err != nil || old.BonusMicros != 300*Dollar {
		t.Fatal("legacy order's promised gift was rewritten")
	}
	if err := s.settle(old.ID, "manual:legacy", "manual", 1000*Dollar); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.First(&u, "id = ?", u.ID).Error; err != nil || u.BalanceMicros != 1423*Dollar {
		t.Fatal("legacy numeric balance or promised gift was rewritten")
	}
	settings.RechargeBonusEnabled = false
	settings.USDTRateMicros = 990000
	settings.RechargeBonusTiers = []RechargeTier{{2000 * Dollar, 20}}
	if err := s.DB.Save(&settings).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	settings, err = getSettings(s.DB)
	if err != nil || settings.RechargeBonusEnabled || settings.USDTRateMicros != 990000 || len(settings.RechargeBonusTiers) != 1 || settings.RechargeBonusTiers[0].Percent != 20 {
		t.Fatal("repeat migration reset saved campaign or USDT rate")
	}
	if err := s.DB.First(&Migration{}, "version = 3").Error; err != nil {
		t.Fatal(err)
	}
}

func TestRechargeBonusAdminSettings(t *testing.T) {
	s := testServer(t)
	admin := seedUser(t, s, 0)
	if err := s.DB.Model(&admin).Update("role", "admin").Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := sessionFor(t, s, admin)
	settings, err := getSettings(s.DB)
	if err != nil {
		t.Fatal(err)
	}
	var plans []Plan
	if err := s.DB.Find(&plans).Error; err != nil {
		t.Fatal(err)
	}
	settings.RechargeBonusTiers = []RechargeTier{{2000 * Dollar, 25}}
	for _, enabled := range []bool{false, true} {
		settings.RechargeBonusEnabled = enabled
		requireCode(t, call(t, s, "PUT", "/api/admin/settings", map[string]any{"settings": settings, "plans": plans}, cookie, csrf), 200)
		w := call(t, s, "GET", "/api/config", nil, nil, "")
		requireCode(t, w, 200)
		var public Settings
		if err := json.Unmarshal(w.Body.Bytes(), &public); err != nil {
			t.Fatal(err)
		}
		if public.RechargeBonusEnabled != enabled || !reflect.DeepEqual(public.RechargeBonusTiers, settings.RechargeBonusTiers) {
			t.Fatal("public campaign differs from the administrator's saved settings")
		}
		order := createBonusOrder(t, s, cookie, csrf, randomHex(16), "2000", "manual")
		want := int64(0)
		if enabled {
			want = 500 * Dollar
		}
		if order.BonusMicros != want {
			t.Fatalf("saved promotion credited %d, want %d", order.BonusMicros, want)
		}
	}
	settings.RechargeBonusTiers[0].Percent = 101
	requireCode(t, call(t, s, "PUT", "/api/admin/settings", map[string]any{"settings": settings, "plans": plans}, cookie, csrf), 400)
}
