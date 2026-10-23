package platform

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGo7PaymentSignaturesAndCallbackRouting(t *testing.T) {
	// Independent fixtures for go7's exact MD5 and HMAC-SHA256 wire formats.
	params := map[string]string{"pid": "test-merchant", "type": "alipay", "out_trade_no": "d1-test-order", "notify_url": "https://d1apk.com/api/payments/futoon/notify", "return_url": "https://d1apk.com/#/billing?order=d1-test-order", "name": "D1 Apk 余额充值", "money": "1000.00", "clientip": "127.0.0.1", "device": "pc", "sign": "ignored", "sign_type": "MD5", "empty": ""}
	if sign := FutoonSign(params, "test-key"); sign != "38d96fa3182aa0962b687585acbabd1e" {
		t.Fatal("incompatible go7 MD5 signature")
	}
	if sign := MonitorSign(map[string]string{"amount": "142.86", "currency": "USDT"}, "1791331200", "test-monitor-secret"); sign != "54f439bb7b4d397917d75b82dd5df6a583f49ff32ffbf9ad6f1d8ea4861c1062" {
		t.Fatal("incompatible go7 monitor signature")
	}
	s := testServer(t)
	s.Config.PublicURL = "https://d1apk.com"
	s.Config.FutoonURL = "https://gateway.example/submit.php"
	s.Config.FutoonPID = "test-merchant"
	s.Config.FutoonKey = "test-key"
	u, err := url.Parse(s.paymentURL(Order{ID: "d1-test-order", Method: "alipay", AmountMicros: 1000 * Dollar, BonusMicros: 300 * Dollar}, "127.0.0.1"))
	if err != nil || u.Path != "/submit.php" {
		t.Fatal("incorrect payment endpoint")
	}
	for key, value := range params {
		if key == "sign" || key == "empty" {
			continue
		}
		if u.Query().Get(key) != value {
			t.Fatalf("incorrect gateway parameter %s", key)
		}
	}
	if u.Query().Get("sign") != "38d96fa3182aa0962b687585acbabd1e" {
		t.Fatal("payment URL does not follow go7's signature protocol")
	}
}

func TestPaymentChannelAvailabilityAndNewOrderRestrictions(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	channels := func(want []string) {
		t.Helper()
		w := call(t, s, "GET", "/api/config", nil, nil, "")
		requireCode(t, w, 200)
		var result struct {
			Methods []string `json:"payment_methods"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Methods, want) {
			t.Fatalf("available channels = %v, want %v", result.Methods, want)
		}
	}
	post := func(method string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://localhost:8088/api/orders", bytes.NewBufferString(`{"amount":"100","method":"`+method+`"}`))
		r.AddCookie(cookie)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", randomHex(16))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	channels([]string{"manual"})
	requireCode(t, post("alipay"), 400)
	requireCode(t, post("usdt"), 503)
	s.Config.FutoonURL = "https://gateway.example/submit.php"
	s.Config.FutoonPID = "test-merchant"
	channels([]string{"manual"})
	requireCode(t, post("alipay"), 400)
	s.Config.FutoonKey = "test-secret"
	s.Config.USDTWallet = usdtContract
	channels([]string{"usdt", "manual"})
	requireCode(t, post("wechat"), 400)
	requireCode(t, post("alipay"), 400)
	for _, method := range []string{"usdt", "manual"} {
		requireCode(t, post(method), 201)
	}
}

func TestGo7PaymentConfigAliases(t *testing.T) {
	for key, value := range map[string]string{
		"APP_ENV": "test", "APP_SECRET": strings.Repeat("a", 32), "APP_URL": "http://localhost:8088", "SERVER_ROLE": "all",
		"FUTOON_API_URL": "https://gateway.example", "FUTOON_PID": "merchant", "FUTOON_KEY": "test-key",
		"USDT_MONITOR_URL": "", "USDT_MONITOR_SECRET": "", "USDT_WALLET": "",
		"USDT_API_URL": "https://monitor.example/check", "USDT_API_SECRET": "test-monitor-secret", "USDT_WALLET_ADDRESS": usdtContract,
		"TRON_API_URL": "https://api.trongrid.io", "REMOTE_STORAGE_URL": "", "S3_ENDPOINT": "", "TRUSTED_PROXY_CIDRS": "",
	} {
		t.Setenv(key, value)
	}
	for _, tc := range []struct{ input, want string }{
		{"https://gateway.example", "https://gateway.example/submit.php"},
		{"https://gateway.example/", "https://gateway.example/submit.php"},
		{"https://gateway.example/submit.php", "https://gateway.example/submit.php"},
		{"https://gateway.example/custom/submit.php", "https://gateway.example/custom/submit.php"},
	} {
		t.Setenv("FUTOON_API_URL", tc.input)
		config, err := LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if config.FutoonURL != tc.want || config.USDTMonitorURL != "https://monitor.example/check" || config.USDTMonitorSecret != "test-monitor-secret" || config.USDTWallet != usdtContract {
			t.Fatal("go7 environment settings were not loaded correctly")
		}
	}
	t.Setenv("USDT_MONITOR_URL", "https://override.example/check")
	t.Setenv("USDT_MONITOR_SECRET", "override-secret")
	config, err := LoadConfig()
	if err != nil || config.USDTMonitorURL != "https://override.example/check" || config.USDTMonitorSecret != "override-secret" {
		t.Fatal("explicit D1 settings must take precedence over go7 aliases")
	}
}

func TestGo7MonitorRequestAndVerifiedUSDTSettlement(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	order := Order{ID: randomHex(16), UserID: u.ID, IdempotencyKey: randomHex(16), Method: "usdt", AmountMicros: 1000 * Dollar, BonusMicros: 300 * Dollar, Status: "pending", USDTUnits: 142867123, Wallet: usdtContract, CreatedAt: s.Now().Add(-time.Minute), ExpiresAt: s.Now().Add(time.Minute)}
	if err := s.DB.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	var monitorCalls, chainCalls atomic.Int32
	var confirmed atomic.Bool
	s.Config.USDTMonitorSecret = "test-monitor-secret"
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/check" {
			monitorCalls.Add(1)
			q := r.URL.Query()
			want := MonitorSign(map[string]string{"amount": "142.867123", "currency": "USDT"}, q.Get("timestamp"), "test-monitor-secret")
			if r.Method != "GET" || q.Get("amount") != "142.867123" || q.Get("currency") != "USDT" || q.Get("timestamp") == "" || q.Get("signature") != want {
				t.Error("monitor request is not go7-compatible")
			}
			jsonOut(w, 200, map[string]any{"data": map[string]bool{"found": true}, "message": "ok"})
			return
		}
		chainCalls.Add(1)
		if r.URL.Path != "/v1/accounts/"+order.Wallet+"/transactions/trc20" || r.URL.Query().Get("only_confirmed") != "true" {
			t.Error("incorrect chain verification request")
		}
		transfers := []tronTransfer{}
		if confirmed.Load() {
			transfer := tronTransfer{TransactionID: strings.Repeat("b", 64), To: order.Wallet, Value: "142867123", Type: "Transfer", BlockTimestamp: s.Now().UnixMilli()}
			transfer.TokenInfo.Address = usdtContract
			transfer.TokenInfo.Decimals = 6
			transfers = append(transfers, transfer)
		}
		jsonOut(w, 200, map[string]any{"success": true, "data": transfers})
	}))
	defer provider.Close()
	s.HTTP = provider.Client()
	s.Config.USDTMonitorURL = provider.URL + "/check"
	s.Config.TronAPIURL = provider.URL
	if err := s.reconcileUSDT(t.Context(), order); err != nil {
		t.Fatal(err)
	}
	if countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("unverified monitor hint credited a payment")
	}
	confirmed.Store(true)
	for range 2 {
		if err := s.reconcileUSDT(t.Context(), order); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DB.First(&u, "id = ?", u.ID).Error; err != nil || u.BalanceMicros != 1300*Dollar || countRows(t, s, &Ledger{}) != 2 {
		t.Fatal("confirmed USDT principal and bonus were not credited exactly once")
	}
	if monitorCalls.Load() != 3 || chainCalls.Load() != 3 {
		t.Fatal("monitor or verification endpoint was skipped")
	}
}
