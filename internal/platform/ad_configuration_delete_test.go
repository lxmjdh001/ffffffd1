package platform

import (
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestAdConfigurationDeletePreservesResourcesAndHistory(t *testing.T) {
	for _, kind := range []string{"apps", "pwas"} {
		for _, mode := range []string{"quick", "deep"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				s := testServer(t)
				u := seedUser(t, s, Dollar)
				a := seedApp(t, s, u, 100)
				p := createPWAFixture(t, s, u, "Keep this PWA")
				id, app := a.ID, a
				other := pwaAdApplication(p)
				if kind == "pwas" {
					id, app, other = p.ID, pwaAdApplication(p), a
				}
				for _, resource := range []Application{app, other} {
					cfg := quickAdConfig(resource)
					cfg.Mode, cfg.FacebookToken, cfg.APISecret = mode, "encrypted-credential", "encrypted-secret"
					if err := s.DB.Create(&cfg).Error; err != nil {
						t.Fatal(err)
					}
				}
				for _, resource := range []Application{app, other} {
					for _, status := range []string{"pending", "processing", "sent", "failed"} {
						event := Conversion{ID: randomHex(16), AppID: resource.ID, UserID: u.ID, AttributionID: randomHex(16), Event: "download", EventID: randomHex(32), Consent: true, EventTime: s.Now()}
						if err := s.DB.Create(&event).Error; err != nil {
							t.Fatal(err)
						}
						delivery := Delivery{ID: randomHex(16), ConversionID: event.ID, Platform: "facebook", Status: status, NextAttemptAt: s.Now(), LastError: "existing history"}
						if err := s.DB.Create(&delivery).Error; err != nil {
							t.Fatal(err)
						}
					}
				}
				cookie, csrf := sessionFor(t, s, u)
				path := "/api/ad-configurations/" + kind + "/" + id
				requireCode(t, call(t, s, "DELETE", path, nil, cookie, csrf), 200)
				var n int64
				if err := s.DB.Unscoped().Model(&AdConfig{}).Where("app_id = ?", app.ID).Count(&n).Error; err != nil || n != 0 {
					t.Fatal("configuration or credentials were not physically deleted", err)
				}
				if countRows(t, s, &Application{}) != 1 || countRows(t, s, &PWAApp{}) != 1 || countRows(t, s, &AdConfig{}) != 1 || countRows(t, s, &Conversion{}) != 8 || countRows(t, s, &Delivery{}) != 8 {
					t.Fatal("deletion affected applications, another configuration, or history")
				}
				var deliveries []Delivery
				if err := s.DB.Joins("JOIN conversions ON conversions.id = deliveries.conversion_id").Where("conversions.app_id = ?", app.ID).Select("deliveries.*").Find(&deliveries).Error; err != nil {
					t.Fatal(err)
				}
				cancelled := 0
				for _, d := range deliveries {
					if d.Status == "pending" || d.Status == "processing" {
						t.Fatal("deleted configuration retained a queued retry")
					}
					if d.LastError == "回传配置已删除" {
						cancelled++
					}
				}
				if cancelled != 2 {
					t.Fatal("completed/failed history was rewritten")
				}
				w := call(t, s, "GET", "/api/ad-configurations", nil, cookie, "")
				requireCode(t, w, 200)
				if strings.Contains(w.Body.String(), `"resource_id":"`+id+`"`) {
					t.Fatal("deleted config is still in the list")
				}
				requireCode(t, call(t, s, "DELETE", path, nil, cookie, csrf), 404)
				cfg := quickAdConfig(app)
				requireCode(t, call(t, s, "POST", "/api/ad-configurations", map[string]any{"resource_kind": kind, "resource_id": id, "config": cfg, "facebook_token": "new-fixture-token"}, cookie, csrf), 200)
				// Other resources retain their own queued work. Deleted work stays failed,
				// even when the same resource gets a fresh configuration.
				if err := s.DB.Model(&Delivery{}).Joins("JOIN conversions ON conversions.id = deliveries.conversion_id").Where("conversions.app_id = ? AND deliveries.status IN ?", app.ID, []string{"pending", "processing"}).Count(&n).Error; err != nil {
					t.Fatal(err)
				}
				if n != 0 {
					t.Fatal("recreating a configuration reactivated its old queue")
				}
			})
		}
	}
}

func TestAdConfigurationDeleteOwnershipCSRFAndAtomicity(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	other := seedUser(t, s, Dollar)
	a := seedApp(t, s, u, 100)
	p := createPWAFixture(t, s, u, "Private PWA")
	cookie, csrf := sessionFor(t, s, u)
	foreignCookie, foreignCSRF := sessionFor(t, s, other)
	for kind, app := range map[string]Application{"apps": a, "pwas": pwaAdApplication(p)} {
		cfg := quickAdConfig(app)
		if err := s.DB.Create(&cfg).Error; err != nil {
			t.Fatal(err)
		}
		id := a.ID
		if kind == "pwas" {
			id = p.ID
		}
		path := "/api/ad-configurations/" + kind + "/" + id
		requireCode(t, call(t, s, "DELETE", path, nil, nil, ""), 401)
		requireCode(t, call(t, s, "DELETE", path, nil, foreignCookie, foreignCSRF), 404)
		requireCode(t, call(t, s, "DELETE", path, nil, cookie, "wrong"), 403)
	}
	requireCode(t, call(t, s, "DELETE", "/api/ad-configurations/unknown/"+a.ID, nil, cookie, csrf), 404)
	if countRows(t, s, &AdConfig{}) != 2 {
		t.Fatal("unauthorized deletion changed a configuration")
	}
	// A failed audit must roll back the credential deletion as well.
	const callback = "fail_ad_delete_audit"
	if err := s.DB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audits" {
			tx.AddError(errors.New("forced audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer s.DB.Callback().Create().Remove(callback)
	requireCode(t, call(t, s, "DELETE", "/api/ad-configurations/apps/"+a.ID, nil, cookie, csrf), 500)
	if countRows(t, s, &AdConfig{}) != 2 {
		t.Fatal("failed deletion did not roll back")
	}
}
