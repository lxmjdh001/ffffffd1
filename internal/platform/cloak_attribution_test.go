package platform

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

func routedCloakFixture(t *testing.T, mode string) (*routedAdFixture, Cloak, string) {
	t.Helper()
	f := routedAdFixtureFor(t, true)
	addChosenDomain(t, f.s, f.owner, "chosen.example")
	c := Cloak{ID: randomHex(16), UserID: f.owner.ID, Slug: randomHex(4), Name: "Campaign handoff", Status: "rules", Enabled: true, TargetType: "app", TargetID: f.apps[1].ID, AppTargetMode: mode, AllowedPlatforms: []string{"facebook"}}
	if err := f.s.DB.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	raw := "https://chosen.example/c/" + c.Slug + "?keep=original"
	if err := f.s.DB.Model(&TrafficLink{}).Where("pool_slug = ?", f.pool.Slug).Update("url", raw).Error; err != nil {
		t.Fatal(err)
	}
	return f, c, raw
}

func cloakBrowserCookies(cookies []*http.Cookie, path string) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range cookies {
		if c.Path == "" || c.Path == "/" || path == c.Path || strings.HasPrefix(path, strings.TrimSuffix(c.Path, "/")+"/") {
			out = append(out, c)
		}
	}
	return out
}

func mergeCloakCookies(a, b []*http.Cookie) []*http.Cookie {
	out := append([]*http.Cookie{}, a...)
	for _, c := range b {
		for i := len(out) - 1; i >= 0; i-- {
			if out[i].Name == c.Name && out[i].Path == c.Path {
				out = append(out[:i], out[i+1:]...)
			}
		}
		if c.MaxAge >= 0 {
			out = append(out, c)
		}
	}
	return out
}

func cloakNativeFields(t *testing.T, page *httptest.ResponseRecorder) url.Values {
	t.Helper()
	fields := url.Values{}
	for _, name := range []string{"request_key", "attribution_token"} {
		match := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(html.UnescapeString(page.Body.String()))
		if len(match) != 2 {
			t.Fatal("missing native form field", name)
		}
		fields.Set(name, match[1])
	}
	return fields
}

func cloakTicketFromPage(t *testing.T, f *routedAdFixture, raw, mode string, page *httptest.ResponseRecorder, jar []*http.Cookie) (string, []*http.Cookie) {
	t.Helper()
	u, _ := url.Parse(raw)
	base := u.Scheme + "://" + u.Host
	jar = mergeCloakCookies(jar, page.Result().Cookies())
	if mode == "direct" {
		requireCode(t, page, 307)
		return base + page.Header().Get("Location"), jar
	}
	requireCode(t, page, 200)
	if page.Header().Get("Location") != "" || !strings.Contains(page.Body.String(), `action="/a/`+f.apps[1].Slug+`/download"`) {
		t.Fatal("cloaked detail changed the selected domain or button")
	}
	path := "/a/" + f.apps[1].Slug + "/download"
	w := routedAdRequest(f.s, "POST", base+path, cloakBrowserCookies(jar, path), cloakNativeFields(t, page), "")
	requireCode(t, w, 200)
	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return base + result["download_url"], mergeCloakCookies(jar, w.Result().Cookies())
}

func TestCloakRoutedAPKStartUsesSourceCampaign(t *testing.T) {
	for _, mode := range []string{"direct", "info"} {
		t.Run(mode, func(t *testing.T) {
			f, c, original := routedCloakFixture(t, mode)
			s := f.s
			other := defaultAds(f.apps[1])
			other.FacebookEnabled = true
			other.TikTokEnabled = true
			other.FacebookPixel = "999999999"
			other.TikTokPixel = "DESTINATIONPIXEL"
			if err := s.DB.Create(&other).Error; err != nil {
				t.Fatal(err)
			}
			raw := f.click(t, true)
			u, _ := url.Parse(raw)
			if u.Host != "chosen.example" || u.Path != "/c/"+c.Slug || u.Query().Get("keep") != "original" || u.Query().Get(routedAttributionParam) == "" || u.Query().Get("fbclid") != "source-click" || u.Query().Get("ttclid") != "source-click" || raw == original {
				t.Fatal("cloak handoff lost campaign, existing parameters or entry host")
			}
			head := routedAdRequest(s, "HEAD", raw, nil, nil, "")
			requireCode(t, head, 200)
			if countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Conversion{}) != 0 {
				t.Fatal("HEAD generated a download")
			}
			page := routedAdRequest(s, "GET", raw, nil, nil, "")
			if countRows(t, s, &Conversion{}) != 0 {
				t.Fatal("cloak entry counted as download")
			}
			file, jar := cloakTicketFromPage(t, f, raw, mode, page, nil)
			parsed, _ := url.Parse(file)
			if parsed.Host != "chosen.example" {
				t.Fatal("download left the selected cloak domain")
			}
			requireCode(t, routedAdRequest(s, "GET", file, cloakBrowserCookies(jar, parsed.Path), nil, "bytes=0-0"), 206)
			var event Conversion
			if err := s.DB.Preload("Deliveries").First(&event).Error; err != nil {
				t.Fatal(err)
			}
			if event.AppID != f.apps[0].ID || event.DownloadedAppID != f.apps[1].ID || event.AttributionID != f.root.ID || event.SourceURL != f.root.PageURL || event.Event != "download_start" || len(event.Deliveries) != 2 || countRows(t, s, &Download{}) != 0 {
				t.Fatal("cloak lost source attribution or waited for completion")
			}
			for _, d := range event.Deliveries {
				if d.EventName != "SourceDownload" || d.Platform == "facebook" && d.PixelID != f.cfg.FacebookPixel || d.Platform == "tiktok" && d.PixelID != f.cfg.TikTokPixel {
					t.Fatal("destination configuration replaced the source")
				}
			}
			calls := 0
			s.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				body, _ := io.ReadAll(r.Body)
				var payload map[string]any
				json.Unmarshal(body, &payload)
				data := payload["data"].([]any)[0].(map[string]any)
				if r.URL.Host == "graph.facebook.com" {
					if r.Header.Get("Authorization") != "Bearer source-facebook-only" || data["user_data"].(map[string]any)["fbc"] != f.root.FBC {
						t.Error("source Meta credentials or click changed")
					}
					return jsonResponse(200, `{"events_received":1}`), nil
				}
				if r.Header.Get("Access-Token") != "source-tiktok-only" || data["user"].(map[string]any)["ttclid"] != f.root.TTClid {
					t.Error("source TikTok credentials or click changed")
				}
				return jsonResponse(200, `{"code":0}`), nil
			})}
			for range 2 {
				if !s.deliverOne(context.Background()) {
					t.Fatal("missing delivery")
				}
			}
			requireCode(t, routedAdRequest(s, "GET", file, cloakBrowserCookies(jar, parsed.Path), nil, "bytes=0-0"), 206)
			requireCode(t, routedAdRequest(s, "GET", file, cloakBrowserCookies(jar, parsed.Path), nil, "bytes=1-99"), 206)
			requireCode(t, routedAdRequest(s, "GET", file, cloakBrowserCookies(jar, parsed.Path), nil, ""), 200)
			if calls != 2 || s.deliverOne(context.Background()) || countRows(t, s, &Conversion{}) != 1 || countRows(t, s, &Delivery{}) != 2 || countRows(t, s, &Download{}) != 1 {
				t.Fatal("retries, completion or ranges duplicated callbacks")
			}
		})
	}
}

func TestCloakRoutedCookieReloadExpiryAndNewClick(t *testing.T) {
	f, c, _ := routedCloakFixture(t, "info")
	s := f.s
	raw := f.click(t, true)
	page := routedAdRequest(s, "GET", raw, nil, nil, "")
	requireCode(t, page, 200)
	jar := page.Result().Cookies()
	found := false
	for _, cookie := range jar {
		if cookie.Name == cloakAttributionCookie(c) {
			found = cookie.HttpOnly && cookie.Path == "/c/"+c.Slug && cookie.MaxAge > 900
		}
	}
	if !found {
		t.Fatal("cloak campaign was not remembered in a scoped HttpOnly cookie")
	}
	now := s.Now().Add(20 * time.Minute)
	s.Now = func() time.Time { return now }
	u, _ := url.Parse(raw)
	page = routedAdRequest(s, "GET", raw, cloakBrowserCookies(jar, u.Path), nil, "")
	requireCode(t, page, 200)
	fields := cloakNativeFields(t, page)
	attr, err := s.readAttribution(fields.Get("attribution_token"), f.apps[1].ID)
	if err != nil || attr.SourceAttributionID != f.root.ID {
		t.Fatal("reload with an expired URL lost the established campaign")
	}
	jar = mergeCloakCookies(jar, page.Result().Cookies())
	q := u.Query()
	q.Del(routedAttributionParam)
	u.RawQuery = q.Encode()
	page = routedAdRequest(s, "GET", u.String(), cloakBrowserCookies(jar, u.Path), nil, "")
	attr, err = s.readAttribution(cloakNativeFields(t, page).Get("attribution_token"), f.apps[1].ID)
	if err != nil || attr.SourceAttributionID != f.root.ID {
		t.Fatal("reload without handoff query lost the campaign")
	}
	jar = mergeCloakCookies(jar, page.Result().Cookies())
	q.Set("fbclid", "new-campaign")
	q.Del("ttclid")
	u.RawQuery = q.Encode()
	page = routedAdRequest(s, "GET", u.String(), cloakBrowserCookies(jar, u.Path), nil, "")
	attr, err = s.readAttribution(cloakNativeFields(t, page).Get("attribution_token"), f.apps[1].ID)
	if err != nil || attr.SourceAttributionID != "" || attr.FBClid != "new-campaign" {
		t.Fatal("old cloak cookie overrode an explicit new click")
	}
	cleared := false
	for _, cookie := range page.Result().Cookies() {
		if cookie.Name == cloakAttributionCookie(c) && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("old campaign cookie was not cleared")
	}
	// An expired URL without the previously established cookie cannot inherit.
	page = routedAdRequest(s, "GET", raw, nil, nil, "")
	attr, err = s.readAttribution(cloakNativeFields(t, page).Get("attribution_token"), f.apps[1].ID)
	if err != nil || attr.SourceAttributionID != "" {
		t.Fatal("expired handoff created a fresh inherited campaign")
	}
}

func TestCloakRoutedAttributionCannotBypassRulesOrLeakToSafePage(t *testing.T) {
	for _, reason := range []string{"status", "ip", "ua", "language", "platform", "device"} {
		t.Run(reason, func(t *testing.T) {
			f, c, _ := routedCloakFixture(t, "direct")
			s := f.s
			c.SafeURL = "https://safe.example/notice?keep=1"
			switch reason {
			case "status":
				c.Status = "blocked"
			case "ip":
				c.BlockedIPs = []string{"203.0.113.81"}
			case "ua":
				c.BlockedUAs = []string{"Route attribution"}
			case "language":
				c.BlockEmptyLanguage = true
			case "platform":
				c.AllowedPlatforms = []string{"google"}
			case "device":
				c.BlockedDevices = []CloakDeviceRule{{Device: "desktop", Systems: []string{"all"}}}
			}
			if err := s.DB.Save(&c).Error; err != nil {
				t.Fatal(err)
			}
			raw := f.click(t, true)
			w := routedAdRequest(s, "GET", raw, nil, nil, "")
			requireCode(t, w, 302)
			if w.Header().Get("Location") != c.SafeURL || countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Conversion{}) != 0 {
				t.Fatal("attribution bypassed cloak rules or contaminated safe URL")
			}
			for _, cookie := range w.Result().Cookies() {
				if cookie.Name == cloakAttributionCookie(c) {
					t.Fatal("blocked visit remembered the campaign")
				}
			}
		})
	}
}

func TestCloakRoutedAttributionRejectsTamperForeignOwnerAndHost(t *testing.T) {
	f, c, _ := routedCloakFixture(t, "info")
	s := f.s
	raw := f.click(t, true)
	valid, _ := url.Parse(raw)
	addChosenDomain(t, s, f.owner, "other.example")
	other := c
	other.ID = randomHex(16)
	other.Slug = randomHex(4)
	s.DB.Create(&other)
	for _, kind := range []string{"signature", "host", "cloak", "app-token"} {
		t.Run(kind, func(t *testing.T) {
			u := *valid
			switch kind {
			case "signature":
				q := u.Query()
				q.Set(routedAttributionParam, q.Get(routedAttributionParam)+"x")
				u.RawQuery = q.Encode()
			case "host":
				u.Host = "other.example"
			case "cloak":
				u.Path = "/c/" + other.Slug
			case "app-token":
				q := u.Query()
				q.Set(routedAttributionParam, s.routedCapability(f.root, "routed-attribution", f.apps[1].ID, u.Host, 15*time.Minute))
				u.RawQuery = q.Encode()
			}
			page := routedAdRequest(s, "GET", u.String(), nil, nil, "")
			requireCode(t, page, 200)
			attr, err := s.readAttribution(cloakNativeFields(t, page).Get("attribution_token"), f.apps[1].ID)
			if err != nil || attr.SourceAttributionID != "" {
				t.Fatal("invalid cloak capability inherited a campaign")
			}
		})
	}
	foreignOwner := seedUser(t, s, 10*Dollar)
	foreignApp := seedApp(t, s, foreignOwner, 100)
	foreign := c
	foreign.ID = randomHex(16)
	foreign.Slug = randomHex(4)
	foreign.UserID = foreignOwner.ID
	foreign.TargetID = foreignApp.ID
	s.DB.Create(&foreign)
	u := *valid
	u.Path = "/c/" + foreign.Slug
	q := u.Query()
	q.Del(routedAttributionParam)
	u.RawQuery = q.Encode()
	s.DB.First(&f.root, "id = ?", f.root.ID)
	if out, err := s.routedAttributionURL(httptest.NewRequest("GET", s.Config.PublicURL, nil), f.apps[0], f.root, u.String()); err != nil || out != u.String() {
		t.Fatal("issuer decorated another customer's cloak")
	}
	// Even a correctly signed server capability cannot cross tenant boundaries.
	u.Host = "download.example" // A public domain can serve either owner.
	q.Set(routedAttributionParam, s.routedCapability(f.root, routedCloakNamespace, foreign.ID, u.Host, 15*time.Minute))
	u.RawQuery = q.Encode()
	page := routedAdRequest(s, "GET", u.String(), nil, nil, "")
	requireCode(t, page, 200)
	attr, err := s.readAttribution(cloakNativeFields(t, page).Get("attribution_token"), foreignApp.ID)
	if err != nil || attr.SourceAttributionID != "" {
		t.Fatal("receiver accepted a cross-account campaign")
	}
	for _, external := range []string{"https://external.example/c/" + c.Slug, "https://chosen.example:8443/c/" + c.Slug} {
		if out, err := s.routedAttributionURL(httptest.NewRequest("GET", s.Config.PublicURL, nil), f.apps[0], f.root, external); err != nil || out != external {
			t.Fatal("handoff leaked to an untrusted service")
		}
	}
}

func TestCloakRoutedAttributionContinuesThroughOwnedPool(t *testing.T) {
	f, c, _ := routedCloakFixture(t, "direct")
	s := f.s
	addChosenDomain(t, s, f.owner, "other.example")
	p := apkButtonPool(t, s, f.owner, "https://other.example/a/"+f.apps[1].Slug+"/direct", "https://external.example/file.apk?keep=1")
	c.TargetType = "traffic_pool"
	c.TrafficPoolSlug = p.Slug
	c.TargetID = ""
	s.DB.Save(&c)
	raw := f.click(t, true)
	w := routedAdRequest(s, "GET", raw, nil, nil, "")
	requireCode(t, w, 302)
	u, _ := url.Parse(w.Header().Get("Location"))
	if u.Host != "other.example" || u.Path != "/a/"+f.apps[1].Slug+"/direct" || u.Query().Get(routedAttributionParam) == "" {
		t.Fatal("cloak pool lost the campaign or rewrote its own destination")
	}
	file, jar := f.targetTicket(t, u.String(), f.apps[1])
	requireCode(t, routedAdRequest(s, "GET", file, jar, nil, "bytes=0-0"), 206)
	var event Conversion
	s.DB.First(&event)
	if event.AppID != f.apps[0].ID || event.DownloadedAppID != f.apps[1].ID {
		t.Fatal("nested pool lost source ownership")
	}
	w = routedAdRequest(s, "GET", raw, nil, nil, "")
	requireCode(t, w, 302)
	u, _ = url.Parse(w.Header().Get("Location"))
	if u.Host != "external.example" || u.Query().Has(routedAttributionParam) || strings.Contains(w.Header().Get("Location"), f.root.ID) {
		t.Fatal("private handoff leaked to an external pool destination")
	}
}

func TestCloakRoutedAttributionDisabledSourceNeverUsesDestinationPixel(t *testing.T) {
	f, c, _ := routedCloakFixture(t, "direct")
	s := f.s
	p := apkButtonPool(t, s, f.owner, "https://download.example/a/"+f.apps[1].Slug+"/direct")
	c.TargetType = "traffic_pool"
	c.TrafficPoolSlug = p.Slug
	c.TargetID = ""
	s.DB.Save(&c)
	other := defaultAds(f.apps[1])
	other.FacebookEnabled = true
	other.FacebookPixel = "999999999"
	s.DB.Create(&other)
	raw := f.click(t, true)
	s.DB.Model(&AdConfig{}).Where("app_id = ?", f.apps[0].ID).Updates(map[string]any{"facebook_enabled": false, "tik_tok_enabled": false})
	w := routedAdRequest(s, "GET", raw, nil, nil, "")
	requireCode(t, w, 302)
	file, jar := f.targetTicket(t, w.Header().Get("Location"), f.apps[1])
	requireCode(t, routedAdRequest(s, "GET", file, jar, nil, "bytes=0-0"), 206)
	var event Conversion
	if err := s.DB.First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.AppID != f.apps[0].ID || countRows(t, s, &Delivery{}) != 0 {
		t.Fatal("disabled original campaign was replaced with a destination campaign")
	}
}

func TestCloakRoutedNoConsentStillPassesPlatformRulesWithoutCallback(t *testing.T) {
	f, _, _ := routedCloakFixture(t, "direct")
	s := f.s
	pageURL := f.root.PageURL + "&gclid=google-click&bbg=bigo-click&do_not_forward=private"
	s.DB.Model(&Attribution{}).Where("id = ?", f.root.ID).Update("page_url", pageURL)
	raw := f.click(t, false)
	u, _ := url.Parse(raw)
	if u.Query().Has(routedAttributionParam) || u.Query().Get("fbclid") != "source-click" || u.Query().Get("gclid") != "google-click" || u.Query().Get("bbg") != "bigo-click" || u.Query().Has("do_not_forward") {
		t.Fatal("consent affected functional platform parameters or leaked unrelated data")
	}
	w := routedAdRequest(s, "GET", raw, nil, nil, "")
	requireCode(t, w, 307)
	file := "https://chosen.example" + w.Header().Get("Location")
	requireCode(t, routedAdRequest(s, "GET", file, w.Result().Cookies(), nil, ""), 200)
	if countRows(t, s, &Download{}) != 1 || countRows(t, s, &Conversion{}) != 0 || countRows(t, s, &Delivery{}) != 0 {
		t.Fatal("unconsented visitor was blocked or reported")
	}
}

func TestCloakRoutedCampaignSurvivesAnotherAPKAndCloak(t *testing.T) {
	t.Run("enabled", func(t *testing.T) { walkCloakRoutedCampaign(t, false) })
	t.Run("source disabled mid-chain", func(t *testing.T) { walkCloakRoutedCampaign(t, true) })
}

func walkCloakRoutedCampaign(t *testing.T, disabled bool) {
	t.Helper()
	f, first, _ := routedCloakFixture(t, "info")
	s := f.s
	addChosenDomain(t, s, f.owner, "second.example")
	second := first
	second.ID = randomHex(16)
	second.Slug = randomHex(4)
	second.TargetID = f.apps[2].ID
	second.AppTargetMode = "direct"
	if err := s.DB.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	p := apkButtonPool(t, s, f.owner, "https://second.example/c/"+second.Slug)
	d := defaultPageDesign(f.apps[1])
	d.ButtonAction = "traffic"
	d.TrafficPoolID = p.StatisticsID
	if err := s.DB.Create(&AppPageDesign{AppID: f.apps[1].ID, Design: d}).Error; err != nil {
		t.Fatal(err)
	}
	raw := f.click(t, true)
	page := routedAdRequest(s, "GET", raw, nil, nil, "")
	requireCode(t, page, 200)
	if !strings.Contains(page.Body.String(), `action="/a/`+f.apps[1].Slug+`/button"`) {
		t.Fatal("intermediate cloak lost the APK's own traffic binding")
	}
	path := "/a/" + f.apps[1].Slug + "/button"
	if disabled {
		s.DB.Model(&AdConfig{}).Where("app_id = ?", f.apps[0].ID).Updates(map[string]any{"facebook_enabled": false, "tik_tok_enabled": false})
		destination := defaultAds(f.apps[2])
		destination.FacebookEnabled = true
		destination.FacebookPixel = "999999999"
		s.DB.Create(&destination)
	}
	w := routedAdRequest(s, "POST", "https://chosen.example"+path, cloakBrowserCookies(page.Result().Cookies(), path), cloakNativeFields(t, page), "")
	requireCode(t, w, 200)
	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	u, _ := url.Parse(result["redirect_url"])
	if u.Host != "second.example" || u.Path != "/c/"+second.Slug || u.Query().Get(routedAttributionParam) == "" {
		t.Fatal("intermediate APK lost root campaign or next domain")
	}
	if countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Conversion{}) != 0 {
		t.Fatal("intermediate pages or clicks counted as downloads")
	}
	w = routedAdRequest(s, "GET", u.String(), nil, nil, "")
	requireCode(t, w, 307)
	file := "https://second.example" + w.Header().Get("Location")
	requireCode(t, routedAdRequest(s, "GET", file, w.Result().Cookies(), nil, "bytes=0-0"), 206)
	var event Conversion
	if err := s.DB.Preload("Deliveries").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	want := 2
	if disabled {
		want = 0
	}
	if event.AttributionID != f.root.ID || event.AppID != f.apps[0].ID || event.DownloadedAppID != f.apps[2].ID || len(event.Deliveries) != want {
		t.Fatal("additional cloak/APK changed the original campaign")
	}
}

func TestCloakRoutedOriginalCloakToAPKToPoolRemainsSupported(t *testing.T) {
	f := routedAdFixtureFor(t, true)
	s := f.s
	addChosenDomain(t, s, f.owner, "chosen.example")
	c := Cloak{ID: randomHex(16), UserID: f.owner.ID, Slug: randomHex(4), Name: "Original entry", Status: "off", TargetType: "app", TargetID: f.apps[0].ID, AppTargetMode: "info"}
	s.DB.Create(&c)
	s.DB.Model(&f.pool).Update("cursor", 1)
	page := routedAdRequest(s, "GET", "https://chosen.example/c/"+c.Slug+"?fbclid=cloaked-source&ttclid=cloaked-source", nil, nil, "")
	requireCode(t, page, 200)
	fields := cloakNativeFields(t, page)
	fields.Set("ad_consent", "yes")
	root, err := s.readAttribution(fields.Get("attribution_token"), f.apps[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	path := "/a/" + f.apps[0].Slug + "/button"
	w := routedAdRequest(s, "POST", "https://chosen.example"+path, cloakBrowserCookies(page.Result().Cookies(), path), fields, "")
	requireCode(t, w, 200)
	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	u, _ := url.Parse(result["redirect_url"])
	if u.Host != "download.example" {
		t.Fatal("APK routing button did not preserve the configured pool hostname")
	}
	file, jar := f.targetTicket(t, u.String(), f.apps[1])
	requireCode(t, routedAdRequest(s, "GET", file, jar, nil, "bytes=0-0"), 206)
	var event Conversion
	s.DB.First(&event)
	if event.AttributionID != root.ID || event.AppID != f.apps[0].ID || event.DownloadedAppID != f.apps[1].ID {
		t.Fatal("existing cloak-to-APK-to-pool flow regressed")
	}
}
