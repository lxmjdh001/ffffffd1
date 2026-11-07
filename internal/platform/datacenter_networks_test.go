package platform

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestDatacenterNetworkNormalization(t *testing.T) {
	for _, tc := range [][2]string{{"39.173.76.5", "39.173.76.5/32"}, {"192.0.2.19/24", "192.0.2.0/24"}, {"2001:0db8::abcd/32", "2001:db8::/32"}, {"::ffff:192.0.2.19/120", "192.0.2.0/24"}, {"::ffff:192.0.2.19", "192.0.2.19/32"}, {"2401:b60::/32", "2401:b60::/32"}} {
		prefix, err := normalizeDatacenterNetwork(tc[0])
		if err != nil || prefix.String() != tc[1] {
			t.Fatalf("%q normalized to %v: %v", tc[0], prefix, err)
		}
	}
	for _, raw := range []string{"", "example.com", "1.2.3.4:80", "[::1]", "192.0.2.0/33", "fe80::1%en0", "::ffff:192.0.2.1/80"} {
		if _, err := normalizeDatacenterNetwork(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
func TestDatacenterSeedOnlyOnce(t *testing.T) {
	s := testServer(t)
	var count int64
	s.DB.Model(&DatacenterNetwork{}).Count(&count)
	if count != 258 {
		t.Fatalf("seed count %d", count)
	}
	var row DatacenterNetwork
	if err := s.DB.First(&row, "cidr = ?", "39.173.76.5/32").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Delete(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&DatacenterNetwork{}).Where("cidr = ?", "2401:b60::/32").Updates(map[string]any{"remark": "edited", "enabled": false}).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	s.DB.Model(&DatacenterNetwork{}).Count(&count)
	if count != 257 {
		t.Fatal("restart restored deleted rule")
	}
	row = DatacenterNetwork{}
	if err := s.DB.First(&row, "cidr = ?", "2401:b60::/32").Error; err != nil {
		t.Fatal(err)
	}
	if row.Enabled || row.Remark != "edited" {
		t.Fatal("restart overwrote edited rule")
	}
}
func datacenterAdmin(t *testing.T, s *Server) (*http.Cookie, string) {
	t.Helper()
	u := seedUser(t, s, 0)
	if err := s.DB.Model(&u).Update("role", "admin").Error; err != nil {
		t.Fatal(err)
	}
	return sessionFor(t, s, u)
}
func TestDatacenterAdminCRUDAndImport(t *testing.T) {
	s := testServer(t)
	cookie, csrf := datacenterAdmin(t, s)
	member := seedUser(t, s, 0)
	mc, mcsrf := sessionFor(t, s, member)
	base := "/api/admin/datacenter-networks"
	for _, verb := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		p := base
		if verb == "PUT" || verb == "PATCH" || verb == "DELETE" {
			p += "/missing"
		}
		requireCode(t, call(t, s, verb, p, map[string]any{"cidr": "203.0.113.5"}, mc, mcsrf), 403)
	}
	requireCode(t, call(t, s, "GET", base, nil, nil, ""), 401)
	w := call(t, s, "POST", base, map[string]any{"cidr": "203.0.113.5/24", "remark": "custom"}, cookie, csrf)
	requireCode(t, w, 200)
	var row DatacenterNetwork
	if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row.CIDR != "203.0.113.0/24" || !row.Enabled {
		t.Fatalf("normalization/default: %s", w.Body.String())
	}
	requireCode(t, call(t, s, "POST", base, map[string]any{"cidr": "203.0.113.199/24"}, cookie, csrf), 409)
	requireCode(t, call(t, s, "POST", base, map[string]any{"cidr": "2001:db8::/32"}, cookie, csrf), 200)
	// Real IP membership search, family/status filters, pagination and auth.
	w = call(t, s, "GET", base+"?q=203.0.113.99", nil, cookie, "")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), row.ID) {
		t.Fatal("host search did not find containing network")
	}
	matched, err := s.datacenterIPBlocked(context.Background(), "203.0.113.99")
	if err != nil || !matched {
		t.Fatal("enabled rule not matched")
	}
	requireCode(t, call(t, s, "PATCH", base+"/"+row.ID, map[string]any{"enabled": false}, cookie, csrf), 200)
	matched, err = s.datacenterIPBlocked(context.Background(), "203.0.113.99")
	if err != nil || matched {
		t.Fatal("disabled rule survived cache invalidation")
	}
	w = call(t, s, "GET", base+"?family=ipv4&enabled=false&q=custom", nil, cookie, "")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), row.ID) {
		t.Fatal("filtered search lost rule")
	}
	w = call(t, s, "POST", base+"/import", map[string]any{"entries": "203.0.113.17/24\n198.51.100.2\n198.51.100.2/32", "remark": "batch"}, cookie, csrf)
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), `"duplicates":2`) || !strings.Contains(w.Body.String(), `"inserted":1`) {
		t.Fatalf("duplicate counts: %s", w.Body.String())
	}
	var saved DatacenterNetwork
	if err := s.DB.First(&saved, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Enabled || saved.Remark != "custom" {
		t.Fatal("import overwrote existing rule")
	}
	var before, after int64
	s.DB.Model(&DatacenterNetwork{}).Count(&before)
	requireCode(t, call(t, s, "POST", base+"/import", map[string]any{"entries": "192.0.2.1\ninvalid"}, cookie, csrf), 400)
	s.DB.Model(&DatacenterNetwork{}).Count(&after)
	if before != after {
		t.Fatal("invalid batch partially imported")
	}
	requireCode(t, call(t, s, "PUT", base+"/"+row.ID, map[string]any{"cidr": "203.0.113.17", "enabled": true, "remark": "host"}, cookie, csrf), 200)
	requireCode(t, call(t, s, "DELETE", base+"/"+row.ID, nil, cookie, csrf), 200)
	requireCode(t, call(t, s, "DELETE", base+"/"+row.ID, nil, cookie, csrf), 404)
	var audits int64
	s.DB.Model(&Audit{}).Where("action LIKE ?", "datacenter.%").Count(&audits)
	if audits < 5 {
		t.Fatal("missing mutation audit records")
	}
}
func TestDatacenterCloakRoutingAndTrustedIP(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	body := cloakFixture()
	body["block_datacenter"] = true
	c := createTestCloak(t, s, owner, body)
	_, proxy, _ := net.ParseCIDR("127.0.0.1/32")
	s.Config.TrustedProxies = []*net.IPNet{proxy}
	for _, tc := range []struct {
		name, peer, xff, visitor string
		blocked                  bool
	}{
		{"seed IPv4", "3.9.8.7:2222", "", "", true}, {"seed IPv6", "[2401:b60:abcd::1]:2222", "", "", true}, {"single IP", "39.173.76.5:1234", "", "", true}, {"outside", "203.0.113.9:1234", "", "", false},
		{"CF edge with real visitor", "127.0.0.1:1234", "173.245.48.5", "203.0.113.9", false}, {"blocked real visitor through CF", "127.0.0.1:1234", "173.245.48.5", "3.9.8.7", true},
		{"spoofed forwarding ignored", "3.9.8.7:2222", "173.245.48.5", "203.0.113.9", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://localhost:8088/c/"+c.Slug, nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-For", tc.xff)
			r.Header.Set("CF-Connecting-IP", tc.visitor)
			r.Header.Set("User-Agent", "Mozilla/5.0")
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			requireCode(t, w, 302)
			if strings.HasPrefix(w.Header().Get("Location"), c.SafeURL) != tc.blocked {
				t.Fatalf("wrong route %s", w.Header().Get("Location"))
			}
		})
	}
	cookie, csrf := sessionFor(t, s, owner)
	body["allowed_ips"] = []string{"3.9.8.7"}
	requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf), 200)
	r := httptest.NewRequest("GET", "http://localhost:8088/c/"+c.Slug, nil)
	r.RemoteAddr = "3.9.8.7:1234"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 302)
	if !strings.HasPrefix(w.Header().Get("Location"), c.TargetURL) {
		t.Fatal("explicit whitelist did not bypass datacenter filter")
	}
	// Older clients omitting the new flag must not turn it off on other edits.
	delete(body, "block_datacenter")
	requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf), 200)
	var saved Cloak
	s.DB.First(&saved, "id = ?", c.ID)
	if !saved.BlockDatacenter {
		t.Fatal("legacy client cleared opt-in")
	}
	body["status"] = "off"
	body["allowed_ips"] = []string{}
	requireCode(t, call(t, s, "PUT", "/api/cloaks/"+c.ID, body, cookie, csrf), 200)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if !strings.HasPrefix(w.Header().Get("Location"), c.TargetURL) {
		t.Fatal("off status applied datacenter filter")
	}
}
func TestDatacenterCacheConcurrentInvalidation(t *testing.T) {
	s := testServer(t)
	var group sync.WaitGroup
	for range 10 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 20 {
				s.invalidateDatacenterNetworks()
				if hit, err := s.datacenterIPBlocked(context.Background(), "2401:b60::123"); err != nil || !hit {
					t.Errorf("match failed: %v", err)
				}
			}
		}()
	}
	group.Wait()
}

func TestDatacenterCatalogueFailureDoesNotAllowTarget(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	body := cloakFixture()
	body["block_datacenter"] = true
	c := createTestCloak(t, s, owner, body)
	if err := s.DB.Migrator().DropTable(&DatacenterNetwork{}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://localhost:8088/c/"+c.Slug, nil)
	r.RemoteAddr = "3.9.8.7:1234"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 503)
	if w.Header().Get("Location") != "" {
		t.Fatal("catalogue failure allowed target")
	}
}
