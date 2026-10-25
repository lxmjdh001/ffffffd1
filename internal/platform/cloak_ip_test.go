package platform

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestCloakIPListsAPI(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, 0), seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	otherCookie, otherCSRF := sessionFor(t, s, other)
	body := cloakFixture()
	legacy := createTestCloak(t, s, owner, body)
	if len(legacy.AllowedIPs)+len(legacy.BlockedIPs) != 0 {
		t.Fatal("IP filtering enabled by default")
	}
	body["allowed_ips"] = []string{" 203.0.113.8 ", "::ffff:203.0.113.8", "2001:0DB8:0:0::1"}
	body["blocked_ips"] = []string{"198.51.100.9", "2001:db8::2", "2001:0db8::2"}
	c := createTestCloak(t, s, owner, body)
	wantAllowed, wantBlocked := []string{"203.0.113.8", "2001:db8::1"}, []string{"198.51.100.9", "2001:db8::2"}
	assertSaved := func() {
		t.Helper()
		var saved Cloak
		if err := s.DB.First(&saved, "id = ?", c.ID).Error; err != nil || !reflect.DeepEqual(saved.AllowedIPs, wantAllowed) || !reflect.DeepEqual(saved.BlockedIPs, wantBlocked) {
			t.Fatalf("IP lists were not persisted: %+v, %v", saved, err)
		}
	}
	assertSaved()
	w := call(t, s, "GET", "/api/cloaks", nil, cookie, "")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), `"allowed_ips":["203.0.113.8","2001:db8::1"]`) || !strings.Contains(w.Body.String(), `"blocked_ips":["198.51.100.9","2001:db8::2"]`) {
		t.Fatal("list response omitted normalized IP lists")
	}
	w = call(t, s, "GET", "/api/cloaks", nil, otherCookie, "")
	requireCode(t, w, 200)
	if strings.Contains(w.Body.String(), "203.0.113.8") {
		t.Fatal("IP lists leaked to another account")
	}
	requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, otherCookie, otherCSRF), 404)
	tooMany := make([]string, 1001)
	for i := range tooMany {
		tooMany[i] = "203.0.113.8"
	}
	for _, field := range []string{"allowed_ips", "blocked_ips"} {
		original := body[field]
		for _, invalid := range []any{[]string{""}, []string{" \t "}, []string{"example.com"}, []string{"203.0.113.0/24"}, []string{"203.0.113.8:443"}, []string{"[2001:db8::1]"}, []string{"fe80::1%eth0"}, []string{"999.1.1.1"}, []string{"203.0.113.8\n198.51.100.9"}, tooMany, "203.0.113.8"} {
			body[field] = invalid
			requireCode(t, call(t, s, "POST", "/api/cloaks", body, cookie, csrf), 400)
			requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf), 400)
			assertSaved()
		}
		body[field] = original
	}
	if countRows(t, s, &Cloak{}) != 2 {
		t.Fatal("invalid IP input created rules")
	}
	body["allowed_ips"], body["blocked_ips"] = []string{}, []string{}
	w = call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf)
	requireCode(t, w, 200)
	var updated cloakView
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil || updated.AllowedIPs == nil || updated.BlockedIPs == nil || len(updated.AllowedIPs)+len(updated.BlockedIPs) != 0 {
		t.Fatalf("cannot clear lists: %s, %v", w.Body.String(), err)
	}
	wantAllowed, wantBlocked = []string{}, []string{}
	assertSaved()
}

func cloakIPRequest(s *Server, c cloakView, method, peer, forwarded, language string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, c.EntryURL+"?visitor=private", nil)
	r.RemoteAddr = peer
	r.Header.Set("X-Forwarded-For", forwarded)
	r.Header.Set("Accept-Language", language)
	r.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0) Chrome/132 Safari/537.36 Edg/132")
	r.Header.Set("CF-IPCountry", "CN")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestCloakIPWhitelistBypassesAllCloakFilters(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	for _, filter := range []string{"empty language", "language", "country", "UA", "browser", "platform", "device", "all"} {
		t.Run(filter, func(t *testing.T) {
			body := cloakFixture()
			body["blocked_languages"] = []string{}
			body["allowed_ips"] = []string{"203.0.113.8"}
			if filter == "empty language" || filter == "all" {
				body["block_empty_language"] = true
			}
			if filter == "language" || filter == "all" {
				body["blocked_languages"] = []string{"zh"}
			}
			if filter == "country" || filter == "all" {
				body["blocked_countries"] = []string{"XX", "CN"}
			}
			if filter == "UA" || filter == "all" {
				body["blocked_uas"] = []string{"Windows"}
			}
			if filter == "browser" || filter == "all" {
				body["blocked_browsers"] = []string{"edge"}
			}
			if filter == "platform" || filter == "all" {
				body["allowed_platforms"] = []string{"google"}
			}
			if filter == "device" || filter == "all" {
				body["blocked_devices"] = []CloakDeviceRule{{Device: "desktop", Systems: []string{"all"}}}
			}
			c := createTestCloak(t, s, owner, body)
			language := "zh-CN"
			if filter == "empty language" || filter == "all" {
				language = ""
			}
			for _, peer := range []string{"203.0.113.8:1234", "[::ffff:203.0.113.8]:1234", "203.0.113.9:1234"} {
				w := cloakIPRequest(s, c, "GET", peer, "", language)
				requireCode(t, w, 302)
				allowed := strings.HasPrefix(w.Header().Get("Location"), "https://target.example/path?")
				if allowed != (peer != "203.0.113.9:1234") {
					t.Fatalf("%s peer=%s location=%s", filter, peer, w.Header().Get("Location"))
				}
			}
		})
	}
	var visits []AccessVisit
	if err := s.DB.Where("kind = ?", "cloaks").Find(&visits).Error; err != nil {
		t.Fatal(err)
	}
	allowed, blocked := 0, 0
	for _, v := range visits {
		if v.Outcome == "allowed" {
			allowed++
		} else if v.Outcome == "blocked" {
			blocked++
		}
	}
	if allowed != 16 || blocked != 8 {
		t.Fatalf("wrong IP list statistics: allowed=%d blocked=%d", allowed, blocked)
	}
}

func TestCloakIPBlacklistPriorityAndSafeResponses(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	for _, safe := range []string{"", "https://safe.example/help?fixed=yes"} {
		for _, enabled := range []bool{true, false} {
			body := cloakFixture()
			body["safe_url"], body["enabled"], body["blocked_languages"] = safe, enabled, []string{}
			body["allowed_ips"] = []string{"203.0.113.8"}
			body["blocked_ips"] = []string{"203.0.113.8", "2001:db8::2"}
			c := createTestCloak(t, s, owner, body)
			for _, peer := range []string{"203.0.113.8:443", "[2001:0db8:0:0::2]:443"} {
				for _, method := range []string{"GET", "HEAD"} {
					w := cloakIPRequest(s, c, method, peer, "", "en")
					if !enabled {
						requireCode(t, w, 302)
						if !strings.HasPrefix(w.Header().Get("Location"), "https://target.example/path?") {
							t.Fatal("off mode did not bypass IP blacklist")
						}
					} else if safe == "" {
						requireCode(t, w, 404)
					} else {
						requireCode(t, w, 302)
						if w.Header().Get("Location") != safe {
							t.Fatal("IP block leaked query to safe URL")
						}
					}
					if w.Header().Get("Cache-Control") != "no-store" || (method == "HEAD" && w.Body.Len() != 0) {
						t.Fatal("IP block response cacheable or HEAD returned body")
					}
				}
			}
			w := cloakIPRequest(s, c, "GET", "[2001:db8::3]:443", "", "en")
			requireCode(t, w, 302)
			if !strings.HasPrefix(w.Header().Get("Location"), "https://target.example/path?") {
				t.Fatal("unlisted address was blocked")
			}
		}
	}
}

func TestCloakIPListsUseTrustedClientAddress(t *testing.T) {
	s := testServer(t)
	_, proxy, _ := net.ParseCIDR("10.0.0.0/8")
	s.Config.TrustedProxies = []*net.IPNet{proxy}
	owner := seedUser(t, s, 0)
	body := cloakFixture()
	body["blocked_languages"] = []string{}
	body["blocked_ips"] = []string{"203.0.113.8"}
	c := createTestCloak(t, s, owner, body)
	for _, tc := range []struct {
		peer, forwarded string
		blocked         bool
	}{
		{"203.0.113.8:443", "198.51.100.9", true},
		{"198.51.100.9:443", "203.0.113.8", false},
		{"10.0.0.1:443", "203.0.113.8, 10.0.0.2", true},
		{"10.0.0.1:443", "203.0.113.8, 198.51.100.9", false},
	} {
		w := cloakIPRequest(s, c, "GET", tc.peer, tc.forwarded, "en")
		requireCode(t, w, 302)
		if got := w.Header().Get("Location") == c.SafeURL; got != tc.blocked {
			t.Fatalf("peer=%s XFF=%s blocked=%v", tc.peer, tc.forwarded, got)
		}
	}
}

func TestCloakIPBlacklistRunsBeforeTargets(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	app := seedApp(t, s, owner, 120)
	pwa := createPWAFixture(t, s, owner, "IP filtered PWA")
	pool := TrafficPool{Slug: randomHex(4), UserID: owner.ID, Enabled: true, Mode: "sequential"}
	if err := s.DB.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&TrafficLink{ID: randomHex(16), PoolSlug: pool.Slug, UserID: owner.ID, Position: 0, URL: "https://pool.example/first", Status: "safe"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"app", "pwa", "traffic_pool"} {
		body := cloakFixture()
		body["target_type"], body["target_id"], body["app_target_mode"] = kind, app.ID, "direct"
		body["blocked_languages"], body["blocked_ips"] = []string{}, []string{"203.0.113.8"}
		if kind == "pwa" {
			body["target_id"] = pwa.ID
		}
		if kind == "traffic_pool" {
			body["traffic_pool_slug"] = pool.Slug
		}
		c := createTestCloak(t, s, owner, body)
		w := cloakIPRequest(s, c, "GET", "203.0.113.8:443", "", "en")
		requireCode(t, w, 302)
		if w.Header().Get("Location") != c.SafeURL {
			t.Fatalf("blocked %s entered target", kind)
		}
	}
	var current TrafficPool
	if err := s.DB.First(&current, "slug = ?", pool.Slug).Error; err != nil || current.Cursor != 0 || countRows(t, s, &Ticket{}) != 0 {
		t.Fatalf("IP block caused target side effects: cursor=%d err=%v", current.Cursor, err)
	}
}

func TestCloakIPWhitelistPreservesIndependentAccessControls(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	app := seedApp(t, s, owner, 120)
	body := cloakFixture()
	body["allowed_ips"], body["block_empty_language"] = []string{"203.0.113.8"}, true
	c := createTestCloak(t, s, owner, body)
	ua := "Mozilla/5.0 (Windows NT 10.0) Chrome/132 Safari/537.36 Edg/132"
	if err := s.DB.Create(&UABlock{UserID: owner.ID, UAHash: uaFingerprint(ua), CreatedAt: s.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, cloakIPRequest(s, c, "GET", "203.0.113.8:443", "", ""), 404)
	if err := s.DB.Where("user_id = ?", owner.ID).Delete(&UABlock{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&owner).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, cloakIPRequest(s, c, "GET", "203.0.113.8:443", "", ""), 404)
	if err := s.DB.Model(&owner).Update("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	body["target_type"], body["target_id"], body["app_target_mode"] = "app", app.ID, "direct"
	c = createTestCloak(t, s, owner, body)
	if err := s.DB.Model(&app).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	w := cloakIPRequest(s, c, "GET", "203.0.113.8:443", "", "")
	requireCode(t, w, 302)
	if w.Header().Get("Location") != c.SafeURL || countRows(t, s, &Ticket{}) != 0 {
		t.Fatal("whitelisted visitor bypassed unavailable target protection")
	}
}
