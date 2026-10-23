package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	c := Config{Env: "test", Role: "all", DataDir: t.TempDir(), PublicURL: "http://localhost:8088", CNAMEHost: "dns.d1apk.com", Secret: strings.Repeat("test-secret-", 4), MetaAPIVersion: "v25.0"}
	var db *gorm.DB
	var err error
	if raw := os.Getenv("TEST_DATABASE_URL"); raw != "" {
		admin, e := gorm.Open(postgres.Open(raw), &gorm.Config{})
		if e != nil {
			t.Fatal(e)
		}
		schema := "test_" + randomHex(8)
		if e = admin.Exec("CREATE SCHEMA " + schema).Error; e != nil {
			t.Fatal(e)
		}
		u, e := url.Parse(raw)
		if e != nil {
			t.Fatal(e)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		c.DatabaseURL = u.String()
		t.Cleanup(func() {
			_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
			sql, _ := admin.DB()
			_ = sql.Close()
		})
	}
	db, err = OpenDB(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = Migrate(db); err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	t.Cleanup(func() { _ = sql.Close() })
	s := NewServer(db, c)
	s.Blobs, err = NewBlobStore(c)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func seedUser(t *testing.T, s *Server, balance int64) User {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-password-12345"), bcrypt.MinCost)
	u := User{ID: randomHex(16), Email: randomHex(5) + "@example.com", Name: "测试客户", Role: "member", BalanceMicros: balance, PasswordHash: string(hash), CreatedAt: s.Now()}
	if e := s.DB.Create(&u).Error; e != nil {
		t.Fatal(e)
	}
	return u
}
func seedApp(t *testing.T, s *Server, u User, size int) Application {
	t.Helper()
	key := randomHex(16)
	body := bytes.Repeat([]byte("A"), size)
	if size > 2 {
		body[0] = 'M'
		body[1] = 'Z'
	}
	if e := os.WriteFile(filepath.Join(s.Config.DataDir, "files", key), body, 0600); e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(body)
	a := Application{ID: randomHex(16), UserID: u.ID, Slug: randomHex(6), Name: "测试应用", Version: "1.0.0", Platform: "Windows", FileKey: key, FileName: "sample.exe", Size: int64(size), SHA256: hex.EncodeToString(h[:]), Enabled: true}
	if e := s.DB.Create(&a).Error; e != nil {
		t.Fatal(e)
	}
	return a
}
func seedTicket(t *testing.T, s *Server, a Application) (Ticket, string, string) {
	t.Helper()
	raw := randomHex(32)
	visitor := randomHex(32)
	ticket := Ticket{Hash: hashString(raw), UserID: a.UserID, AppID: a.ID, FileKey: a.FileKey, VisitorHash: s.mac(visitor), Host: "localhost", ExpiresAt: s.Now().Add(time.Hour), IP: "203.0.113.12", UserAgent: "Mozilla/5.0 (Linux; Android 14; Mobile) Chrome/123", Source: "https://example.com/ad", Device: "手机", OS: "Android", Browser: "Chrome"}
	if e := s.DB.Create(&ticket).Error; e != nil {
		t.Fatal(e)
	}
	return ticket, raw, visitor
}
func sessionFor(t *testing.T, s *Server, u User) (*http.Cookie, string) {
	t.Helper()
	raw := randomHex(32)
	csrf := randomHex(32)
	if e := s.DB.Create(&Session{Hash: hashString(raw), UserID: u.ID, CSRF: csrf, ExpiresAt: s.Now().Add(time.Hour)}).Error; e != nil {
		t.Fatal(e)
	}
	return &http.Cookie{Name: s.cookieName(), Value: raw}, csrf
}
func call(t *testing.T, s *Server, method, path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, "http://localhost:8088"+path, reader)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func download(t *testing.T, s *Server, raw, visitor, method, rangeHeader string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://localhost:8088/d/"+raw, nil)
	r.AddCookie(&http.Cookie{Name: "d1_visitor", Value: visitor})
	r.Header.Set("Range", rangeHeader)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func countRows(t *testing.T, s *Server, model any) int64 {
	t.Helper()
	var n int64
	if e := s.DB.Model(model).Count(&n).Error; e != nil {
		t.Fatal(e)
	}
	return n
}
func requireCode(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
	}
}

func TestDownloadSuccessRangesAndMetadata(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 100000)
	a := seedApp(t, s, u, 10)
	ticket, raw, visitor := seedTicket(t, s, a)
	requireCode(t, download(t, s, raw, visitor, "HEAD", ""), 200)
	requireCode(t, download(t, s, raw, visitor, "GET", "bytes=100-110"), 416)
	if n := countRows(t, s, &Download{}); n != 0 {
		t.Fatal("probe billed")
	}
	requireCode(t, download(t, s, raw, visitor, "GET", "bytes=0-5"), 206)
	if n := countRows(t, s, &Download{}); n != 0 {
		t.Fatal("partial download marked successful")
	}
	var usage Usage
	s.DB.First(&usage)
	if usage.FreeReserved != 1 || usage.FreeUsed != 0 {
		t.Fatalf("bad hold %+v", usage)
	}
	requireCode(t, download(t, s, raw, visitor, "GET", "bytes=3-9"), 206)
	requireCode(t, download(t, s, raw, visitor, "GET", ""), 200)
	if n := countRows(t, s, &Download{}); n != 1 {
		t.Fatalf("expected exactly one completed download, got %d", n)
	}
	var d Download
	s.DB.First(&d)
	if d.IP != ticket.IP || d.UserAgent != ticket.UserAgent || d.Source != ticket.Source || d.Device != "手机" || d.Bytes != 10 || d.CompletedAt.Before(d.StartedAt) {
		t.Fatalf("incorrect metadata %+v", d)
	}
	s.DB.First(&usage)
	if usage.FreeReserved != 0 || usage.FreeUsed != 1 {
		t.Fatalf("bad settlement %+v", usage)
	}
}

type partialWriter struct {
	header http.Header
	sent   int
	status int
}

func (w *partialWriter) Header() http.Header  { return w.header }
func (w *partialWriter) WriteHeader(code int) { w.status = code }
func (w *partialWriter) Write(p []byte) (int, error) {
	if w.sent > 0 {
		return 0, io.ErrUnexpectedEOF
	}
	w.sent += len(p)
	return len(p), nil
}
func TestInterruptedPrefixCanResume(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	a := seedApp(t, s, u, 128<<10)
	ticket, raw, v := seedTicket(t, s, a)
	w := &partialWriter{header: make(http.Header)}
	r := httptest.NewRequest("GET", "http://localhost:8088/d/"+raw, nil)
	r.AddCookie(&http.Cookie{Name: "d1_visitor", Value: v})
	s.Handler().ServeHTTP(w, r)
	if w.sent <= 0 || w.sent >= int(a.Size) {
		t.Fatalf("fixture did not interrupt %d", w.sent)
	}
	if countRows(t, s, &Download{}) != 0 {
		t.Fatal("interrupted transfer counted successful")
	}
	requireCode(t, download(t, s, raw, v, "GET", fmt.Sprintf("bytes=%d-", w.sent)), 206)
	if countRows(t, s, &Download{}) != 1 {
		t.Fatal("successfully written prefix was not merged with resume")
	}
	var stored Ticket
	s.DB.First(&stored, "hash = ?", ticket.Hash)
	if !stored.Charged {
		t.Fatal("resumed completion not settled")
	}
}
func TestPaidReservationFailureAndExpiry(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 3000)
	a := seedApp(t, s, u, 100)
	s.DB.Model(&Plan{}).Where("id = ?", "free").Update("download_limit", 0)
	ticket, raw, visitor := seedTicket(t, s, a)
	requireCode(t, download(t, s, raw, visitor, "GET", "bytes=0-9"), 206)
	var after User
	s.DB.First(&after, "id = ?", u.ID)
	if after.BalanceMicros != 3000 || after.HeldMicros != 3000 || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("partial download was charged instead of held")
	}
	_, raw2, visitor2 := seedTicket(t, s, a)
	requireCode(t, download(t, s, raw2, visitor2, "GET", ""), 402)
	s.DB.Model(&Ticket{}).Where("hash = ?", ticket.Hash).Update("expires_at", s.Now().Add(-time.Minute))
	if e := s.releaseTicket(ticket); e != nil {
		t.Fatal(e)
	}
	if e := s.releaseTicket(ticket); e != nil {
		t.Fatal(e)
	}
	s.DB.First(&after, "id = ?", u.ID)
	if after.HeldMicros != 0 || after.BalanceMicros != 3000 {
		t.Fatal("failed download hold not released")
	}
	requireCode(t, download(t, s, raw2, visitor2, "GET", ""), 200)
	s.DB.First(&after, "id = ?", u.ID)
	if after.BalanceMicros != 0 || after.HeldMicros != 0 {
		t.Fatalf("unexpected final balance %+v", after)
	}
}

type failWriter struct {
	header http.Header
	status int
}

func (w *failWriter) Header() http.Header         { return w.header }
func (w *failWriter) WriteHeader(n int)           { w.status = n }
func (w *failWriter) Write(b []byte) (int, error) { return 0, io.ErrClosedPipe }
func TestConnectionFailureDoesNotCountSuccess(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	a := seedApp(t, s, u, 4096)
	_, raw, v := seedTicket(t, s, a)
	r := httptest.NewRequest("GET", "http://localhost:8088/d/"+raw, nil)
	r.AddCookie(&http.Cookie{Name: "d1_visitor", Value: v})
	s.Handler().ServeHTTP(&failWriter{header: make(http.Header)}, r)
	if countRows(t, s, &Download{}) != 0 {
		t.Fatal("failed transport counted")
	}
	requireCode(t, download(t, s, raw, v, "GET", ""), 200)
	if countRows(t, s, &Download{}) != 1 {
		t.Fatal("resumed transfer not counted")
	}
}
func TestConcurrentReservationsNeverOverdraw(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 15000)
	a := seedApp(t, s, u, 64)
	s.DB.Model(&Plan{}).Where("id = ?", "free").Update("download_limit", 3)
	tickets := []Ticket{}
	for i := 0; i < 32; i++ {
		x, _, _ := seedTicket(t, s, a)
		tickets = append(tickets, x)
	}
	var wg sync.WaitGroup
	var ok atomic.Int64
	for _, x := range tickets {
		wg.Add(1)
		go func(x Ticket) {
			defer wg.Done()
			if s.reserveDownload(x) == nil {
				ok.Add(1)
				if e := s.finishRange(x, 0, 63); e != nil {
					t.Error(e)
				}
			}
		}(x)
	}
	wg.Wait()
	if ok.Load() != 8 || countRows(t, s, &Download{}) != 8 {
		t.Fatalf("got %d approvals; expected 3 free + 5 paid", ok.Load())
	}
	var after User
	s.DB.First(&after, "id = ?", u.ID)
	if after.BalanceMicros != 0 || after.HeldMicros != 0 {
		t.Fatalf("balance invariant failed %+v", after)
	}
}
func TestMonthBoundaryUpgradeAndRenewal(t *testing.T) {
	s := testServer(t)
	now := time.Date(2026, 10, 31, 15, 59, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	u := seedUser(t, s, 1000*Dollar)
	a := seedApp(t, s, u, 8)
	s.DB.Model(&Plan{}).Where("id = ?", "free").Update("download_limit", 1)
	ticket, _, _ := seedTicket(t, s, a)
	if e := s.reserveDownload(ticket); e != nil {
		t.Fatal(e)
	}
	now = now.Add(2 * time.Minute)
	if e := s.finishRange(ticket, 0, 7); e != nil {
		t.Fatal(e)
	}
	var october Usage
	s.DB.First(&october, "user_id = ? AND period = ?", u.ID, "2026-10")
	if october.FreeUsed != 1 {
		t.Fatal("in-flight reservation settled in wrong month")
	}
	_, raw, v := seedTicket(t, s, a)
	requireCode(t, download(t, s, raw, v, "GET", ""), 200)
	cookie, csrf := sessionFor(t, s, u)
	key := randomHex(16)
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest("POST", "http://localhost:8088/api/subscription", strings.NewReader("{}"))
		r.AddCookie(cookie)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		requireCode(t, w, 200)
	}
	var after User
	s.DB.First(&after, "id = ?", u.ID)
	if after.BalanceMicros != 901*Dollar-3000 {
		t.Fatal("duplicate subscription purchase charged twice")
	}
	var november Usage
	s.DB.First(&november, "user_id = ? AND period = ?", u.ID, "2026-11")
	if november.FreeUsed != 0 || november.PaidUsed != 1 {
		t.Fatal("upgrade reset usage")
	}
	if nextMonth(time.Date(2027, 1, 31, 0, 0, 0, 0, billingZone)).In(billingZone).Day() != 28 {
		t.Fatal("anniversary not clamped")
	}
}
func TestTenantIsolationAndCSRF(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	other := seedUser(t, s, 0)
	a := seedApp(t, s, u, 10)
	cookie, csrf := sessionFor(t, s, other)
	requireCode(t, call(t, s, "GET", "/api/apps/"+a.ID, nil, cookie, csrf), 404)
	requireCode(t, call(t, s, "PATCH", "/api/apps/"+a.ID, map[string]any{"name": "stolen", "description": "", "version": "1", "enabled": true}, cookie, csrf), 404)
	requireCode(t, call(t, s, "GET", "/api/admin/users", nil, cookie, csrf), 403)
	requireCode(t, call(t, s, "POST", "/api/orders", map[string]string{"amount": "100", "method": "manual"}, cookie, "wrong"), 403)
	verified := s.Now()
	d := Domain{ID: randomHex(16), UserID: other.ID, Hostname: "downloads.customer.example", Token: randomHex(24), VerifiedAt: &verified}
	s.DB.Create(&d)
	r := httptest.NewRequest("GET", "https://downloads.customer.example/a/"+a.Slug, nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 404)
	r = httptest.NewRequest("GET", "https://downloads.customer.example/api/me", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 404)
}
func TestFutoonDuplicateAndMismatchedNotification(t *testing.T) {
	s := testServer(t)
	s.Config.FutoonPID = "merchant-test"
	s.Config.FutoonKey = "gateway-test-key"
	u := seedUser(t, s, 0)
	o := Order{ID: randomHex(16), UserID: u.ID, IdempotencyKey: randomHex(16), Method: "alipay", AmountMicros: 25 * Dollar, Status: "pending", ExpiresAt: s.Now().Add(time.Hour)}
	s.DB.Create(&o)
	notify := func(amount string) *httptest.ResponseRecorder {
		p := map[string]string{"pid": s.Config.FutoonPID, "type": "alipay", "out_trade_no": o.ID, "money": amount, "trade_no": "external-test-001", "trade_status": "TRADE_SUCCESS", "sign_type": "MD5"}
		p["sign"] = FutoonSign(p, s.Config.FutoonKey)
		q := url.Values{}
		for k, v := range p {
			q.Set(k, v)
		}
		return call(t, s, "GET", "/api/payments/futoon/notify?"+q.Encode(), nil, nil, "")
	}
	requireCode(t, notify("0.01"), 400)
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := notify("25.00")
			if w.Code != 200 {
				t.Errorf("notify failed %d %s", w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	if countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("duplicate callback credited twice")
	}
	var after User
	s.DB.First(&after, "id = ?", u.ID)
	if after.BalanceMicros != 25*Dollar {
		t.Fatal("wrong credit amount")
	}
}
func TestUSDTStrictMatchingAndTradeReplay(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	o := Order{ID: randomHex(16), UserID: u.ID, IdempotencyKey: randomHex(16), Method: "usdt", AmountMicros: 100 * Dollar, Status: "pending", USDTUnits: 14286001, Wallet: usdtContract, CreatedAt: s.Now().Add(-time.Minute), ExpiresAt: s.Now().Add(time.Minute)}
	s.DB.Create(&o)
	x := tronTransfer{TransactionID: strings.Repeat("a", 64), To: o.Wallet, Value: "14286001", Type: "Transfer", BlockTimestamp: s.Now().UnixMilli()}
	x.TokenInfo.Address = usdtContract
	x.TokenInfo.Decimals = 6
	if !validateTransfer(o, x) {
		t.Fatal("valid transfer rejected")
	}
	bad := x
	bad.BlockTimestamp = o.CreatedAt.Add(-time.Second).UnixMilli()
	if validateTransfer(o, bad) {
		t.Fatal("historical transfer accepted")
	}
	bad = x
	bad.Value = "14286000"
	if validateTransfer(o, bad) {
		t.Fatal("wrong amount accepted")
	}
	bad = x
	bad.TokenInfo.Address = "fake-token"
	if validateTransfer(o, bad) {
		t.Fatal("counterfeit token accepted")
	}
	if e := s.settle(o.ID, "tron:"+x.TransactionID, "usdt", o.AmountMicros); e != nil {
		t.Fatal(e)
	}
	o2 := o
	o2.ID = randomHex(16)
	o2.IdempotencyKey = randomHex(16)
	s.DB.Create(&o2)
	if e := s.settle(o2.ID, "tron:"+x.TransactionID, "usdt", o2.AmountMicros); e == nil {
		t.Fatal("same chain transfer paid another order")
	}
	if countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("replay credited")
	}
}
func TestTrustedProxyIP(t *testing.T) {
	s := testServer(t)
	_, block, _ := net.ParseCIDR("10.20.0.0/16")
	s.Config.TrustedProxies = []*net.IPNet{block}
	r := httptest.NewRequest("GET", "http://localhost/", nil)
	r.RemoteAddr = "198.51.100.2:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if s.clientIP(r) != "198.51.100.2" {
		t.Fatal("trusted spoofed forwarding header")
	}
	r.RemoteAddr = "10.20.0.5:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.99, 10.20.0.6")
	if s.clientIP(r) != "203.0.113.99" {
		t.Fatal("wrong proxy chain resolution")
	}
}
func TestRemoteStorageIntegrityAndRole(t *testing.T) {
	s := testServer(t)
	s.Config.Role = "download"
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	b := RemoteStore{Base: ts.URL, Secret: s.Config.Secret, HTTP: ts.Client()}
	f, e := os.CreateTemp(t.TempDir(), "package-")
	if e != nil {
		t.Fatal(e)
	}
	data := []byte("MZ-test-binary")
	_, _ = f.Write(data)
	_ = f.Close()
	sum := sha256.Sum256(data)
	key := randomHex(16)
	if e = b.Put(context.Background(), key, f.Name(), int64(len(data)), strings.Repeat("0", 64)); e == nil {
		t.Fatal("invalid checksum accepted")
	}
	if e = b.Put(context.Background(), key, f.Name(), int64(len(data)), hex.EncodeToString(sum[:])); e != nil {
		t.Fatal(e)
	}
	r, n, e := s.Blobs.Open(context.Background(), key)
	if e != nil || n != int64(len(data)) {
		t.Fatal("remote blob not stored")
	}
	_ = r.Close()
	if e = b.Delete(context.Background(), key); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Blobs.Open(context.Background(), key); e == nil {
		t.Fatal("remote delete failed")
	}
	requireCode(t, call(t, s, "GET", "/api/config", nil, nil, ""), 404)
}
func TestMoneyParser(t *testing.T) {
	for _, bad := range []string{"NaN", "0.0000001", "1e9", "-1", "100000000.00", "1.2.3", " 1", "01"} {
		if _, e := ParseMoney(bad); e == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if n, e := ParseMoney("0.002"); e != nil || n != 2000 {
		t.Fatal("fractional pricing lost precision")
	}
}

// Opt-in integration test: actual local TCP requests, 800 simultaneous clients,
// one tenant. This validates correctness, not production Internet bandwidth.
func TestConcurrentHTTPDownloads800(t *testing.T) {
	if os.Getenv("RUN_LOAD_TEST") != "1" {
		t.Skip("set RUN_LOAD_TEST=1 and TEST_DATABASE_URL for the 800-client run")
	}
	s := testServer(t)
	if s.DB.Dialector.Name() != "postgres" {
		t.Skip("requires PostgreSQL")
	}
	u := seedUser(t, s, 2*Dollar)
	a := seedApp(t, s, u, 64<<10)
	s.DB.Model(&Plan{}).Where("id = ?", "free").Update("download_limit", 50)
	type request struct{ raw, v string }
	reqs := make([]request, 800)
	tickets := make([]Ticket, 800)
	for i := range tickets {
		raw, v := randomHex(32), randomHex(32)
		reqs[i] = request{raw, v}
		tickets[i] = Ticket{Hash: hashString(raw), UserID: u.ID, AppID: a.ID, FileKey: a.FileKey, VisitorHash: s.mac(v), Host: "127.0.0.1", ExpiresAt: s.Now().Add(time.Hour)}
	}
	if e := s.DB.CreateInBatches(tickets, 100).Error; e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	transport := &http.Transport{MaxConnsPerHost: 1000, MaxIdleConns: 1000, MaxIdleConnsPerHost: 1000}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Minute}
	var wg sync.WaitGroup
	start := make(chan struct{})
	var success atomic.Int64
	begin := time.Now()
	for _, item := range reqs {
		wg.Add(1)
		go func(item request) {
			defer wg.Done()
			<-start
			r, _ := http.NewRequest("GET", server.URL+"/d/"+item.raw, nil)
			r.AddCookie(&http.Cookie{Name: "d1_visitor", Value: item.v})
			resp, e := client.Do(r)
			if e != nil {
				t.Error(e)
				return
			}
			n, e := io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if e == nil && resp.StatusCode == 200 && n == a.Size {
				success.Add(1)
			} else {
				t.Errorf("download status=%d bytes=%d err=%v", resp.StatusCode, n, e)
			}
		}(item)
	}
	close(start)
	wg.Wait()
	deadline := time.Now().Add(30 * time.Second)
	for countRows(t, s, &Download{}) != 800 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	elapsed := time.Since(begin)
	if success.Load() != 800 || countRows(t, s, &Download{}) != 800 {
		t.Fatalf("completed %d clients", success.Load())
	}
	var after User
	s.DB.First(&after, "id = ?", u.ID)
	if after.BalanceMicros != 500000 || after.HeldMicros != 0 {
		t.Fatalf("balance mismatch: %+v", after)
	}
	t.Logf("800 concurrent clients; completed=%d; file_bytes=%d; elapsed=%s; approximate_requests_per_second=%.1f; quota=50; paid=750; final_balance=0.500000", success.Load(), a.Size, elapsed, 800/elapsed.Seconds())
}

var _ = fmt.Sprintf
