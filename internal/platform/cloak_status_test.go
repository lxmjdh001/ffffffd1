package platform

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCloakStatusAPITransitionsAndCompatibility(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	body := cloakFixture()
	c := createTestCloak(t, s, owner, body)
	check := func(want string, wantEnabled bool) {
		t.Helper()
		var saved Cloak
		if err := s.DB.First(&saved, "id = ?", c.ID).Error; err != nil || saved.Status != want || saved.Enabled != wantEnabled {
			t.Fatalf("status not persisted: %+v err=%v", saved, err)
		}
		w := call(t, s, "GET", "/api/cloaks", nil, cookie, "")
		requireCode(t, w, 200)
		var response struct {
			Items []cloakView `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range response.Items {
			if item.ID == c.ID {
				found = true
				if item.Status != want || item.Enabled != wantEnabled {
					t.Fatalf("wrong list status: %+v", item)
				}
			}
		}
		if !found {
			t.Fatalf("missing rule in %s", w.Body.String())
		}
	}
	check("rules", true)
	for _, tc := range []struct {
		status  string
		enabled bool
	}{{"off", true}, {"blocked", true}, {"rules", false}, {"off", false}, {"rules", true}} {
		body["status"], body["enabled"] = tc.status, tc.enabled
		w := call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf)
		requireCode(t, w, 200)
		check(tc.status, tc.status == "rules")
	}
	for _, invalid := range []any{"unknown", "RULES", " block ", 123, true} {
		body["status"] = invalid
		requireCode(t, call(t, s, "POST", "/api/cloaks", body, cookie, csrf), 400)
		requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf), 400)
		check("rules", true)
	}
	delete(body, "status")
	for _, enabled := range []bool{false, true} {
		body["enabled"] = enabled
		requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf), 200)
		want := "off"
		if enabled {
			want = "rules"
		}
		check(want, enabled)
	}
}

func TestCloakStatusControlsIPAndOrdinaryRules(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	for _, status := range []string{"rules", "off", "blocked"} {
		body := cloakFixture()
		body["status"] = status
		body["allowed_ips"], body["blocked_ips"] = []string{"203.0.113.8"}, []string{"203.0.113.9"}
		c := createTestCloak(t, s, owner, body)
		for _, tc := range []struct {
			peer, language string
			rulesBlocked   bool
		}{{"203.0.113.8:443", "zh", false}, {"203.0.113.9:443", "en", true}, {"203.0.113.10:443", "zh", true}, {"203.0.113.10:443", "en", false}} {
			w := cloakIPRequest(s, c, "GET", tc.peer, "", tc.language)
			requireCode(t, w, 302)
			wantBlocked := status == "blocked" || status == "rules" && tc.rulesBlocked
			if got := w.Header().Get("Location") == c.SafeURL; got != wantBlocked {
				t.Fatalf("status=%s peer=%s lang=%s blocked=%v want=%v", status, tc.peer, tc.language, got, wantBlocked)
			}
		}
	}
}

func TestCloakStatusLegacyEmptyDatabaseValue(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	for _, enabled := range []bool{true, false} {
		c := createTestCloak(t, s, owner, cloakFixture())
		if err := s.DB.Model(&Cloak{}).Where("id = ?", c.ID).Updates(map[string]any{"status": "", "enabled": enabled}).Error; err != nil {
			t.Fatal(err)
		}
		w := cloakIPRequest(s, c, "GET", "203.0.113.8:443", "", "zh")
		requireCode(t, w, 302)
		if got := w.Header().Get("Location") == c.SafeURL; got != enabled {
			t.Fatalf("legacy enabled=%v blocked=%v", enabled, got)
		}
	}
}

func TestCloakStatusBlockedSafeResponses(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	for _, safe := range []string{"", "https://safe.example/help?fixed=yes"} {
		body := cloakFixture()
		body["status"], body["safe_url"], body["allowed_ips"] = "blocked", safe, []string{"203.0.113.8"}
		c := createTestCloak(t, s, owner, body)
		for _, method := range []string{"GET", "HEAD"} {
			w := cloakIPRequest(s, c, method, "203.0.113.8:443", "", "en")
			if safe == "" {
				requireCode(t, w, 404)
				if method == "GET" && !strings.Contains(w.Body.String(), "Account not found") {
					t.Fatal("missing default safe page")
				}
			} else {
				requireCode(t, w, 302)
				if w.Header().Get("Location") != safe {
					t.Fatal("blocked mode forwarded query to safety URL")
				}
			}
			if w.Header().Get("Cache-Control") != "no-store" || method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("blocked response cacheable or HEAD has a body")
			}
		}
	}
}

func TestCloakStatusBlockedRunsBeforeTargets(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	app := seedApp(t, s, owner, 120)
	pwa := createPWAFixture(t, s, owner, "Blocked PWA")
	pool := TrafficPool{Slug: randomHex(4), UserID: owner.ID, Enabled: true, Mode: "sequential"}
	if err := s.DB.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&TrafficLink{ID: randomHex(16), PoolSlug: pool.Slug, UserID: owner.ID, Position: 0, URL: "https://pool.example/first", Status: "safe"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"app", "pwa", "traffic_pool"} {
		body := cloakFixture()
		body["status"], body["allowed_ips"], body["blocked_languages"] = "blocked", []string{"203.0.113.8"}, []string{}
		body["target_type"], body["target_id"], body["app_target_mode"] = kind, app.ID, "direct"
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
			t.Fatalf("blocked mode entered %s target", kind)
		}
	}
	var current TrafficPool
	if err := s.DB.First(&current, "slug = ?", pool.Slug).Error; err != nil || current.Cursor != 0 || countRows(t, s, &Ticket{}) != 0 {
		t.Fatalf("blocked mode caused target side effects: cursor=%d err=%v", current.Cursor, err)
	}
	var visits []AccessVisit
	if err := s.DB.Where("kind = ?", "cloaks").Find(&visits).Error; err != nil {
		t.Fatal(err)
	}
	if len(visits) != 3 {
		t.Fatalf("missing blocked visits: %+v", visits)
	}
	for _, v := range visits {
		if v.Outcome != "blocked" {
			t.Fatalf("wrong status statistics: %+v", v)
		}
	}
}
