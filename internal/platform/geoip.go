package platform

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

type countryResolver interface {
	Country(net.IP) string
}

type GeoIPDatabaseInfo struct {
	Type      string    `json:"type"`
	BuildTime time.Time `json:"build_time"`
}

type GeoIPInfo struct {
	Country *GeoIPDatabaseInfo `json:"country,omitempty"`
	ASN     *GeoIPDatabaseInfo `json:"asn,omitempty"`
}

// Readers are reused by every request. Files must be replaced by rename, never
// rewritten in place, because the reader maps the database into memory.
type GeoIPDatabases struct {
	mu      sync.RWMutex
	country *maxminddb.Reader
	asn     *maxminddb.Reader
	closed  bool
}

func openGeoIPFile(configured, dataDir, filename, kind string) (*maxminddb.Reader, error) {
	if strings.EqualFold(strings.TrimSpace(configured), "none") {
		return nil, nil
	}
	path := configured
	if path == "" {
		path = filepath.Join(dataDir, "geoip", filename)
	}
	info, err := os.Stat(path)
	if err != nil {
		if configured == "" && errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s GeoIP database: %w", kind, err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64<<20 {
		return nil, fmt.Errorf("invalid %s GeoIP file size or type", kind)
	}
	db, err := maxminddb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read %s GeoIP database: %w", kind, err)
	}
	if db.Metadata.DatabaseType != "GeoLite2-"+kind && db.Metadata.DatabaseType != "GeoIP2-"+kind {
		_ = db.Close()
		return nil, fmt.Errorf("GeoIP %s database has the wrong database type", kind)
	}
	if err := db.Verify(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("verify %s GeoIP database: %w", kind, err)
	}
	return db, nil
}

func OpenGeoIP(c Config) (*GeoIPDatabases, error) {
	db := &GeoIPDatabases{}
	var err error
	db.country, err = openGeoIPFile(c.GeoIPCountryDB, c.DataDir, "GeoLite2-Country.mmdb", "Country")
	if err == nil {
		db.asn, err = openGeoIPFile(c.GeoIPASNDB, c.DataDir, "GeoLite2-ASN.mmdb", "ASN")
	}
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (d *GeoIPDatabases) Info() GeoIPInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()
	var info GeoIPInfo
	if d.country != nil {
		info.Country = &GeoIPDatabaseInfo{Type: d.country.Metadata.DatabaseType, BuildTime: d.country.Metadata.BuildTime().UTC()}
	}
	if d.asn != nil {
		info.ASN = &GeoIPDatabaseInfo{Type: d.asn.Metadata.DatabaseType, BuildTime: d.asn.Metadata.BuildTime().UTC()}
	}
	return info
}

func geoIPAddr(ip net.IP) (netip.Addr, bool) {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Addr{}, false
	}
	addr = addr.Unmap()
	return addr, addr.IsGlobalUnicast() && !addr.IsPrivate() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast()
}

func (d *GeoIPDatabases) Country(ip net.IP) string {
	addr, ok := geoIPAddr(ip)
	if !ok {
		return "XX"
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.country == nil {
		return "XX"
	}
	var code string
	if err := d.country.Lookup(addr).DecodePath(&code, "country", "iso_code"); err != nil {
		return "XX"
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 2 || !cloakCountry.MatchString(code) || code == "T1" {
		return "XX"
	}
	return code
}

func (d *GeoIPDatabases) ASN(ip net.IP) (uint, string) {
	addr, ok := geoIPAddr(ip)
	if !ok {
		return 0, ""
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.asn == nil {
		return 0, ""
	}
	var record struct {
		Number       uint   `maxminddb:"autonomous_system_number"`
		Organization string `maxminddb:"autonomous_system_organization"`
	}
	if err := d.asn.Lookup(addr).Decode(&record); err != nil {
		return 0, ""
	}
	return record.Number, record.Organization
}

func (d *GeoIPDatabases) Reload(c Config) error {
	next, err := OpenGeoIP(c)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.country != nil && next.country == nil || d.asn != nil && next.asn == nil {
		_ = next.Close()
		return errors.New("GeoIP reload would discard an active database")
	}
	previous := &GeoIPDatabases{country: d.country, asn: d.asn}
	d.country, d.asn = next.country, next.asn
	return previous.Close()
}

func (d *GeoIPDatabases) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var errs []error
	for _, reader := range []*maxminddb.Reader{d.country, d.asn} {
		if reader != nil {
			errs = append(errs, reader.Close())
		}
	}
	d.country, d.asn, d.closed = nil, nil, true
	return errors.Join(errs...)
}

// Cloudflare country headers are consumed only after the existing edge check.
// GeoIP sees only an authenticated visitor IP, never an internal/CDN hop.
func (s *Server) cloakRequestCountry(r *http.Request) string {
	if country := s.trustedEdgeCountry(r); country != "XX" {
		return country
	}
	if s.GeoIP == nil {
		return "XX"
	}
	ip := net.ParseIP(s.clientIP(r))
	if _, ok := geoIPAddr(ip); !ok || ipInCloakNetworks(ip, s.Config.TrustedProxies) || ipInCloakNetworks(ip, clientCloudflareNetworks) || ipInCloakNetworks(ip, s.Config.cloakCountryNetworks()) {
		return "XX"
	}
	code := s.GeoIP.Country(ip)
	if len(code) != 2 || !cloakCountry.MatchString(code) || code == "T1" {
		return "XX"
	}
	return code
}
