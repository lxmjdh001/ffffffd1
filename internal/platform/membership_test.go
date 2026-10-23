package platform

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func buyMember(t *testing.T, s *Server, u User, key string) *httptest.ResponseRecorder {
	t.Helper()
	cookie, csrf := sessionFor(t, s, u)
	r := httptest.NewRequest("POST", "http://localhost:8088/api/subscription", nil)
	r.AddCookie(cookie)
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestMembershipDefaultsAndRegistrationOnlyGift(t *testing.T) {
	s := testServer(t)
	now := time.Date(2026, 10, 31, 15, 59, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	u := seedUser(t, s, 3000)
	a := seedApp(t, s, u, 10)
	var plans []Plan
	if err := s.DB.Order("price_micros").Find(&plans).Error; err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 || plans[0].Name != "免费套餐" || plans[0].AppLimit != 10 || plans[0].DownloadLimit != 1000 || plans[0].StorageBytes != 10<<30 || plans[1].PriceMicros != 99*Dollar || plans[1].AppLimit != 500 || plans[1].DownloadLimit != 10000 || plans[1].StorageBytes != 30<<30 {
		t.Fatalf("incorrect membership defaults: %+v", plans)
	}
	if err := s.DB.Model(&Plan{}).Where("id = ?", "free").Update("download_limit", 2).Error; err != nil {
		t.Fatal(err)
	}
	_, token, visitor := seedTicket(t, s, a)
	requireCode(t, download(t, s, token, visitor, "GET", ""), 200)
	var before User
	s.DB.First(&before, "id = ?", u.ID)
	if before.BalanceMicros != 3000 {
		t.Fatal("registration gift was charged")
	}
	now = now.Add(2 * time.Minute)
	_, token, visitor = seedTicket(t, s, a)
	requireCode(t, download(t, s, token, visitor, "GET", ""), 200)
	s.DB.First(&before, "id = ?", u.ID)
	if before.BalanceMicros != 3000 {
		t.Fatal("unused registration gift did not carry over")
	}
	_, token, visitor = seedTicket(t, s, a)
	requireCode(t, download(t, s, token, visitor, "GET", ""), 200)
	s.DB.First(&before, "id = ?", u.ID)
	if before.BalanceMicros != 0 {
		t.Fatal("registration gift was granted again in the next calendar month")
	}
	var paid Download
	if err := s.DB.First(&paid, "amount_micros > 0").Error; err != nil || paid.AmountMicros != 3000 {
		t.Fatal("overage must be $0.003")
	}
	cookie, _ := sessionFor(t, s, u)
	w := call(t, s, "GET", "/api/dashboard", nil, cookie, "")
	requireCode(t, w, 200)
	var d struct {
		Limit     int64 `json:"quota_limit"`
		Remaining int64 `json:"quota_remaining"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || d.Limit != 2 || d.Remaining != 0 {
		t.Fatal("dashboard gift counters are incorrect")
	}
}

func TestPaidGiftFollowsPurchasedPeriodsAndRenewalIsIdempotent(t *testing.T) {
	s := testServer(t)
	now := time.Date(2026, 1, 31, 4, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	u := seedUser(t, s, 500*Dollar)
	a := seedApp(t, s, u, 10)
	if err := s.DB.Model(&Plan{}).Where("id = ?", "free").Update("download_limit", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&Plan{}).Where("id = ?", "pro").Update("download_limit", 2).Error; err != nil {
		t.Fatal(err)
	}
	key := randomHex(16)
	for range 2 {
		requireCode(t, buyMember(t, s, u, key), 200)
	}
	if countRows(t, s, &DownloadGrant{}) != 1 || countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("purchase retry generated another period")
	}
	var after User
	s.DB.First(&after, "id = ?", u.ID)
	if after.BalanceMicros != 401*Dollar || after.ProUntil == nil || after.ProUntil.In(billingZone).Day() != 28 {
		t.Fatal("$99 purchase or month-end expiry incorrect")
	}
	for range 2 {
		_, token, visitor := seedTicket(t, s, a)
		requireCode(t, download(t, s, token, visitor, "GET", ""), 200)
	}
	now = now.Add(24 * time.Hour) // Calendar month changed; paid period did not.
	_, token, visitor := seedTicket(t, s, a)
	requireCode(t, download(t, s, token, visitor, "GET", ""), 200)
	s.DB.First(&after, "id = ?", u.ID)
	if after.BalanceMicros != 401*Dollar-3000 {
		t.Fatal("paid gift replenished without a renewal")
	}
	firstExpiry := *after.ProUntil
	requireCode(t, buyMember(t, s, u, randomHex(16)), 200)
	var plan Plan
	s.DB.First(&plan, "id = ?", "pro")
	grant, err := activeDownloadGrant(s.DB, after, plan, now)
	if err != nil || grant == nil || grant.Used != 2 {
		t.Fatal("early renewal reset the active period")
	}
	now = firstExpiry
	s.DB.First(&after, "id = ?", u.ID)
	grant, err = activeDownloadGrant(s.DB, after, plan, now)
	if err != nil || grant == nil || grant.Used != 0 || grant.Limit != 2 {
		t.Fatal("next purchased period failed to activate its gift")
	}
	_, token, visitor = seedTicket(t, s, a)
	requireCode(t, download(t, s, token, visitor, "GET", ""), 200)
	now = *after.ProUntil
	_, token, visitor = seedTicket(t, s, a)
	requireCode(t, download(t, s, token, visitor, "GET", ""), 200)
	s.DB.First(&after, "id = ?", u.ID)
	if after.PlanID(now) != "free" || after.BalanceMicros != 302*Dollar-6000 {
		t.Fatal("expired membership kept granting free downloads or renewed itself")
	}
}

func TestGiftReservationSurvivesUpgradeAndExpiry(t *testing.T) {
	s := testServer(t)
	now := time.Date(2026, 10, 31, 15, 59, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	u := seedUser(t, s, 200*Dollar)
	a := seedApp(t, s, u, 10)
	old, token, visitor := seedTicket(t, s, a)
	requireCode(t, download(t, s, token, visitor, "GET", "bytes=0-4"), 206)
	requireCode(t, buyMember(t, s, u, randomHex(16)), 200)
	now = now.Add(2 * time.Minute)
	requireCode(t, download(t, s, token, visitor, "GET", "bytes=5-9"), 206)
	var free DownloadGrant
	s.DB.First(&free, "id = ?", "free:"+u.ID)
	if free.Used != 1 || free.Reserved != 0 {
		t.Fatal("old registration reservation was lost during upgrade")
	}
	var paid DownloadGrant
	s.DB.First(&paid, "user_id = ? AND plan_id = ?", u.ID, "pro")
	if paid.Used != 0 || paid.Reserved != 0 {
		t.Fatal("old completion consumed the new paid period")
	}
	if err := s.releaseTicket(old); err != nil {
		t.Fatal(err)
	}
	failed, raw, browser := seedTicket(t, s, a)
	requireCode(t, download(t, s, raw, browser, "GET", "bytes=0-4"), 206)
	if err := s.DB.Model(&Ticket{}).Where("hash = ?", failed.Hash).Update("expires_at", now.Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.releaseTicket(failed); err != nil {
			t.Fatal(err)
		}
	}
	s.DB.First(&paid, "id = ?", paid.ID)
	if paid.Used != 0 || paid.Reserved != 0 {
		t.Fatal("failed download gift reservation was lost or released twice")
	}
}

func TestMembershipMigrationPreservesUsageAndInflightReservation(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 123*Dollar)
	a := seedApp(t, s, u, 10)
	ticket, _, _ := seedTicket(t, s, a)
	if err := s.DB.Create(&Usage{UserID: u.ID, Period: period(u.CreatedAt), FreeUsed: 400, FreeReserved: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&ticket).Updates(map[string]any{"free": true, "reserved": true, "period": period(u.CreatedAt), "size": 10, "app_name": a.Name}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Delete(&Migration{}, "version = 4").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	var grant DownloadGrant
	s.DB.First(&grant, "id = ?", "free:"+u.ID)
	if grant.Used != 400 || grant.Reserved != 1 || grant.Limit != 1000 {
		t.Fatal("migration reset consumed or reserved gifts")
	}
	if err := s.finishRange(ticket, 0, 9); err != nil {
		t.Fatal(err)
	}
	s.DB.First(&grant, "id = ?", grant.ID)
	if grant.Used != 401 || grant.Reserved != 0 {
		t.Fatal("legacy transfer did not settle against imported grant")
	}
	if err := s.DB.Model(&Plan{}).Where("id = ?", "pro").Update("price_micros", 110*Dollar).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	var plan Plan
	s.DB.First(&plan, "id = ?", "pro")
	s.DB.First(&u, "id = ?", u.ID)
	if plan.PriceMicros != 110*Dollar || u.BalanceMicros != 123*Dollar {
		t.Fatal("repeat migration overwrote pricing or funds")
	}
}
