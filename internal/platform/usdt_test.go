package platform

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestUZFConfigurationRequiresPrivateTransportAndHashedSecondarySecret(t *testing.T) {
	c := Config{Env: "production", PublicURL: "https://d1apk.com", Secret: strings.Repeat("s", 32), DatabaseURL: "postgres://unused/test", ClamAVAddr: "127.0.0.1:3310", USDTProvider: "uzf", USDTMonitorURL: "http://127.0.0.1:16000/api/check", USDTMonitorSecret: strings.Repeat("k", 32)}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"http://monitor.example/api/check", "http://localhost:16000/api/check", "https://user:pass@monitor.example/api/check", "https://monitor.example/api/check?secret=bad"} {
		bad := c
		bad.USDTMonitorURL = endpoint
		if bad.Validate() == nil {
			t.Fatal("unsafe payment monitor transport accepted")
		}
	}
	c.USDTAddressPasswordHash = "not-a-hash"
	if c.Validate() == nil {
		t.Fatal("plaintext secondary password configuration accepted")
	}
}

func TestUZFLegacyHTTPRequiresExplicitScopedOptIn(t *testing.T) {
	c := Config{Env: "production", PublicURL: "https://d1apk.com", Secret: strings.Repeat("s", 32), DatabaseURL: "postgres://unused/test", ClamAVAddr: "127.0.0.1:3310", USDTProvider: "uzf", USDTMonitorURL: "http://monitor.example/api/check", USDTMonitorSecret: "legacy-secret"}
	if c.Validate() == nil {
		t.Fatal("public HTTP accepted without explicit operator opt-in")
	}
	c.USDTMonitorAllowHTTP = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"http://user:pass@monitor.example/api/check", "http://monitor.example/api/check?secret=bad", "ftp://monitor.example/api/check", "http://monitor.example/api/check#fragment"} {
		bad := c
		bad.USDTMonitorURL = endpoint
		if bad.Validate() == nil {
			t.Fatal("HTTP compatibility bypassed endpoint validation")
		}
	}
	bad := c
	bad.USDTProvider = "tron"
	if bad.Validate() == nil {
		t.Fatal("UZF opt-in changed the legacy TRON monitor policy")
	}
	bad = c
	bad.TronAPIURL = c.USDTMonitorURL
	if bad.Validate() == nil {
		t.Fatal("UZF opt-in relaxed the TRON API, even at the same URL")
	}
	bad = c
	bad.FutoonURL, bad.FutoonPID, bad.FutoonKey = c.USDTMonitorURL, "test", "test"
	if bad.Validate() == nil {
		t.Fatal("UZF opt-in relaxed Futoon transport")
	}
}

func TestUZFLegacyHTTPEnvironmentFlag(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("APP_SECRET", strings.Repeat("s", 32))
	t.Setenv("USDT_MONITOR_ALLOW_HTTP", "")
	c, err := LoadConfig()
	if err != nil || c.USDTMonitorAllowHTTP {
		t.Fatalf("HTTP compatibility must default off: %v", err)
	}
	t.Setenv("USDT_MONITOR_ALLOW_HTTP", "true")
	c, err = LoadConfig()
	if err != nil || !c.USDTMonitorAllowHTTP {
		t.Fatalf("explicit HTTP compatibility flag not loaded: %v", err)
	}
}

func TestUSDTWalletRequiresSecondaryPasswordAndCannotBypass(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	memberCookie, memberCSRF := sessionFor(t, s, u)
	body := map[string]string{"wallet": usdtContract, "secondary_password": "test-secondary-secret"}
	requireCode(t, call(t, s, "PUT", "/api/admin/usdt-wallet", body, memberCookie, memberCSRF), 403)
	s.DB.Model(&u).Update("role", "admin")
	cookie, csrf := sessionFor(t, s, u)
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-secondary-secret"), bcrypt.MinCost)
	s.Config.USDTAddressPasswordHash = string(hash)
	body["secondary_password"] = "wrong"
	requireCode(t, call(t, s, "PUT", "/api/admin/usdt-wallet", body, cookie, csrf), 403)
	settings, _ := getSettings(s.DB)
	if settings.USDTWallet != nil {
		t.Fatal("wrong password changed the wallet")
	}
	body["secondary_password"] = "test-secondary-secret"
	requireCode(t, call(t, s, "PUT", "/api/admin/usdt-wallet", body, cookie, "wrong-csrf"), 403)
	requireCode(t, call(t, s, "PUT", "/api/admin/usdt-wallet", body, cookie, csrf), 200)
	settings, _ = getSettings(s.DB)
	if settings.USDTWallet == nil || *settings.USDTWallet != usdtContract {
		t.Fatal("verified wallet was not saved")
	}
	var plans []Plan
	s.DB.Find(&plans)
	draft := settings
	malicious := "TThisAddressMustNeverBeAccepted"
	draft.USDTWallet = &malicious
	requireCode(t, call(t, s, "PUT", "/api/admin/settings", map[string]any{"settings": draft, "plans": plans}, cookie, csrf), 200)
	settings, _ = getSettings(s.DB)
	if *settings.USDTWallet != usdtContract {
		t.Fatal("general settings endpoint bypassed secondary verification")
	}
	// New orders snapshot the database address, not the legacy environment value.
	s.Config.USDTWallet = "legacy-environment-address"
	create := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://localhost:8088/api/orders", bytes.NewBufferString(`{"amount":"10","method":"usdt"}`))
		r.AddCookie(cookie)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", randomHex(16))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	w := create()
	requireCode(t, w, 201)
	var order Order
	json.Unmarshal(w.Body.Bytes(), &order)
	if order.Wallet != usdtContract {
		t.Fatal("order ignored configured receiving address")
	}
	body["wallet"] = "bad-address"
	requireCode(t, call(t, s, "PUT", "/api/admin/usdt-wallet", body, cookie, csrf), 400)
	body["wallet"] = ""
	requireCode(t, call(t, s, "PUT", "/api/admin/usdt-wallet", body, cookie, csrf), 200)
	requireCode(t, create(), 503)
	s.DB.First(&order, "id = ?", order.ID)
	if order.Wallet != usdtContract {
		t.Fatal("address change rewrote an existing order")
	}
	var audits []Audit
	s.DB.Find(&audits)
	raw, _ := json.Marshal(audits)
	if strings.Contains(string(raw), "test-secondary-secret") || strings.Contains(string(raw), string(hash)) {
		t.Fatal("secondary secret leaked into audit log")
	}
}

func TestUZFReceiptAndReplayProtection(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	o := Order{ID: randomHex(16), UserID: u.ID, IdempotencyKey: randomHex(16), Method: "usdt", USDTProvider: "uzf", AmountMicros: 100 * Dollar, BonusMicros: 15 * Dollar, Status: "pending", USDTUnits: 100001234, Wallet: usdtContract, CreatedAt: s.Now().Add(-time.Minute), ExpiresAt: s.Now().Add(time.Minute)}
	s.DB.Create(&o)
	valid := uzfReceipt{BillID: "3020596814559830016", Amount: "100.001234", Currency: "USDT", BillTimestamp: s.Now().UnixMilli()}
	var proof atomic.Value
	proof.Store(valid)
	var found, unavailable, omitProof atomic.Bool
	s.Config.USDTMonitorSecret = strings.Repeat("test", 8)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unavailable.Load() {
			w.WriteHeader(503)
			return
		}
		q := r.URL.Query()
		params := map[string]string{"amount": q.Get("amount"), "currency": q.Get("currency")}
		if r.URL.Path != "/api/check" || len(q) != 4 || q.Get("signature") != MonitorSign(params, q.Get("timestamp"), s.Config.USDTMonitorSecret) || q.Get("amount") != "100.001234" || q.Get("currency") != "USDT" {
			t.Error("request does not match original go7/UZF protocol")
		}
		var transfer any = proof.Load()
		if omitProof.Load() {
			transfer = nil
		}
		jsonOut(w, 200, map[string]any{"success": true, "data": map[string]any{"found": found.Load(), "transfer": transfer}})
	}))
	defer provider.Close()
	s.Config.USDTMonitorURL = provider.URL + "/api/check"
	s.HTTP = provider.Client()
	if err := s.reconcileUSDT(t.Context(), o); err != nil {
		t.Fatal(err)
	}
	if countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("missing payment credited")
	}
	found.Store(true)
	for name, mutate := range map[string]func(*uzfReceipt){
		"amount":       func(p *uzfReceipt) { p.Amount = "100.001235" },
		"currency":     func(p *uzfReceipt) { p.Currency = "USDC" },
		"history":      func(p *uzfReceipt) { p.BillTimestamp = o.CreatedAt.Add(-time.Second).UnixMilli() },
		"late":         func(p *uzfReceipt) { p.BillTimestamp = o.ExpiresAt.Add(time.Second).UnixMilli() },
		"missing-bill": func(p *uzfReceipt) { p.BillID = "" },
		"missing-time": func(p *uzfReceipt) { p.BillTimestamp = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := valid
			mutate(&bad)
			proof.Store(bad)
			if s.reconcileUSDT(t.Context(), o) == nil || countRows(t, s, &Ledger{}) != 0 {
				t.Fatal("invalid receipt was accepted")
			}
		})
	}
	proof.Store(valid)
	omitProof.Store(true)
	if s.reconcileUSDT(t.Context(), o) == nil {
		t.Fatal("bare found=true without a bill was credited")
	}
	omitProof.Store(false)
	unavailable.Store(true)
	if s.reconcileUSDT(t.Context(), o) == nil {
		t.Fatal("offline monitor accepted")
	}
	unavailable.Store(false)
	for range 2 {
		if err := s.reconcileUSDT(t.Context(), o); err != nil {
			t.Fatal(err)
		}
	}
	s.DB.First(&u, "id = ?", u.ID)
	if u.BalanceMicros != 115*Dollar || countRows(t, s, &Ledger{}) != 2 {
		t.Fatal("principal/bonus were not credited exactly once")
	}
	other := o
	other.ID, other.IdempotencyKey = randomHex(16), randomHex(16)
	s.DB.Create(&other)
	if s.reconcileUSDT(t.Context(), other) == nil || countRows(t, s, &Ledger{}) != 2 {
		t.Fatal("same receipt funded another order")
	}
}

func TestOriginalUZFAmountNumberAndStringCompatibility(t *testing.T) {
	o := Order{USDTUnits: 12340000, CreatedAt: time.UnixMilli(1000), ExpiresAt: time.UnixMilli(3000)}
	for _, amount := range []string{`12.34`, `"12.34"`} {
		var result uzfResult
		if err := json.Unmarshal([]byte(`{"success":true,"data":{"found":true,"transfer":{"bill_id":"bill-123","amount":`+amount+`,"currency":"USDT","bill_timestamp":2000}}}`), &result); err != nil {
			t.Fatal(err)
		}
		if result.Data.Transfer == nil || !validUZFReceipt(o, *result.Data.Transfer) {
			t.Fatal("original numeric/string UZF amount rejected")
		}
	}
}

func TestUSDTPaymentMigrationAndMissingSecondaryConfiguration(t *testing.T) {
	s := testServer(t)
	s.DB.Migrator().DropColumn(&Order{}, "usdt_provider")
	s.DB.Migrator().DropColumn(&Settings{}, "usdt_wallet")
	s.DB.Delete(&Migration{}, "version = 8")
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	if !s.DB.Migrator().HasColumn(&Order{}, "usdt_provider") || !s.DB.Migrator().HasColumn(&Settings{}, "usdt_wallet") {
		t.Fatal("existing database migration failed")
	}
	u := seedUser(t, s, 0)
	s.DB.Model(&u).Update("role", "admin")
	cookie, csrf := sessionFor(t, s, u)
	requireCode(t, call(t, s, "PUT", "/api/admin/usdt-wallet", map[string]string{"wallet": usdtContract, "secondary_password": "anything"}, cookie, csrf), 503)
}
