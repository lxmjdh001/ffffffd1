package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type routedAdFixture struct {
	s       *Server
	owner   User
	apps    []Application
	pool    TrafficPool
	cookies []*http.Cookie
	root    Attribution
	cfg     AdConfig
}

func routedAdRequest(s *Server, method, raw string, cookies []*http.Cookie, form url.Values, byteRange string) *httptest.ResponseRecorder {
	w := routedAdRequestOnly(s, method, raw, cookies, form, byteRange)
	// Production uses the independent preparation worker. Advance that worker
	// explicitly here so attribution assertions do not race its poll interval.
	drainDownloadAds(s)
	return w
}

func drainDownloadAds(s *Server) {
	for {
		select {
		case event := <-s.downloadAdStarts:
			s.spoolDownloadAdStart(context.Background(), event)
		default:
			s.replayDownloadAds(context.Background())
			return
		}
	}
}

func routedAdRequestOnly(s *Server, method, raw string, cookies []*http.Cookie, form url.Values, byteRange string) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, raw, body)
	r.RemoteAddr = "203.0.113.81:3210"
	r.Header.Set("User-Agent", "Route attribution test browser")
	r.Header.Set("Accept", "application/json")
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if byteRange != "" {
		r.Header.Set("Range", byteRange)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func routedAdFixtureFor(t *testing.T, crossDomain bool) *routedAdFixture {
	t.Helper()
	s := meteredServer(t, 0)
	owner := seedUser(t, s, 100*Dollar)
	f := &routedAdFixture{s: s, owner: owner}
	for range 5 {
		f.apps = append(f.apps, seedApp(t, s, owner, 100))
	}
	f.cfg = defaultAds(f.apps[0])
	f.cfg.FacebookEnabled = true
	f.cfg.TikTokEnabled = true
	f.cfg.FacebookPixel = "123456789"
	f.cfg.TikTokPixel = "SOURCEPIXEL"
	f.cfg.DownloadFacebookEvent = "SourceDownload"
	f.cfg.DownloadTikTokEvent = "SourceDownload"
	f.cfg.FacebookToken, _ = s.encryptSecret("source-facebook-only")
	f.cfg.TikTokToken, _ = s.encryptSecret("source-tiktok-only")
	if e := s.DB.Create(&f.cfg).Error; e != nil {
		t.Fatal(e)
	}
	base := "http://localhost:8088"
	if crossDomain {
		base = "https://download.example"
		x, _ := getSettings(s.DB)
		x.DownloadDomainRules = []DownloadDomainRule{{URL: base, Audience: "all"}}
		if e := s.DB.Model(&x).Select("DownloadDomainRules").Updates(&x).Error; e != nil {
			t.Fatal(e)
		}
	}
	urls := []string{}
	for i, a := range f.apps {
		ending := "/direct"
		if i == 2 || i == 4 {
			ending = "/info"
		}
		urls = append(urls, base+"/a/"+a.Slug+ending)
	}
	f.pool = apkButtonPool(t, s, owner, urls...)
	d := defaultPageDesign(f.apps[0])
	d.ButtonAction = "traffic"
	d.TrafficPoolID = f.pool.StatisticsID
	if e := s.DB.Create(&AppPageDesign{AppID: f.apps[0].ID, Design: d}).Error; e != nil {
		t.Fatal(e)
	}
	f.landing(t, "source-click")
	return f
}
func (f *routedAdFixture) landing(t *testing.T, click string) {
	t.Helper()
	w := routedAdRequest(f.s, "GET", "http://localhost:8088/a/"+f.apps[0].Slug+"/info?fbclid="+click+"&ttclid="+click, nil, nil, "")
	requireCode(t, w, 200)
	f.cookies = w.Result().Cookies()
	if e := f.s.DB.First(&f.root, "app_id = ? AND fb_clid = ?", f.apps[0].ID, click).Error; e != nil {
		t.Fatal(e)
	}
}
func (f *routedAdFixture) click(t *testing.T, consent bool) string {
	t.Helper()
	form := url.Values{"request_key": {randomHex(16)}, "attribution_token": {f.s.attributionToken(f.root)}}
	if consent {
		form.Set("ad_consent", "yes")
	}
	w := routedAdRequest(f.s, "POST", "http://localhost:8088/a/"+f.apps[0].Slug+"/button", f.cookies, form, "")
	requireCode(t, w, 200)
	var out map[string]string
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out["redirect_url"]
}
func (f *routedAdFixture) targetTicket(t *testing.T, raw string, app Application) (string, []*http.Cookie) {
	t.Helper()
	u, _ := url.Parse(raw)
	w := routedAdRequest(f.s, "GET", raw, nil, nil, "")
	cookies := w.Result().Cookies()
	if strings.HasSuffix(u.Path, "/info") {
		requireCode(t, w, 200)
		var attr Attribution
		if e := f.s.DB.First(&attr, "app_id = ? AND source_attribution_id = ?", app.ID, f.root.ID).Error; e != nil {
			t.Fatal(e)
		}
		form := url.Values{"request_key": {randomHex(16)}, "attribution_token": {f.s.attributionToken(attr)}}
		w = routedAdRequest(f.s, "POST", u.Scheme+"://"+u.Host+"/a/"+app.Slug+"/download", cookies, form, "")
		requireCode(t, w, 200)
		var out map[string]string
		json.Unmarshal(w.Body.Bytes(), &out)
		return u.Scheme + "://" + u.Host + out["download_url"], cookies
	}
	requireCode(t, w, 307)
	return u.Scheme + "://" + u.Host + w.Header().Get("Location"), cookies
}

func TestRoutedAdsFiveAPKsStartInheritSourceAndDeduplicate(t *testing.T) {
	for _, cross := range []bool{false, true} {
		t.Run(strconv.FormatBool(cross), func(t *testing.T) {
			f := routedAdFixtureFor(t, cross)
			s := f.s
			for i := range 5 {
				if i > 0 {
					f.root = Attribution{}
					f.landing(t, "source-click-"+strconv.Itoa(i))
				}
				raw := f.click(t, true)
				if !strings.Contains(raw, routedAttributionParam+"=") {
					t.Fatal("source attribution was not signed for owned APK")
				}
				if countRows(t, s, &Conversion{}) != int64(i) || countRows(t, s, &Ticket{}) != int64(i) {
					t.Fatal("landing click triggered advertising or a source download")
				}
				file, cookies := f.targetTicket(t, raw, f.apps[i])
				if countRows(t, s, &Conversion{}) != int64(i) {
					t.Fatal("ticket redirect counted as download start")
				}
				requireCode(t, routedAdRequest(s, "HEAD", file, cookies, nil, ""), 200)
				requireCode(t, routedAdRequest(s, "GET", file, cookies, nil, "bytes=1000-2000"), 416)
				if countRows(t, s, &Conversion{}) != int64(i) {
					t.Fatal("HEAD generated advertising")
				}
				requireCode(t, routedAdRequest(s, "GET", file, cookies, nil, "bytes=0-0"), 206)
				if countRows(t, s, &Conversion{}) != int64(i+1) || countRows(t, s, &Download{}) != int64(i) {
					t.Fatal("routed advertising waited for completed download")
				}
				var event Conversion
				if e := s.DB.Preload("Deliveries").First(&event, "attribution_id = ?", f.root.ID).Error; e != nil {
					t.Fatal(e)
				}
				if event.AppID != f.apps[0].ID || event.DownloadedAppID != f.apps[i].ID || event.Event != "download_start" || event.VerifiedBy != "download_server_start" || event.SourceURL != f.root.PageURL || len(event.Deliveries) != 2 {
					t.Fatalf("incorrect routed event: %+v", event)
				}
				for _, d := range event.Deliveries {
					if d.EventName != "SourceDownload" || d.Platform == "facebook" && d.PixelID != f.cfg.FacebookPixel || d.Platform == "tiktok" && d.PixelID != f.cfg.TikTokPixel {
						t.Fatal("destination configuration replaced the source campaign")
					}
				}
				requireCode(t, routedAdRequest(s, "GET", file, cookies, nil, "bytes=0-0"), 206)
				requireCode(t, routedAdRequest(s, "GET", file, cookies, nil, "bytes=1-99"), 206)
				requireCode(t, routedAdRequest(s, "GET", file, cookies, nil, ""), 200)
				if countRows(t, s, &Conversion{}) != int64(i+1) || countRows(t, s, &Delivery{}) != int64((i+1)*2) || countRows(t, s, &Download{}) != int64(i+1) {
					t.Fatal("ranges, retries, or completion duplicated callback or broke download records")
				}
			}
		})
	}
}

func TestRoutedAdsConsentScopeTamperAndExternalTargets(t *testing.T) {
	f := routedAdFixtureFor(t, true)
	s := f.s
	if raw := f.click(t, false); strings.Contains(raw, routedAttributionParam+"=") {
		t.Fatal("unconsented source issued advertising handoff")
	}
	raw := f.click(t, true)
	u, _ := url.Parse(raw)
	target := f.apps[1]
	if !strings.Contains(u.Path, target.Slug) {
		t.Fatal("wrong sequential fixture destination")
	}
	for _, kind := range []string{"signature", "target", "host", "expiry"} {
		t.Run(kind, func(t *testing.T) {
			bad := *u
			actual := target
			visitor := randomHex(32)
			switch kind {
			case "signature":
				q := bad.Query()
				q.Set(routedAttributionParam, q.Get(routedAttributionParam)+"x")
				bad.RawQuery = q.Encode()
			case "target":
				actual = f.apps[2]
			case "host":
				bad.Host = "other.example"
			case "expiry":
				parts := strings.Split(bad.Query().Get(routedAttributionParam), ".")
				parts[1] = strconv.FormatInt(s.Now().Add(-time.Minute).Unix(), 10)
				q := bad.Query()
				q.Set(routedAttributionParam, strings.Join(parts, "."))
				bad.RawQuery = q.Encode()
			}
			request := httptest.NewRequest("GET", bad.String(), nil)
			w := httptest.NewRecorder()
			attr, ok, e := s.prepareRoutedAttribution(w, request, actual, visitor)
			if e != nil || ok || attr.ID != "" {
				t.Fatal("invalid handoff inherited campaign")
			}
		})
	}
	r := httptest.NewRequest("GET", "http://localhost:8088/", nil)
	s.DB.First(&f.root, "id = ?", f.root.ID)
	for _, raw := range []string{"https://external.example/a/" + target.Slug + "/direct", "https://download.example/external.apk", "https://download.example:8443/a/" + target.Slug + "/direct"} {
		out, e := s.routedAttributionURL(r, f.apps[0], f.root, raw)
		if e != nil || out != raw {
			t.Fatal("handoff leaked to an external URL or different service")
		}
	}
	other := seedUser(t, s, Dollar)
	foreign := seedApp(t, s, other, 100)
	raw = "https://download.example/a/" + foreign.Slug + "/direct"
	out, e := s.routedAttributionURL(r, f.apps[0], f.root, raw)
	if e != nil || out != raw {
		t.Fatal("cross-account attribution was allowed")
	}
}

func TestRoutedAdsConcurrentStartsAndOriginalDownloadRemainSeparate(t *testing.T) {
	f := routedAdFixtureFor(t, true)
	s := f.s
	f.s.DB.Model(&f.pool).Update("cursor", 1)
	raw := f.click(t, true)
	file, cookies := f.targetTicket(t, raw, f.apps[1])
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := routedAdRequest(s, "GET", file, cookies, nil, "bytes=0-0")
			if w.Code != 206 {
				t.Errorf("parallel range: %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if countRows(t, s, &Conversion{}) != 1 || countRows(t, s, &Delivery{}) != 2 || countRows(t, s, &Download{}) != 0 {
		t.Fatal("concurrent starts duplicated callback")
	}
	// Source-only callbacks must not silently move the existing non-routed
	// download-complete trigger to download start.
	page := routedAdRequest(s, "GET", "http://localhost:8088/a/"+f.apps[0].Slug+"/info?fbclid=plain&ttclid=plain", nil, nil, "")
	requireCode(t, page, 200)
	var attr Attribution
	s.DB.First(&attr, "app_id = ? AND fb_clid = ?", f.apps[0].ID, "plain")
	s.DB.Model(&attr).Update("consent", true)
	w := routedAdRequest(s, "GET", "http://localhost:8088/a/"+f.apps[0].Slug+"/direct", page.Result().Cookies(), nil, "")
	requireCode(t, w, 307)
	plainFile := "http://localhost:8088" + w.Header().Get("Location")
	allCookies := append(page.Result().Cookies(), w.Result().Cookies()...)
	requireCode(t, routedAdRequest(s, "GET", plainFile, allCookies, nil, "bytes=0-0"), 206)
	if countRows(t, s, &Conversion{}) != 1 {
		t.Fatal("ordinary download trigger changed")
	}
	requireCode(t, routedAdRequest(s, "GET", plainFile, allCookies, nil, "bytes=1-99"), 206)
	if countRows(t, s, &Conversion{}) != 2 {
		t.Fatal("ordinary completed callback disappeared")
	}
}

func TestRoutedAdsProviderUsesSourceCredentials(t *testing.T) {
	f := routedAdFixtureFor(t, true)
	s := f.s
	s.DB.Model(&f.pool).Update("cursor", 1)
	file, cookies := f.targetTicket(t, f.click(t, true), f.apps[1])
	requireCode(t, routedAdRequest(s, "GET", file, cookies, nil, "bytes=0-0"), 206)
	var calls int
	s.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		var p map[string]any
		json.Unmarshal(raw, &p)
		data := p["data"].([]any)[0].(map[string]any)
		if r.URL.Host == "graph.facebook.com" {
			if !strings.Contains(r.URL.Path, f.cfg.FacebookPixel) || r.Header.Get("Authorization") != "Bearer source-facebook-only" || data["event_name"] != "SourceDownload" || data["user_data"].(map[string]any)["fbc"] == "" {
				t.Error("source Meta configuration or click ID lost")
			}
			return jsonResponse(200, `{"events_received":1,"fbtrace_id":"fixture"}`), nil
		}
		if p["event_source_id"] != f.cfg.TikTokPixel || r.Header.Get("Access-Token") != "source-tiktok-only" || data["user"].(map[string]any)["ttclid"] != "source-click" || data["event"] != "SourceDownload" {
			t.Error("source TikTok configuration or click ID lost")
		}
		return jsonResponse(200, `{"code":0,"request_id":"fixture"}`), nil
	})}
	for range 2 {
		if !s.deliverOne(context.Background()) {
			t.Fatal("no queued routed event")
		}
	}
	if calls != 2 || s.deliverOne(context.Background()) {
		t.Fatal("wrong delivery count")
	}
}

func TestRoutedAdsDestinationConfigReloadAndAlternateTargets(t *testing.T) {
	f := routedAdFixtureFor(t, true)
	s := f.s
	config := defaultAds(f.apps[2])
	config.FacebookEnabled, config.TikTokEnabled = true, true
	config.FacebookPixel, config.TikTokPixel = "999999999", "DESTINATIONPIXEL"
	if e := s.DB.Create(&config).Error; e != nil {
		t.Fatal(e)
	}
	s.DB.Model(&f.pool).Update("cursor", 2)
	raw := f.click(t, true)
	w := routedAdRequest(s, "GET", raw, nil, nil, "")
	requireCode(t, w, 200)
	// Reload without the query handoff: the signed destination cookie must keep
	// the original campaign rather than creating a destination-owned campaign.
	u, _ := url.Parse(raw)
	u.RawQuery = ""
	requireCode(t, routedAdRequest(s, "GET", u.String(), w.Result().Cookies(), nil, ""), 200)
	var derived Attribution
	if e := s.DB.First(&derived, "app_id = ? AND source_attribution_id = ?", f.apps[2].ID, f.root.ID).Error; e != nil {
		t.Fatal(e)
	}
	form := url.Values{"request_key": {randomHex(16)}, "attribution_token": {s.attributionToken(derived)}}
	w2 := routedAdRequest(s, "POST", "https://download.example/a/"+f.apps[2].Slug+"/download", w.Result().Cookies(), form, "")
	requireCode(t, w2, 200)
	var out map[string]string
	if e := json.Unmarshal(w2.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	requireCode(t, routedAdRequest(s, "GET", "https://download.example"+out["download_url"], w.Result().Cookies(), nil, "bytes=0-0"), 206)
	// The same original click may route to another APK, including one with its
	// own ad settings. All destinations must share one source conversion ID.
	file, cookies := f.targetTicket(t, f.click(t, true), f.apps[3])
	requireCode(t, routedAdRequest(s, "GET", file, cookies, nil, "bytes=0-0"), 206)
	var event Conversion
	if e := s.DB.Preload("Deliveries").First(&event).Error; e != nil {
		t.Fatal(e)
	}
	if countRows(t, s, &Conversion{}) != 1 || len(event.Deliveries) != 2 || event.AppID != f.apps[0].ID || event.DownloadedAppID != f.apps[2].ID {
		t.Fatal("alternate destination duplicated or replaced the first real download")
	}
	for _, delivery := range event.Deliveries {
		if delivery.PixelID == config.FacebookPixel || delivery.PixelID == config.TikTokPixel {
			t.Fatal("destination ad configuration overrode source")
		}
	}
	// Even a correctly signed capability cannot cross owner boundaries.
	foreign := seedApp(t, s, seedUser(t, s, Dollar), 100)
	stamp := strconv.FormatInt(s.Now().Add(time.Minute).Unix(), 10)
	token := f.root.ID + "." + stamp + "." + s.mac("routed-attribution:"+f.root.ID+":"+foreign.ID+":download.example:"+stamp)
	r := httptest.NewRequest("GET", "https://download.example/a/"+foreign.Slug+"?d1_route="+url.QueryEscape(token), nil)
	if _, ok, err := s.prepareRoutedAttribution(httptest.NewRecorder(), r, foreign, randomHex(32)); ok || err != nil {
		t.Fatal("signed cross-owner handoff was accepted")
	}
}

func TestRoutedAdsMigrationPreservesExistingCampaignsAndMoney(t *testing.T) {
	f := routedAdFixtureFor(t, false)
	s := f.s
	beforeUser, beforeConfig := f.owner, f.cfg
	if e := s.DB.First(&beforeUser, "id = ?", f.owner.ID).Error; e != nil {
		t.Fatal(e)
	}
	legacy := Conversion{ID: randomHex(16), AppID: f.apps[0].ID, UserID: f.owner.ID, AttributionID: f.root.ID, Event: "download_complete", EventID: randomHex(32), CreatedAt: s.Now()}
	if e := s.DB.Create(&legacy).Error; e != nil {
		t.Fatal(e)
	}
	for _, column := range []struct {
		model        any
		field, index string
	}{
		{&Attribution{}, "SourceAttributionID", "idx_attributions_source_attribution_id"},
		{&Conversion{}, "DownloadedAppID", "idx_conversions_downloaded_app_id"},
	} {
		if e := s.DB.Migrator().DropIndex(column.model, column.index); e != nil {
			t.Fatal(e)
		}
		if e := s.DB.Migrator().DropColumn(column.model, column.field); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.DB.Delete(&Migration{}, "version = ?", 30).Error; e != nil {
		t.Fatal(e)
	}
	for range 2 {
		if e := Migrate(s.DB); e != nil {
			t.Fatal(e)
		}
	}
	var owner User
	var config AdConfig
	var attr Attribution
	var event Conversion
	s.DB.First(&owner, "id = ?", f.owner.ID)
	s.DB.First(&config, "app_id = ?", f.cfg.AppID)
	if e := s.DB.First(&attr, "id = ? AND source_attribution_id = ?", f.root.ID, "").Error; e != nil {
		t.Fatal(e)
	}
	s.DB.First(&event, "id = ?", legacy.ID)
	if owner.BalanceMicros != beforeUser.BalanceMicros || owner.GiftBalanceMicros != beforeUser.GiftBalanceMicros || config.FacebookToken != beforeConfig.FacebookToken || config.TikTokToken != beforeConfig.TikTokToken || attr.FBC != f.root.FBC || event.EventID != legacy.EventID || event.Event != legacy.Event || event.DownloadedAppID != "" {
		t.Fatalf("migration preservation: balance=%v gift=%v meta=%v tiktok=%v attribution=%v event_id=%v event=%v destination=%q", owner.BalanceMicros == beforeUser.BalanceMicros, owner.GiftBalanceMicros == beforeUser.GiftBalanceMicros, config.FacebookToken == beforeConfig.FacebookToken, config.TikTokToken == beforeConfig.TikTokToken, attr.FBC == f.root.FBC, event.EventID == legacy.EventID, event.Event == legacy.Event, event.DownloadedAppID)
	}
	var n int64
	s.DB.Model(&Migration{}).Where("version = ?", 30).Count(&n)
	if n != 1 || countRows(t, s, &Conversion{}) != 1 {
		t.Fatal("migration was not idempotent")
	}
}
