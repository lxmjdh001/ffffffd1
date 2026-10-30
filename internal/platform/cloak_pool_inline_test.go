package platform

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestCloakPoolLandingStaysInPlaceAndButtonHonorsItsOwnPool(t *testing.T) {
	for _, status := range []string{"off", "rules"} {
		t.Run(status, func(t *testing.T) {
			f := routedAdFixtureFor(t, true)
			s := f.s
			s.Config.Env = "production"
			addChosenDomain(t, s, f.owner, "cloak.example")
			if err := s.DB.Model(&f.cfg).Update("mode", "quick").Error; err != nil {
				t.Fatal(err)
			}
			if err := s.DB.Model(&f.apps[0]).Update("download_mode", "info").Error; err != nil {
				t.Fatal(err)
			}
			entryURL := "https://download.example/a/" + f.apps[0].Slug + "?keep=target"
			entryPool := apkButtonPool(t, s, f.owner, entryURL)
			cloak := Cloak{ID: randomHex(16), UserID: f.owner.ID, Slug: randomHex(4), Name: "Inline pool", Status: status, Enabled: status == "rules", TargetType: "traffic_pool", TrafficPoolSlug: entryPool.Slug}
			if err := s.DB.Create(&cloak).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.DB.Model(&f.pool).Update("cursor", 1).Error; err != nil {
				t.Fatal(err)
			}
			entry := "https://cloak.example/c/" + cloak.Slug + "?keep=entry"
			page := routedAdRequest(s, "GET", entry, nil, nil, "")
			requireCode(t, page, 200)
			if page.Header().Get("Location") != "" || !strings.Contains(page.Body.String(), `action="/a/`+f.apps[0].Slug+`/button"`) {
				t.Fatal("cloak entry did not render the selected APK and its routing button")
			}
			fields := cloakNativeFields(t, page)
			root, err := s.readAttribution(fields.Get("attribution_token"), f.apps[0].ID)
			if err != nil || !strings.HasPrefix(root.PageURL, "https://cloak.example/c/"+cloak.Slug) || !strings.Contains(root.PageURL, "keep=entry") || root.Consent {
				t.Fatal("inline rendering changed the document origin, query or consent", err)
			}
			if countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Conversion{}) != 0 || countRows(t, s, &MeteredEvent{}) != 0 {
				t.Fatal("rendering a cloak created a download or a charge")
			}
			path := "/a/" + f.apps[0].Slug + "/button"
			button := routedAdRequest(s, "POST", "https://cloak.example"+path, cloakBrowserCookies(page.Result().Cookies(), path), fields, "")
			requireCode(t, button, 200)
			var answer map[string]string
			if err := json.Unmarshal(button.Body.Bytes(), &answer); err != nil {
				t.Fatal(err)
			}
			target, err := url.Parse(answer["redirect_url"])
			if err != nil || target.Host != "download.example" || target.Path != "/a/"+f.apps[1].Slug+"/direct" || target.Query().Get(routedAttributionParam) == "" {
				t.Fatal("detail button stopped respecting its configured pool domain", target, err)
			}
			file, jar := f.targetTicket(t, target.String(), f.apps[1])
			requireCode(t, routedAdRequest(s, "GET", file, jar, nil, "bytes=0-0"), 206)
			requireCode(t, routedAdRequest(s, "GET", file, jar, nil, "bytes=1-1"), 206)
			var conversion Conversion
			if err := s.DB.Preload("Deliveries").First(&conversion).Error; err != nil {
				t.Fatal(err)
			}
			if conversion.AppID != f.apps[0].ID || conversion.DownloadedAppID != f.apps[1].ID || conversion.AttributionID != root.ID || conversion.SourceURL != root.PageURL || len(conversion.Deliveries) != 2 || countRows(t, s, &Conversion{}) != 1 || countRows(t, s, &MeteredEvent{}) != 1 {
				t.Fatal("inline cloak lost the campaign or duplicated reporting/billing")
			}
			if got := usageOwner(t, s, f.owner.ID); got.BalanceMicros != f.owner.BalanceMicros-50000 {
				t.Fatal("download did not retain the original usage charge")
			}
			standalone := routedAdRequest(s, "GET", "https://cloak.example/r/"+entryPool.Slug, nil, nil, "")
			requireCode(t, standalone, 302)
			if standalone.Header().Get("Location") != entryURL {
				t.Fatal("standalone routing changed its configured target")
			}
			if err := s.DB.Model(&cloak).Update("status", "blocked").Error; err != nil {
				t.Fatal(err)
			}
			blocked := routedAdRequest(s, "GET", entry, nil, nil, "")
			requireCode(t, blocked, 404)
			if blocked.Header().Get("Location") != "" || !strings.Contains(blocked.Body.String(), "Account not found") {
				t.Fatal("inline pool rendering bypassed the cloak's blocked status")
			}
		})
	}
}

func TestCloakPoolPreservesAPKLinkModes(t *testing.T) {
	for _, mode := range []string{"direct", "info"} {
		for _, ending := range []string{"", "/direct", "/info"} {
			t.Run(mode+ending, func(t *testing.T) {
				s := testServer(t)
				u := seedUser(t, s, Dollar)
				a := seedApp(t, s, u, 100)
				// "info" is also a valid slug; it must not turn a bare direct
				// download URL into a detail-page link.
				if err := s.DB.Model(&a).Updates(map[string]any{"slug": "info", "download_mode": mode}).Error; err != nil {
					t.Fatal(err)
				}
				addChosenDomain(t, s, u, "entry.example")
				pool := apkButtonPool(t, s, u, s.Config.PublicURL+"/a/info"+ending)
				c := Cloak{ID: randomHex(16), Slug: randomHex(4), UserID: u.ID, Status: "off", TargetType: "traffic_pool", TrafficPoolSlug: pool.Slug}
				if err := s.DB.Create(&c).Error; err != nil {
					t.Fatal(err)
				}
				w := chosenCall(s, "GET", "entry.example", "/c/"+c.Slug)
				if ending == "/info" || ending == "" && mode == "info" {
					requireCode(t, w, 200)
					if w.Header().Get("Location") != "" || !strings.Contains(w.Body.String(), `/a/info/download`) {
						t.Fatal("detail page left the entry or used the wrong app")
					}
				} else {
					requireCode(t, w, 307)
					if !strings.HasPrefix(w.Header().Get("Location"), "/d/") {
						t.Fatal("direct file ticket left the entry domain")
					}
				}
			})
		}
	}
}

func TestCloakPoolDoesNotInlineForeignOrExternalTargets(t *testing.T) {
	s := testServer(t)
	u, other := seedUser(t, s, Dollar), seedUser(t, s, Dollar)
	app, foreign := seedApp(t, s, u, 100), seedApp(t, s, other, 100)
	pwa, foreignPWA := createPWAFixture(t, s, u, "Own PWA"), createPWAFixture(t, s, other, "Foreign PWA")
	addChosenDomain(t, s, u, "entry.example")
	for _, target := range []string{
		s.Config.PublicURL + "/a/" + foreign.Slug + "/info",
		"https://external.example/a/" + app.Slug + "/info",
		s.Config.PublicURL + "/p/" + foreignPWA.Slug + "/",
		"https://external.example/p/" + pwa.Slug + "/",
		"https://external.example/download.apk",
	} {
		pool := apkButtonPool(t, s, u, target)
		c := Cloak{ID: randomHex(16), Slug: randomHex(4), UserID: u.ID, Status: "off", TargetType: "traffic_pool", TrafficPoolSlug: pool.Slug}
		if err := s.DB.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
		w := chosenCall(s, "GET", "entry.example", "/c/"+c.Slug)
		requireCode(t, w, 302)
		if w.Header().Get("Location") != target {
			t.Fatal("unowned or external target was proxied through this customer's domain")
		}
	}
}
