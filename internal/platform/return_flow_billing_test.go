package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func billedReturnApp(t *testing.T, s *Server, u User, kind string) (string, string) {
	t.Helper()
	c := defaultReturnFlowConfig()
	c.Enabled = true
	c.ShowOnce = false
	if kind == "pwa" {
		a := createPWAFixture(t, s, u, "Paid return PWA")
		raw, _ := json.Marshal(c)
		if e := s.DB.Model(&a).Update("return_flow_json", string(raw)).Error; e != nil {
			t.Fatal(e)
		}
		return a.ID, "/p/" + a.Slug + "/"
	}
	a := seedApp(t, s, u, 128)
	d := defaultPageDesign(a)
	d.ReturnFlowConfig = &c
	if e := s.DB.Create(&AppPageDesign{AppID: a.ID, Design: d}).Error; e != nil {
		t.Fatal(e)
	}
	return a.ID, "/a/" + a.Slug + "/info"
}
func returnBillingPage(t *testing.T, s *Server, path, ip string) string {
	t.Helper()
	r := httptest.NewRequest("GET", "http://localhost:8088"+path, nil)
	r.RemoteAddr = net.JoinHostPort(ip, "1234")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 200)
	m := regexp.MustCompile(`data-return-flow-token="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if len(m) != 2 {
		t.Fatal("billing proof missing on public page")
	}
	return m[1]
}
func returnBillingCall(s *Server, path, token, ip, origin string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(map[string]string{"token": token})
	r := httptest.NewRequest("POST", "http://localhost:8088"+path, bytes.NewReader(raw))
	r.RemoteAddr = net.JoinHostPort(ip, "1234")
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Forwarded-For", "198.51.100.99")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestReturnFlowBillingPerApplicationRollingWindowAndConcurrency(t *testing.T) {
	for _, kind := range []string{"apk", "pwa"} {
		t.Run(kind, func(t *testing.T) {
			s := testServer(t)
			now := time.Date(2026, 10, 24, 23, 59, 0, 0, time.UTC)
			s.Now = func() time.Time { return now }
			u := seedUser(t, s, Dollar)
			_, page := billedReturnApp(t, s, u, kind)
			endpoint := strings.TrimSuffix(page, "/info")
			endpoint = strings.TrimSuffix(endpoint, "/") + "/return-flow/view"
			ip := "203.0.113.77"
			token := returnBillingPage(t, s, page, ip)
			if countRows(t, s, &Ledger{}) != 0 {
				t.Fatal("opening page charged before display")
			}
			var wg sync.WaitGroup
			for i := 0; i < 32; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					w := returnBillingCall(s, endpoint, token, ip, s.Config.PublicURL)
					if w.Code != 200 {
						t.Errorf("parallel display %d: %s", w.Code, w.Body.String())
					}
				}()
			}
			wg.Wait()
			assert := func(count, spent int64) {
				t.Helper()
				var owner User
				s.DB.First(&owner, "id = ?", u.ID)
				if countRows(t, s, &Ledger{}) != count || owner.BalanceMicros != Dollar-spent || owner.HeldMicros != 0 {
					t.Fatalf("count=%d balance=%d held=%d", countRows(t, s, &Ledger{}), owner.BalanceMicros, owner.HeldMicros)
				}
			}
			assert(1, 10000)
			// A new date, cookie or page token must not reset the rolling window.
			now = now.Add(2 * time.Minute)
			token = returnBillingPage(t, s, page, ip)
			requireCode(t, returnBillingCall(s, endpoint, token, "::ffff:203.0.113.77", s.Config.PublicURL), 200)
			assert(1, 10000)
			now = now.Add(23*time.Hour + 57*time.Minute)
			token = returnBillingPage(t, s, page, ip)
			requireCode(t, returnBillingCall(s, endpoint, token, ip, s.Config.PublicURL), 200)
			assert(1, 10000)
			now = now.Add(time.Minute)
			token = returnBillingPage(t, s, page, ip)
			requireCode(t, returnBillingCall(s, endpoint, token, ip, s.Config.PublicURL), 200)
			assert(2, 20000)
			_, otherPage := billedReturnApp(t, s, u, kind)
			otherEndpoint := strings.TrimSuffix(strings.TrimSuffix(otherPage, "/info"), "/") + "/return-flow/view"
			otherToken := returnBillingPage(t, s, otherPage, ip)
			requireCode(t, returnBillingCall(s, otherEndpoint, otherToken, ip, s.Config.PublicURL), 200)
			assert(3, 30000)
			newIP := "203.0.113.78"
			newToken := returnBillingPage(t, s, page, newIP)
			requireCode(t, returnBillingCall(s, endpoint, newToken, newIP, s.Config.PublicURL), 200)
			assert(4, 40000)
			if countRows(t, s, &Download{}) != 0 || countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Usage{}) != 0 {
				t.Fatal("return impression changed download counts/quota")
			}
			var guards []ReturnFlowViewCharge
			s.DB.Find(&guards)
			if len(guards) != 3 {
				t.Fatal("dedup guards do not match application/IP boundaries")
			}
			for _, g := range guards {
				if strings.Contains(g.Key, ip) || g.UserID != u.ID || g.AppKind != kind {
					t.Fatal("guard isolation or IP hashing incorrect")
				}
			}
			otherOwner := seedUser(t, s, Dollar)
			_, isolatedPage := billedReturnApp(t, s, otherOwner, kind)
			isolatedToken := returnBillingPage(t, s, isolatedPage, ip)
			isolatedEndpoint := strings.TrimSuffix(strings.TrimSuffix(isolatedPage, "/info"), "/") + "/return-flow/view"
			requireCode(t, returnBillingCall(s, isolatedEndpoint, isolatedToken, ip, s.Config.PublicURL), 200)
			var isolated User
			s.DB.First(&isolated, "id = ?", otherOwner.ID)
			var original User
			s.DB.First(&original, "id = ?", u.ID)
			if isolated.BalanceMicros != Dollar-10000 || original.BalanceMicros != Dollar-40000 {
				t.Fatal("tenant charging was not isolated")
			}
			t.Log(fmt.Sprintf("%s: 32 concurrent displays charge $0.01 once; rolling expiry, per-app separation and normalized IP verified", kind))
		})
	}
}
func TestReturnFlowBillingProofAvailabilityAndBalance(t *testing.T) {
	for _, kind := range []string{"apk", "pwa"} {
		t.Run(kind, func(t *testing.T) {
			s := testServer(t)
			u := seedUser(t, s, 10000)
			id, page := billedReturnApp(t, s, u, kind)
			endpoint := strings.TrimSuffix(strings.TrimSuffix(page, "/info"), "/") + "/return-flow/view"
			ip := "203.0.113.90"
			token := returnBillingPage(t, s, page, ip)
			for _, test := range []struct{ token, ip, origin string }{{token + "x", ip, s.Config.PublicURL}, {token, "203.0.113.91", s.Config.PublicURL}, {token, ip, ""}, {token, ip, "https://hostile.example"}} {
				requireCode(t, returnBillingCall(s, endpoint, test.token, test.ip, test.origin), 403)
			}
			if e := s.DB.Model(&u).Update("held_micros", 1).Error; e != nil {
				t.Fatal(e)
			}
			requireCode(t, returnBillingCall(s, endpoint, token, ip, s.Config.PublicURL), 402)
			var owner User
			s.DB.First(&owner, "id = ?", u.ID)
			if owner.BalanceMicros != 10000 || countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &ReturnFlowViewCharge{}) != 0 {
				t.Fatal("failed impression changed money or dedup guard")
			}
			s.DB.Model(&u).Update("held_micros", 0)
			requireCode(t, returnBillingCall(s, endpoint, token, ip, s.Config.PublicURL), 200)
			requireCode(t, returnBillingCall(s, endpoint, token, ip, s.Config.PublicURL), 200)
			s.DB.First(&owner, "id = ?", u.ID)
			if owner.BalanceMicros != 0 || countRows(t, s, &Ledger{}) != 1 {
				t.Fatal("repeat view should work at zero balance without a second charge")
			}
			if kind == "apk" {
				s.DB.Model(&Application{}).Where("id = ?", id).Update("enabled", false)
			} else {
				s.DB.Model(&PWAApp{}).Where("id = ?", id).Update("enabled", false)
			}
			requireCode(t, returnBillingCall(s, endpoint, token, ip, s.Config.PublicURL), 404)
		})
	}
}
func TestReturnFlowBillingConcurrentIPsNeverOverdraw(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 10000)
	id, page := billedReturnApp(t, s, u, "apk")
	endpoint := strings.TrimSuffix(page, "/info") + "/return-flow/view"
	var wg sync.WaitGroup
	codes := make(chan int, 24)
	for i := 0; i < 24; i++ {
		ip := fmt.Sprintf("203.0.113.%d", i+1)
		r := httptest.NewRequest("GET", "http://localhost:8088"+page, nil)
		r.RemoteAddr = net.JoinHostPort(ip, "1234")
		token := s.returnFlowViewToken("apk", id, r)
		wg.Add(1)
		go func() { defer wg.Done(); codes <- returnBillingCall(s, endpoint, token, ip, s.Config.PublicURL).Code }()
	}
	wg.Wait()
	close(codes)
	ok := 0
	for code := range codes {
		if code == 200 {
			ok++
		} else if code != 402 {
			t.Fatal("unexpected status", code)
		}
	}
	var owner User
	s.DB.First(&owner, "id = ?", u.ID)
	if ok != 1 || owner.BalanceMicros != 0 || owner.HeldMicros != 0 || countRows(t, s, &Ledger{}) != 1 || countRows(t, s, &ReturnFlowViewCharge{}) != 1 {
		t.Fatalf("concurrent charging overdrew or duplicated: successes=%d balance=%d", ok, owner.BalanceMicros)
	}
}
func TestReturnFlowBillingTransactionRollbackAndMigration(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	_, page := billedReturnApp(t, s, u, "apk")
	endpoint := strings.TrimSuffix(page, "/info") + "/return-flow/view"
	ip := "203.0.113.10"
	token := returnBillingPage(t, s, page, ip)
	if e := s.DB.Migrator().DropTable(&ReturnFlowViewCharge{}); e != nil {
		t.Fatal(e)
	}
	s.DB.Where("version = ?", 17).Delete(&Migration{})
	var persisted User
	s.DB.First(&persisted, "id = ?", u.ID)
	before, _ := json.Marshal(persisted)
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	var after User
	s.DB.First(&after, "id = ?", u.ID)
	afterJSON, _ := json.Marshal(after)
	if string(before) != string(afterJSON) || countRows(t, s, &ReturnFlowViewCharge{}) != 0 {
		t.Fatal("migration changed existing customer or seeded charges")
	}
	if e := s.DB.Migrator().DropTable(&Ledger{}); e != nil {
		t.Fatal(e)
	}
	requireCode(t, returnBillingCall(s, endpoint, token, ip, s.Config.PublicURL), 500)
	s.DB.First(&after, "id = ?", u.ID)
	if after.BalanceMicros != Dollar || countRows(t, s, &ReturnFlowViewCharge{}) != 0 {
		t.Fatal("ledger failure did not roll back charge")
	}
}
