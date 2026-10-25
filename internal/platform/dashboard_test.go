package platform

import (
	"encoding/json"
	"testing"
)

func TestDashboardAppTotalIncludesOwnedPWAAndExcludesDeleted(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, 0)
	other := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	assertCount := func(want int64) {
		t.Helper()
		w := call(t, s, "GET", "/api/dashboard", nil, cookie, csrf)
		requireCode(t, w, 200)
		var got struct {
			AppCount int64 `json:"app_count"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.AppCount != want {
			t.Fatalf("app_count=%d want=%d", got.AppCount, want)
		}
	}
	assertCount(0)
	pwa := createPWAFixture(t, s, u, "PWA only")
	assertCount(1)
	app := seedApp(t, s, u, 100)
	createPWAFixture(t, s, u, "Second PWA")
	seedApp(t, s, other, 100)
	createPWAFixture(t, s, other, "Other account")
	assertCount(3)
	// Paused apps still exist; deleting either type removes it from the total.
	if err := s.DB.Model(&pwa).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	assertCount(3)
	requireCode(t, call(t, s, "DELETE", "/api/pwas/"+pwa.ID, nil, cookie, csrf), 200)
	assertCount(2)
	requireCode(t, call(t, s, "DELETE", "/api/apps/"+app.ID, nil, cookie, csrf), 200)
	assertCount(1)
}
