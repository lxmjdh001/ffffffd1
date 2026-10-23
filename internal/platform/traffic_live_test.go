package platform

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

// Opt-in: uses real Google quota, with an isolated test database.
func TestTrafficLiveWebRisk(t *testing.T) {
	if os.Getenv("D1_TEST_LIVE_WEB_RISK") != "1" {
		t.Skip("live check disabled")
	}
	key := os.Getenv("GOOGLE_WEB_RISK_API_KEY")
	if key == "" {
		t.Fatal("Web Risk key missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 15 * time.Second}
	urls := []string{"https://example.com/download.apk", "http://testsafebrowsing.appspot.com/s/malware.html"}
	s := testServer(t)
	u := seedUser(t, s, 0)
	if err := s.DB.Model(&u).Update("pro_until", s.Now().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := sessionFor(t, s, u)
	requireCode(t, call(t, s, "POST", "/api/traffic-pools", map[string]any{"enabled": true, "risk_enabled": true, "urls": urls}, cookie, csrf), 200)
	var pool TrafficPool
	if err := s.DB.First(&pool, "user_id = ?", u.ID).Error; err != nil {
		t.Fatal(err)
	}
	for i, uri := range urls {
		unsafe, err := lookupWebRisk(ctx, client, "https://webrisk.googleapis.com/v1/uris:search", key, uri)
		if err != nil {
			t.Fatal(err)
		}
		if unsafe != (i == 1) {
			t.Fatalf("unexpected threat verdict for fixture %d", i)
		}
		status := "clean"
		if unsafe {
			status = "unsafe"
		}
		if err := s.DB.Model(&TrafficLink{}).Where("pool_slug = ? AND url = ?", pool.Slug, uri).Updates(map[string]any{"status": status, "checked_at": s.Now()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	w := call(t, s, "GET", "/r/"+pool.Slug, nil, nil, "")
	requireCode(t, w, 302)
	if w.Header().Get("Location") != urls[0] {
		t.Fatal("unsafe target was not excluded")
	}
	if err := s.DB.Model(&TrafficLink{}).Where("pool_slug = ?", pool.Slug).Update("status", "unsafe").Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, call(t, s, "GET", "/r/"+pool.Slug, nil, nil, ""), 503)
	t.Log("real Google lookup passed; unsafe target excluded; all-unsafe pool blocked")
}
