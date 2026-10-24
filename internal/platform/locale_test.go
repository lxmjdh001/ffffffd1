package platform

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicConfigCountryHintIsTrustedAndPrivate(t *testing.T) {
	s := testServer(t)
	_, local, _ := net.ParseCIDR("127.0.0.0/8")
	s.Config.TrustedProxies = []*net.IPNet{local}
	for _, tc := range []struct{ name, peer, forwarded, country, want string }{
		{"no country", "203.0.113.8:1234", "", "", "XX"},
		{"forged country", "203.0.113.8:1234", "", "JP", "XX"},
		{"forged chain", "203.0.113.8:1234", "173.245.48.5", "KR", "XX"},
		{"trusted CDN", "173.245.48.5:1234", "203.0.113.8", "BR", "BR"},
		{"trusted proxy and CDN", "127.0.0.1:1234", "203.0.113.8, 173.245.48.5", "fr", "FR"},
		{"untrusted intervening hop", "127.0.0.1:1234", "173.245.48.5, 203.0.113.8", "DE", "XX"},
		{"malformed country", "173.245.48.5:1234", "", "Japan", "XX"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, s.Config.PublicURL+"/api/config", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-For", tc.forwarded)
			r.Header.Set("CF-IPCountry", tc.country)
			w := httptest.NewRecorder()
			s.publicConfig(w, r)
			requireCode(t, w, http.StatusOK)
			var config struct {
				Country string `json:"country_code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &config); err != nil {
				t.Fatal(err)
			}
			if config.Country != tc.want {
				t.Fatalf("country=%q, want %q", config.Country, tc.want)
			}
			if w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("visitor-specific locale hint must not enter a shared cache")
			}
		})
	}
}
