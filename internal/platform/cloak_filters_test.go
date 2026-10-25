package platform

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestCloakEmptyLanguageFilter(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []string
		empty   bool
	}{
		{"missing header", nil, true},
		{"blank header", []string{" \t "}, true},
		{"empty items", []string{", ,"}, true},
		{"wildcard only", []string{"*"}, true},
		{"invalid language", []string{"zh_CN, invalid-language!"}, true},
		{"all refused", []string{"en;q=0,zh-CN;q=0.0"}, true},
		{"invalid quality", []string{"en;q=no,fr;q=NaN,de;q=+Inf"}, true},
		{"out of range quality", []string{"en;q=-0.1,fr;q=1.1"}, true},
		{"concrete language", []string{"en-US"}, false},
		{"case and whitespace", []string{"  ZH-CN ;q=0.8 "}, false},
		{"positive quality", []string{"*;q=1,en;q=0.001"}, false},
		{"valid fallback", []string{"zh_CN,fr;q=bad,en;q=0.5"}, false},
		{"second header valid", []string{"*;q=1", "en-US;q=0.7"}, false},
		{"second header refused", []string{" ", "en;q=0"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/c/example", nil)
			// Language comes from Accept-Language, independently of whether
			// the UA happens to contain a locale-looking compatibility token.
			r.Header.Set("User-Agent", "Mozilla/5.0 (en-US) Chrome/123 Safari/537.36")
			for _, value := range tc.headers {
				r.Header.Add("Accept-Language", value)
			}
			if got := cloakBlocked(Cloak{BlockEmptyLanguage: true}, r, "XX"); got != tc.empty {
				t.Fatalf("blocked=%v, want %v for headers %q", got, tc.empty, tc.headers)
			}
			if cloakBlocked(Cloak{}, r, "XX") {
				t.Fatal("omitted empty-language option changed existing unfiltered traffic")
			}
		})
	}
	// A language in a later header also participates in the existing blacklist.
	r := httptest.NewRequest("GET", "/c/example", nil)
	r.Header.Add("Accept-Language", "en")
	r.Header.Add("Accept-Language", "zh-CN;q=0.5")
	if !cloakBlocked(Cloak{BlockedLanguages: []string{"zh"}}, r, "XX") {
		t.Fatal("language blacklist ignored the second Accept-Language header")
	}
}

func TestCloakBrowserFiltersIdentifyOneClient(t *testing.T) {
	const desktop = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/132.0.0.0 Safari/537.36"
	const ios = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) "
	const android = "Mozilla/5.0 (Linux; Android 14; Mobile) AppleWebKit/537.36 Chrome/132.0.0.0 Mobile Safari/537.36"
	for _, tc := range []struct{ name, ua, browser string }{
		{"desktop Chrome", desktop, "chrome"},
		{"desktop Edge", desktop + " Edg/132.0.0.0", "edge"},
		{"legacy Edge", desktop + " Edge/18.19045", "edge"},
		{"desktop Safari", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/18.0 Safari/605.1.15", "safari"},
		{"desktop Firefox", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:132.0) Gecko/20100101 Firefox/132.0", "firefox"},
		{"iOS Chrome", ios + "CriOS/132.0.0.0 Mobile/15E148 Safari/604.1", "chrome"},
		{"iOS Edge", ios + "EdgiOS/132.0.0.0 Mobile/15E148 Safari/605.1.15", "edge"},
		{"iOS Firefox", ios + "FxiOS/132.0 Mobile/15E148 Safari/605.1.15", "firefox"},
		{"iOS Safari", ios + "Version/18.0 Mobile/15E148 Safari/604.1", "safari"},
		{"Android Chrome", android, "chrome"},
		{"Android Edge", android + " EdgA/132.0.0.0", "edge"},
		{"Android Firefox", "Mozilla/5.0 (Android 14; Mobile; rv:132.0) Gecko/132.0 Firefox/132.0", "firefox"},
		{"Opera compatibility tokens", desktop + " OPR/115.0.0.0", ""},
		{"iOS Opera", ios + "OPiOS/5.0 Mobile/15E148 Safari/604.1", ""},
		{"Samsung compatibility tokens", android + " SamsungBrowser/27.0", ""},
		{"Vivaldi compatibility tokens", desktop + " Vivaldi/7.0", ""},
		{"unknown client", "curl/8.10.0", ""},
		{"missing client", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/c/example", nil)
			r.Header.Set("User-Agent", tc.ua)
			if got := classifyCloakBrowser(tc.ua); got != tc.browser {
				t.Fatalf("client=%q, want %q", got, tc.browser)
			}
			for _, selected := range []string{"chrome", "safari", "firefox", "edge"} {
				if got := cloakBlocked(Cloak{BlockedBrowsers: []string{selected}}, r, "XX"); got != (selected == tc.browser) {
					t.Errorf("selected=%q blocked=%v, identified client=%q", selected, got, tc.browser)
				}
			}
			if cloakBlocked(Cloak{}, r, "XX") {
				t.Fatal("client filtering was enabled by default")
			}
		})
	}
}

func TestCloakFilterAPIPersistenceAndValidation(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	body := cloakFixture()
	legacy := createTestCloak(t, s, owner, body)
	if legacy.BlockEmptyLanguage || len(legacy.BlockedBrowsers) != 0 {
		t.Fatal("omitted filters did not retain existing defaults")
	}
	body["block_empty_language"] = true
	body["blocked_browsers"] = []string{" EDGE ", "Chrome", "edge", " FIREFOX "}
	c := createTestCloak(t, s, owner, body)
	wantBrowsers := []string{"edge", "chrome", "firefox"}
	if !c.BlockEmptyLanguage || !reflect.DeepEqual(c.BlockedBrowsers, wantBrowsers) {
		t.Fatalf("new filters not normalized: %+v", c.Cloak)
	}
	var saved Cloak
	if err := s.DB.First(&saved, "id = ?", c.ID).Error; err != nil || !saved.BlockEmptyLanguage || !reflect.DeepEqual(saved.BlockedBrowsers, wantBrowsers) {
		t.Fatalf("new filters not persisted: %+v, %v", saved, err)
	}
	for _, invalid := range [][]string{{"opera"}, {""}, {"  "}, {"chrome", "safari", "firefox", "edge", "edge"}} {
		body["blocked_browsers"], body["block_empty_language"] = invalid, false
		requireCode(t, call(t, s, "POST", "/api/cloaks", body, cookie, csrf), 400)
		requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf), 400)
		if err := s.DB.First(&saved, "id = ?", c.ID).Error; err != nil || !saved.BlockEmptyLanguage || !reflect.DeepEqual(saved.BlockedBrowsers, wantBrowsers) {
			t.Fatalf("rejected update mutated saved filters: %+v, %v", saved, err)
		}
	}
	if countRows(t, s, &Cloak{}) != 2 {
		t.Fatal("invalid filter input created rules")
	}
	body["blocked_browsers"], body["block_empty_language"] = []string{}, false
	w := call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf)
	requireCode(t, w, 200)
	var updated cloakView
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil || updated.BlockEmptyLanguage || updated.BlockedBrowsers == nil || len(updated.BlockedBrowsers) != 0 {
		t.Fatalf("filters could not be explicitly cleared: %s, %v", w.Body.String(), err)
	}
	if err := s.DB.First(&saved, "id = ?", c.ID).Error; err != nil || saved.BlockEmptyLanguage || len(saved.BlockedBrowsers) != 0 {
		t.Fatalf("cleared filters were not persisted: %+v, %v", saved, err)
	}
	w = call(t, s, "GET", "/api/cloaks", nil, cookie, "")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), `"block_empty_language":false`) || !strings.Contains(w.Body.String(), `"blocked_browsers":[]`) {
		t.Fatal("list response omitted explicit cleared filter values")
	}
}

func TestCloakNewFiltersRouteToSafeDestination(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	for _, filter := range []string{"empty language", "Edge client"} {
		for _, safe := range []string{"", "https://safe.example/help?fixed=yes"} {
			t.Run(filter+"/"+safe, func(t *testing.T) {
				body := cloakFixture()
				body["safe_url"], body["blocked_languages"] = safe, []string{}
				body["block_empty_language"] = filter == "empty language"
				if filter == "Edge client" {
					body["blocked_browsers"] = []string{"edge"}
				}
				c := createTestCloak(t, s, owner, body)
				for _, method := range []string{"GET", "HEAD"} {
					r := httptest.NewRequest(method, c.EntryURL+"?visitor=private", nil)
					if filter == "Edge client" {
						r.Header.Set("Accept-Language", "en-US")
						r.Header.Set("User-Agent", "Mozilla/5.0 Chrome/132 Safari/537.36 Edg/132")
					}
					w := httptest.NewRecorder()
					s.Handler().ServeHTTP(w, r)
					if safe == "" {
						requireCode(t, w, 404)
						if w.Header().Get("Location") != "" || (method == "GET" && !strings.Contains(w.Body.String(), "Account not found")) {
							t.Fatal("blocked visitor did not receive the default safe page")
						}
					} else {
						requireCode(t, w, 302)
						if w.Header().Get("Location") != safe {
							t.Fatal("safe URL changed or received visitor parameters")
						}
					}
					if w.Header().Get("Cache-Control") != "no-store" || (method == "HEAD" && w.Body.Len() != 0) {
						t.Fatal("filter response is cacheable or HEAD returned a body")
					}
				}
				if err := s.DB.Model(&Cloak{}).Where("id = ?", c.ID).Updates(map[string]any{"enabled": false, "status": "off"}).Error; err != nil {
					t.Fatal(err)
				}
				r := httptest.NewRequest("GET", c.EntryURL, nil)
				r.Header.Set("User-Agent", "Chrome/132 Safari/537.36 Edg/132")
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				requireCode(t, w, 302)
				if !strings.HasPrefix(w.Header().Get("Location"), "https://target.example/path?") {
					t.Fatal("disabled rule did not bypass the new filters")
				}
			})
		}
	}
}

func TestCloakNewFiltersRunBeforeAppPWAAndPool(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	app := seedApp(t, s, owner, 120)
	pwa := createPWAFixture(t, s, owner, "Filtered PWA")
	pool := TrafficPool{Slug: randomHex(4), UserID: owner.ID, Enabled: true, Mode: "sequential"}
	if err := s.DB.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&TrafficLink{ID: randomHex(16), PoolSlug: pool.Slug, UserID: owner.ID, Position: 0, URL: "https://pool.example/first", Status: "safe"}).Error; err != nil {
		t.Fatal(err)
	}
	cloaks := map[string]cloakView{}
	for _, kind := range []string{"app", "pwa", "traffic_pool"} {
		t.Run(kind, func(t *testing.T) {
			body := cloakFixture()
			body["target_type"], body["target_id"], body["app_target_mode"] = kind, app.ID, "direct"
			body["blocked_languages"], body["block_empty_language"], body["blocked_browsers"] = []string{}, true, []string{"edge"}
			if kind == "pwa" {
				body["target_id"] = pwa.ID
			}
			if kind == "traffic_pool" {
				body["traffic_pool_slug"] = pool.Slug
			}
			c := createTestCloak(t, s, owner, body)
			cloaks[kind] = c
			for _, blockedBy := range []string{"empty language", "Edge client"} {
				r := httptest.NewRequest("GET", c.EntryURL, nil)
				if blockedBy == "Edge client" {
					r.Header.Set("Accept-Language", "en")
					r.Header.Set("User-Agent", "Chrome/132 Safari/537.36 Edg/132")
				}
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				requireCode(t, w, 302)
				if w.Header().Get("Location") != "https://safe.example/help?fixed=yes" {
					t.Fatalf("%s was handled by the target before %s filtering", kind, blockedBy)
				}
				if countRows(t, s, &Ticket{}) != 0 {
					t.Fatal("blocked app request created a download ticket")
				}
				var current TrafficPool
				if err := s.DB.First(&current, "slug = ?", pool.Slug).Error; err != nil || current.Cursor != 0 {
					t.Fatalf("blocked pool request advanced its cursor: %+v, %v", current, err)
				}
			}
		})
	}
	// Positive controls ensure the targets are usable, so safe-page fallbacks
	// cannot accidentally make the filter-ordering assertions above pass.
	for _, tc := range []struct {
		kind string
		code int
	}{{"app", 307}, {"pwa", 200}, {"traffic_pool", 302}} {
		r := httptest.NewRequest("GET", cloaks[tc.kind].EntryURL, nil)
		r.Header.Set("Accept-Language", "en-US")
		r.Header.Set("User-Agent", "Chrome/132 Safari/537.36")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		requireCode(t, w, tc.code)
		if w.Header().Get("Location") == "https://safe.example/help?fixed=yes" {
			t.Fatalf("allowed %s request fell back to the safe page", tc.kind)
		}
	}
	var current TrafficPool
	if err := s.DB.First(&current, "slug = ?", pool.Slug).Error; err != nil || current.Cursor != 1 || countRows(t, s, &Ticket{}) != 1 {
		t.Fatalf("allowed requests did not run their target handlers: cursor=%d, %v", current.Cursor, err)
	}
}
