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
func TestReturnFlowFreeConcurrencyAndNoBalanceRequirement(t *testing.T) {
	for _, kind := range []string{"apk", "pwa"} {
		t.Run(kind, func(t *testing.T) {
			s := testServer(t)
			u := seedUser(t, s, 0)
			_, page := billedReturnApp(t, s, u, kind)
			endpoint := strings.TrimRight(strings.TrimSuffix(page, "/info"), "/") + "/return-flow/view"
			ip := "203.0.113.90"
			token := returnBillingPage(t, s, page, ip)
			var wg sync.WaitGroup
			for range 24 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					w := returnBillingCall(s, endpoint, token, ip, s.Config.PublicURL)
					if w.Code != 200 {
						t.Errorf("free return %d", w.Code)
					}
				}()
			}
			wg.Wait()
			now := s.Now()
			s.Now = func() time.Time { return now.Add(25 * time.Hour) }
			token = returnBillingPage(t, s, page, ip)
			requireCode(t, returnBillingCall(s, endpoint, token, ip, s.Config.PublicURL), 200)
			got := usageOwner(t, s, u.ID)
			if got.BalanceMicros != 0 || got.HeldMicros != 0 || countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &ReturnFlowViewCharge{}) != 0 {
				t.Fatal("free return billed or created billing guards")
			}
		})
	}
}

func TestReturnFlowFreeStillValidatesProofOriginAndFeature(t *testing.T) {
	for _, kind := range []string{"apk", "pwa"} {
		t.Run(kind, func(t *testing.T) {
			s := testServer(t)
			u := seedUser(t, s, 0)
			id, page := billedReturnApp(t, s, u, kind)
			endpoint := strings.TrimRight(strings.TrimSuffix(page, "/info"), "/") + "/return-flow/view"
			ip := "203.0.113.90"
			token := returnBillingPage(t, s, page, ip)
			for _, test := range []struct{ token, ip, origin string }{{token + "x", ip, s.Config.PublicURL}, {token, "203.0.113.91", s.Config.PublicURL}, {token, ip, ""}, {token, ip, "https://hostile.example"}} {
				requireCode(t, returnBillingCall(s, endpoint, test.token, test.ip, test.origin), 403)
			}
			requireCode(t, returnBillingCall(s, endpoint, token, ip, s.Config.PublicURL), 200)
			if kind == "pwa" {
				s.DB.Model(&PWAApp{}).Where("id = ?", id).Update("return_flow_json", "{}")
			} else {
				var page AppPageDesign
				s.DB.First(&page, "app_id = ?", id)
				page.Design.ReturnFlowConfig = nil
				s.DB.Save(&page)
			}
			requireCode(t, returnBillingCall(s, endpoint, token, ip, s.Config.PublicURL), 409)
			if countRows(t, s, &Ledger{}) != 0 {
				t.Fatal("invalid or disabled free return changed ledger")
			}
		})
	}
}

func TestReturnFlowDoesNotDependOnBillingLedger(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	_, page := billedReturnApp(t, s, u, "pwa")
	ip := "203.0.113.92"
	token := returnBillingPage(t, s, page, ip)
	if e := s.DB.Migrator().DropTable(&Ledger{}); e != nil {
		t.Fatal(e)
	}
	endpoint := strings.TrimRight(page, "/") + "/return-flow/view"
	requireCode(t, returnBillingCall(s, endpoint, token, ip, s.Config.PublicURL), 200)
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != Dollar {
		t.Fatal(fmt.Sprint("free return touched money ", got.BalanceMicros))
	}
}
