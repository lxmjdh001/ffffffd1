package platform

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	valid := uzfReceipt{BillID: "deposit-123", Amount: "100.001234", Currency: "USDT", Source: "okx_deposit", Chain: "USDT-TRC20", To: o.Wallet, State: "2", TransactionID: strings.Repeat("b", 64), BillTimestamp: s.Now().UnixMilli()}
	proof := valid
	found, stale, unavailable := false, false, false
	s.Config.USDTMonitorSecret = strings.Repeat("test", 8)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unavailable {
			w.WriteHeader(503)
			return
		}
		q := r.URL.Query()
		params := map[string]string{}
		for _, key := range []string{"amount", "currency", "wallet", "min_timestamp", "max_timestamp"} {
			params[key] = q.Get(key)
		}
		if q.Get("signature") != MonitorSign(params, q.Get("timestamp"), s.Config.USDTMonitorSecret) || q.Get("amount") != "100.001234" || q.Get("wallet") != o.Wallet {
			t.Error("incorrect signed UZF query")
		}
		updated := s.Now().Unix()
		if stale {
			updated -= 121
		}
		jsonOut(w, 200, map[string]any{"success": true, "data": map[string]any{"found": found, "last_update_timestamp": updated, "transfer": proof}})
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
	found = true
	for name, mutate := range map[string]func(*uzfReceipt){
		"amount":        func(p *uzfReceipt) { p.Amount = "100.001235" },
		"wallet":        func(p *uzfReceipt) { p.To = "other-wallet" },
		"chain":         func(p *uzfReceipt) { p.Chain = "USDT-ERC20" },
		"currency":      func(p *uzfReceipt) { p.Currency = "USDC" },
		"pending":       func(p *uzfReceipt) { p.State = "1" },
		"history":       func(p *uzfReceipt) { p.BillTimestamp = o.CreatedAt.Add(-time.Second).UnixMilli() },
		"late":          func(p *uzfReceipt) { p.BillTimestamp = o.ExpiresAt.Add(time.Second).UnixMilli() },
		"internal-bill": func(p *uzfReceipt) { p.Source = "account_bills" },
		"missing-tx":    func(p *uzfReceipt) { p.TransactionID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			proof = valid
			mutate(&proof)
			if s.reconcileUSDT(t.Context(), o) == nil || countRows(t, s, &Ledger{}) != 0 {
				t.Fatal("invalid receipt was accepted")
			}
		})
	}
	proof = valid
	stale = true
	if s.reconcileUSDT(t.Context(), o) == nil {
		t.Fatal("stale snapshot accepted")
	}
	stale, unavailable = false, true
	if s.reconcileUSDT(t.Context(), o) == nil {
		t.Fatal("offline monitor accepted")
	}
	unavailable = false
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
	// Also prevent replay through the direct-chain provider.
	if s.settle(other.ID, "tron:"+valid.TransactionID, "usdt", other.AmountMicros) == nil {
		t.Fatal("transaction replayed across providers")
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
