package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestQuickOrganicAPKDownloadsKeepSourceAndDispatch(t *testing.T) {
	for _, routed := range []bool{false, true} {
		name := "direct"
		if routed {
			name = "details_with_pool"
		}
		t.Run(name, func(t *testing.T) {
			f := routedAdFixtureFor(t, true)
			s := f.s
			if err := s.DB.Model(&f.cfg).Update("mode", "quick").Error; err != nil {
				t.Fatal(err)
			}
			f.root = Attribution{}
			f.landing(t, "")
			target := f.apps[0]
			raw := "http://localhost:8088/a/" + target.Slug + "/direct"
			if routed {
				s.DB.Model(&f.pool).Update("cursor", 1)
				target = f.apps[1]
				raw = f.click(t, false)
				if !strings.Contains(raw, routedAttributionParam+"=") {
					t.Fatal("ordinary routed download lost its source")
				}
			}
			file, cookies := f.targetTicket(t, raw, target)
			requireCode(t, routedAdRequest(s, "HEAD", file, cookies, nil, ""), 200)
			requireCode(t, routedAdRequest(s, "GET", file, cookies, nil, "bytes=1000-2000"), 416)
			if countRows(t, s, &Conversion{}) != 0 || countRows(t, s, &Delivery{}) != 0 {
				t.Fatal("page visit, ticket, or failed transfer triggered reporting")
			}
			requireCode(t, routedAdRequest(s, "GET", file, cookies, nil, "bytes=0-0"), 206)
			var event Conversion
			if err := s.DB.Preload("Deliveries").First(&event).Error; err != nil {
				t.Fatal(err)
			}
			if event.AppID != f.apps[0].ID || event.DownloadedAppID != target.ID || event.Event != "download_start" || event.VerifiedBy != "quick_download_server" || event.Consent || len(event.Deliveries) != 2 {
				t.Fatalf("ordinary download was not reported correctly: %+v", event)
			}
			if routed && event.AttributionID != f.root.ID {
				t.Fatal("routed reporting used the destination's identity")
			}
			var attr Attribution
			s.DB.First(&attr, "id = ?", event.AttributionID)
			if attr.FBC != "" || attr.FBClid != "" || attr.TTClid != "" {
				t.Fatal("download reporting fabricated a click identifier")
			}
			calls := 0
			s.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				data := payload["data"].([]any)[0].(map[string]any)
				if r.URL.Host == "graph.facebook.com" {
					user := data["user_data"].(map[string]any)
					if _, exists := user["fbc"]; exists || user["client_ip_address"] != event.IP || user["client_user_agent"] != event.UserAgent {
						t.Fatal("incorrect Meta identity payload")
					}
					return jsonResponse(200, `{"events_received":1,"fbtrace_id":"organic-fixture"}`), nil
				}
				user := data["user"].(map[string]any)
				if _, exists := user["ttclid"]; exists || user["ip"] != event.IP || user["user_agent"] != event.UserAgent {
					t.Fatal("incorrect TikTok identity payload")
				}
				return jsonResponse(200, `{"code":0,"request_id":"organic-fixture"}`), nil
			})}
			for range 2 {
				if !s.deliverOne(context.Background()) {
					t.Fatal("ordinary download delivery was not dispatched")
				}
			}
			if calls != 2 {
				t.Fatal("configured channels did not receive the event")
			}
			requireCode(t, routedAdRequest(s, "GET", file, cookies, nil, ""), 200)
			if countRows(t, s, &Conversion{}) != 1 || countRows(t, s, &Delivery{}) != 2 {
				t.Fatal("retry or completion duplicated download reporting")
			}
		})
	}
}

func TestQuickOrganicPWAClickCreatesDelivery(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	p := createPWAFixture(t, s, u, "Organic PWA")
	cfg := quickAdConfig(pwaAdApplication(p))
	if err := s.DB.Create(&cfg).Error; err != nil {
		t.Fatal(err)
	}
	page := adPWARequest(s, "GET", "/p/"+p.Slug+"/", nil, nil)
	requireCode(t, page, 200)
	fields := adPWAFields(t, page.Body.String())
	if countRows(t, s, &Conversion{}) != 0 {
		t.Fatal("page visit triggered a PWA event")
	}
	for range 2 {
		requireCode(t, adPWARequest(s, "POST", "/p/"+p.Slug+"/ad-event", map[string]any{"event": "install_click", "attribution_token": fields["token"], "proof": fields["proof"]}, page.Result().Cookies()), 202)
	}
	if countRows(t, s, &Conversion{}) != 1 || countRows(t, s, &Delivery{}) != 1 {
		t.Fatal("ordinary PWA click was lost or duplicated")
	}
}

func TestQuickOrganicReportingPreservesDeepAndDisabledChannels(t *testing.T) {
	for _, name := range []string{"deep", "disabled", "one_channel"} {
		t.Run(name, func(t *testing.T) {
			s := testServer(t)
			u := seedUser(t, s, Dollar)
			a := seedApp(t, s, u, 10)
			cfg, attr, _ := seedAds(t, s, a)
			cfg.Mode = "quick"
			if name == "deep" {
				cfg.Mode = "deep"
			}
			cfg.Disabled = name == "disabled"
			if name == "one_channel" {
				cfg.FacebookEnabled = false
			}
			s.DB.Save(&cfg)
			attr.FBC, attr.FBClid, attr.TTClid = "", "", ""
			s.DB.Save(&attr)
			event := Conversion{ID: randomHex(16), AppID: a.ID, UserID: u.ID, AttributionID: attr.ID, Event: "download_start", EventID: randomHex(32), VerifiedBy: "quick_download_server", EventTime: s.Now(), CreatedAt: s.Now()}
			if name == "deep" {
				event.Consent, event.VerifiedBy = true, "download_server"
			}
			if err := s.DB.Transaction(func(tx *gorm.DB) error { return s.createConversion(tx, &event, attr) }); err != nil {
				t.Fatal(err)
			}
			want := 0
			if name == "one_channel" {
				want = 1
			}
			if len(event.Deliveries) != want || want == 1 && event.Deliveries[0].Platform != "tiktok" {
				t.Fatal("deep mode, disabled reporting or channel selection changed")
			}
		})
	}
}
