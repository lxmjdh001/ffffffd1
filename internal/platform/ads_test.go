package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func jsonResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}

func seedAds(t *testing.T, s *Server, a Application) (AdConfig, Attribution, string) {
	t.Helper()
	secret := "server-signing-key-test-only"
	c := defaultAds(a)
	c.FacebookEnabled = true
	c.FacebookPixel = "123456789"
	c.TikTokEnabled = true
	c.TikTokPixel = "TESTPIXEL1"
	c.FacebookToken, _ = s.encryptSecret("facebook-secret")
	c.TikTokToken, _ = s.encryptSecret("tiktok-secret")
	c.APISecret, _ = s.encryptSecret(secret)
	c.AllowedOrigin = "https://customer.example.com"
	if e := s.DB.Create(&c).Error; e != nil {
		t.Fatal(e)
	}
	attr := Attribution{ID: randomHex(16), AppID: a.ID, VisitorHash: s.mac("visitor"), FBC: "fb.1.123.click", FBClid: "click", TTClid: "tt-click", IP: "203.0.113.1", UserAgent: "Android", PageURL: "https://download.example.com/a/abc", Consent: true, CreatedAt: s.Now(), ExpiresAt: s.Now().Add(7 * 24 * time.Hour)}
	if e := s.DB.Create(&attr).Error; e != nil {
		t.Fatal(e)
	}
	return c, attr, secret
}
func sendSDK(t *testing.T, s *Server, a Application, secret string, event SDKEvent, origin string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(event)
	r := httptest.NewRequest("POST", "http://localhost:8088/api/sdk/apps/"+a.ID+"/events", bytes.NewReader(body))
	if secret != "" {
		ts := strconv.FormatInt(s.Now().Unix(), 10)
		r.Header.Set("X-D1-Time", ts)
		r.Header.Set("X-D1-Signature", sdkSignature(secret, ts, body))
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestAPISecretCanBeReadAgainByOwner(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	a := seedApp(t, s, u, 100)
	_, _, original := seedAds(t, s, a)
	cookie, csrf := sessionFor(t, s, u)
	path := "/api/apps/" + a.ID + "/ads"
	readSecret := func(w *httptest.ResponseRecorder) string {
		t.Helper()
		requireCode(t, w, 200)
		var out struct {
			Secret string `json:"api_secret"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(w.Body.String(), "facebook-secret") || strings.Contains(w.Body.String(), "tiktok-secret") {
			t.Fatal("unrelated advertising credentials were exposed")
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("credential response must not be cached")
		}
		return out.Secret
	}
	for range 3 {
		if got := readSecret(call(t, s, "GET", path, nil, cookie, "")); got != original {
			t.Fatal("previously generated key could not be read again")
		}
	}
	outsider := seedUser(t, s, 0)
	otherCookie, _ := sessionFor(t, s, outsider)
	for _, request := range []struct {
		cookie *http.Cookie
		status int
	}{{nil, 401}, {otherCookie, 404}} {
		w := call(t, s, "GET", path, nil, request.cookie, "")
		requireCode(t, w, request.status)
		if strings.Contains(w.Body.String(), original) {
			t.Fatal("key leaked to a different visitor")
		}
	}
	rotated := readSecret(call(t, s, "POST", path+"/rotate-key", map[string]any{}, cookie, csrf))
	if len(rotated) != 64 || rotated == original {
		t.Fatal("key was not replaced")
	}
	var saved AdConfig
	if err := s.DB.First(&saved, "app_id = ?", a.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.APISecret == rotated {
		t.Fatal("key stored without encryption")
	}
	if got := readSecret(call(t, s, "GET", path, nil, cookie, "")); got != rotated {
		t.Fatal("readback returned a stale key")
	}
	// Saving ordinary configuration must not replace the retained signing key.
	if got := readSecret(call(t, s, "PUT", path, map[string]any{"config": saved}, cookie, csrf)); got != rotated {
		t.Fatal("saving settings lost the key")
	}
	// Reopening the app on the same database has no dependency on process memory.
	reopened := NewServer(s.DB, s.Config)
	if got := readSecret(call(t, reopened, "GET", path, nil, cookie, "")); got != rotated {
		t.Fatal("key was not persisted")
	}
}

func TestSDKAuthorizationDedupAndConsent(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	a := seedApp(t, s, u, 100)
	_, attr, secret := seedAds(t, s, a)
	event := SDKEvent{AttributionToken: s.attributionToken(attr), Event: "register", EventID: "customer-register-10001", Consent: true, PageURL: "https://customer.example.com/success", ClientIP: "198.51.100.1", UserAgent: "Customer APK", ExternalID: "12345"}
	requireCode(t, sendSDK(t, s, a, secret, event, ""), 409)
	ticket, raw, visitor := seedTicket(t, s, a)
	s.DB.Model(&ticket).Update("attribution_id", attr.ID)
	requireCode(t, download(t, s, raw, visitor, "GET", ""), 200)
	requireCode(t, sendSDK(t, s, a, "wrong", event, ""), 401)
	requireCode(t, sendSDK(t, s, a, "", event, ""), 401)
	requireCode(t, sendSDK(t, s, a, secret, event, "https://evil.example.com"), 403)
	var wg sync.WaitGroup
	codes := make(chan int, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- sendSDK(t, s, a, secret, event, "").Code }()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 202 {
			t.Fatalf("concurrent event status %d", code)
		}
	}
	var n int64
	s.DB.Model(&Conversion{}).Where("event = ?", "register").Count(&n)
	if n != 1 {
		t.Fatalf("duplicate conversions %d", n)
	}
	if n = countRows(t, s, &Delivery{}); n != 4 {
		t.Fatalf("expected 2 download + 2 registration deliveries, got %d", n)
	}
	var saved Conversion
	s.DB.First(&saved, "event = ?", "register")
	if saved.IP != event.ClientIP || saved.UserAgent != event.UserAgent || saved.VerifiedBy != "server_signature" || saved.ExternalIDHash != hashString(event.ExternalID) {
		t.Fatalf("incorrect event %+v", saved)
	}
	noConsent := event
	noConsent.Event = "app_open"
	noConsent.EventID = "open-without-consent"
	noConsent.Consent = false
	requireCode(t, sendSDK(t, s, a, secret, noConsent, ""), 202)
	if countRows(t, s, &Delivery{}) != 4 {
		t.Fatal("event without consent was dispatched")
	}
	other := seedApp(t, s, u, 100)
	seedAds(t, s, other)
	requireCode(t, sendSDK(t, s, other, secret, event, ""), 400)
	if token, _ := s.encryptSecret("plain"); token == "plain" {
		t.Fatal("token stored in plaintext")
	} else if decrypted, e := s.decryptSecret(token); e != nil || decrypted != "plain" {
		t.Fatal("secret roundtrip failed")
	}
}
func TestAdProviderPayloadsAndDurableRetry(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	a := seedApp(t, s, u, 10)
	_, attr, _ := seedAds(t, s, a)
	event := Conversion{ID: randomHex(16), AppID: a.ID, UserID: u.ID, AttributionID: attr.ID, Event: "register", EventID: hashString("stable-event"), IP: "203.0.113.1", UserAgent: "WebView", SourceURL: "https://customer.example.com/success", Consent: true, VerifiedBy: "server_signature", EventTime: s.Now(), CreatedAt: s.Now()}
	if e := s.DB.Transaction(func(tx *gorm.DB) error { return s.createConversion(tx, &event, attr) }); e != nil {
		t.Fatal(e)
	}
	var fb Delivery
	s.DB.First(&fb, "platform = ?", "facebook")
	var tt Delivery
	s.DB.First(&tt, "platform = ?", "tiktok")
	failFirst := true
	s.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var p map[string]any
		if e := json.Unmarshal(body, &p); e != nil {
			t.Error(e)
		}
		data := p["data"].([]any)[0].(map[string]any)
		if data["event_id"] != event.EventID {
			t.Error("unstable event id")
		}
		if strings.Contains(string(body), "secret") || r.URL.Query().Get("access_token") != "" {
			t.Error("provider credential leaked in payload or URL")
		}
		if r.URL.Host == "graph.facebook.com" {
			if data["action_source"] != "website" || r.Header.Get("Authorization") != "Bearer facebook-secret" {
				t.Error("wrong Meta payload")
			}
			if failFirst {
				return jsonResponse(503, `{"error":{"code":2,"message":"temporary facebook-secret"}}`), nil
			}
			return jsonResponse(200, `{"events_received":1,"fbtrace_id":"fb-trace"}`), nil
		}
		if p["event_source"] != "web" || p["event_source_id"] != "TESTPIXEL1" || data["event"] != "CompleteRegistration" || r.Header.Get("Access-Token") != "tiktok-secret" {
			t.Error("wrong TikTok payload")
		}
		return jsonResponse(200, `{"code":0,"request_id":"tt-trace"}`), nil
	})}
	_, e := s.sendDelivery(context.Background(), fb)
	if e == nil || strings.Contains(e.Error(), "facebook-secret") {
		t.Fatal("error did not fail safely/redact")
	}
	id, e := s.sendDelivery(context.Background(), tt)
	if e != nil || id != "tt-trace" {
		t.Fatal(id, e)
	}
	// Force the Facebook task first; the failure must be persisted and delayed.
	s.DB.Model(&Delivery{}).Where("id = ?", tt.ID).Update("next_attempt_at", s.Now().Add(time.Hour))
	if !s.deliverOne(context.Background()) {
		t.Fatal("worker failed to claim")
	}
	s.DB.First(&fb, "id = ?", fb.ID)
	if fb.Status != "pending" || fb.Attempts != 1 || !fb.NextAttemptAt.After(s.Now()) || strings.Contains(fb.LastError, "facebook-secret") {
		t.Fatalf("bad retry %+v", fb)
	}
	failFirst = false
	s.DB.Model(&fb).Update("next_attempt_at", s.Now().Add(-time.Second))
	if !s.deliverOne(context.Background()) {
		t.Fatal("retry not claimed")
	}
	s.DB.First(&fb, "id = ?", fb.ID)
	if fb.Status != "sent" || fb.Attempts != 2 || fb.ProviderRequestID != "fb-trace" {
		t.Fatalf("retry not committed %+v", fb)
	}
}
func TestCompletionJournalRecovery(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	a := seedApp(t, s, u, 100)
	ticket, _, _ := seedTicket(t, s, a)
	if e := s.reserveDownload(ticket); e != nil {
		t.Fatal(e)
	}
	if _, e := s.writeReceipt(ticket, 0, 59); e != nil {
		t.Fatal(e)
	}
	if _, e := s.writeReceipt(ticket, 40, 99); e != nil {
		t.Fatal(e)
	}
	restarted := NewServer(s.DB, s.Config)
	restarted.replayCompletions(context.Background())
	restarted.replayCompletions(context.Background())
	if countRows(t, s, &Download{}) != 1 {
		t.Fatal("journal replay missing or duplicated completion")
	}
}
func TestBotVerificationAndSingleUseCaptcha(t *testing.T) {
	s := testServer(t)
	id := randomHex(16)
	s.DB.Create(&Challenge{ID: id, Hash: s.mac(id + ":12345"), ExpiresAt: s.Now().Add(time.Minute)})
	r := httptest.NewRequest("POST", "http://localhost:8088/api/auth/register", nil)
	in := authInput{CaptchaID: id, CaptchaCode: "12345"}
	if e := s.verifyBot(r, in, "register"); e != nil {
		t.Fatal(e)
	}
	if e := s.verifyBot(r, in, "register"); e == nil {
		t.Fatal("captcha reused")
	}
	s.Config.Env = "production"
	id = randomHex(16)
	s.DB.Create(&Challenge{ID: id, Hash: s.mac(id + ":12345"), ExpiresAt: s.Now().Add(time.Minute)})
	in.CaptchaID = id
	if e := s.verifyBot(r, in, "login"); e != nil {
		t.Fatal(e)
	}
}
