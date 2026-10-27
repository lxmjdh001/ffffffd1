package platform

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRechargeMinimumAmount(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	request := func(amount, method, key string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"amount": amount, "method": method})
		r := httptest.NewRequest("POST", "http://localhost:8088/api/orders", bytes.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	for _, method := range []string{"manual", "usdt"} {
		for _, amount := range []string{"0", "1", "49", "49.99", "-50", "10000.01", "50.001"} {
			w := request(amount, method, randomHex(16))
			requireCode(t, w, 400)
			if !strings.Contains(w.Body.String(), "50–10,000") {
				t.Fatal("missing recharge limits")
			}
		}
	}
	if countRows(t, s, &Order{}) != 0 {
		t.Fatal("invalid requests created orders")
	}
	for _, amount := range []string{"50", "50.01", "10000"} {
		requireCode(t, request(amount, "manual", randomHex(16)), 201)
	}
	if countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("unpaid orders credited balance")
	}
	// A retry of a historical low-value order must still return the same order.
	key := randomHex(16)
	old := Order{ID: randomHex(16), UserID: u.ID, Method: "manual", AmountMicros: 10 * Dollar, IdempotencyKey: u.ID + ":" + key, Status: "pending", CreatedAt: s.Now(), ExpiresAt: s.Now().AddDate(0, 0, 1)}
	if err := s.DB.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	w := request("10", "manual", key)
	requireCode(t, w, 201)
	var got Order
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.ID != old.ID {
		t.Fatal("historical retry changed order")
	}
}
