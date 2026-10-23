package platform

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProxyVerificationDoesNotAuthorizePendingDomain(t *testing.T) {
	s := testServer(t)
	s.DNS = &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("CNAME hidden by proxy")
	}}
	owner := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	d := Domain{ID: randomHex(16), UserID: owner.ID, Hostname: "proxy.example.com", Token: randomHex(24)}
	if err := s.DB.Create(&d).Error; err != nil {
		t.Fatal(err)
	}
	w := call(t, s, "POST", "/api/domains/"+d.ID+"/verify", map[string]any{}, cookie, csrf)
	requireCode(t, w, 200)
	if err := s.DB.First(&d, "id = ?", d.ID).Error; err != nil {
		t.Fatal(err)
	}
	if d.VerifiedAt != nil || d.VerificationMethod != "http" || d.SSLStatus != "verifying" || d.VerificationRequestedAt == nil {
		t.Fatalf("incorrect pending verification: %+v", d)
	}
	r := httptest.NewRequest("GET", "https://proxy.example.com/r/example", nil)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 404)
	r = httptest.NewRequest("GET", "http://localhost/internal/tls/ask?domain=proxy.example.com", nil)
	w = httptest.NewRecorder()
	s.tlsAsk(w, r)
	requireCode(t, w, 404)
}

func TestFirstPartyDomainExceptionsStayNarrow(t *testing.T) {
	s := testServer(t)
	s.Config.PublicURL = "https://d1apk.com"
	s.Config.CNAMEHost = "dns.d1apk.com"
	t.Setenv("CUSTOM_DOMAIN_TEST_HOSTS", "aaa.d1apk.com,bbb.d1apk.com,d1apk.com,dns.d1apk.com")
	for _, host := range []string{"aaa.d1apk.com", "bbb.d1apk.com", "download.customer.com"} {
		if !s.customerHostnameAllowed(host) {
			t.Fatalf("expected allowed: %s", host)
		}
	}
	for _, host := range []string{"d1apk.com", "dns.d1apk.com", "admin.d1apk.com"} {
		if s.customerHostnameAllowed(host) {
			t.Fatalf("reserved host allowed: %s", host)
		}
	}
}

func TestCustomerDomainRoutesOnlyOwnersTraffic(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	other := seedUser(t, s, 0)
	now := s.Now()
	domain := Domain{ID: randomHex(16), UserID: owner.ID, Hostname: "customer.example.com", Token: randomHex(24), VerifiedAt: &now, SSLStatus: "ready"}
	if err := s.DB.Create(&domain).Error; err != nil {
		t.Fatal(err)
	}
	for _, u := range []User{owner, other} {
		p := TrafficPool{Slug: u.ID, UserID: u.ID, Enabled: true, Mode: "random"}
		if err := s.DB.Create(&p).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.DB.Create(&TrafficLink{ID: randomHex(16), PoolSlug: p.Slug, UserID: u.ID, URL: "https://example.com/app.apk"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		slug string
		code int
	}{{owner.ID, 302}, {other.ID, 404}} {
		r := httptest.NewRequest("GET", "https://customer.example.com/r/"+tc.slug, nil)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		requireCode(t, w, tc.code)
	}
	cookie, csrf := sessionFor(t, s, owner)
	w := call(t, s, "GET", "/api/traffic-pools", nil, cookie, csrf)
	requireCode(t, w, 200)
	domain.VerifiedAt = ptrDomainTime(now.Add(-49 * time.Hour))
	if err := s.DB.Save(&domain).Error; err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "https://customer.example.com/r/"+owner.ID, nil)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 404)
}

func ptrDomainTime(t time.Time) *time.Time { return &t }
