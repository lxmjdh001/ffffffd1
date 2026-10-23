package platform

import (
	"net/http/httptest"
	"testing"
	"time"
)

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
