package platform

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestUSDTCentAmountRangeAndExhaustion(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	seen := map[int64]bool{}
	for range 110 {
		o := Order{ID: randomHex(16), UserID: u.ID, IdempotencyKey: randomHex(16), Method: "usdt", AmountMicros: 500 * Dollar,
			RateMicros: Dollar, Wallet: usdtContract, Status: "pending", CreatedAt: s.Now(), ExpiresAt: s.Now().Add(30 * time.Minute)}
		if err := s.DB.Transaction(func(tx *gorm.DB) error {
			if err := s.reserveUSDT(tx, &o); err != nil {
				return err
			}
			return tx.Create(&o).Error
		}); err != nil {
			t.Fatal(err)
		}
		if o.USDTUnits < 499010000 || o.USDTUnits > 500100000 || o.USDTUnits%10000 != 0 || seen[o.USDTUnits] {
			t.Fatalf("invalid or duplicated cent payment amount: %d", o.USDTUnits)
		}
		seen[o.USDTUnits] = true
	}
	if !seen[499010000] || !seen[500100000] {
		t.Fatal("allocation did not include both requested range endpoints")
	}
	// Paid/expired amounts are not silently recycled into another order.
	if err := s.DB.Model(&Order{}).Where("user_id = ?", u.ID).Updates(map[string]any{"status": "expired", "expires_at": s.Now().Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	o := Order{ID: randomHex(16), AmountMicros: 500 * Dollar, RateMicros: Dollar, Wallet: usdtContract}
	if err := s.reserveUSDT(s.DB, &o); err == nil || o.USDTUnits != 0 || countRows(t, s, &USDTReservation{}) != 110 {
		t.Fatal("exhausted range recycled a reserved amount or created another slot")
	}
}

func TestUSDTCentsKeepHistoricalReservationsAndExactMatching(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	old := Order{ID: randomHex(16), UserID: u.ID, IdempotencyKey: randomHex(16), Method: "usdt", AmountMicros: 500 * Dollar,
		RateMicros: Dollar, Wallet: usdtContract, USDTUnits: 500009314, Status: "pending", CreatedAt: s.Now(), ExpiresAt: s.Now().Add(30 * time.Minute)}
	if err := s.DB.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	reservation := USDTReservation{Wallet: old.Wallet, Units: old.USDTUnits, OrderID: old.ID}
	if err := s.DB.Create(&reservation).Error; err != nil {
		t.Fatal(err)
	}
	newOrder := Order{ID: randomHex(16), AmountMicros: 500 * Dollar, RateMicros: Dollar, Wallet: old.Wallet}
	if err := s.reserveUSDT(s.DB, &newOrder); err != nil {
		t.Fatal(err)
	}
	var saved Order
	if err := s.DB.First(&saved, "id = ?", old.ID).Error; err != nil || saved.USDTUnits != old.USDTUnits {
		t.Fatal("new cent allocation changed a historical order")
	}
	newOrder.CreatedAt, newOrder.ExpiresAt = s.Now(), s.Now().Add(time.Minute)
	receipt := uzfReceipt{BillID: "cent-bill", Amount: json.Number(formatMoney(newOrder.USDTUnits)), Currency: "USDT", BillTimestamp: s.Now().UnixMilli()}
	if !validUZFReceipt(newOrder, receipt) {
		t.Fatal("an exact cent receipt did not match")
	}
	receipt.Amount = json.Number(formatMoney(newOrder.USDTUnits + 1))
	if validUZFReceipt(newOrder, receipt) {
		t.Fatal("receipt matching rounded away a different payment amount")
	}
}

func TestUSDTCentAmountsStayPositiveAndRespectExchangeRate(t *testing.T) {
	for _, tc := range []struct{ amount, rate, low, high int64 }{
		{Dollar, Dollar, 10000, 1100000},
		{Dollar, 1000 * Dollar, 10000, 110000},
		{100 * Dollar, 6650000, 14050000, 15140000},
	} {
		s := testServer(t)
		o := Order{ID: randomHex(16), AmountMicros: tc.amount, RateMicros: tc.rate, Wallet: usdtContract}
		if err := s.reserveUSDT(s.DB, &o); err != nil || o.USDTUnits < tc.low || o.USDTUnits > tc.high || o.USDTUnits%10000 != 0 {
			t.Fatalf("incorrect cent quote: amount=%d rate=%d units=%d err=%v", tc.amount, tc.rate, o.USDTUnits, err)
		}
	}
	s := testServer(t)
	if err := s.reserveUSDT(s.DB, &Order{RateMicros: 0}); err == nil {
		t.Fatal("zero USDT exchange rate was accepted")
	}
}

func TestUZFCentOrderCreationAndRepeatedSettlement(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	s.Config.USDTProvider = "uzf"
	s.Config.USDTWallet = usdtContract
	s.Config.USDTMonitorSecret = "test-monitor-secret"
	var funded atomic.Bool
	var order Order
	monitor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		params := map[string]string{"amount": q.Get("amount"), "currency": q.Get("currency")}
		if q.Get("signature") != MonitorSign(params, q.Get("timestamp"), s.Config.USDTMonitorSecret) {
			t.Error("cent amount query did not retain the existing UZF signature")
		}
		if !funded.Load() {
			jsonOut(w, 200, map[string]any{"success": true, "data": map[string]any{"found": false}})
			return
		}
		if q.Get("amount") != formatMoney(order.USDTUnits) {
			t.Error("UZF request rounded or changed the reserved cent amount")
		}
		proof := uzfReceipt{BillID: "test-cent-order-bill", Amount: json.Number(formatMoney(order.USDTUnits)), Currency: "USDT", BillTimestamp: order.CreatedAt.UnixMilli()}
		jsonOut(w, 200, map[string]any{"success": true, "data": map[string]any{"found": true, "transfer": proof}})
	}))
	defer monitor.Close()
	s.Config.USDTMonitorURL = monitor.URL + "/api/check"
	s.HTTP = monitor.Client()
	cookie, csrf := sessionFor(t, s, u)
	key := randomHex(16)
	order = createBonusOrder(t, s, cookie, csrf, key, "500", "usdt")
	if order.USDTUnits < 499010000 || order.USDTUnits > 500100000 || order.USDTUnits%10000 != 0 || order.AmountMicros != 500*Dollar || order.BonusMicros != 175*Dollar {
		t.Fatal("new cent order lost the requested range, principal or activity bonus")
	}
	retry := createBonusOrder(t, s, cookie, csrf, key, "500", "usdt")
	if retry.ID != order.ID || retry.USDTUnits != order.USDTUnits || countRows(t, s, &USDTReservation{}) != 1 {
		t.Fatal("an idempotent retry allocated another payment amount")
	}
	funded.Store(true)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if err := s.reconcileUSDT(t.Context(), order); err != nil {
				t.Errorf("repeated cent-payment query: %v", err)
			}
		})
	}
	wg.Wait()
	if err := s.DB.First(&u, "id = ?", u.ID).Error; err != nil || u.BalanceMicros != 675*Dollar || countRows(t, s, &Ledger{}) != 2 {
		t.Fatal("cent payment did not credit the original principal and bonus exactly once")
	}
}
