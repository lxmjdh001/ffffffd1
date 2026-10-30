package platform

import (
	"net"
	"net/http/httptest"
	"testing"
)

func TestClientIPCloudflareVerifiedEdgeAndUntrustedHeaders(t *testing.T) {
	_, local, _ := net.ParseCIDR("127.0.0.0/8")
	s := &Server{Config: Config{TrustedProxies: []*net.IPNet{local}}}
	for _, tc := range []struct{ name, peer, xff, cf, want string }{
		{"direct browser spoof", "203.0.113.9:1234", "173.245.48.5", "1.2.3.4", "203.0.113.9"},
		{"nginx direct browser spoof", "127.0.0.1:1234", "173.245.48.5, 203.0.113.9", "1.2.3.4", "203.0.113.9"},
		{"nginx edge", "127.0.0.1:1234", "173.245.48.5", "203.0.113.9", "203.0.113.9"},
		{"nginx rotated edge", "127.0.0.1:1234", "104.16.0.5", "203.0.113.9", "203.0.113.9"},
		{"direct edge", "173.245.48.5:443", "malformed", "203.0.113.9", "203.0.113.9"},
		{"ipv6 edge visitor", "[2606:4700::1]:443", "", "2001:db8::1234", "2001:db8::1234"},
		{"mapped IPv4 visitor", "173.245.48.5:443", "", "::ffff:203.0.113.9", "203.0.113.9"},
		{"malformed CF header", "127.0.0.1:1234", "104.16.0.5", "203.0.113.9,1.2.3.4", "104.16.0.5"},
		{"missing CF header", "127.0.0.1:1234", "104.16.0.5", "", "104.16.0.5"},
		{"broken internal chain", "127.0.0.1:1234", "104.16.0.5,invalid", "203.0.113.9", "127.0.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "https://d1apk.com/", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-For", tc.xff)
			r.Header.Set("CF-Connecting-IP", tc.cf)
			if got := s.clientIP(r); got != tc.want {
				t.Fatalf("IP=%s want=%s", got, tc.want)
			}
		})
	}
}

func TestAPKDownloadStartBillingRotatingCloudflareEdgesShareUV(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, 50000)
	app := seedApp(t, s, u, 100)
	_, local, _ := net.ParseCIDR("127.0.0.0/8")
	s.Config.TrustedProxies = []*net.IPNet{local}
	for _, edge := range []string{"173.245.48.5", "104.16.0.5"} {
		r := httptest.NewRequest("GET", s.Config.PublicURL+"/a/"+app.Slug+"/direct", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("User-Agent", "Same browser")
		r.Header.Set("X-Forwarded-For", edge)
		r.Header.Set("CF-Connecting-IP", "203.0.113.9")
		raw, err := s.createDownloadTicket(r, app, randomHex(32), randomHex(16), Attribution{})
		if err != nil {
			t.Fatal(err)
		}
		var ticket Ticket
		if err := s.DB.First(&ticket, "hash = ?", hashString(raw)).Error; err != nil {
			t.Fatal(err)
		}
		if ticket.IP != "203.0.113.9" {
			t.Fatal("stored the CDN edge instead of visitor")
		}
		if err := s.reserveDownload(ticket); err != nil {
			t.Fatal(err)
		}
		if err := s.confirmAPKDownloadStart(ticket, s.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if got := usageOwner(t, s, u.ID); got.BalanceMicros != 0 || got.HeldMicros != 0 || countRows(t, s, &MeteredEvent{}) != 1 {
		t.Fatal("CDN edge rotation duplicated UV billing")
	}
}
