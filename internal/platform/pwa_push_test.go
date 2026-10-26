package platform

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"golang.org/x/crypto/hkdf"
	"gorm.io/gorm"
)

func pushTestApp(t *testing.T, s *Server, u User) PWAApp {
	t.Helper()
	priv, pub, e := webpush.GenerateVAPIDKeys()
	if e != nil {
		t.Fatal(e)
	}
	encrypted, e := s.encryptSecret(priv)
	if e != nil {
		t.Fatal(e)
	}
	a := PWAApp{ID: randomHex(16), Slug: randomHex(8), UserID: u.ID, Name: "PWA 测试应用", TargetURL: "https://customer.example/app", Enabled: true, PushEnabled: true, ThemeColor: "#1677ff", VAPIDPublicKey: pub, VAPIDPrivateKey: encrypted, CreatedAt: s.Now(), UpdatedAt: s.Now()}
	if e = s.DB.Create(&a).Error; e != nil {
		t.Fatal(e)
	}
	return a
}
func pushTestInput(t *testing.T, endpoint string) (pwaSubscriptionInput, *ecdh.PrivateKey, []byte) {
	t.Helper()
	priv, e := ecdh.P256().GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	in := pwaSubscriptionInput{Endpoint: endpoint}
	in.Keys.P256DH = base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
	in.Keys.Auth = base64.RawURLEncoding.EncodeToString(auth)
	return in, priv, auth
}
func pushPublicCall(t *testing.T, s *Server, path string, body any, origin string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", s.Config.PublicURL+path, bytes.NewReader(raw))
	r.Header.Set("Origin", origin)
	r.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 14; Mobile) Chrome/123")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func pushTestSubscribe(t *testing.T, s *Server, a PWAApp, in pwaSubscriptionInput) (PWAPushSubscription, string) {
	t.Helper()
	w := pushPublicCall(t, s, "/p/"+a.Slug+"/subscribe", in, s.Config.PublicURL)
	requireCode(t, w, 200)
	var out struct{ SubscriptionID, Token string }
	var mapped map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &mapped)
	out.SubscriptionID = mapped["subscription_id"]
	out.Token = mapped["token"]
	var sub PWAPushSubscription
	if e := s.DB.First(&sub, "id = ?", out.SubscriptionID).Error; e != nil {
		t.Fatal(e)
	}
	if out.Token == "" {
		t.Fatal("missing subscription capability")
	}
	return sub, out.Token
}
func pushTaskCall(t *testing.T, s *Server, method, path string, in any, u User, key, csrfOverride string) *httptest.ResponseRecorder {
	t.Helper()
	cookie, csrf := sessionFor(t, s, u)
	if csrfOverride != "correct" {
		csrf = csrfOverride
	}
	var reader io.Reader
	if in != nil {
		raw, _ := json.Marshal(in)
		reader = bytes.NewReader(raw)
	}
	r := httptest.NewRequest(method, s.Config.PublicURL+path, reader)
	r.AddCookie(cookie)
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func pushTestTask(t *testing.T, s *Server, u User, a PWAApp, kind string, modify func(*pwaPushTaskInput)) PWAPushTask {
	t.Helper()
	in := pwaPushTaskInput{PWAID: a.ID, Title: "通知标题", Body: "通知正文", ScheduleType: kind}
	if modify != nil {
		modify(&in)
	}
	w := pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", in, u, randomHex(16), "correct")
	requireCode(t, w, 201)
	var view pwaPushTaskView
	if e := json.Unmarshal(w.Body.Bytes(), &view); e != nil {
		t.Fatal(e)
	}
	var task PWAPushTask
	if e := s.DB.First(&task, "id = ?", view.ID).Error; e != nil {
		t.Fatal(e)
	}
	return task
}
func pushExpand(t *testing.T, s *Server) {
	t.Helper()
	for range 1000 {
		ok, e := s.expandOnePWAPushTask(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		if !ok {
			return
		}
	}
	t.Fatal("audience expansion did not terminate")
}
func pushDeliveries(t *testing.T, s *Server, task PWAPushTask) []PWAPushDelivery {
	t.Helper()
	var rows []PWAPushDelivery
	if e := s.DB.Where("task_id = ?", task.ID).Order("id").Find(&rows).Error; e != nil {
		t.Fatal(e)
	}
	return rows
}
func pushFakeResponse(code int) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("provider response"))}
}

func TestPWAPushSubscriptionValidationAndProviderNetwork(t *testing.T) {
	in, _, _ := pushTestInput(t, "https://fcm.googleapis.com/wp/valid")
	if e := validatePWASubscription(&in); e != nil {
		t.Fatal(e)
	}
	for _, endpoint := range []string{"http://fcm.googleapis.com/wp/x", "https://127.0.0.1/x", "https://169.254.169.254/x", "https://fcm.googleapis.com.evil.example/x", "https://user:pass@fcm.googleapis.com/x", "https://fcm.googleapis.com:8443/x", "https://fcm.googleapis.com/x#secret", "https://fcm.googleapis.com/", "https://fcm.googleapis.com./x", "https://example.com/push"} {
		bad := in
		bad.Endpoint = endpoint
		if validatePWASubscription(&bad) == nil {
			t.Fatalf("accepted invalid endpoint %q", endpoint)
		}
	}
	for _, endpoint := range []string{"https://fcm.googleapis.com/fcm/send/x", "https://updates.push.services.mozilla.com/wpush/v2/x", "https://updates-autopush.stage.mozaws.push.services.mozilla.com/wpush/x", "https://web.push.apple.com/Qx", "https://wns2-by3p.notify.windows.com/w/?token=x"} {
		if !pwaPushProvider(endpoint) {
			t.Fatalf("missing browser provider %s", endpoint)
		}
	}
	for _, key := range []string{"", strings.Repeat("A", 87), base64.RawURLEncoding.EncodeToString(make([]byte, 65))} {
		bad := in
		bad.Keys.P256DH = key
		if validatePWASubscription(&bad) == nil {
			t.Fatal("accepted invalid P-256 key")
		}
	}
	for _, key := range []string{"", "bad", base64.RawURLEncoding.EncodeToString(make([]byte, 15))} {
		bad := in
		bad.Keys.Auth = key
		if validatePWASubscription(&bad) == nil {
			t.Fatal("accepted invalid auth key")
		}
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "172.16.1.1", "192.168.1.1", "169.254.169.254", "100.64.0.2", "0.0.0.0", "192.0.2.1", "198.51.100.1", "203.0.113.2", "198.18.1.2", "::1", "fc00::1", "fe80::1", "2001:db8::1", "::ffff:127.0.0.1"} {
		if pwaPublicIP(net.ParseIP(ip)) {
			t.Fatalf("allowed nonpublic IP %s", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "142.250.1.1", "2606:4700:4700::1111"} {
		if !pwaPublicIP(net.ParseIP(ip)) {
			t.Fatalf("rejected public IP %s", ip)
		}
	}
	s := testServer(t)
	client := s.securePWAPushClient()
	if client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("push transport must not inherit ambient HTTP proxy")
	}
	if client.CheckRedirect(&http.Request{}, nil) != http.ErrUseLastResponse {
		t.Fatal("push redirects allowed")
	}
	if _, e := client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "127.0.0.1:443"); e == nil {
		t.Fatal("dial reached a private nonprovider")
	}
}
func TestPWAPushPublicCapabilitiesAndFirstSubscriptionDedup(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	app := pushTestApp(t, s, u)
	otherApp := pushTestApp(t, s, u)
	rule := pushTestTask(t, s, u, app, "first_subscribe", nil)
	in, _, _ := pushTestInput(t, "https://fcm.googleapis.com/wp/one")
	path := "/p/" + app.Slug + "/subscribe"
	for _, origin := range []string{"", "null", "https://hostile.example"} {
		requireCode(t, pushPublicCall(t, s, path, in, origin), 403)
	}
	sub, token := pushTestSubscribe(t, s, app, in)
	if sub.Status != "active" || strings.Contains(sub.EncryptedSubscription, in.Endpoint) || strings.Contains(sub.EncryptedSubscription, in.Keys.Auth) {
		t.Fatal("subscription secrets stored in plaintext")
	}
	if rows := pushDeliveries(t, s, rule); len(rows) != 1 {
		t.Fatalf("welcome delivery count %d", len(rows))
	}
	sub2, token2 := pushTestSubscribe(t, s, app, in)
	if sub2.ID != sub.ID || token2 != token {
		t.Fatal("same-browser registration was not idempotent")
	}
	bad := in
	bad.Keys.Auth = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16))
	requireCode(t, pushPublicCall(t, s, path, bad, s.Config.PublicURL), 403)
	unsubscribe := map[string]string{"subscription_id": sub.ID, "token": "wrong"}
	requireCode(t, pushPublicCall(t, s, "/p/"+app.Slug+"/unsubscribe", unsubscribe, s.Config.PublicURL), 403)
	unsubscribe["token"] = token
	requireCode(t, pushPublicCall(t, s, "/p/"+otherApp.Slug+"/unsubscribe", unsubscribe, s.Config.PublicURL), 404)
	requireCode(t, pushPublicCall(t, s, "/p/"+app.Slug+"/unsubscribe", unsubscribe, s.Config.PublicURL), 200)
	requireCode(t, pushPublicCall(t, s, "/p/"+app.Slug+"/unsubscribe", unsubscribe, s.Config.PublicURL), 200)
	in.Token = token
	pushTestSubscribe(t, s, app, in)
	in.Endpoint = "https://fcm.googleapis.com/wp/rotated"
	rotated, _ := pushTestSubscribe(t, s, app, in)
	if rotated.ID != sub.ID {
		t.Fatal("browser endpoint renewal created a new welcome audience")
	}
	if countRows(t, s, &PWAPushSubscription{}) != 1 || len(pushDeliveries(t, s, rule)) != 1 {
		t.Fatal("opt out/re-subscribe replayed first-subscribe delivery")
	}
	w := pushTaskCall(t, s, "GET", "/api/pwa-push/subscriptions", nil, u, "", "correct")
	requireCode(t, w, 200)
	for _, secret := range []string{in.Endpoint, in.Keys.Auth, in.Keys.P256DH, token, "encrypted_subscription", "endpoint_hash"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("subscriber report exposed a capability")
		}
	}
	// A future welcome rule never retroactively queues existing subscribers.
	future := pushTestTask(t, s, u, app, "first_subscribe", nil)
	pushTestSubscribe(t, s, app, in)
	if len(pushDeliveries(t, s, future)) != 0 {
		t.Fatal("welcome rule backfilled an existing browser")
	}
	_ = s.DB.Model(&app).Update("push_enabled", false).Error
	requireCode(t, pushPublicCall(t, s, path, in, s.Config.PublicURL), 409)
}

func TestPWAPushAutomaticRenewalRespectsExplicitUnsubscribe(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	app := pushTestApp(t, s, u)
	rule := pushTestTask(t, s, u, app, "first_subscribe", nil)
	in, _, _ := pushTestInput(t, "https://fcm.googleapis.com/wp/original")
	sub, token := pushTestSubscribe(t, s, app, in)
	path := "/p/" + app.Slug + "/subscribe"
	unsubscribe := map[string]string{"subscription_id": sub.ID, "token": token}
	requireCode(t, pushPublicCall(t, s, "/p/"+app.Slug+"/unsubscribe", unsubscribe, s.Config.PublicURL), 200)

	// Both same-endpoint recovery and token-authenticated endpoint rotation
	// must preserve opt-out when initiated automatically by another window.
	in.Automatic = true
	for _, rotate := range []bool{false, true} {
		if rotate {
			in.Endpoint = "https://web.push.apple.com/rotated"
			in.Token = token
		}
		w := pushPublicCall(t, s, path, in, s.Config.PublicURL)
		requireCode(t, w, http.StatusConflict)
		var response map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response["code"] != "subscription_unsubscribed" {
			t.Fatalf("missing machine-readable opt-out response: %s", w.Body.String())
		}
		var unchanged PWAPushSubscription
		if err := s.DB.First(&unchanged, "id = ?", sub.ID).Error; err != nil {
			t.Fatal(err)
		}
		if unchanged.Status != "unsubscribed" || unchanged.EndpointHash != sub.EndpointHash || unchanged.EncryptedSubscription != sub.EncryptedSubscription || !unchanged.LastSeenAt.Equal(sub.LastSeenAt) {
			t.Fatal("automatic renewal modified an explicitly unsubscribed browser")
		}
	}

	// A deliberate click keeps the existing identity and cannot repeat welcome
	// messages, including when the browser supplies a new provider endpoint.
	in.Automatic = false
	restored, restoredToken := pushTestSubscribe(t, s, app, in)
	if restored.ID != sub.ID || restoredToken != token || restored.Status != "active" || restored.EndpointHash != hashString(in.Endpoint) {
		t.Fatal("manual re-subscription did not restore the original identity")
	}

	// Provider expiration is recoverable without a new permission gesture. Both
	// a current endpoint and a rotated endpoint still belong to this browser.
	in.Automatic = true
	for _, endpoint := range []string{in.Endpoint, "https://fcm.googleapis.com/wp/renewed"} {
		if err := s.DB.Model(&PWAPushSubscription{}).Where("id = ?", sub.ID).Update("status", "expired").Error; err != nil {
			t.Fatal(err)
		}
		in.Endpoint = endpoint
		renewed, renewedToken := pushTestSubscribe(t, s, app, in)
		if renewed.ID != sub.ID || renewedToken != token || renewed.Status != "active" || renewed.EndpointHash != hashString(endpoint) {
			t.Fatal("automatic renewal failed to recover an expired browser")
		}
		raw, err := s.decryptSecret(renewed.EncryptedSubscription)
		if err != nil || strings.Contains(raw, "automatic") {
			t.Fatal("request-only renewal mode persisted with subscription keys")
		}
	}
	if countRows(t, s, &PWAPushSubscription{}) != 1 || len(pushDeliveries(t, s, rule)) != 1 {
		t.Fatal("automatic/manual renewal repeated the first-subscription audience")
	}
}

func TestPWAPushAutomaticRenewalWaitsForConcurrentPostgresUnsubscribe(t *testing.T) {
	s := testServer(t)
	if s.DB.Dialector.Name() != "postgres" {
		t.Skip("PostgreSQL is required to verify subscription row locks")
	}
	u := seedUser(t, s, 0)
	app := pushTestApp(t, s, u)
	rule := pushTestTask(t, s, u, app, "first_subscribe", nil)
	in, _, _ := pushTestInput(t, "https://web.push.apple.com/concurrent-renewal")
	sub, token := pushTestSubscribe(t, s, app, in)
	in.Token, in.Automatic = token, true

	// Keep an actual opt-out UPDATE uncommitted so the handler must acquire
	// this same subscription's row lock before deciding whether to renew it.
	tx := s.DB.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	var finished <-chan struct{}
	defer func() {
		_ = tx.Rollback().Error
		if finished != nil {
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Error("automatic renewal did not finish after releasing the opt-out lock")
			}
		}
	}()
	var blockerPID int
	if err := tx.Raw("SELECT pg_backend_pid()").Scan(&blockerPID).Error; err != nil {
		t.Fatal(err)
	}
	if result := tx.Model(&PWAPushSubscription{}).Where("id = ?", sub.ID).Update("status", "unsubscribed"); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("could not hold the subscription opt-out row lock: %v", result.Error)
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", s.Config.PublicURL+"/p/"+app.Slug+"/subscribe", bytes.NewReader(raw))
	r.Header.Set("Origin", s.Config.PublicURL)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	finished = done
	go func() {
		defer close(done)
		s.Handler().ServeHTTP(w, r)
	}()

	// Observe the real SELECT FOR UPDATE waiting behind this transaction.
	// The relation OID resolves in this test's isolated schema; the blocker PID
	// ties the observed lock to the exact UPDATE above, never another test.
	watch, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		err := s.DB.WithContext(watch).Raw(`SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity a
			WHERE a.datname = current_database() AND a.usename = current_user
			  AND a.wait_event_type = 'Lock' AND a.query ILIKE '%FOR UPDATE%'
			  AND ? = ANY(pg_blocking_pids(a.pid))
			  AND EXISTS (SELECT 1 FROM pg_locks l WHERE l.pid = a.pid
			    AND l.relation = 'pwa_push_subscriptions'::regclass)
		)`, blockerPID).Scan(&waiting).Error
		if err != nil {
			t.Fatalf("could not observe the subscription SELECT FOR UPDATE: %v", err)
		}
		if waiting {
			break
		}
		select {
		case <-done:
			t.Fatal("automatic renewal returned without waiting for the subscription row lock")
		case <-watch.Done():
			t.Fatal("subscription SELECT FOR UPDATE did not enter a lock wait within three seconds")
		case <-ticker.C:
		}
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("automatic renewal remained blocked after opt-out committed")
	}
	requireCode(t, w, http.StatusConflict)
	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response["code"] != "subscription_unsubscribed" {
		t.Fatalf("concurrent opt-out did not produce the expected renewal rejection: %s", w.Body.String())
	}
	var final PWAPushSubscription
	if err := s.DB.First(&final, "id = ?", sub.ID).Error; err != nil {
		t.Fatal(err)
	}
	if final.Status != "unsubscribed" || countRows(t, s, &PWAPushSubscription{}) != 1 || len(pushDeliveries(t, s, rule)) != 1 {
		t.Fatal("concurrent automatic renewal changed opt-out or repeated the welcome delivery")
	}
}

func TestPWAPushTaskOwnershipValidationIdempotencyAndPagination(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	v := seedUser(t, s, 0)
	app := pushTestApp(t, s, u)
	otherApp := pushTestApp(t, s, v)
	in := pwaPushTaskInput{PWAID: app.ID, Title: "  Hello  ", Body: " Message ", ScheduleType: "now", TargetCountries: []string{"us", "CN", "US"}}
	path := "/api/pwa-push/tasks"
	key := randomHex(16)
	requireCode(t, pushTaskCall(t, s, "POST", path, in, u, key, ""), 403)
	requireCode(t, pushTaskCall(t, s, "POST", path, in, u, "", "correct"), 400)
	requireCode(t, pushTaskCall(t, s, "POST", path, in, v, key, "correct"), 404)
	w := pushTaskCall(t, s, "POST", path, in, u, key, "correct")
	requireCode(t, w, 201)
	var task pwaPushTaskView
	_ = json.Unmarshal(w.Body.Bytes(), &task)
	if task.Title != "Hello" || task.Body != "Message" || strings.Join(task.TargetCountries, ",") != "CN,US" || task.URL != s.Config.PublicURL+"/p/"+app.Slug+"/launch" {
		t.Fatalf("task not normalized %+v", task)
	}
	in.Title = "Hello"
	in.Body = "Message"
	in.TargetCountries = []string{"US", "CN"}
	requireCode(t, pushTaskCall(t, s, "POST", path, in, u, key, "correct"), 200)
	in.Title = "Changed"
	requireCode(t, pushTaskCall(t, s, "POST", path, in, u, key, "correct"), 409)
	if countRows(t, s, &PWAPushTask{}) != 1 {
		t.Fatal("request retry created duplicate broadcast")
	}
	for _, method := range []string{"GET", "DELETE"} {
		requireCode(t, pushTaskCall(t, s, method, path+"/"+task.ID, nil, v, "", "correct"), 404)
	}
	requireCode(t, pushTaskCall(t, s, "GET", path+"/"+task.ID+"/deliveries", nil, v, "", "correct"), 404)
	for _, mutate := range []func(*pwaPushTaskInput){func(i *pwaPushTaskInput) { i.Title = "" }, func(i *pwaPushTaskInput) { i.Title = strings.Repeat("字", 101) }, func(i *pwaPushTaskInput) { i.Body = strings.Repeat("文", 501) }, func(i *pwaPushTaskInput) { i.URL = "javascript:alert(1)" }, func(i *pwaPushTaskInput) { i.ScheduleType = "weekly" }, func(i *pwaPushTaskInput) { i.DelayMinutes = 1 }, func(i *pwaPushTaskInput) { i.TargetCountries = []string{"XX"} }, func(i *pwaPushTaskInput) { i.ScheduleType = "scheduled" }, func(i *pwaPushTaskInput) { i.ScheduleType = "first_subscribe"; i.DelayMinutes = 43201 }, func(i *pwaPushTaskInput) {
		past := s.Now().Add(-time.Minute)
		i.ScheduleType = "scheduled"
		i.ScheduledAt = &past
	}, func(i *pwaPushTaskInput) {
		future := s.Now().Add(366 * 24 * time.Hour)
		i.ScheduleType = "scheduled"
		i.ScheduledAt = &future
	}, func(i *pwaPushTaskInput) {
		i.Title = strings.Repeat("😀", 100)
		i.Body = strings.Repeat("😀", 500)
		i.URL = "https://example.com/" + strings.Repeat("a", 1800)
	}} {
		bad := pwaPushTaskInput{PWAID: app.ID, Title: "标题", Body: "正文", ScheduleType: "now"}
		mutate(&bad)
		requireCode(t, pushTaskCall(t, s, "POST", path, bad, u, randomHex(16), "correct"), 400)
	}
	// Pagination uses the existing pageParams offset correctly, including page 2.
	for range 2 {
		pushTestTask(t, s, u, app, "now", nil)
	}
	pushTestTask(t, s, v, otherApp, "now", nil)
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		w = pushTaskCall(t, s, "GET", fmt.Sprintf("%s?page=%d&per_page=1", path, page), nil, u, "", "correct")
		requireCode(t, w, 200)
		var out struct {
			Data  []pwaPushTaskView
			Total int64
			Page  int
		}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		if out.Total != 3 || out.Page != page || len(out.Data) != 1 || seen[out.Data[0].ID] {
			t.Fatalf("pagination incorrect %s", w.Body)
		}
		seen[out.Data[0].ID] = true
	}
	w = pushTaskCall(t, s, "GET", "/api/pwa-push/subscriptions?pwa_id="+otherApp.ID, nil, u, "", "correct")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatal("subscription tenant filter leaked another owner")
	}
	_ = s.DB.Model(&u).Update("disabled", true).Error
	requireCode(t, pushTaskCall(t, s, "POST", path, in, u, randomHex(16), "correct"), 401)
}

func decryptPushTestPayload(t *testing.T, r *http.Request, private *ecdh.PrivateKey, auth []byte) map[string]any {
	t.Helper()
	raw, e := io.ReadAll(r.Body)
	if e != nil || len(raw) < 102 {
		t.Fatal("invalid encrypted push")
	}
	keyLen := int(raw[20])
	if keyLen != 65 {
		t.Fatal("invalid sender public key")
	}
	pub, e := ecdh.P256().NewPublicKey(raw[21 : 21+keyLen])
	if e != nil {
		t.Fatal(e)
	}
	shared, e := private.ECDH(pub)
	if e != nil {
		t.Fatal(e)
	}
	info := append([]byte("WebPush: info\x00"), private.PublicKey().Bytes()...)
	info = append(info, pub.Bytes()...)
	ikm := make([]byte, 32)
	_, e = io.ReadFull(hkdf.New(sha256.New, shared, auth, info), ikm)
	if e != nil {
		t.Fatal(e)
	}
	key := make([]byte, 16)
	nonce := make([]byte, 12)
	_, _ = io.ReadFull(hkdf.New(sha256.New, ikm, raw[:16], []byte("Content-Encoding: aes128gcm\x00")), key)
	_, _ = io.ReadFull(hkdf.New(sha256.New, ikm, raw[:16], []byte("Content-Encoding: nonce\x00")), nonce)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	plain, e := gcm.Open(nil, nonce, raw[21+keyLen:], nil)
	if e != nil {
		t.Fatal(e)
	}
	plain = bytes.TrimRight(plain, "\x00")
	if len(plain) == 0 || plain[len(plain)-1] != 2 {
		t.Fatal("invalid payload delimiter")
	}
	plain = plain[:len(plain)-1]
	var payload map[string]any
	if e = json.Unmarshal(plain, &payload); e != nil {
		t.Fatal(e)
	}
	return payload
}
func TestPWAPushWorkerEncryptedPayloadVAPIDSnapshotAndEvents(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	a := pushTestApp(t, s, u)
	b := pushTestApp(t, s, u)
	in, private, auth := pushTestInput(t, "https://fcm.googleapis.com/wp/receiver-a")
	sub, _ := pushTestSubscribe(t, s, a, in)
	other, _, _ := pushTestInput(t, "https://web.push.apple.com/receiver-b")
	pushTestSubscribe(t, s, b, other)
	task := pushTestTask(t, s, u, a, "now", func(i *pwaPushTaskInput) { i.URL = "https://customer.example/notification" })
	otherTask := pushTestTask(t, s, u, b, "now", nil)
	pushExpand(t, s)
	// Editing app presentation later cannot silently change an already queued message.
	_ = s.DB.Model(&a).Updates(map[string]any{"name": "Changed", "target_url": "https://customer.example/changed"}).Error
	var events map[string]any
	sent := 0
	s.pwaPushHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		sent++
		if r.Method != "POST" || r.Header.Get("TTL") != "86400" || r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("Urgency") != "high" {
			t.Fatal("invalid Web Push request")
		}
		expected := b.VAPIDPublicKey
		if strings.HasSuffix(r.URL.Path, "receiver-a") {
			expected = a.VAPIDPublicKey
			events = decryptPushTestPayload(t, r, private, auth)
			if events["title"] != task.Title || events["body"] != task.Body || events["url"] != task.URL || events["icon"] != task.Icon {
				t.Fatal("payload snapshot changed")
			}
		}
		if !strings.Contains(r.Header.Get("Authorization"), expected) {
			t.Fatal("worker used a different app VAPID identity")
		}
		return pushFakeResponse(201), nil
	})}
	for range 2 {
		if !s.deliverOnePWAPush(context.Background()) {
			t.Fatal("delivery not claimed")
		}
	}
	if sent != 2 {
		t.Fatal("missing send")
	}
	if s.deliverOnePWAPush(context.Background()) {
		t.Fatal("accepted push resent")
	}
	if e := s.finishPWAPushTasks(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, task := range []PWAPushTask{task, otherTask} {
		var fresh PWAPushTask
		s.DB.First(&fresh, "id = ?", task.ID)
		if fresh.Status != "completed" {
			t.Fatal("finished task did not complete")
		}
	}
	delivery := pushDeliveries(t, s, task)[0]
	if delivery.SubscriptionID != sub.ID || delivery.Status != "sent" || delivery.DisplayedAt != nil || delivery.ClickedAt != nil {
		t.Fatal("provider acceptance counted as display/click")
	}
	event := map[string]any{"delivery_id": delivery.ID, "event": "displayed", "token": "wrong"}
	requireCode(t, pushPublicCall(t, s, "/p/"+a.Slug+"/event", event, s.Config.PublicURL), 403)
	event["token"] = events["event_token"]
	requireCode(t, pushPublicCall(t, s, "/p/"+b.Slug+"/event", event, s.Config.PublicURL), 404)
	requireCode(t, pushPublicCall(t, s, "/p/"+a.Slug+"/event", event, s.Config.PublicURL), 200)
	var first PWAPushDelivery
	s.DB.First(&first, "id = ?", delivery.ID)
	if first.DisplayedAt == nil {
		t.Fatal("display not recorded")
	}
	requireCode(t, pushPublicCall(t, s, "/p/"+a.Slug+"/event", event, s.Config.PublicURL), 200)
	event["event"] = "clicked"
	requireCode(t, pushPublicCall(t, s, "/p/"+a.Slug+"/event", event, s.Config.PublicURL), 200)
	var latest PWAPushDelivery
	s.DB.First(&latest, "id = ?", delivery.ID)
	if latest.ClickedAt == nil || !latest.DisplayedAt.Equal(*first.DisplayedAt) {
		t.Fatal("event replay changed first timestamp")
	}
	views, e := s.pwaPushTaskViews([]PWAPushTask{task})
	if e != nil || views[0].SentCount != 1 || views[0].DisplayedCount != 1 || views[0].ClickedCount != 1 {
		t.Fatalf("wrong task metrics %+v %v", views, e)
	}
}
func TestPWAPushScheduledCountryWelcomeDelayCancellationAndDisabled(t *testing.T) {
	s := testServer(t)
	now := s.Now().Truncate(time.Second)
	s.Now = func() time.Time { return now }
	u := seedUser(t, s, 0)
	a := pushTestApp(t, s, u)
	in, _, _ := pushTestInput(t, "https://fcm.googleapis.com/wp/timing")
	sub, _ := pushTestSubscribe(t, s, a, in)
	_ = s.DB.Model(&sub).Update("country", "US").Error
	due := now.Add(time.Hour)
	input := pwaPushTaskInput{PWAID: a.ID, Title: "Scheduled", Body: "Only US", ScheduleType: "scheduled", ScheduledAt: &due, TargetCountries: []string{"US"}}
	key := randomHex(16)
	w := pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", input, u, key, "correct")
	requireCode(t, w, 201)
	var task PWAPushTask
	_ = json.Unmarshal(w.Body.Bytes(), &task)
	pushExpand(t, s)
	if len(pushDeliveries(t, s, task)) != 0 {
		t.Fatal("scheduled task expanded before due")
	}
	now = due.Add(time.Second)
	pushExpand(t, s)
	if len(pushDeliveries(t, s, task)) != 1 {
		t.Fatal("due country audience missing")
	}
	requireCode(t, pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", input, u, key, "correct"), 200)
	exclude := pushTestTask(t, s, u, a, "now", func(i *pwaPushTaskInput) { i.TargetCountries = []string{"CN"} })
	pushExpand(t, s)
	if len(pushDeliveries(t, s, exclude)) != 0 {
		t.Fatal("country filter ignored")
	}
	unknown, _, _ := pushTestInput(t, "https://fcm.googleapis.com/wp/unknown")
	pushTestSubscribe(t, s, a, unknown)
	filtered := pushTestTask(t, s, u, a, "now", func(i *pwaPushTaskInput) { i.TargetCountries = []string{"US"} })
	pushExpand(t, s)
	if len(pushDeliveries(t, s, filtered)) != 1 {
		t.Fatal("unknown country entered filtered audience")
	}
	// A welcome delay may exceed a day: its retry expiry starts at delivery due,
	// rather than the subscription date.
	welcome := pushTestTask(t, s, u, a, "first_subscribe", func(i *pwaPushTaskInput) { i.DelayMinutes = 2 * 24 * 60 })
	newInput, _, _ := pushTestInput(t, "https://fcm.googleapis.com/wp/new-welcome")
	pushTestSubscribe(t, s, a, newInput)
	rows := pushDeliveries(t, s, welcome)
	if len(rows) != 1 || !rows[0].NextAttemptAt.Equal(now.Add(48*time.Hour)) || !rows[0].ExpiresAt.Equal(now.Add(72*time.Hour)) {
		t.Fatal("welcome schedule/retry deadline wrong")
	}
	requireCode(t, pushTaskCall(t, s, "DELETE", "/api/pwa-push/tasks/"+task.ID, nil, u, "", "correct"), 200)
	requireCode(t, pushTaskCall(t, s, "DELETE", "/api/pwa-push/tasks/"+task.ID, nil, u, "", "correct"), 200)
	if pushDeliveries(t, s, task)[0].Status != "cancelled" {
		t.Fatal("cancel left pending delivery")
	}
	_ = s.DB.Model(&a).Update("push_enabled", false).Error
	called := 0
	s.pwaPushHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { called++; return pushFakeResponse(201), nil })}
	if !s.deliverOnePWAPush(context.Background()) {
		t.Fatal("disabled queued delivery not resolved")
	}
	if called != 0 {
		t.Fatal("disabled app was sent notification")
	}
	_ = s.DB.Model(&a).Update("push_enabled", true).Error
	_ = s.DB.Model(&u).Update("disabled", true).Error
	now = now.Add(48 * time.Hour)
	if !s.deliverOnePWAPush(context.Background()) {
		t.Fatal("disabled owner delivery not resolved")
	}
	if called != 0 {
		t.Fatal("disabled owner was sent notification")
	}
}
func TestPWAPushRetryExpiryLeaseFenceAndConcurrency(t *testing.T) {
	s := testServer(t)
	now := s.Now().Truncate(time.Second)
	s.Now = func() time.Time { return now }
	u := seedUser(t, s, 0)
	a := pushTestApp(t, s, u)
	in, _, _ := pushTestInput(t, "https://fcm.googleapis.com/wp/retry")
	sub, _ := pushTestSubscribe(t, s, a, in)
	task := pushTestTask(t, s, u, a, "now", nil)
	pushExpand(t, s)
	sends := 0
	s.pwaPushHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		sends++
		response := pushFakeResponse(429)
		response.Header.Set("Retry-After", "180")
		return response, nil
	})}
	if !s.deliverOnePWAPush(context.Background()) {
		t.Fatal("missing first claim")
	}
	d := pushDeliveries(t, s, task)[0]
	if d.Status != "pending" || d.Attempts != 1 || !d.NextAttemptAt.Equal(now.Add(3*time.Minute)) {
		t.Fatal("429 retry backoff ignored")
	}
	if s.deliverOnePWAPush(context.Background()) {
		t.Fatal("retry ignored future due")
	}
	now = d.NextAttemptAt
	old, e := s.claimPWAPushDelivery(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.claimPWAPushDelivery(context.Background()); e == nil {
		t.Fatal("active lease claimed twice")
	}
	now = now.Add(61 * time.Second)
	fresh, e := s.claimPWAPushDelivery(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if fresh.LeaseToken == old.LeaseToken {
		t.Fatal("stale lease not fenced")
	}
	if e = s.finishPWAPushDelivery(context.Background(), old, pwaPushOutcome{accepted: true}); e != nil {
		t.Fatal(e)
	}
	var current PWAPushDelivery
	s.DB.First(&current, "id = ?", d.ID)
	if current.Status != "processing" || current.LeaseToken != fresh.LeaseToken {
		t.Fatal("late worker overwrote new lease")
	}
	if e = s.finishPWAPushDelivery(context.Background(), fresh, pwaPushOutcome{retryable: true, message: "temporary"}); e != nil {
		t.Fatal(e)
	}
	for range 10 {
		var latest PWAPushDelivery
		s.DB.First(&latest, "id = ?", d.ID)
		if latest.Status == "failed" {
			break
		}
		now = latest.NextAttemptAt
		s.deliverOnePWAPush(context.Background())
	}
	current = PWAPushDelivery{}
	s.DB.First(&current, "id = ?", d.ID)
	if current.Status != "failed" || current.Attempts != 5 {
		t.Fatalf("retry not bounded: %+v", current)
	}
	for _, code := range []int{404, 410} {
		freshTask := pushTestTask(t, s, u, a, "now", nil)
		pushExpand(t, s)
		s.pwaPushHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return pushFakeResponse(code), nil })}
		if !s.deliverOnePWAPush(context.Background()) {
			t.Fatal("expired sub delivery missing")
		}
		var after PWAPushSubscription
		s.DB.First(&after, "id = ?", sub.ID)
		if after.Status != "expired" || pushDeliveries(t, s, freshTask)[0].Status != "failed" {
			t.Fatal("404/410 did not expire subscription")
		}
		_ = s.DB.Model(&sub).Update("status", "active").Error
	}
	// Multiple workers never acquire the same unexpired queue lease.
	finalTask := pushTestTask(t, s, u, a, "now", nil)
	pushExpand(t, s)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := []string{}
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, e := s.claimPWAPushDelivery(context.Background())
			if e == nil {
				mu.Lock()
				ids = append(ids, claimed.ID)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(ids) != 1 || ids[0] != pushDeliveries(t, s, finalTask)[0].ID {
		t.Fatalf("concurrent lease acquired %v", ids)
	}
	if sends < 1 {
		t.Fatal("retry test did not contact mocked provider")
	}
}

func TestPWAPushLongWelcomeDelayAndRenewalExpiryFence(t *testing.T) {
	s := testServer(t)
	now := s.Now().Truncate(time.Second)
	s.Now = func() time.Time { return now }
	u := seedUser(t, s, 0)
	a := pushTestApp(t, s, u)
	rule := pushTestTask(t, s, u, a, "first_subscribe", func(i *pwaPushTaskInput) { i.DelayMinutes = 2 * 24 * 60 })
	in, _, _ := pushTestInput(t, "https://fcm.googleapis.com/wp/delayed")
	sub, token := pushTestSubscribe(t, s, a, in)
	sends := 0
	s.pwaPushHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { sends++; return pushFakeResponse(201), nil })}
	if s.deliverOnePWAPush(context.Background()) {
		t.Fatal("welcome delivered before due")
	}
	now = now.Add(48 * time.Hour)
	if !s.deliverOnePWAPush(context.Background()) || sends != 1 {
		t.Fatal("two day welcome expired before first attempt")
	}
	if pushDeliveries(t, s, rule)[0].Status != "sent" {
		t.Fatal("long welcome not accepted")
	}
	next := pushTestTask(t, s, u, a, "now", nil)
	pushExpand(t, s)
	claimed, e := s.claimPWAPushDelivery(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	s.pwaPushHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		// A push endpoint may rotate while the previous endpoint HTTP request is in
		// flight. Its terminal 410 must not revoke the newly registered endpoint.
		in.Token = token
		in.Endpoint = "https://fcm.googleapis.com/wp/renewed"
		pushTestSubscribe(t, s, a, in)
		return pushFakeResponse(410), nil
	})}
	outcome := s.sendPWAPush(context.Background(), claimed)
	if !outcome.expired {
		t.Fatal("expected terminal provider response")
	}
	if e = s.finishPWAPushDelivery(context.Background(), claimed, outcome); e != nil {
		t.Fatal(e)
	}
	var after PWAPushSubscription
	s.DB.First(&after, "id = ?", sub.ID)
	if after.Status != "active" {
		t.Fatal("stale provider410 expired a renewed subscription")
	}
	if pushDeliveries(t, s, next)[0].Status != "failed" {
		t.Fatal("old endpoint failure not recorded")
	}
}

func TestPWAPushDefaultTargetUsesExactLoopbackOrigin(t *testing.T) {
	for _, origin := range []string{"http://localhost:8088", "http://127.0.0.1:5189", "http://[::1]:8088"} {
		in := pwaPushTaskInput{Title: "Hello", Body: "Message", ScheduleType: "now"}
		if err := normalizedPWATaskInput(&in, PWAApp{Slug: "0123456789abcdef"}, time.Now(), origin); err != nil {
			t.Fatalf("local default target rejected for %s: %v", origin, err)
		}
		if in.URL != origin+"/p/0123456789abcdef/launch" {
			t.Fatal("default target changed origin")
		}
	}
	for _, origin := range []string{"http://localhost.attacker.example", "http://example.com", "http://192.168.1.1"} {
		in := pwaPushTaskInput{Title: "Hello", Body: "Message", ScheduleType: "now"}
		if err := normalizedPWATaskInput(&in, PWAApp{Slug: "0123456789abcdef"}, time.Now(), origin); err == nil {
			t.Fatalf("non-loopback HTTP default accepted for %s", origin)
		}
	}
}

func TestPWAPushTaskAuditRollback(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	a := pushTestApp(t, s, u)
	failAudit := func() {
		s.DB.Callback().Create().Before("gorm:create").Register("test:pwa-push-audit-fail", func(tx *gorm.DB) {
			if _, ok := tx.Statement.Dest.(*Audit); ok {
				tx.AddError(errors.New("forced audit failure"))
			}
		})
	}
	removeAudit := func() { s.DB.Callback().Create().Remove("test:pwa-push-audit-fail") }
	failAudit()
	input := pwaPushTaskInput{PWAID: a.ID, Title: "Hi", Body: "Body", ScheduleType: "now"}
	key := randomHex(16)
	requireCode(t, pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", input, u, key, "correct"), 500)
	if countRows(t, s, &PWAPushTask{}) != 0 {
		t.Fatal("failed audit left a broadcast task")
	}
	removeAudit()
	in, _, _ := pushTestInput(t, "https://fcm.googleapis.com/wp/audit")
	pushTestSubscribe(t, s, a, in)
	w := pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", input, u, key, "correct")
	requireCode(t, w, 201)
	var task PWAPushTask
	_ = json.Unmarshal(w.Body.Bytes(), &task)
	pushExpand(t, s)
	failAudit()
	requireCode(t, pushTaskCall(t, s, "DELETE", "/api/pwa-push/tasks/"+task.ID, nil, u, "", "correct"), 500)
	removeAudit()
	var fresh PWAPushTask
	s.DB.First(&fresh, "id = ?", task.ID)
	if fresh.Status != "sending" || pushDeliveries(t, s, task)[0].Status != "pending" {
		t.Fatal("failed cancellation audit partially changed queue")
	}
	requireCode(t, pushTaskCall(t, s, "DELETE", "/api/pwa-push/tasks/"+task.ID, nil, u, "", "correct"), 200)
}
