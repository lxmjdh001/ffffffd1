package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestAPKDownloadStartBillingAllEntryChains(t *testing.T) {
	for _, mode := range []string{"direct", "detail", "pool", "cloak-direct", "cloak-detail", "cloak-pool", "source-pool", "source-pool-cloak"} {
		t.Run(mode, func(t *testing.T) {
			s := meteredServer(t, 0)
			u := seedUser(t, s, Dollar)
			if err := s.DB.Model(&u).Update("gift_balance_micros", 75000).Error; err != nil {
				t.Fatal(err)
			}
			source, target := seedApp(t, s, u, 100), seedApp(t, s, u, 100)
			base := s.Config.PublicURL
			c := Cloak{ID: randomHex(16), UserID: u.ID, Slug: randomHex(4), Status: "rules", Enabled: true, TargetType: "app", TargetID: target.ID, AppTargetMode: "direct"}
			if mode == "cloak-detail" || mode == "source-pool-cloak" {
				c.AppTargetMode = "info"
			}
			if err := s.DB.Create(&c).Error; err != nil {
				t.Fatal(err)
			}
			destination := base + "/a/" + target.Slug + "/direct"
			if mode == "source-pool-cloak" {
				destination = base + "/c/" + c.Slug
			}
			pool := apkButtonPool(t, s, u, destination)
			if mode == "cloak-pool" {
				if err := s.DB.Model(&c).Updates(map[string]any{"target_type": "traffic_pool", "traffic_pool_slug": pool.Slug}).Error; err != nil {
					t.Fatal(err)
				}
			}
			d := defaultPageDesign(source)
			d.ButtonAction = "traffic"
			d.TrafficPoolID = pool.StatisticsID
			if err := s.DB.Create(&AppPageDesign{AppID: source.ID, Design: d}).Error; err != nil {
				t.Fatal(err)
			}
			var jar []*http.Cookie
			get := func(raw string) *httptest.ResponseRecorder {
				w := routedAdRequestOnly(s, "GET", raw, jar, nil, "")
				jar = mergeCloakCookies(jar, w.Result().Cookies())
				return w
			}
			raw := destination
			if mode == "pool" {
				raw = base + "/r/" + pool.Slug
			}
			if strings.HasPrefix(mode, "cloak-") {
				raw = base + "/c/" + c.Slug
			}
			if mode == "detail" {
				raw = base + "/a/" + target.Slug + "/info"
			}
			if strings.HasPrefix(mode, "source-") {
				page := get(base + "/a/" + source.Slug + "/info")
				requireCode(t, page, 200)
				w := routedAdRequestOnly(s, "POST", base+"/a/"+source.Slug+"/button", jar, cloakNativeFields(t, page), "")
				requireCode(t, w, 200)
				var answer map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil {
					t.Fatal(err)
				}
				raw = answer["redirect_url"]
			}
			w := get(raw)
			if w.Code == 302 {
				raw = base + "/a/" + target.Slug + "/direct"
				w = get(raw)
			}
			if w.Code == 200 {
				w = routedAdRequestOnly(s, "POST", base+"/a/"+target.Slug+"/download", jar, cloakNativeFields(t, w), "")
				requireCode(t, w, 200)
				var out map[string]string
				json.Unmarshal(w.Body.Bytes(), &out)
				raw = base + out["download_url"]
			} else {
				requireCode(t, w, 307)
				raw = base + w.Header().Get("Location")
			}
			if got := usageOwner(t, s, u.ID); got.BalanceMicros != Dollar || got.GiftBalanceMicros != 75000 || got.HeldMicros != 0 || got.GiftHeldMicros != 0 || countRows(t, s, &MeteredEvent{}) != 0 {
				t.Fatal("page, routing, cloak or ticket charged funds")
			}
			requireCode(t, routedAdRequestOnly(s, "HEAD", raw, jar, nil, ""), 200)
			requireCode(t, routedAdRequestOnly(s, "GET", raw, jar, nil, "bytes=1000-2000"), 416)
			if countRows(t, s, &MeteredEvent{}) != 0 {
				t.Fatal("metadata or invalid range billed")
			}
			requireCode(t, routedAdRequestOnly(s, "GET", raw, jar, nil, "bytes=0-0"), 206)
			got := usageOwner(t, s, u.ID)
			if got.GiftBalanceMicros != 25000 || got.BalanceMicros != Dollar || got.GiftHeldMicros != 0 || got.HeldMicros != 0 || countRows(t, s, &Download{}) != 0 {
				t.Fatalf("first-byte charge %+v", got)
			}
			var event MeteredEvent
			if err := s.DB.First(&event).Error; err != nil {
				t.Fatal(err)
			}
			if event.ResourceID != target.ID || event.Kind != "apk_uv" || event.State != "charged" || event.PriceMicros != 50000 {
				t.Fatalf("wrong charged target %+v", event)
			}
			for _, span := range []string{"bytes=0-0", "bytes=1-99", ""} {
				routedAdRequestOnly(s, "GET", raw, jar, nil, span)
			}
			if got := usageOwner(t, s, u.ID); got.GiftBalanceMicros != 25000 || got.BalanceMicros != Dollar || countRows(t, s, &MeteredEvent{}) != 1 || countRows(t, s, &Ledger{}) != 1 {
				t.Fatal("range or completion duplicated billing")
			}
		})
	}
}

func meteredAPKTicket(t *testing.T, s *Server, u User, app Application, key string) (Ticket, string, string) {
	t.Helper()
	visitor := randomHex(32)
	r := usageRequest(s, "/a/"+app.Slug+"/direct", "203.0.113.50")
	r.AddCookie(&http.Cookie{Name: "d1_visitor", Value: visitor})
	raw, err := s.createDownloadTicket(r, app, visitor, key, Attribution{})
	if err != nil {
		t.Fatal(err)
	}
	var ticket Ticket
	if err := s.DB.First(&ticket, "hash = ?", hashString(raw)).Error; err != nil {
		t.Fatal(err)
	}
	return ticket, raw, visitor
}

func TestAPKDownloadStartBillingConcurrencyGiftAndRollingWindow(t *testing.T) {
	s := meteredServer(t, 0)
	now := s.Now()
	s.Now = func() time.Time { return now }
	u := seedUser(t, s, 30000)
	s.DB.Model(&u).Update("gift_balance_micros", 20000)
	app := seedApp(t, s, u, 100)
	var tickets []Ticket
	for range 8 {
		ticket, _, _ := meteredAPKTicket(t, s, u, app, randomHex(16))
		tickets = append(tickets, ticket)
	}
	var wg sync.WaitGroup
	for _, ticket := range tickets {
		wg.Add(1)
		go func(ticket Ticket) {
			defer wg.Done()
			if err := s.reserveDownload(ticket); err != nil {
				t.Error(err)
			}
		}(ticket)
	}
	wg.Wait()
	got := usageOwner(t, s, u.ID)
	if got.GiftHeldMicros != 20000 || got.HeldMicros != 30000 || got.BalanceMicros != 30000 || got.GiftBalanceMicros != 20000 || countRows(t, s, &MeteredEvent{}) != 1 {
		t.Fatalf("shared hold %+v", got)
	}
	for _, ticket := range tickets {
		wg.Add(1)
		go func(ticket Ticket) {
			defer wg.Done()
			if err := s.confirmAPKDownloadStart(ticket, now); err != nil {
				t.Error(err)
			}
		}(ticket)
	}
	wg.Wait()
	got = usageOwner(t, s, u.ID)
	if got.BalanceMicros != 0 || got.GiftBalanceMicros != 0 || got.HeldMicros != 0 || got.GiftHeldMicros != 0 || countRows(t, s, &Ledger{}) != 2 {
		t.Fatalf("concurrent debit %+v", got)
	}
	if err := s.reserveDownload(tickets[0]); err != nil {
		t.Fatal(err)
	}
	now = now.Add(24*time.Hour - time.Second)
	ticket, _, _ := meteredAPKTicket(t, s, u, app, randomHex(16))
	if err := s.reserveDownload(ticket); err != nil {
		t.Fatal(err)
	}
	if err := s.confirmAPKDownloadStart(ticket, now); err != nil {
		t.Fatal(err)
	}
	if countRows(t, s, &MeteredEvent{}) != 1 {
		t.Fatal("UV reset before 24 hours")
	}
	now = now.Add(time.Second)
	ticket, _, _ = meteredAPKTicket(t, s, u, app, randomHex(16))
	if err := s.reserveDownload(ticket); err == nil {
		t.Fatal("new UV streamed without balance")
	}
	s.DB.Model(&u).Update("balance_micros", 50000)
	if err := s.reserveDownload(ticket); err != nil {
		t.Fatal(err)
	}
	if err := s.confirmAPKDownloadStart(ticket, now); err != nil {
		t.Fatal(err)
	}
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != 0 || countRows(t, s, &MeteredEvent{}) != 2 {
		t.Fatal("24h UV did not charge once")
	}
}

func TestAPKDownloadStartBillingZeroWriteExpiryAndUnavailableFile(t *testing.T) {
	s := meteredServer(t, 0)
	now := s.Now()
	s.Now = func() time.Time { return now }
	u := seedUser(t, s, 50000)
	app := seedApp(t, s, u, 100)
	ticket, raw, visitor := meteredAPKTicket(t, s, u, app, randomHex(16))
	r := usageRequest(s, "/d/"+raw, "203.0.113.50")
	r.AddCookie(&http.Cookie{Name: "d1_visitor", Value: visitor})
	w := &failWriter{header: make(http.Header)}
	s.Handler().ServeHTTP(w, r)
	got := usageOwner(t, s, u.ID)
	if got.BalanceMicros != 50000 || got.HeldMicros != 50000 || countRows(t, s, &MeteredUV{}) != 0 {
		t.Fatal("zero-byte failure charged funds")
	}
	now = now.Add(41 * time.Minute)
	if err := s.cleanupMeteredUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != 50000 || got.HeldMicros != 0 {
		t.Fatal("unused hold not released")
	}
	// A retry must replace its expired reservation and can settle normally.
	if err := s.reserveDownload(ticket); err != nil {
		t.Fatal(err)
	}
	if err := s.confirmAPKDownloadStart(ticket, now); err != nil {
		t.Fatal(err)
	}
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != 0 || got.HeldMicros != 0 {
		t.Fatal("retry did not settle")
	}
	app2 := seedApp(t, s, u, 100)
	_, raw, visitor = meteredAPKTicket(t, s, u, app2, randomHex(16))
	os.Remove(s.Config.DataDir + "/files/" + app2.FileKey)
	requireCode(t, download(t, s, raw, visitor, "GET", ""), 503)
	if countRows(t, s, &MeteredEvent{}) != 2 {
		t.Fatal("missing file reserved or charged")
	}
}

func TestAPKDownloadStartBillingDurableRecoveryAndMigration(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, Dollar)
	app := seedApp(t, s, u, 100)
	ticket, _, _ := meteredAPKTicket(t, s, u, app, randomHex(16))
	if err := s.reserveDownload(ticket); err != nil {
		t.Fatal(err)
	}
	path, err := s.writeReceipt(ticket, -1, -1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Callback().Create().Before("gorm:create").Register("test:bill-fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "ledgers" {
			tx.AddError(errors.New("temporary billing error"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer s.DB.Callback().Create().Remove("test:bill-fail")
	s.replayCompletions(context.Background())
	if _, err := os.Stat(path); err != nil {
		t.Fatal("failed billing lost receipt")
	}
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != Dollar || got.HeldMicros != 50000 {
		t.Fatal("failed transaction partially debited")
	}
	s.DB.Callback().Create().Remove("test:bill-fail")
	restarted := NewServer(s.DB, s.Config)
	restarted.replayCompletions(context.Background())
	restarted.replayCompletions(context.Background())
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != Dollar-50000 || got.HeldMicros != 0 || countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("start receipt not idempotent")
	}
	if countRows(t, s, &Download{}) != 0 {
		t.Fatal("start receipt fabricated a completed file")
	}
	before := usageOwner(t, s, u.ID)
	var settings Settings
	s.DB.First(&settings)
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	after := usageOwner(t, s, u.ID)
	var settingsAfter Settings
	s.DB.First(&settingsAfter)
	if before.BalanceMicros != after.BalanceMicros || before.HeldMicros != after.HeldMicros || settings.DownloadPriceMicros != settingsAfter.DownloadPriceMicros || countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("migration changed money or price")
	}
	var migration Migration
	if err := s.DB.First(&migration, "version = ?", 31).Error; err != nil {
		t.Fatal(err)
	}
}

func TestAPKDownloadStartBillingRetainsExistingPaidUV(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, 50000)
	app := seedApp(t, s, u, 100)
	r := usageRequest(s, "/a/"+app.Slug+"/info", "203.0.113.50")
	if err := s.chargeUsageUV(r, u.ID, "apk_uv", app.ID); err != nil {
		t.Fatal(err)
	}
	before := usageOwner(t, s, u.ID)
	ticket, _, _ := meteredAPKTicket(t, s, u, app, randomHex(16))
	if err := s.reserveDownload(ticket); err != nil {
		t.Fatal(err)
	}
	if err := s.confirmAPKDownloadStart(ticket, s.Now()); err != nil {
		t.Fatal(err)
	}
	if after := usageOwner(t, s, u.ID); after.BalanceMicros != before.BalanceMicros || after.HeldMicros != 0 || countRows(t, s, &MeteredEvent{}) != 1 {
		t.Fatal("cutover charged an already-paid UV again")
	}
}

func TestAPKDownloadStartBillingRecoversAfterHoldExpiryAtQuotedPrice(t *testing.T) {
	s := meteredServer(t, 0)
	now := s.Now()
	s.Now = func() time.Time { return now }
	u := seedUser(t, s, Dollar)
	app := seedApp(t, s, u, 100)
	ticket, _, _ := meteredAPKTicket(t, s, u, app, randomHex(16))
	if err := s.reserveDownload(ticket); err != nil {
		t.Fatal(err)
	}
	if _, err := s.writeReceipt(ticket, -1, -1); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if err := s.cleanupMeteredUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.DB.Model(&Settings{}).Where("id = 1").Update("download_price_micros", 90000)
	s.replayCompletions(context.Background())
	s.replayCompletions(context.Background())
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != Dollar-50000 || got.HeldMicros != 0 || countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("delayed recovery lost charge, changed quoted price or duplicated debit")
	}
}
