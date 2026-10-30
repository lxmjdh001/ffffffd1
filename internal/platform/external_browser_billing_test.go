package platform

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestExternalClickChargesAtClickOnceForAPKAndPWA(t *testing.T) {
	for _, kind := range []string{"apk", "pwa"} {
		t.Run(kind, func(t *testing.T) {
			s := meteredServer(t, 0)
			owner := seedUser(t, s, Dollar)
			if e := s.DB.Model(&owner).Update("gift_balance_micros", 75000).Error; e != nil {
				t.Fatal(e)
			}
			var id, base string
			config := &ExternalBrowserConfig{Enabled: true, URL: "https://shop.example/?utm=1#/checkout"}
			if kind == "apk" {
				app := seedApp(t, s, owner, 128)
				id, base = app.ID, "/a/"+app.Slug+"/"
				d := defaultPageDesign(app)
				d.ExternalBrowser = config
				if e := s.DB.Create(&AppPageDesign{AppID: id, Design: d}).Error; e != nil {
					t.Fatal(e)
				}
			} else {
				app := createPWAFixture(t, s, owner, "Click billing")
				id, base = app.ID, "/p/"+app.Slug+"/"
				c := landingFixture()
				c.Settings.ExternalBrowser = config
				raw, _ := json.Marshal(c)
				if e := s.DB.Model(&app).Update("landing_json", string(raw)).Error; e != nil {
					t.Fatal(e)
				}
			}
			page := base
			if kind == "apk" {
				page += "info"
			}
			requireCode(t, call(t, s, "GET", page, nil, nil, ""), 200)
			if countRows(t, s, &MeteredEvent{}) != 0 {
				t.Fatal("a page view charged before a click")
			}
			request := usageRequest(s, base, "203.0.113.10")
			body := map[string]string{"key": randomHex(16), "token": s.externalClickToken(kind, id, config.URL)}
			// This is charged even when no external browser ever requests the relay.
			requireCode(t, usagePost(s, base+"external/click", body, request), 200)
			u := usageOwner(t, s, owner.ID)
			if u.GiftBalanceMicros != 25000 || u.BalanceMicros != Dollar || u.GiftHeldMicros != 0 || u.HeldMicros != 0 {
				t.Fatalf("wrong click charge: %+v", u)
			}
			relay := base + "external/open?key=" + body["key"] + "&token=" + url.QueryEscape(body["token"])
			for range 3 {
				requireCode(t, call(t, s, "GET", relay, nil, nil, ""), 302)
				requireCode(t, usagePost(s, base+"external/click", body, request), 200)
			}
			if countRows(t, s, &MeteredEvent{}) != 1 || countRows(t, s, &Ledger{}) != 1 {
				t.Fatal("report, retry and relay charged more than once")
			}
			// A new deliberate click is a new charge, not a 24-hour UV.
			body["key"] = randomHex(16)
			requireCode(t, usagePost(s, base+"external/click", body, request), 200)
			u = usageOwner(t, s, owner.ID)
			if u.GiftBalanceMicros != 0 || u.BalanceMicros != Dollar-25000 || u.HeldMicros != 0 || u.GiftHeldMicros != 0 {
				t.Fatalf("gift/cash split incorrect: %+v", u)
			}
			if countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Download{}) != 0 {
				t.Fatal("a click fabricated a download")
			}
			requireCode(t, call(t, s, "GET", base+"external/open", nil, nil, ""), 403)
			bad := map[string]string{"key": randomHex(16), "token": "forged"}
			requireCode(t, usagePost(s, base+"external/click", bad, request), 403)
			badOrigin := httptest.NewRequest("POST", "http://localhost:8088"+base+"external/click", strings.NewReader(`{}`))
			badOrigin.Header.Set("Origin", "https://attacker.example")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, badOrigin)
			requireCode(t, w, 403)
			if e := s.DB.Model(&owner).Updates(map[string]any{"balance_micros": 0, "gift_balance_micros": 0}).Error; e != nil {
				t.Fatal(e)
			}
			body["key"] = randomHex(16)
			requireCode(t, usagePost(s, base+"external/click", body, request), 402)
			requireCode(t, call(t, s, "GET", base+"external/open?key="+body["key"]+"&token="+url.QueryEscape(body["token"]), nil, nil, ""), 402)
			if countRows(t, s, &MeteredEvent{}) != 2 {
				t.Fatal("failed click created a charged event")
			}
		})
	}
}

func TestExternalClickPWAReturnDoesNotReserveAnotherInstall(t *testing.T) {
	s := meteredServer(t, 0)
	owner := seedUser(t, s, Dollar)
	app := createPWAFixture(t, s, owner, "Paid return")
	base := "/p/" + app.Slug + "/"
	target := "http://localhost:8088" + base + "?source=1#/checkout"
	c := landingFixture()
	c.Settings.ExternalBrowser = &ExternalBrowserConfig{Enabled: true, URL: target}
	raw, _ := json.Marshal(c)
	if e := s.DB.Model(&app).Update("landing_json", string(raw)).Error; e != nil {
		t.Fatal(e)
	}
	// Relay-first covers a click report cancelled by the old browser unloading.
	relay := call(t, s, "GET", externalClickURL(s, "pwa", app.ID, base, target), nil, nil, "")
	requireCode(t, relay, 302)
	returned, _ := url.Parse(relay.Header().Get("Location"))
	request := usageRequest(s, base, "203.0.113.20")
	body := installBody(s, app, request, randomHex(16))
	body["external_return"] = returned.Query().Get(externalReturnParam)
	requireCode(t, usagePost(s, base+"install/start", body, request), 200)
	requireCode(t, usagePost(s, base+"install/complete", body, request), 200)
	if countRows(t, s, &MeteredEvent{}) != 1 || countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("native installation charged the paid handoff again")
	}
	if got := usageOwner(t, s, owner.ID); got.BalanceMicros != Dollar-50000 || got.HeldMicros != 0 {
		t.Fatalf("unexpected balance/hold: %+v", got)
	}
}

func TestExternalClickConcurrentRetriesAndReturnProof(t *testing.T) {
	s := meteredServer(t, 0)
	owner := seedUser(t, s, Dollar)
	app := seedApp(t, s, owner, 128)
	target := "http://localhost:8088/a/" + app.Slug + "/info#/checkout"
	d := defaultPageDesign(app)
	d.ExternalBrowser = &ExternalBrowserConfig{Enabled: true, URL: target}
	if e := s.DB.Create(&AppPageDesign{AppID: app.ID, Design: d}).Error; e != nil {
		t.Fatal(e)
	}
	key, token := randomHex(16), s.externalClickToken("apk", app.ID, target)
	var wait sync.WaitGroup
	errors := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, e := s.chargeExternalClick("apk", app.ID, owner.ID, target, key, token)
			errors <- e
		}()
	}
	wait.Wait()
	close(errors)
	for e := range errors {
		if e != nil {
			t.Fatal(e)
		}
	}
	if countRows(t, s, &MeteredEvent{}) != 1 {
		t.Fatal("concurrent retries duplicated the click")
	}
	base := "/a/" + app.Slug + "/"
	relay := call(t, s, "GET", base+"external/open?key="+key+"&token="+url.QueryEscape(token), nil, nil, "")
	requireCode(t, relay, 302)
	returned, _ := url.Parse(relay.Header().Get("Location"))
	returned.Fragment = ""
	r := httptest.NewRequest("GET", returned.String(), nil)
	if !s.paidExternalReturn("apk", app.ID, target, r.URL.Query().Get(externalReturnParam)) {
		t.Fatal("paid return proof missing")
	}
	raw, err := s.createDownloadTicket(r, app, randomHex(32), randomHex(16), Attribution{})
	if err != nil {
		t.Fatal(err)
	}
	var ticket Ticket
	if err := s.DB.First(&ticket, "hash = ?", hashString(raw)).Error; err != nil {
		t.Fatal(err)
	}
	if !ticket.BillingPrepaid {
		t.Fatal("prepaid return lost its billing proof")
	}
	if err := s.reserveDownload(ticket); err != nil {
		t.Fatal(err)
	}
	if err := s.confirmAPKDownloadStart(ticket, s.Now()); err != nil {
		t.Fatal(err)
	}
	if countRows(t, s, &MeteredEvent{}) != 1 {
		t.Fatal("returning to the landing page charged a second time")
	}
}
