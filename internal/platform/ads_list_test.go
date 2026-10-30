package platform

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAppListAdStatus(t *testing.T) {
	s := testServer(t)
	owner, outsider := seedUser(t, s, 0), seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	expected := map[string]bool{}
	var facebookApp Application
	for i, mode := range []string{"unconfigured", "disabled", "facebook", "tiktok", "both", "other-owner-config"} {
		app := seedApp(t, s, owner, 10)
		if err := s.DB.Model(&app).Updates(map[string]any{"name": mode, "created_at": s.Now().Add(time.Duration(i) * time.Second), "enabled": false}).Error; err != nil {
			t.Fatal(err)
		}
		expected[app.ID] = mode == "facebook" || mode == "tiktok" || mode == "both"
		if mode == "unconfigured" {
			continue
		}
		cfg := defaultAds(app)
		cfg.FacebookEnabled = mode == "facebook" || mode == "both" || mode == "other-owner-config"
		cfg.TikTokEnabled = mode == "tiktok" || mode == "both"
		cfg.FacebookPixel = "123456789"
		cfg.TikTokPixel = "TESTPIXEL1"
		cfg.FacebookToken, _ = s.encryptSecret("private-facebook-token")
		cfg.TikTokToken, _ = s.encryptSecret("private-tiktok-token")
		cfg.APISecret, _ = s.encryptSecret("private-signing-key")
		if mode == "other-owner-config" {
			cfg.UserID = outsider.ID
		}
		if err := s.DB.Create(&cfg).Error; err != nil {
			t.Fatal(err)
		}
		if mode == "facebook" {
			facebookApp = app
		}
	}
	otherApp := seedApp(t, s, outsider, 10)
	seedAds(t, s, otherApp)
	removed := seedApp(t, s, owner, 10)
	seedAds(t, s, removed)
	if err := s.DB.Delete(&removed).Error; err != nil {
		t.Fatal(err)
	}
	read := func(query string, total int64, count int) []appListView {
		t.Helper()
		w := call(t, s, "GET", "/api/apps"+query, nil, cookie, "")
		requireCode(t, w, 200)
		for _, secret := range []string{"private-facebook-token", "private-tiktok-token", "private-signing-key", "facebook_token", "tiktok_token", "api_secret"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatalf("list leaked %s", secret)
			}
		}
		var result struct {
			Data  []appListView `json:"data"`
			Total int64         `json:"total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Total != total || len(result.Data) != count {
			t.Fatalf("wrong pagination: %s", w.Body.String())
		}
		for _, app := range result.Data {
			want, owned := expected[app.ID]
			if !owned || app.AdsEnabled != want {
				t.Fatalf("incorrect ad status or owner: %+v", app)
			}
		}
		return result.Data
	}
	read("", 6, 6)
	read("?q=facebook", 1, 1)
	first, second := read("?page=1&per_page=2", 6, 2), read("?page=2&per_page=2", 6, 2)
	if first[0].ID == second[0].ID {
		t.Fatal("pagination repeated rows")
	}
	// The list follows persisted platform switches immediately after a save.
	var cfg AdConfig
	if err := s.DB.First(&cfg, "app_id = ?", facebookApp.ID).Error; err != nil {
		t.Fatal(err)
	}
	cfg.FacebookEnabled = false
	requireCode(t, call(t, s, "PUT", "/api/apps/"+facebookApp.ID+"/ads", map[string]any{"config": cfg}, cookie, csrf), 200)
	expected[facebookApp.ID] = false
	read("?q=facebook", 1, 1)
}

func TestConversionListApplicationNumbers(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, 0), seedUser(t, s, 0)
	source, target := seedApp(t, s, owner, 10), seedApp(t, s, owner, 10)
	foreign := seedApp(t, s, other, 10)
	removed := seedApp(t, s, owner, 10)
	if err := s.DB.Delete(&removed).Error; err != nil {
		t.Fatal(err)
	}
	pwa := createPWAFixture(t, s, owner, "PWA events")
	cookie, _ := sessionFor(t, s, owner)
	add := func(appID, targetID, event string) Conversion {
		t.Helper()
		c := Conversion{ID: randomHex(16), AppID: appID, UserID: owner.ID, DownloadedAppID: targetID, Event: event, EventID: randomHex(32), AttributionID: randomHex(16), EventTime: s.Now(), CreatedAt: s.Now()}
		if err := s.DB.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
		return c
	}
	direct := add(source.ID, source.ID, "download_start")
	routed := add(source.ID, target.ID, "download_start")
	opened := add(source.ID, "", "app_open")
	deleted := add(source.ID, removed.ID, "download_complete")
	foreignTarget := add(source.ID, foreign.ID, "download_start")
	pwaEvent := add(pwaAdID(pwa.ID), "", "pwa_click")
	delivery := Delivery{ID: randomHex(16), ConversionID: routed.ID, Platform: "facebook", EventName: "Lead", Status: "sent"}
	if err := s.DB.Create(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	read := func(path string, total int64, count int) []conversionListView {
		t.Helper()
		w := call(t, s, "GET", path, nil, cookie, "")
		requireCode(t, w, 200)
		var result struct {
			Data  []conversionListView `json:"data"`
			Total int64                `json:"total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Total != total || len(result.Data) != count {
			t.Fatalf("unexpected page: %s", w.Body.String())
		}
		if strings.Contains(w.Body.String(), foreign.Slug) {
			t.Fatal("another account's application number leaked")
		}
		return result.Data
	}
	path := "/api/apps/" + source.ID + "/events"
	expected := map[string]string{direct.ID: source.Slug, routed.ID: target.Slug, opened.ID: source.Slug, deleted.ID: "", foreignTarget.ID: ""}
	for _, c := range read(path, 5, 5) {
		if slug, ok := expected[c.ID]; !ok || c.ApplicationSlug != slug {
			t.Fatalf("wrong application number: %+v", c)
		}
		if c.ID == routed.ID && (len(c.Deliveries) != 1 || c.Deliveries[0].Status != "sent") {
			t.Fatal("delivery status was lost")
		}
	}
	first, second := read(path+"?per_page=2&page=1", 5, 2), read(path+"?per_page=2&page=2", 5, 2)
	if first[0].ID == second[0].ID || first[1].ID == second[0].ID {
		t.Fatal("pagination repeated an event")
	}
	if err := s.DB.Model(&target).Update("slug", "renamed-target").Error; err != nil {
		t.Fatal(err)
	}
	for _, c := range read(path, 5, 5) {
		if c.ID == routed.ID && c.ApplicationSlug != "renamed-target" {
			t.Fatal("application number did not follow the application list")
		}
	}
	pwaRows := read("/api/pwas/"+pwa.ID+"/events", 1, 1)
	if pwaRows[0].ID != pwaEvent.ID || pwaRows[0].ApplicationSlug != pwa.Slug {
		t.Fatal("PWA application number or event scope is incorrect")
	}
	requireCode(t, call(t, s, "GET", "/api/apps/"+foreign.ID+"/events", nil, cookie, ""), 404)
}
