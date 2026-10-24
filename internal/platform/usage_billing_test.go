package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

func meteredServer(t *testing.T, gifts int64) *Server {
	t.Helper()
	s := testServer(t)
	if e := s.DB.Model(&Settings{}).Where("id = 1").Updates(map[string]any{"metered_billing": true, "registration_gift_limit": gifts}).Error; e != nil {
		t.Fatal(e)
	}
	return s
}
func usageRequest(s *Server, path, ip string) *http.Request {
	r := httptest.NewRequest("GET", s.Config.PublicURL+path, nil)
	r.RemoteAddr = ip + ":1234"
	r.Header.Set("User-Agent", "Usage test browser")
	return r
}
func usagePost(s *Server, path string, body any, r *http.Request) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", s.Config.PublicURL+path, bytes.NewReader(raw))
	req.RemoteAddr = r.RemoteAddr
	req.Header.Set("User-Agent", r.UserAgent())
	req.Header.Set("Origin", s.Config.PublicURL)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	return w
}
func installBody(s *Server, app PWAApp, r *http.Request, key string) map[string]string {
	return map[string]string{"key": key, "token": s.returnFlowViewToken("pwa", app.ID, r)}
}
func usageOwner(t *testing.T, s *Server, id string) User {
	t.Helper()
	var u User
	if e := s.DB.First(&u, "id = ?", id).Error; e != nil {
		t.Fatal(e)
	}
	return u
}

func TestMeteredSharedGiftAndAllFivePrices(t *testing.T) {
	s := meteredServer(t, 2)
	u := seedUser(t, s, Dollar)
	r := usageRequest(s, "/", "203.0.113.10")
	if e := s.chargeUsageUV(r, u.ID, "apk_uv", "apk-one"); e != nil {
		t.Fatal(e)
	}
	app := createPWAFixture(t, s, u, "Shared gift")
	base := "/p/" + app.Slug + "/install/"
	first := installBody(s, app, r, randomHex(16))
	requireCode(t, usagePost(s, base+"start", first, r), 200)
	requireCode(t, usagePost(s, base+"complete", first, r), 200)
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != Dollar || got.HeldMicros != 0 {
		t.Fatal("shared registration gift did not cover both kinds")
	}
	second := installBody(s, app, r, randomHex(16))
	requireCode(t, usagePost(s, base+"start", second, r), 200)
	requireCode(t, usagePost(s, base+"complete", second, r), 200)
	requireCode(t, usagePost(s, base+"complete", second, r), 200)
	if e := s.chargeUsageUV(r, u.ID, "cloak_uv", "cloak-one"); e != nil {
		t.Fatal(e)
	}
	_, page := billedReturnApp(t, s, u, "pwa")
	var pwa PWAApp
	s.DB.First(&pwa, "slug = ?", strings.Split(page, "/")[2])
	token := s.returnFlowViewToken("pwa", pwa.ID, r)
	body := map[string]string{"token": token, "event_id": randomHex(16)}
	endpoint := "/p/" + pwa.Slug + "/return-flow/view"
	requireCode(t, usagePost(s, endpoint, body, r), 200)
	requireCode(t, usagePost(s, endpoint, body, r), 200)
	body["event_id"] = randomHex(16)
	requireCode(t, usagePost(s, endpoint, body, r), 200)
	delivery := PWAPushDelivery{ID: randomHex(16), UserID: u.ID, PWAID: app.ID, ExpiresAt: s.Now().Add(time.Hour)}
	if e := s.reservePushUsage(context.Background(), delivery); e != nil {
		t.Fatal(e)
	}
	settle := func() error {
		return s.DB.Transaction(func(tx *gorm.DB) error { return s.settlePushUsage(tx, delivery, true, false) })
	}
	if e := settle(); e != nil {
		t.Fatal(e)
	}
	if e := settle(); e != nil {
		t.Fatal(e)
	}
	if e := s.chargeUsageUV(r, u.ID, "apk_uv", "apk-two"); e != nil {
		t.Fatal(e)
	}
	got := usageOwner(t, s, u.ID)
	if got.BalanceMicros != Dollar-30000-1000-2000-1000-3000 || got.HeldMicros != 0 {
		t.Fatalf("five-price settlement: %+v", got)
	}
	x, _ := getSettings(s.DB)
	limit, used, reserved, e := meteredGiftTotals(s.DB, got, x, s.Now())
	if e != nil || limit != 2 || used != 2 || reserved != 0 {
		t.Fatalf("shared gift totals %d/%d/%d %v", limit, used, reserved, e)
	}
}

func TestMeteredRegistrationCreatesOneSharedGift(t *testing.T) {
	s := meteredServer(t, 1000)
	in := authInput{Email: "metered@example.com", Name: "Metered", Password: "test123456", CaptchaCode: "12345", CaptchaID: randomHex(16)}
	if e := s.DB.Create(&Challenge{ID: in.CaptchaID, Hash: s.mac(in.CaptchaID + ":" + in.CaptchaCode), ExpiresAt: s.Now().Add(time.Minute)}).Error; e != nil {
		t.Fatal(e)
	}
	w := call(t, s, "POST", "/api/auth/register", in, nil, "")
	requireCode(t, w, 200)
	var result struct {
		User User `json:"user"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	u := result.User
	for range 3 {
		x, _ := getSettings(s.DB)
		limit, used, reserved, e := meteredGiftTotals(s.DB, u, x, s.Now())
		if e != nil || limit != 1000 || used != 0 || reserved != 0 {
			t.Fatalf("registration gift %d/%d/%d: %v", limit, used, reserved, e)
		}
	}
	var count int64
	s.DB.Model(&DownloadGrant{}).Where("user_id = ?", u.ID).Count(&count)
	if count != 1 {
		t.Fatal("more than one registration gift")
	}
	// Editing the default never retroactively changes existing grants.
	s.DB.Model(&Settings{}).Where("id = 1").Update("registration_gift_limit", 17)
	x, _ := getSettings(s.DB)
	limit, _, _, _ := meteredGiftTotals(s.DB, u, x, s.Now())
	if limit != 1000 {
		t.Fatal("existing gift reset by configuration change")
	}
}

func TestMeteredReturnFlowChargesOnlyConfirmedDisplay(t *testing.T) {
	s := meteredServer(t, 1000)
	u := seedUser(t, s, 1000)
	_, page := billedReturnApp(t, s, u, "pwa")
	var app PWAApp
	s.DB.First(&app, "slug = ?", strings.Split(page, "/")[2])
	r := usageRequest(s, page, "203.0.113.80")
	body := map[string]string{"token": s.returnFlowViewToken("pwa", app.ID, r), "event_id": randomHex(16), "action": "reserve"}
	endpoint := "/p/" + app.Slug + "/return-flow/view"
	requireCode(t, usagePost(s, endpoint, body, r), 200)
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != 1000 || got.HeldMicros != 1000 {
		t.Fatal("display authorization debited funds")
	}
	body["action"] = "cancel"
	requireCode(t, usagePost(s, endpoint, body, r), 200)
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != 1000 || got.HeldMicros != 0 {
		t.Fatal("invisible display did not release hold")
	}
	body["action"] = "complete"
	requireCode(t, usagePost(s, endpoint, body, r), 410)
	body["event_id"] = randomHex(16)
	body["action"] = "reserve"
	requireCode(t, usagePost(s, endpoint, body, r), 200)
	body["action"] = "complete"
	for range 3 {
		requireCode(t, usagePost(s, endpoint, body, r), 200)
	}
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != 0 || got.HeldMicros != 0 {
		t.Fatal("display was not charged exactly once")
	}
	body["event_id"] = randomHex(16)
	body["action"] = "reserve"
	requireCode(t, usagePost(s, endpoint, body, r), 402)
}

func TestMeteredPushWorkerResultSettlement(t *testing.T) {
	for _, outcome := range []string{"accepted", "failed", "retry", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			s := meteredServer(t, 1000)
			u := seedUser(t, s, 1000)
			task := PWAPushTask{ID: randomHex(16), UserID: u.ID, PWAID: randomHex(16), RequestKeyHash: randomHex(32), Status: "sending"}
			if outcome == "cancelled" {
				task.Status = "cancelled"
			}
			if e := s.DB.Create(&task).Error; e != nil {
				t.Fatal(e)
			}
			d := PWAPushDelivery{ID: randomHex(16), TaskID: task.ID, SubscriptionID: randomHex(16), UserID: u.ID, PWAID: task.PWAID, Status: "processing", Attempts: 1, LeaseToken: randomHex(16), ExpiresAt: s.Now().Add(time.Hour)}
			if e := s.DB.Create(&d).Error; e != nil {
				t.Fatal(e)
			}
			if e := s.reservePushUsage(context.Background(), d); e != nil {
				t.Fatal(e)
			}
			result := pwaPushOutcome{accepted: outcome == "accepted", retryable: outcome == "retry"}
			for range 2 {
				if e := s.finishPWAPushDelivery(context.Background(), d, result); e != nil {
					t.Fatal(e)
				}
			}
			got := usageOwner(t, s, u.ID)
			if outcome == "accepted" {
				if got.BalanceMicros != 0 || got.HeldMicros != 0 {
					t.Fatal("accepted delivery charge")
				}
			} else if outcome == "retry" {
				if got.BalanceMicros != 1000 || got.HeldMicros != 1000 {
					t.Fatal("retry lost reservation")
				}
				s.DB.Model(&d).Update("status", "failed")
				if e := s.cleanupMeteredUsage(context.Background()); e != nil {
					t.Fatal(e)
				}
				if got = usageOwner(t, s, u.ID); got.BalanceMicros != 1000 || got.HeldMicros != 0 {
					t.Fatal("retry exhaustion did not release")
				}
			} else if got.BalanceMicros != 1000 || got.HeldMicros != 0 {
				t.Fatal("failed/cancelled delivery was charged")
			}
		})
	}
}

func TestMeteredUVRollingWindowConcurrencyAndIsolation(t *testing.T) {
	for _, kind := range []string{"apk_uv", "cloak_uv"} {
		t.Run(kind, func(t *testing.T) {
			s := meteredServer(t, 0)
			now := time.Now().UTC()
			s.Now = func() time.Time { return now }
			u := seedUser(t, s, Dollar)
			r := usageRequest(s, "/", "203.0.113.20")
			var wg sync.WaitGroup
			for range 24 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if e := s.chargeUsageUV(r, u.ID, kind, "one"); e != nil {
						t.Error(e)
					}
				}()
			}
			wg.Wait()
			if n := countRows(t, s, &MeteredEvent{}); n != 1 {
				t.Fatalf("same visitor charged %d times", n)
			}
			now = now.Add(24*time.Hour - time.Second)
			if e := s.chargeUsageUV(r, u.ID, kind, "one"); e != nil {
				t.Fatal(e)
			}
			if n := countRows(t, s, &MeteredEvent{}); n != 1 {
				t.Fatal("rolling boundary reset early")
			}
			now = now.Add(time.Second)
			if e := s.chargeUsageUV(r, u.ID, kind, "one"); e != nil {
				t.Fatal(e)
			}
			if e := s.chargeUsageUV(r, u.ID, kind, "two"); e != nil {
				t.Fatal(e)
			}
			other := seedUser(t, s, Dollar)
			if e := s.chargeUsageUV(r, other.ID, kind, "one"); e != nil {
				t.Fatal(e)
			}
			if n := countRows(t, s, &MeteredEvent{}); n != 4 {
				t.Fatalf("resource/owner/window isolation: %d", n)
			}
			before := countRows(t, s, &MeteredEvent{})
			head := r.Clone(r.Context())
			head.Method = "HEAD"
			if e := s.chargeUsageUV(head, u.ID, kind, "metadata"); e != nil {
				t.Fatal(e)
			}
			if countRows(t, s, &MeteredEvent{}) != before {
				t.Fatal("HEAD was charged")
			}
		})
	}
}

func TestMeteredPWAReserveCancelPriceSnapshotAndProof(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, 30000)
	app := createPWAFixture(t, s, u, "Install proof")
	r := usageRequest(s, "/", "203.0.113.30")
	base := "/p/" + app.Slug + "/install/"
	body := installBody(s, app, r, randomHex(16))
	requireCode(t, usagePost(s, base+"start", body, r), 200)
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != 30000 || got.HeldMicros != 30000 {
		t.Fatal("install must reserve, not charge, before confirmation")
	}
	requireCode(t, usagePost(s, base+"start", body, r), 200)
	requireCode(t, usagePost(s, base+"cancel", body, r), 200)
	requireCode(t, usagePost(s, base+"cancel", body, r), 200)
	requireCode(t, usagePost(s, base+"complete", body, r), 410)
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != 30000 || got.HeldMicros != 0 {
		t.Fatal("cancel did not release funds")
	}
	body = installBody(s, app, r, randomHex(16))
	requireCode(t, usagePost(s, base+"start", body, r), 200)
	s.DB.Model(&Settings{}).Where("id = 1").Update("pwa_install_price_micros", 50000)
	requireCode(t, usagePost(s, base+"complete", body, r), 200)
	requireCode(t, usagePost(s, base+"complete", body, r), 200)
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != 0 || got.HeldMicros != 0 {
		t.Fatal("install price was not locked or retry billed twice")
	}
	body = installBody(s, app, r, randomHex(16))
	requireCode(t, usagePost(s, base+"start", body, r), 402)
	otherIP := usageRequest(s, "/", "203.0.113.31")
	requireCode(t, usagePost(s, base+"start", body, otherIP), 403)
	body["token"] += "bad"
	requireCode(t, usagePost(s, base+"start", body, r), 403)
}

func TestMeteredPWAConcurrentReservationsNeverOverdraw(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, 30000)
	app := createPWAFixture(t, s, u, "Concurrent install")
	r := usageRequest(s, "/", "203.0.113.40")
	base := "/p/" + app.Slug + "/install/"
	var wg sync.WaitGroup
	var mu sync.Mutex
	var winner map[string]string
	success := 0
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := installBody(s, app, r, randomHex(16))
			w := usagePost(s, base+"start", body, r)
			if w.Code == 200 {
				mu.Lock()
				success++
				winner = body
				mu.Unlock()
			} else if w.Code != 402 {
				t.Errorf("start %d %s", w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("reserved %d installations with funds for one", success)
	}
	requireCode(t, usagePost(s, base+"complete", winner, r), 200)
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != 0 || got.HeldMicros != 0 {
		t.Fatal("concurrent install overdraw")
	}
}

func TestMeteredAPKVisitAndDownloadAreChargedOnlyOnce(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, Dollar)
	app := seedApp(t, s, u, 128)
	r := usageRequest(s, "/a/"+app.Slug+"/info", "203.0.113.50")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 200)
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "d1_visitor" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("visitor cookie absent")
	}
	r.AddCookie(cookie)
	token, e := s.createDownloadTicket(r, app, cookie.Value, randomHex(16), Attribution{})
	if e != nil {
		t.Fatal(e)
	}
	var ticket Ticket
	s.DB.First(&ticket, "hash = ?", hashString(token))
	if !ticket.Metered {
		t.Fatal("new ticket did not use visit billing")
	}
	if e = s.reserveDownload(ticket); e != nil {
		t.Fatal(e)
	}
	if e = s.finishRange(ticket, 0, 127); e != nil {
		t.Fatal(e)
	}
	if e = s.finishRange(ticket, 0, 127); e != nil {
		t.Fatal(e)
	}
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != Dollar-3000 || got.HeldMicros != 0 {
		t.Fatal("file transfer charged the already-billed UV again")
	}
	if countRows(t, s, &Download{}) != 1 || countRows(t, s, &MeteredEvent{}) != 1 {
		t.Fatal("download completion/visit event mismatch")
	}
}

func TestMeteredBalanceOnlyFunctionsAndCleanup(t *testing.T) {
	s := meteredServer(t, 1000)
	u := seedUser(t, s, 0)
	r := usageRequest(s, "/", "203.0.113.60")
	if e := s.chargeUsageUV(r, u.ID, "cloak_uv", "one"); e == nil {
		t.Fatal("cloak consumed registration gifts instead of requiring balance")
	}
	d := PWAPushDelivery{ID: randomHex(16), UserID: u.ID, PWAID: randomHex(16), ExpiresAt: s.Now().Add(time.Hour)}
	if e := s.reservePushUsage(context.Background(), d); e == nil {
		t.Fatal("push consumed gifts")
	}
	s.DB.Model(&User{}).Where("id = ?", u.ID).Update("balance_micros", 1000)
	if e := s.reservePushUsage(context.Background(), d); e != nil {
		t.Fatal(e)
	}
	s.DB.Model(&MeteredEvent{}).Where("source_id = ?", d.ID).Update("expires_at", s.Now().Add(-time.Second))
	if e := s.cleanupMeteredUsage(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := s.cleanupMeteredUsage(context.Background()); e != nil {
		t.Fatal(e)
	}
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != 1000 || got.HeldMicros != 0 {
		t.Fatal("expired reservation did not release exactly once")
	}
}

func TestMeteredFreeMembershipRiskAndSettings(t *testing.T) {
	s := meteredServer(t, 1)
	u := seedUser(t, s, Dollar)
	cookie, csrf := sessionFor(t, s, u)
	requireCode(t, call(t, s, "POST", "/api/subscription", nil, cookie, csrf), 410)
	eligible, e := s.riskEligible(s.DB, u)
	if e != nil || !eligible {
		t.Fatal("free member with credit cannot use risk detection")
	}
	noMoney := seedUser(t, s, 0)
	eligible, e = s.riskEligible(s.DB, noMoney)
	if e != nil || !eligible {
		t.Fatal("registration gift is not considered available credit")
	}
	s.DB.Model(&DownloadGrant{}).Where("user_id = ?", noMoney.ID).Update("used", 1)
	eligible, e = s.riskEligible(s.DB, noMoney)
	if e != nil || eligible {
		t.Fatal("empty account can still run risk checks")
	}
	admin := seedUser(t, s, Dollar)
	s.DB.Model(&admin).Update("role", "admin")
	admin.Role = "admin"
	ac, at := sessionFor(t, s, admin)
	x, _ := getSettings(s.DB)
	var plans []Plan
	s.DB.Find(&plans)
	x.PWAInstallPriceMicros = 45000
	x.CloakUVPriceMicros = 2000
	x.ReturnFlowPriceMicros = 3000
	x.PWAPushPriceMicros = 4000
	x.DownloadPriceMicros = 5000
	requireCode(t, call(t, s, "PUT", "/api/admin/settings", map[string]any{"section": "billing", "settings": x, "plans": plans}, ac, at), 200)
	after, _ := getSettings(s.DB)
	if after.PWAInstallPriceMicros != 45000 || after.CloakUVPriceMicros != 2000 || after.ReturnFlowPriceMicros != 3000 || after.PWAPushPriceMicros != 4000 || after.DownloadPriceMicros != 5000 {
		t.Fatal("five admin prices did not save")
	}
	x.PWAPushPriceMicros = -1
	requireCode(t, call(t, s, "PUT", "/api/admin/settings", map[string]any{"section": "billing", "settings": x, "plans": plans}, ac, at), 400)
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != Dollar {
		t.Fatal("retired subscription charged money")
	}
}

func TestMeteredMigrationPreservesMoneyGrantsOrdersAndAdminPrices(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 42*Dollar)
	s.DB.Create(&DownloadGrant{ID: "free:" + u.ID, UserID: u.ID, PlanID: "free", Limit: 1000, Used: 23, Reserved: 2, StartsAt: u.CreatedAt})
	s.DB.Model(&User{}).Where("id = ?", u.ID).Update("held_micros", 1234)
	s.DB.Model(&Settings{}).Where("id = 1").Update("download_price_micros", 7000)
	xBefore, _ := getSettings(s.DB)
	xBefore.DownloadDomainRules = []DownloadDomainRule{{URL: "https://public.example", Audience: "paid", LegacyOnly: true}}
	if e := s.DB.Model(&xBefore).Select("DownloadDomainRules").Updates(&xBefore).Error; e != nil {
		t.Fatal(e)
	}
	s.DB.Where("version = ?", meteredBillingVersion).Delete(&Migration{})
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	x, _ := getSettings(s.DB)
	if !x.MeteredBilling || x.DownloadPriceMicros != 7000 || x.PWAInstallPriceMicros != 30000 || x.RegistrationGiftLimit != 1000 {
		t.Fatal("migration defaults or existing APK price lost")
	}
	if len(x.DownloadDomainRules) != 1 || x.DownloadDomainRules[0].Audience != "all" || !x.DownloadDomainRules[0].LegacyOnly {
		t.Fatal("domain audience migration changed legacy-link policy")
	}
	got := usageOwner(t, s, u.ID)
	if got.BalanceMicros != 42*Dollar || got.HeldMicros != 1234 {
		t.Fatal("migration changed money")
	}
	var grant DownloadGrant
	s.DB.First(&grant, "id = ?", "free:"+u.ID)
	if grant.Used != 23 || grant.Reserved != 2 || grant.Limit != 1000 {
		t.Fatal("migration reset gifts")
	}
	s.DB.Model(&Settings{}).Where("id = 1").Update("pwa_install_price_micros", 45000)
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	x, _ = getSettings(s.DB)
	if x.PWAInstallPriceMicros != 45000 {
		t.Fatal("restart overwrote configured price")
	}
	if countRows(t, s, &Ledger{}) != 0 {
		t.Fatal(fmt.Sprint("migration retroactively charged ", countRows(t, s, &Ledger{})))
	}
}

func TestMeteredRiskDoesNotRestoreUnsafeTargetsWhenCreditRunsOut(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, 0)
	p := TrafficPool{Slug: randomHex(4), UserID: u.ID, Enabled: true, RiskEnabled: true, Mode: "sequential"}
	if e := s.DB.Create(&p).Error; e != nil {
		t.Fatal(e)
	}
	checked := s.Now()
	link := TrafficLink{ID: randomHex(16), UserID: u.ID, PoolSlug: p.Slug, URL: "https://unsafe.example/app.apk", Status: "unsafe", CheckedAt: &checked}
	if e := s.DB.Create(&link).Error; e != nil {
		t.Fatal(e)
	}
	if target, e := s.trafficTarget(usageRequest(s, "/r/"+p.Slug, "203.0.113.81"), p.Slug, u.ID); e == nil || target != "" {
		t.Fatal("no-credit owner regained unsafe traffic target")
	}
}
