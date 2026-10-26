package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func deletionAdmin(t *testing.T, s *Server) (User, *http.Cookie, string) {
	t.Helper()
	u := seedUser(t, s, 0)
	if err := s.DB.Model(&u).Update("role", "admin").Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := sessionFor(t, s, u)
	return u, cookie, csrf
}

func TestAdminDeleteUserLifecycle(t *testing.T) {
	s := testServer(t)
	_, adminCookie, csrf := deletionAdmin(t, s)
	u, other := seedUser(t, s, 0), seedUser(t, s, 0)
	userCookie, _ := sessionFor(t, s, u)
	otherCookie, _ := sessionFor(t, s, other)
	a, otherApp := seedApp(t, s, u, 10), seedApp(t, s, other, 10)
	_, raw, visitor := seedTicket(t, s, a)
	if err := s.DB.Model(&u).Update("gift_balance_micros", 100*Dollar).Error; err != nil {
		t.Fatal(err)
	}
	for _, owner := range []User{u, other} {
		for _, row := range []any{
			&PWAApp{ID: owner.ID, UserID: owner.ID, Slug: randomHex(8), Name: "PWA", TargetURL: "https://example.com", Enabled: true, PushEnabled: true, ThemeColor: "#ffffff"},
			&TrafficPool{Slug: owner.ID, UserID: owner.ID, Enabled: true},
			&Cloak{ID: owner.ID, UserID: owner.ID, Slug: randomHex(8), Enabled: true, Status: "rules", TargetType: "url", TargetURL: "https://example.com"},
			&DIYLandingPreview{ID: owner.ID, UserID: owner.ID, HTML: "<html></html>", ExpiresAt: s.Now().Add(time.Hour)},
		} {
			if err := s.DB.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	order := Order{ID: randomHex(16), UserID: u.ID, IdempotencyKey: randomHex(16), Status: "paid", AmountMicros: Dollar, Method: "manual"}
	ledger := Ledger{ID: randomHex(16), UserID: u.ID, Kind: "recharge", AmountMicros: Dollar}
	for _, row := range []any{&order, &ledger} {
		if err := s.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	body := map[string]string{"confirm_email": "  " + strings.ToUpper(u.Email) + "  "}
	for range 2 {
		requireCode(t, call(t, s, "DELETE", "/api/admin/users/"+u.ID, body, adminCookie, csrf), 200)
	}
	var stored User
	if err := s.DB.First(&stored, "id = ?", u.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.DeletedAt == nil || !stored.Disabled || stored.GiftBalanceMicros != 100*Dollar || stored.BalanceMicros != 0 {
		t.Fatalf("unexpected retained account: %+v", stored)
	}
	for _, model := range []any{&Session{}, &DIYLandingPreview{}} {
		var count int64
		if err := s.DB.Model(model).Where("user_id = ?", u.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("credentials retained: %T %d %v", model, count, err)
		}
	}
	for _, model := range []any{&Application{}, &PWAApp{}, &TrafficPool{}, &Cloak{}} {
		var count int64
		if err := s.DB.Model(model).Where("user_id = ? AND enabled = ?", u.ID, true).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("owner entry enabled: %T %d %v", model, count, err)
		}
		if err := s.DB.Model(model).Where("user_id = ? AND enabled = ?", other.ID, true).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("other owner changed: %T %d %v", model, count, err)
		}
	}
	var cloak Cloak
	if err := s.DB.First(&cloak, "id = ?", u.ID).Error; err != nil || cloak.Status != "blocked" {
		t.Fatalf("cloak not blocked: %+v %v", cloak, err)
	}
	requireCode(t, call(t, s, "GET", "/api/me", nil, userCookie, ""), 401)
	requireCode(t, call(t, s, "GET", "/api/me", nil, otherCookie, ""), 200)
	if got := download(t, s, raw, visitor, "GET", ""); got.Code == 200 || got.Code == 206 {
		t.Fatal("deleted user's ticket still downloads")
	}
	var listed struct {
		Data  []User `json:"data"`
		Total int64  `json:"total"`
	}
	w := call(t, s, "GET", "/api/admin/users", nil, adminCookie, "")
	requireCode(t, w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 2 || len(listed.Data) != 2 {
		t.Fatalf("deleted user in list: %s", w.Body.String())
	}
	for _, member := range listed.Data {
		if member.ID == u.ID {
			t.Fatal("deleted user listed")
		}
	}
	if countRows(t, s, &Order{}) != 1 || countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("financial history changed")
	}
	var audits []Audit
	if err := s.DB.Where("action = ?", "user.delete").Find(&audits).Error; err != nil || len(audits) != 1 || audits[0].Target != u.ID {
		t.Fatalf("audit not idempotent: %+v %v", audits, err)
	}
	var retainedOrder Order
	if err := s.DB.First(&retainedOrder, "id = ?", order.ID).Error; err != nil || retainedOrder.Status != "paid" || retainedOrder.AmountMicros != Dollar {
		t.Fatal("historical order changed", err)
	}
	var untouched Application
	if err := s.DB.First(&untouched, "id = ?", otherApp.ID).Error; err != nil || !untouched.Enabled {
		t.Fatal("other application changed", err)
	}
	input := authInput{Email: u.Email, Password: "test-password-12345", CaptchaID: randomHex(16), CaptchaCode: "12345"}
	if err := s.DB.Create(&Challenge{ID: input.CaptchaID, Hash: s.mac(input.CaptchaID + ":" + input.CaptchaCode), ExpiresAt: s.Now().Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, call(t, s, "POST", "/api/auth/login", input, nil, ""), 401)
	// Financial callbacks must still be able to lock the retained account.
	if err := s.DB.Transaction(func(tx *gorm.DB) error { _, err := lockUser(tx, u.ID); return err }); err != nil {
		t.Fatal(err)
	}
	if _, err := lockActiveUser(s.DB, u.ID); err != gorm.ErrRecordNotFound {
		t.Fatal("interactive mutation allowed", err)
	}
	for _, request := range []struct {
		method, suffix string
		body           any
	}{
		{"PATCH", "", map[string]bool{"disabled": false}},
		{"POST", "/password", map[string]string{"password": "654321"}},
		{"PUT", "/referral-rates", map[string]any{"rates": []string{"10", "5", "2"}}},
		{"DELETE", "/referral-rates", nil},
	} {
		requireCode(t, call(t, s, request.method, "/api/admin/users/"+u.ID+request.suffix, request.body, adminCookie, csrf), 404)
	}
}

func TestAdminDeleteUserGuards(t *testing.T) {
	s := testServer(t)
	admin, cookie, csrf := deletionAdmin(t, s)
	u := seedUser(t, s, 0)
	memberCookie, memberCSRF := sessionFor(t, s, u)
	body := map[string]string{"confirm_email": u.Email}
	path := "/api/admin/users/" + u.ID
	requireCode(t, call(t, s, "DELETE", path, body, nil, ""), 401)
	requireCode(t, call(t, s, "DELETE", path, body, memberCookie, memberCSRF), 403)
	requireCode(t, call(t, s, "DELETE", path, body, cookie, "wrong"), 403)
	requireCode(t, call(t, s, "DELETE", path, map[string]string{"confirm_email": "wrong@example.com"}, cookie, csrf), 400)
	requireCode(t, call(t, s, "DELETE", path, map[string]string{}, cookie, csrf), 400)
	requireCode(t, call(t, s, "DELETE", "/api/admin/users/missing", body, cookie, csrf), 404)
	for _, a := range []User{admin, func() User { a, _, _ := deletionAdmin(t, s); return a }()} {
		requireCode(t, call(t, s, "DELETE", "/api/admin/users/"+a.ID, map[string]string{"confirm_email": a.Email}, cookie, csrf), 403)
	}
	var stored User
	if err := s.DB.First(&stored, "id = ?", u.ID).Error; err != nil || stored.Disabled || stored.DeletedAt != nil {
		t.Fatal("rejected operation modified user", err)
	}
	if countRows(t, s, &Audit{}) != 0 {
		t.Fatal("rejected operation audited as successful")
	}
}

func TestAdminDeleteUserFinancialGuards(t *testing.T) {
	s := testServer(t)
	_, cookie, csrf := deletionAdmin(t, s)
	for _, kind := range []string{"cash", "held", "gift-held", "order", "withdrawal-pending", "withdrawal-approved", "gift-only", "expired-order", "paid-withdrawal"} {
		t.Run(kind, func(t *testing.T) {
			u := seedUser(t, s, 0)
			var err error
			switch kind {
			case "cash":
				err = s.DB.Model(&u).Update("balance_micros", Dollar).Error
			case "held":
				err = s.DB.Model(&u).Updates(map[string]any{"balance_micros": Dollar, "held_micros": Dollar}).Error
			case "gift-held":
				err = s.DB.Model(&u).Updates(map[string]any{"gift_balance_micros": Dollar, "gift_held_micros": Dollar}).Error
			case "gift-only":
				err = s.DB.Model(&u).Update("gift_balance_micros", 100*Dollar).Error
			case "order", "expired-order":
				expires := s.Now().Add(time.Hour)
				if kind == "expired-order" {
					expires = s.Now().Add(-time.Hour)
				}
				err = s.DB.Create(&Order{ID: randomHex(16), UserID: u.ID, IdempotencyKey: randomHex(16), Status: "pending", ExpiresAt: expires}).Error
			default:
				status := strings.TrimPrefix(kind, "withdrawal-")
				if kind == "paid-withdrawal" {
					status = "paid"
				}
				err = s.DB.Create(&ReferralWithdrawal{ID: randomHex(16), UserID: u.ID, IdempotencyKey: randomHex(16), AmountMicros: 100 * Dollar, Status: status}).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			want := 409
			allowed := kind == "gift-only" || kind == "expired-order" || kind == "paid-withdrawal"
			if allowed {
				want = 200
			}
			requireCode(t, call(t, s, "DELETE", "/api/admin/users/"+u.ID, map[string]string{"confirm_email": u.Email}, cookie, csrf), want)
			var stored User
			if err := s.DB.First(&stored, "id = ?", u.ID).Error; err != nil {
				t.Fatal(err)
			}
			if (stored.DeletedAt != nil) != allowed || stored.Disabled != allowed {
				t.Fatal("unexpected account state", stored)
			}
		})
	}
}

func TestUserDeletionMigrationLegacyAndRepeat(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 123*Dollar)
	if err := s.DB.Delete(&Migration{}, "version = ?", userDeletionMigrationVersion).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Migrator().DropColumn(&User{}, "DeletedAt"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.DB.Transaction(migrateUserDeletion); err != nil {
			t.Fatal(err)
		}
	}
	var stored User
	if err := s.DB.First(&stored, "id = ?", u.ID).Error; err != nil || stored.BalanceMicros != u.BalanceMicros || stored.DeletedAt != nil {
		t.Fatalf("migration changed account: %+v %v", stored, err)
	}
	if !s.DB.Migrator().HasIndex(&User{}, "idx_users_deleted_at") {
		t.Fatal("missing index")
	}
}

func TestUserDeletionLateSettlementAndReferralChain(t *testing.T) {
	s := testServer(t)
	_, cookie, csrf := deletionAdmin(t, s)
	grandparent, removed, payer := seedUser(t, s, 0), seedUser(t, s, 0), seedUser(t, s, 0)
	seedReferralAccount(t, s, grandparent, nil)
	seedReferralAccount(t, s, removed, &grandparent)
	seedReferralAccount(t, s, payer, &removed)
	late := seedReferralOrder(t, s, removed, "usdt", 10*Dollar, Dollar)
	if err := s.DB.Model(&late).Updates(map[string]any{"status": "expired", "expires_at": s.Now().Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, call(t, s, "DELETE", "/api/admin/users/"+removed.ID, map[string]string{"confirm_email": removed.Email}, cookie, csrf), 200)
	childOrder := seedReferralOrder(t, s, payer, "manual", 100*Dollar, 0)
	if err := s.settle(childOrder.ID, randomHex(16), "manual", 100*Dollar); err != nil {
		t.Fatal(err)
	}
	var retained User
	if err := s.DB.First(&retained, "id = ?", removed.ID).Error; err != nil || retained.BalanceMicros != 0 {
		t.Fatal("deleted inviter received new rewards", err)
	}
	var grand User
	if err := s.DB.First(&grand, "id = ?", grandparent.ID).Error; err != nil || grand.BalanceMicros != 2*Dollar {
		t.Fatalf("higher referral level changed: %+v %v", grand, err)
	}
	trade := randomHex(16)
	for range 2 {
		if err := s.settle(late.ID, trade, "usdt", 10*Dollar); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DB.First(&retained, "id = ?", removed.ID).Error; err != nil || retained.BalanceMicros != 11*Dollar || !retained.Disabled || retained.DeletedAt == nil {
		t.Fatalf("late funds lost or account restored: %+v %v", retained, err)
	}
	// No interactive administrator operation may revive or fund a removed account.
	body, _ := json.Marshal(map[string]string{"amount": "1", "note": "deletion test"})
	r := httptest.NewRequest("POST", "http://localhost:8088/api/admin/users/"+removed.ID+"/balance", bytes.NewReader(body))
	r.AddCookie(cookie)
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Idempotency-Key", randomHex(16))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 404)
	// An order request authenticated before deletion must check the tombstone
	// again under the account lock before opening a new payable order.
	body, _ = json.Marshal(map[string]string{"amount": "1", "method": "manual"})
	r = httptest.NewRequest("POST", "http://localhost:8088/api/orders", bytes.NewReader(body))
	r.Header.Set("Idempotency-Key", randomHex(16))
	r = r.WithContext(context.WithValue(r.Context(), userKey, removed))
	w = httptest.NewRecorder()
	s.createOrder(w, r)
	requireCode(t, w, 404)
}
