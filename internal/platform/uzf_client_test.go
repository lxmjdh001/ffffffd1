package platform

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// Exercise D1's HTTP/order/worker/ledger flow against the original response
// contract. No UZF source is imported, started, changed or deployed by this test.
func TestExistingUZFOrderWorkerAndRetry(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	s.Config.USDTProvider, s.Config.USDTWallet = "uzf", usdtContract
	s.Config.USDTMonitorSecret = "existing-service-secret"
	var paid atomic.Bool
	var order Order
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		params := map[string]string{"amount": q.Get("amount"), "currency": q.Get("currency")}
		if len(q) != 4 || q.Get("signature") != MonitorSign(params, q.Get("timestamp"), s.Config.USDTMonitorSecret) {
			t.Error("unexpected UZF protocol parameters or signature")
		}
		if !paid.Load() {
			jsonOut(w, 200, map[string]any{"success": true, "data": map[string]any{"found": false, "transfer": nil}})
			return
		}
		jsonOut(w, 200, map[string]any{"success": true, "data": map[string]any{"found": true, "transfer": uzfReceipt{
			BillID: "original-bill-123", Amount: json.Number(formatMoney(order.USDTUnits)), Currency: "USDT", BillTimestamp: s.Now().UnixMilli(),
		}}})
	}))
	defer provider.Close()
	s.Config.USDTMonitorURL = provider.URL + "/api/check"
	s.HTTP = provider.Client()
	key := randomHex(16)
	create := func(idkey string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://localhost:8088/api/orders", bytes.NewBufferString(`{"amount":"10","method":"usdt"}`))
		r.AddCookie(cookie)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", idkey)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := create(key)
	requireCode(t, w, 201)
	json.Unmarshal(w.Body.Bytes(), &order)
	if order.USDTProvider != "uzf" || order.Wallet != usdtContract || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("order snapshot or pre-payment balance incorrect")
	}
	paid.Store(true)
	s.maintenance(t.Context())
	s.DB.First(&u, "id = ?", u.ID)
	s.DB.First(&order, "id = ?", order.ID)
	if order.Status != "paid" || u.BalanceMicros != 10*Dollar || countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("existing UZF worker settlement failed")
	}
	s.Config.USDTMonitorURL, s.Config.USDTMonitorSecret = "", ""
	requireCode(t, create(key), 201)
	requireCode(t, create(randomHex(16)), 503)
	if countRows(t, s, &Order{}) != 1 || countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("retry created another order or credited funds twice")
	}
	for _, method := range s.Config.paymentMethods() {
		if method == "usdt" {
			t.Fatal("unconfigured existing UZF service was offered as available")
		}
	}
}
