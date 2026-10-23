package platform

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func settlementFixture(t *testing.T, method, status string) (*Server, User, Order, User, *http.Cookie, string) {
	t.Helper()
	s := testServer(t)
	u := seedUser(t, s, 0)
	admin := seedUser(t, s, 0)
	if err := s.DB.Model(&admin).Update("role", "admin").Error; err != nil {
		t.Fatal(err)
	}
	o := Order{ID: randomHex(16), UserID: u.ID, IdempotencyKey: randomHex(16), Method: method, AmountMicros: 100 * Dollar, Status: status, ExpiresAt: s.Now().Add(time.Hour)}
	if err := s.DB.Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := sessionFor(t, s, admin)
	return s, u, o, admin, cookie, csrf
}

func requireSingleSettlement(t *testing.T, s *Server, owner User, order Order, admin User) {
	t.Helper()
	if err := s.DB.First(&owner, "id = ?", owner.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.First(&order, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if owner.BalanceMicros != order.AmountMicros || order.Status != "paid" || order.PaidAt == nil || order.TradeID == nil || *order.TradeID == "" {
		t.Fatal("order was not credited exactly once")
	}
	if countRows(t, s, &Ledger{}) != 1 || countRows(t, s, &Audit{}) != 1 {
		t.Fatal("expected one balance entry and one audit record")
	}
	var audit Audit
	if err := s.DB.First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.ActorID != admin.ID || audit.Target != order.ID || audit.Action != "order.settle" {
		t.Fatal("settlement audit is missing the administrator or order")
	}
}

func TestAdminSettlementOptionalFields(t *testing.T) {
	for _, tc := range []struct{ name, method, status, trade, note, prefix string }{
		{"manual empty", "manual", "pending", "", "", "admin:"},
		{"alipay empty", "alipay", "pending", "", "", "admin:"},
		{"wechat empty", "wechat", "pending", "", "", "admin:"},
		{"usdt empty", "usdt", "pending", "", "", "admin:"},
		{"expired", "manual", "expired", "", "", "admin:"},
		{"short fields", "manual", "pending", "1", "已到账", "manual:"},
		{"usdt short reference", "usdt", "pending", "1", "到账", "tron:"},
		{"whitespace", "manual", "pending", "  ", "  ", "admin:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, owner, order, admin, cookie, csrf := settlementFixture(t, tc.method, tc.status)
			path := "/api/admin/orders/" + order.ID + "/settle"
			body := map[string]string{"trade_id": tc.trade, "note": tc.note}
			requireCode(t, call(t, s, "POST", path, body, cookie, csrf), 200)
			// Retries, including changed optional fields, must not add funds again.
			requireCode(t, call(t, s, "POST", path, body, cookie, csrf), 200)
			requireCode(t, call(t, s, "POST", path, map[string]string{"trade_id": "different", "note": "补充"}, cookie, csrf), 200)
			requireSingleSettlement(t, s, owner, order, admin)
			var saved Order
			s.DB.First(&saved, "id = ?", order.ID)
			if !strings.HasPrefix(*saved.TradeID, tc.prefix) {
				t.Fatal("incorrect settlement reference namespace")
			}
			var audit Audit
			s.DB.First(&audit)
			note := strings.TrimSpace(tc.note)
			if note == "" {
				note = "管理员确认入账"
			}
			if !strings.Contains(audit.Detail, note) {
				t.Fatal("optional note or default description was not recorded")
			}
		})
	}
}

func TestAdminSettlementConcurrentConfirmation(t *testing.T) {
	s, owner, order, admin, cookie, csrf := settlementFixture(t, "manual", "pending")
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			w := call(t, s, "POST", "/api/admin/orders/"+order.ID+"/settle", map[string]string{}, cookie, csrf)
			if w.Code != 200 {
				t.Errorf("confirmation failed: %d %s", w.Code, w.Body.String())
			}
		})
	}
	wg.Wait()
	requireSingleSettlement(t, s, owner, order, admin)
}

func TestAdminSettlementStillRequiresAdminAndCSRF(t *testing.T) {
	s, owner, order, _, cookie, csrf := settlementFixture(t, "manual", "pending")
	path := "/api/admin/orders/" + order.ID + "/settle"
	body := map[string]string{}
	requireCode(t, call(t, s, "POST", path, body, nil, ""), 401)
	memberCookie, memberCSRF := sessionFor(t, s, owner)
	requireCode(t, call(t, s, "POST", path, body, memberCookie, memberCSRF), 403)
	requireCode(t, call(t, s, "POST", path, body, cookie, "wrong-"+csrf), 403)
	if countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("unauthorized request credited an order")
	}
}

func TestAdminSettlementReferenceReplayAndPaidOrder(t *testing.T) {
	s, owner, first, _, cookie, csrf := settlementFixture(t, "alipay", "pending")
	if err := s.settle(first.ID, "futoon:external-1", first.Method, first.AmountMicros); err != nil {
		t.Fatal(err)
	}
	// A stale admin dialog may be confirmed after the automatic notification.
	requireCode(t, call(t, s, "POST", "/api/admin/orders/"+first.ID+"/settle", map[string]string{}, cookie, csrf), 200)
	if countRows(t, s, &Audit{}) != 0 {
		t.Fatal("no-op confirmation must not claim a second settlement")
	}
	second := Order{ID: randomHex(16), UserID: owner.ID, IdempotencyKey: randomHex(16), Method: "wechat", AmountMicros: first.AmountMicros, Status: "pending", ExpiresAt: first.ExpiresAt}
	if err := s.DB.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	body := map[string]string{"trade_id": "external-1", "note": "已到账"}
	requireCode(t, call(t, s, "POST", "/api/admin/orders/"+second.ID+"/settle", body, cookie, csrf), 409)
	if countRows(t, s, &Ledger{}) != 1 || countRows(t, s, &Audit{}) != 0 {
		t.Fatal("reusing a transaction credited another order")
	}
	if err := s.DB.First(&second, "id = ?", second.ID).Error; err != nil || second.Status != "pending" {
		t.Fatal("rejected settlement was not rolled back")
	}
	if err := s.DB.First(&owner, "id = ?", owner.ID).Error; err != nil || owner.BalanceMicros != first.AmountMicros {
		t.Fatal("replay changed the balance")
	}
}
