package platform

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestAPKCustomerDomainRoutingUsesPoolOriginAttributionAndBilling(t *testing.T) {
	for _, ending := range []string{"", "/direct", "/info"} {
		t.Run(ending, func(t *testing.T) {
			f := routedAdFixtureFor(t, true)
			s := f.s
			s.Config.Env = "production"
			addChosenDomain(t, s, f.owner, "customer.example")
			// Any native APK URL becomes a direct download, preserving parameters.
			raw := "https://download.example/a/" + f.apps[1].Slug + ending + "?keep=existing#section"
			if err := s.DB.Model(&TrafficLink{}).Where("pool_slug = ?", f.pool.Slug).Update("url", raw).Error; err != nil {
				t.Fatal(err)
			}
			if ending == "" {
				s.DB.Model(&f.apps[1]).Update("download_mode", "info")
			}
			page := routedAdRequest(s, "GET", "https://customer.example/a/"+f.apps[0].Slug+"/info?fbclid=customer-campaign&ttclid=customer-campaign", nil, nil, "")
			requireCode(t, page, 200)
			jar := page.Result().Cookies()
			form := cloakNativeFields(t, page)
			form.Set("ad_consent", "yes")
			w := routedAdRequest(s, "POST", "https://customer.example/a/"+f.apps[0].Slug+"/button", jar, form, "")
			requireCode(t, w, 200)
			var out map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(out["redirect_url"])
			if err != nil {
				t.Fatal(err)
			}
			if u.Scheme != "https" || u.Host != "download.example" || u.Path != "/a/"+f.apps[1].Slug+"/direct" || u.Query().Get("keep") != "existing" || u.Fragment != "section" || u.Query().Get(routedAttributionParam) == "" {
				t.Fatal("routing changed the configured pool hostname, path, or query", u)
			}
			if got := usageOwner(t, s, f.owner.ID); got.BalanceMicros != f.owner.BalanceMicros || countRows(t, s, &Conversion{}) != 0 {
				t.Fatal("routing charged or sent conversion")
			}
			// The underlying pool is not edited; standalone routing still uses it.
			standalone := routedAdRequest(s, "GET", "https://customer.example/r/"+f.pool.Slug, nil, nil, "")
			requireCode(t, standalone, 302)
			if standalone.Header().Get("Location") != raw {
				t.Fatal("standalone pool behavior changed")
			}
			landing := routedAdRequest(s, "GET", u.String(), nil, nil, "")
			targetJar := landing.Result().Cookies()
			requireCode(t, landing, 307)
			file := "https://download.example" + landing.Header().Get("Location")

			requireCode(t, routedAdRequest(s, "GET", file, targetJar, nil, "bytes=0-0"), 206)
			if got := usageOwner(t, s, f.owner.ID); got.BalanceMicros != f.owner.BalanceMicros-50000 {
				t.Fatal("actual download did not bill once")
			}
			var event Conversion
			if err := s.DB.Preload("Deliveries").First(&event).Error; err != nil {
				t.Fatal(err)
			}
			if event.AppID != f.apps[0].ID || event.DownloadedAppID != f.apps[1].ID || !strings.HasPrefix(event.SourceURL, "https://customer.example/") || len(event.Deliveries) != 2 {
				t.Fatal("customer-host attribution was lost")
			}
		})
	}
}

func TestAPKCustomerDomainRoutingOnlyNormalizesOwnedNativeTargets(t *testing.T) {
	f := routedAdFixtureFor(t, true)
	s := f.s
	addChosenDomain(t, s, f.owner, "customer.example")
	other := seedUser(t, s, 100*Dollar)
	foreign := seedApp(t, s, other, 100)
	for _, raw := range []string{
		"https://external.example/a/" + f.apps[1].Slug + "/direct",
		"https://download.example:8443/a/" + f.apps[1].Slug + "/direct",
		"https://download.example/a/" + foreign.Slug + "/direct",
		"https://download.example/a/unknown/direct",
		"https://download.example/p/pwa/",
		"https://external.example/package.apk",
	} {
		if err := s.DB.Model(&TrafficLink{}).Where("pool_slug = ?", f.pool.Slug).Update("url", raw).Error; err != nil {
			t.Fatal(err)
		}
		w := routedAdRequest(s, "POST", "https://customer.example/a/"+f.apps[0].Slug+"/button", nil, url.Values{"request_key": {randomHex(16)}}, "")
		requireCode(t, w, 200)
		var out map[string]string
		json.Unmarshal(w.Body.Bytes(), &out)
		if out["redirect_url"] != raw {
			t.Fatal("external or foreign target changed", raw, out)
		}
	}
	// A platform-hosted detail page continues to honor its configured pool URL.
	raw := "https://download.example/a/" + f.apps[1].Slug + "/direct"
	s.DB.Model(&TrafficLink{}).Where("pool_slug = ?", f.pool.Slug).Update("url", raw)
	w := routedAdRequest(s, "POST", s.Config.PublicURL+"/a/"+f.apps[0].Slug+"/button", nil, url.Values{"request_key": {randomHex(16)}}, "")
	requireCode(t, w, 200)
	var out map[string]string
	json.Unmarshal(w.Body.Bytes(), &out)
	if out["redirect_url"] != raw {
		t.Fatal("platform routing changed")
	}
}

func TestAPKRoutingButtonDownloadsOnceEvenWhenEveryTargetUsesTheSamePool(t *testing.T) {
	f := routedAdFixtureFor(t, true)
	s := f.s
	for i, app := range f.apps {
		d := defaultPageDesign(app)
		d.ButtonAction = "traffic"
		d.TrafficPoolID = f.pool.StatisticsID
		if err := s.DB.Save(&AppPageDesign{AppID: app.ID, Design: d}).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.DB.Model(&app).Update("download_mode", "info").Error; err != nil {
			t.Fatal(err)
		}
		if err := s.DB.Model(&TrafficLink{}).Where("pool_slug = ? AND position = ?", f.pool.Slug, i).Update("url", "https://download.example/a/"+app.Slug).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, app := range f.apps {
		if i > 0 {
			f.root = Attribution{}
			f.landing(t, "loop-test-"+app.Slug)
		}
		raw := f.click(t, true)
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if u.Path != "/a/"+app.Slug+"/direct" {
			t.Fatal("button selected another detail page", raw)
		}
		file, jar := f.targetTicket(t, raw, app)
		requireCode(t, routedAdRequest(s, "GET", file, jar, nil, "bytes=0-0"), 206)
		if countRows(t, s, &Conversion{}) != int64(i+1) {
			t.Fatal("native download lost original campaign")
		}
		if countRows(t, s, &MeteredEvent{}) != int64(i+1) {
			t.Fatal("routing billed source or counted a second download")
		}
	}
}
