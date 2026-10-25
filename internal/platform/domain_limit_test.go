package platform

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestStandardTwentyDomainLimitAndAccountIsolation(t *testing.T) {
	s := meteredServer(t, 0)
	x, _ := getSettings(s.DB)
	if x.DomainLimit != 20 {
		t.Fatalf("domain limit=%d", x.DomainLimit)
	}
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	for i := 0; i < 19; i++ {
		if e := s.DB.Create(&Domain{ID: randomHex(16), UserID: u.ID, Hostname: fmt.Sprintf("existing%d.example.com", i), Token: randomHex(24)}).Error; e != nil {
			t.Fatal(e)
		}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success, denied := 0, 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := call(t, s, "POST", "/api/domains", map[string]string{"hostname": fmt.Sprintf("new%d.example.com", i)}, cookie, csrf)
			mu.Lock()
			defer mu.Unlock()
			if w.Code == 201 {
				success++
			} else if w.Code == 403 && strings.Contains(w.Body.String(), "联系客服升级") {
				denied++
			} else {
				t.Errorf("unexpected %d %s", w.Code, w.Body.String())
			}
		}(i)
	}
	wg.Wait()
	if success != 1 || denied != 1 {
		t.Fatalf("concurrent limit: accepted=%d denied=%d", success, denied)
	}
	var n int64
	s.DB.Model(&Domain{}).Where("user_id = ?", u.ID).Count(&n)
	if n != 20 {
		t.Fatal("domain limit exceeded")
	}
	other := seedUser(t, s, 0)
	oc, os := sessionFor(t, s, other)
	requireCode(t, call(t, s, "POST", "/api/domains", map[string]string{"hostname": "other.example.com"}, oc, os), 201)
	s.DB.Where("user_id = ? AND hostname = ?", u.ID, "existing0.example.com").Delete(&Domain{})
	requireCode(t, call(t, s, "POST", "/api/domains", map[string]string{"hostname": "replacement.example.com"}, cookie, csrf), 201)
}

func TestStandardDomainLimitMigrationKeepsExistingDomains(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, 0)
	for i := 0; i < 21; i++ {
		s.DB.Create(&Domain{ID: randomHex(16), UserID: u.ID, Hostname: fmt.Sprintf("kept%d.example.com", i), Token: randomHex(24)})
	}
	s.DB.Model(&Settings{}).Where("id = ?", 1).Update("domain_limit", 10)
	s.DB.Where("version = ?", 23).Delete(&Migration{})
	if e := Migrate(s.DB); e != nil {
		t.Fatal(e)
	}
	x, _ := getSettings(s.DB)
	if x.DomainLimit != 20 || countRows(t, s, &Domain{}) != 21 {
		t.Fatal("migration removed existing domains or failed to set twenty")
	}
	cookie, csrf := sessionFor(t, s, u)
	requireCode(t, call(t, s, "POST", "/api/domains", map[string]string{"hostname": "too-many.example.com"}, cookie, csrf), 403)
}
