package platform

import (
	"bytes"
	"image/png"
	"strings"
	"testing"
)

func TestPWANotificationConsentAssets(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	app := createPWAFixture(t, s, u, "Consent app")
	route := "/p/" + app.Slug + "/notification.css"
	w := call(t, s, "GET", route, nil, nil, "")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), ".d1-notification-consent") || !strings.Contains(w.Header().Get("Content-Type"), "text/css") {
		t.Fatal("consent style unavailable")
	}
	logo := call(t, s, "GET", "/pwa-template-assets/push-google.png", nil, nil, "")
	requireCode(t, logo, 200)
	config, err := png.DecodeConfig(bytes.NewReader(logo.Body.Bytes()))
	if err != nil || config.Width != 512 || config.Height != 512 {
		t.Fatal("Google notification logo invalid")
	}
	if err := s.DB.Model(&app).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, call(t, s, "GET", route, nil, nil, ""), 404)
}
