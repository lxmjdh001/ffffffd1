package platform

import (
	"bytes"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func directRequest(t *testing.T, s *Server, method, path string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://localhost:8088"+path, nil)
	r.RemoteAddr = "203.0.113.25:12345"
	r.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 14; Mobile) Chrome/123")
	r.Header.Set("Referer", "https://customer.example.com/ad")
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestDirectDownloadPreservesAttributionAndBillsCompletedRangesOnce(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	a := seedApp(t, s, u, 10)
	if err := s.DB.Model(&Plan{}).Where("id = ?", "free").Update("download_limit", 0).Error; err != nil {
		t.Fatal(err)
	}
	w := directRequest(t, s, "GET", "/a/"+a.Slug+"?fbclid=fb-click&ttclid=tt-click", nil)
	requireCode(t, w, http.StatusTemporaryRedirect)
	location := w.Header().Get("Location")
	if !strings.HasPrefix(location, "/d/") || len(location) != 67 || strings.Contains(w.Body.String(), "下载应用") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("public link did not redirect immediately to an uncached download")
	}
	var visitor string
	for _, c := range w.Result().Cookies() {
		if c.Name == "d1_visitor" {
			visitor = c.Value
		}
	}
	if visitor == "" {
		t.Fatal("new visitors need a cookie for the redirected request")
	}
	var ticket Ticket
	if err := s.DB.First(&ticket, "hash = ?", hashString(strings.TrimPrefix(location, "/d/"))).Error; err != nil {
		t.Fatal(err)
	}
	if ticket.Reserved || ticket.Charged || countRows(t, s, &Download{}) != 0 {
		t.Fatal("issuing a redirect must not consume quota")
	}
	var attr Attribution
	if err := s.DB.First(&attr, "id = ?", ticket.AttributionID).Error; err != nil {
		t.Fatal(err)
	}
	if attr.FBClid != "fb-click" || attr.TTClid != "tt-click" || attr.Source != "https://customer.example.com/ad" || attr.Consent {
		t.Fatal("direct download lost click context or invented advertising consent")
	}
	raw := strings.TrimPrefix(location, "/d/")
	first := download(t, s, raw, visitor, "GET", "bytes=0-4")
	requireCode(t, first, 206)
	if first.Body.String() != "MZAAA" || !strings.HasPrefix(first.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal("download must return an attachment with the original file bytes")
	}
	if countRows(t, s, &Download{}) != 0 || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("partial download must not count as successful")
	}
	requireCode(t, download(t, s, raw, visitor, "GET", "bytes=5-9"), 206)
	requireCode(t, download(t, s, raw, visitor, "GET", ""), 200)
	if countRows(t, s, &Download{}) != 1 || countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("completed ranges and retries must settle exactly once")
	}
	var record Download
	if err := s.DB.First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if record.IP != "203.0.113.25" || record.Device != "手机" || record.OS != "Android" || record.Browser != "Chrome" || record.UserAgent != ticket.UserAgent || record.Source != attr.Source || record.AttributionID != attr.ID || record.Bytes != a.Size || record.CompletedAt.IsZero() {
		t.Fatalf("download metadata missing: %+v", record)
	}
	var after User
	if err := s.DB.First(&after, "id = ?", u.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.BalanceMicros != Dollar-3000 || after.HeldMicros != 0 {
		t.Fatalf("incorrect direct-download settlement: balance=%d held=%d", after.BalanceMicros, after.HeldMicros)
	}
	if countRows(t, s, &Conversion{}) != 0 {
		t.Fatal("unconsented download must not enqueue advertising events")
	}
}

func TestDirectDownloadBrowserRedirectAndHead(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	a := seedApp(t, s, u, 10)
	head := directRequest(t, s, "HEAD", "/a/"+a.Slug, nil)
	requireCode(t, head, 200)
	if head.Body.Len() != 0 || head.Header().Get("Content-Length") != "10" || !strings.HasPrefix(head.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal("HEAD should return file metadata without a body")
	}
	for _, model := range []any{&Ticket{}, &Attribution{}, &Download{}, &Usage{}} {
		if countRows(t, s, model) != 0 {
			t.Fatal("HEAD must not create attribution, tickets, or billing records")
		}
	}
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Jar = jar
	resp, err := client.Get(server.URL + "/a/" + a.Slug)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(resp.Request.URL.Path, "/d/") || !bytes.Equal(body, []byte("MZAAAAAAAA")) {
		t.Fatalf("browser-style GET did not automatically download the file: %d %s", resp.StatusCode, body)
	}
}

func TestDirectDownloadStillEnforcesAvailabilityAndBalance(t *testing.T) {
	for _, scenario := range []string{"paused", "blocked", "owner_disabled", "insufficient_balance"} {
		t.Run(scenario, func(t *testing.T) {
			s := testServer(t)
			u := seedUser(t, s, 0)
			a := seedApp(t, s, u, 10)
			var err error
			switch scenario {
			case "paused":
				err = s.DB.Model(&a).Update("enabled", false).Error
			case "blocked":
				err = s.DB.Model(&a).Update("blocked", true).Error
			case "owner_disabled":
				err = s.DB.Model(&u).Update("disabled", true).Error
			case "insufficient_balance":
				err = s.DB.Model(&Plan{}).Where("id = ?", "free").Update("download_limit", 0).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			w := directRequest(t, s, "GET", "/a/"+a.Slug, nil)
			if scenario == "insufficient_balance" {
				requireCode(t, w, http.StatusTemporaryRedirect)
				w = directRequest(t, s, "GET", w.Header().Get("Location"), w.Result().Cookies())
				requireCode(t, w, 402)
				if w.Header().Get("Content-Disposition") != "" {
					t.Fatal("billing error should not trigger a file download")
				}
			} else {
				requireCode(t, w, 404)
			}
			if countRows(t, s, &Download{}) != 0 || countRows(t, s, &Ledger{}) != 0 {
				t.Fatal("rejected download must not be billed")
			}
		})
	}
}

func TestDirectDownloadOptionalInfoPreservesDeepLinkAndConsent(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	a := seedApp(t, s, u, 10)
	c := defaultAds(a)
	c.FacebookEnabled = true
	c.FacebookPixel = "123456789"
	c.DeepLink = "customerapp://open"
	if err := s.DB.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	first := directRequest(t, s, "GET", "/a/"+a.Slug+"?fbclid=fb-click", nil)
	requireCode(t, first, http.StatusTemporaryRedirect)
	cookies := first.Result().Cookies()
	info := directRequest(t, s, "GET", "/a/"+a.Slug+"/info", cookies)
	requireCode(t, info, 200)
	var attr Attribution
	if err := s.DB.First(&attr).Error; err != nil {
		t.Fatal(err)
	}
	if countRows(t, s, &Attribution{}) != 1 || !strings.Contains(info.Body.String(), "customerapp://open?d1_token="+s.attributionToken(attr)) || !strings.Contains(info.Body.String(), `name="ad_consent"`) {
		t.Fatal("optional details page must reuse the direct-download attribution")
	}
	form := url.Values{"request_key": {randomHex(16)}, "attribution_token": {s.attributionToken(attr)}, "ad_consent": {"yes"}}
	r := httptest.NewRequest("POST", "http://localhost:8088/a/"+a.Slug+"/download", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, http.StatusSeeOther)
	// An already-authorized visitor can also use the automatic public link.
	w = directRequest(t, s, "GET", "/a/"+a.Slug, cookies)
	requireCode(t, w, http.StatusTemporaryRedirect)
	w = directRequest(t, s, "GET", w.Header().Get("Location"), cookies)
	requireCode(t, w, 200)
	drainDownloadAds(s)
	if countRows(t, s, &Conversion{}) != 1 || countRows(t, s, &Delivery{}) != 1 {
		t.Fatal("authorized direct download should still enqueue its advertising conversion")
	}
}
