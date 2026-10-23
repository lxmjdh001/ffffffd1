package platform

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This starts a real isolated Python UZF service with temporary receipts, never
// the running monitor or its production snapshot. No real transfer is sent.
func TestUZFRealPythonIntegration(t *testing.T) {
	python := os.Getenv("UZF_TEST_PYTHON")
	if python == "" {
		t.Skip("set UZF_TEST_PYTHON to the sibling UZF virtualenv's Python")
	}
	repo := os.Getenv("UZF_TEST_REPO")
	if repo == "" {
		repo, _ = filepath.Abs("../../../uzf")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	path := filepath.Join(t.TempDir(), "receipts.json")
	write := func(receipts []uzfReceipt) {
		raw, _ := json.Marshal(map[string]any{"last_update_timestamp": time.Now().Unix(), "receiving_wallets": []string{usdtContract}, "transfers": receipts})
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write([]uzfReceipt{})
	code := `import sys; sys.path.insert(0,sys.argv[1]); import query_api; from waitress import serve; query_api.API_SECRET='integration-only-secret-at-least-32'; query_api.JSON_FILE=sys.argv[2]; serve(query_api.app, host='127.0.0.1', port=int(sys.argv[3]))`
	process := exec.Command(python, "-c", code, repo, path, strconv.Itoa(port))
	process.Stdout, process.Stderr = os.Stderr, os.Stderr
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { process.Process.Kill(); process.Wait() }()
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	client := &http.Client{Timeout: time.Second}
	ready := false
	for i := 0; i < 50; i++ {
		resp, err := client.Get(base + "/health")
		if err == nil {
			resp.Body.Close()
			ready = resp.StatusCode == 200
			if ready {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("isolated UZF service did not start")
	}
	s := testServer(t)
	s.Config.USDTProvider = "uzf"
	s.Config.USDTMonitorURL = base + "/api/check"
	s.Config.USDTMonitorSecret = "integration-only-secret-at-least-32"
	s.Config.USDTWallet = usdtContract
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	r := httptest.NewRequest("POST", "http://localhost:8088/api/orders", bytes.NewBufferString(`{"amount":"10","method":"usdt"}`))
	r.AddCookie(cookie)
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Idempotency-Key", randomHex(16))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 201)
	var order Order
	json.Unmarshal(w.Body.Bytes(), &order)
	if order.USDTProvider != "uzf" || order.Wallet != usdtContract {
		t.Fatal("provider/address not snapshotted on the new order")
	}
	if countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("order creation credited funds")
	}
	// Model a successful read-only OKX deposit result, not a real transfer.
	write([]uzfReceipt{{BillID: "isolated-test-deposit", Amount: formatMoney(order.USDTUnits), Currency: "USDT", Source: "okx_deposit", Chain: "USDT-TRC20", To: order.Wallet, State: "2", TransactionID: strings.Repeat("d", 64), BillTimestamp: time.Now().UnixMilli()}})
	s.maintenance(t.Context())
	if err := s.reconcileUSDT(t.Context(), order); err != nil {
		t.Fatal(err)
	}
	s.DB.First(&u, "id = ?", u.ID)
	s.DB.First(&order, "id = ?", order.ID)
	if order.Status != "paid" || u.BalanceMicros != 10*Dollar || countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("real Go -> signed Python API -> automatic worker -> ledger flow failed")
	}
}
