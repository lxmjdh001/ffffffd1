package platform

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CloakDeviceRule struct {
	Device  string   `json:"device"`
	Systems []string `json:"systems"`
}

// Cloaks contain redirect rules only. Neither destination is fetched by D1.
type Cloak struct {
	ID               string            `json:"id" gorm:"primaryKey;size:32"`
	UserID           string            `json:"-" gorm:"index;size:32;not null"`
	Slug             string            `json:"slug" gorm:"uniqueIndex;size:16;not null"`
	Name             string            `json:"name" gorm:"size:60"`
	Enabled          bool              `json:"enabled"`
	TargetType       string            `json:"target_type" gorm:"size:20;not null"`
	TargetURL        string            `json:"target_url"`
	TargetID         string            `json:"target_id" gorm:"size:32"`
	AppTargetMode    string            `json:"app_target_mode" gorm:"size:10"`
	TrafficPoolSlug  string            `json:"traffic_pool_slug" gorm:"size:32"`
	SafeURL          string            `json:"safe_url"`
	BlockedLanguages []string          `json:"blocked_languages" gorm:"serializer:json;type:text"`
	BlockedUAs       []string          `json:"blocked_uas" gorm:"serializer:json;type:text"`
	BlockedCountries []string          `json:"blocked_countries" gorm:"serializer:json;type:text"`
	AllowedPlatforms []string          `json:"allowed_platforms" gorm:"serializer:json;type:text"`
	BlockedDevices   []CloakDeviceRule `json:"blocked_devices" gorm:"serializer:json;type:text"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type cloakInput struct {
	Name             string            `json:"name"`
	Enabled          bool              `json:"enabled"`
	TargetType       string            `json:"target_type"`
	TargetURL        string            `json:"target_url"`
	TargetID         string            `json:"target_id"`
	AppTargetMode    string            `json:"app_target_mode"`
	TrafficPoolSlug  string            `json:"traffic_pool_slug"`
	SafeURL          string            `json:"safe_url"`
	BlockedLanguages []string          `json:"blocked_languages"`
	BlockedUAs       []string          `json:"blocked_uas"`
	BlockedCountries []string          `json:"blocked_countries"`
	AllowedPlatforms []string          `json:"allowed_platforms"`
	BlockedDevices   []CloakDeviceRule `json:"blocked_devices"`
}

type cloakView struct {
	Cloak
	EntryURL string `json:"entry_url"`
}

func (s *Server) cloakView(c Cloak) cloakView {
	return cloakView{Cloak: c, EntryURL: s.Config.PublicURL + "/c/" + c.Slug}
}

func (s *Server) listCloaks(w http.ResponseWriter, r *http.Request) {
	var rows []Cloak
	if err := s.DB.Where("user_id = ?", userOf(r).ID).Order("created_at DESC, id").Find(&rows).Error; err != nil {
		fail(w, err)
		return
	}
	data := make([]cloakView, 0, len(rows))
	for _, row := range rows {
		data = append(data, s.cloakView(row))
	}
	jsonOut(w, 200, map[string]any{"data": data, "country_detection_configured": len(s.Config.cloakCountryNetworks()) > 0})
}

func (s *Server) saveCloak(w http.ResponseWriter, r *http.Request) {
	var in cloakInput
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	c, err := normalizeCloak(in)
	if err != nil {
		fail(w, err)
		return
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		u, err := lockUser(tx, userOf(r).ID)
		if err != nil {
			return err
		}
		if id := r.PathValue("id"); id != "" {
			var old Cloak
			if err := tx.First(&old, "id = ? AND user_id = ?", id, u.ID).Error; err != nil {
				return err
			}
			c.ID, c.Slug, c.CreatedAt = old.ID, old.Slug, old.CreatedAt
		}
		c.UserID, c.UpdatedAt = u.ID, s.Now()
		if c.TargetType == "traffic_pool" {
			var pool TrafficPool
			if err := tx.First(&pool, "slug = ? AND user_id = ?", c.TrafficPoolSlug, u.ID).Error; err != nil {
				return err
			}
		}
		if c.TargetType == "app" {
			var app Application
			if err := tx.First(&app, "id = ? AND user_id = ?", c.TargetID, u.ID).Error; err != nil {
				return err
			}
		}
		if c.TargetType == "pwa" {
			var app PWAApp
			if err := tx.Select("id").First(&app, "id = ? AND user_id = ?", c.TargetID, u.ID).Error; err != nil {
				return err
			}
		}
		for _, raw := range []string{c.TargetURL, c.SafeURL} {
			if raw != "" {
				loop, err := s.cloakLoop(tx, raw, "")
				if err != nil {
					return err
				}
				if loop {
					return problem(400, "目标页和安全页不能填写本站斗篷入口")
				}
			}
		}
		if c.ID == "" {
			c.CreatedAt = s.Now()
			for range 8 {
				c.ID, c.Slug = randomHex(16), randomHex(4)
				result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&c)
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 1 {
					return recordAudit(tx, u.ID, "cloak.create", c.ID, "新增访问规则")
				}
			}
			return problem(503, "暂时无法生成访问链接，请重试")
		}
		if err := tx.Save(&c).Error; err != nil {
			return err
		}
		return recordAudit(tx, u.ID, "cloak.update", c.ID, "更新访问规则")
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, s.cloakView(c))
}

func (s *Server) deleteCloak(w http.ResponseWriter, r *http.Request) {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, userOf(r).ID); err != nil {
			return err
		}
		var c Cloak
		if err := tx.First(&c, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Error; err != nil {
			return err
		}
		if err := tx.Delete(&c).Error; err != nil {
			return err
		}
		return recordAudit(tx, c.UserID, "cloak.delete", c.ID, "删除访问规则")
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

var cloakLanguage = regexp.MustCompile(`^[a-z]{2,3}(?:-[a-z0-9]{2,8})*$`)
var cloakCountry = regexp.MustCompile(`^(?:[A-Z]{2}|T1)$`)

var cloakSystems = map[string]map[string]bool{
	"desktop": {"all": true, "windows": true, "macos": true, "linux": true, "chromeos": true, "other": true},
	"tablet":  {"all": true, "ios": true, "android": true, "harmonyos": true, "windows": true, "other": true},
	"mobile":  {"all": true, "ios": true, "android": true, "harmonyos": true, "windows": true, "other": true},
}

func normalizeCloak(in cloakInput) (Cloak, error) {
	c := Cloak{Name: strings.TrimSpace(in.Name), Enabled: in.Enabled, TargetType: in.TargetType, TargetURL: strings.TrimSpace(in.TargetURL), TrafficPoolSlug: strings.TrimSpace(in.TrafficPoolSlug), SafeURL: strings.TrimSpace(in.SafeURL)}
	if len([]rune(c.Name)) > 60 {
		return c, problem(400, "斗篷名称最多 60 字")
	}
	if c.Name == "" {
		c.Name = "访问规则"
	}
	if c.TargetType != "url" && c.TargetType != "traffic_pool" && c.TargetType != "app" && c.TargetType != "pwa" {
		return c, problem(400, "请选择分流链接、PWA 或应用")
	}
	if c.SafeURL != "" && !validCloakURL(c.SafeURL) {
		return c, problem(400, "请输入完整的 HTTP/HTTPS 安全页链接")
	}
	if c.TargetType == "url" {
		if !validCloakURL(c.TargetURL) {
			return c, problem(400, "请输入完整的 HTTP/HTTPS 目标页链接")
		}
		c.TrafficPoolSlug = ""
	} else if c.TargetType == "traffic_pool" {
		slug, err := normalizeAppSlug(c.TrafficPoolSlug)
		if err != nil || c.TrafficPoolSlug == "" {
			return c, problem(400, "请选择有效的分流链接")
		}
		c.TrafficPoolSlug = slug
		c.TargetURL = ""
	} else {
		c.TargetID = strings.TrimSpace(in.TargetID)
		if len(c.TargetID) != 32 || !hexKey.MatchString(c.TargetID) {
			return c, problem(400, "请选择有效的应用")
		}
		if c.TargetType == "app" {
			c.AppTargetMode = in.AppTargetMode
			if c.AppTargetMode == "" {
				c.AppTargetMode = "direct"
			}
			if c.AppTargetMode != "direct" && c.AppTargetMode != "info" {
				return c, problem(400, "请选择直接下载或详情页")
			}
		}
		c.TargetURL, c.TrafficPoolSlug = "", ""
	}
	var err error
	c.BlockedLanguages, err = normalizeCloakStrings(in.BlockedLanguages, 100, func(v string) (string, bool) {
		v = strings.ToLower(v)
		return v, len(v) <= 35 && cloakLanguage.MatchString(v)
	})
	if err != nil {
		return c, problem(400, "语言拦截配置无效")
	}
	c.BlockedUAs, err = normalizeCloakStrings(in.BlockedUAs, 100, func(v string) (string, bool) {
		v = strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, v))
		return v, utf8.RuneCountInString(v) <= 200
	})
	if err != nil {
		return c, problem(400, "UA 关键词不能为空且最多 200 个字符，最多配置 100 项")
	}
	c.BlockedCountries, err = normalizeCloakStrings(in.BlockedCountries, 300, func(v string) (string, bool) {
		v = strings.ToUpper(v)
		return v, cloakCountry.MatchString(v)
	})
	if err != nil {
		return c, problem(400, "国家拦截配置无效，请使用两位地区代码、XX 或 T1")
	}
	c.AllowedPlatforms, err = normalizeCloakStrings(in.AllowedPlatforms, 4, func(v string) (string, bool) {
		v = strings.ToLower(v)
		return v, v == "tiktok" || v == "facebook" || v == "google" || v == "bigo"
	})
	if err != nil {
		return c, problem(400, "广告允许配置无效")
	}
	c.BlockedDevices = []CloakDeviceRule{}
	if len(in.BlockedDevices) > 3 {
		return c, problem(400, "设备拦截配置无效")
	}
	seen := map[string]bool{}
	for _, rule := range in.BlockedDevices {
		if seen[rule.Device] || cloakSystems[rule.Device] == nil {
			return c, problem(400, "设备拦截配置无效")
		}
		seen[rule.Device] = true
		systems, err := normalizeCloakStrings(rule.Systems, 8, func(v string) (string, bool) {
			v = strings.ToLower(v)
			return v, cloakSystems[rule.Device][v]
		})
		if err != nil {
			return c, problem(400, "设备与系统不匹配")
		}
		if len(systems) > 0 {
			if containsCloak(systems, "all") {
				systems = []string{"all"}
			}
			c.BlockedDevices = append(c.BlockedDevices, CloakDeviceRule{Device: rule.Device, Systems: systems})
		}
	}
	return c, nil
}

func normalizeCloakStrings(values []string, limit int, normalize func(string) (string, bool)) ([]string, error) {
	out := []string{}
	if len(values) > limit {
		return nil, errors.New("too many rules")
	}
	seen := map[string]bool{}
	for _, raw := range values {
		v, ok := normalize(strings.TrimSpace(raw))
		if !ok || v == "" {
			return nil, errors.New("invalid rule")
		}
		key := strings.ToLower(v)
		if !seen[key] {
			seen[key] = true
			out = append(out, v)
		}
	}
	return out, nil
}

func validCloakURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || strings.ContainsAny(raw, "\r\n\t \\") {
		return false
	}
	if host, err := canonicalCloakHost(u.Hostname()); err != nil || host == "" {
		return false
	}
	return u.Port() == "" || validCloakPort(u.Port())
}

func canonicalCloakHost(host string) (string, error) {
	if host == "" {
		return "", nil
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String(), nil
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSuffix(ascii, ".")), nil
}

func validCloakPort(port string) bool {
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n <= 65535
}

func containsCloak(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func (s *Server) cloakLoop(db *gorm.DB, raw, requestHostname string) (bool, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return false, err
	}
	if !strings.HasPrefix(strings.ToLower(path.Clean(u.Path)), "/c/") {
		return false, nil
	}
	host, err := canonicalCloakHost(u.Hostname())
	if err != nil {
		return false, err
	}
	siteHost, err := canonicalCloakHost(s.Config.Host())
	if err != nil {
		return false, err
	}
	requestHostname, err = canonicalCloakHost(requestHostname)
	if err != nil {
		return false, err
	}
	siteAlias := "www." + siteHost
	if strings.HasPrefix(siteHost, "www.") {
		siteAlias = strings.TrimPrefix(siteHost, "www.")
	}
	// The deployed www redirect preserves paths, so it can recurse into a
	// cloak on the canonical host even when the configured URL uses its alias.
	if host == siteHost || host == siteAlias || host == requestHostname {
		return true, nil
	}
	settings, err := getSettings(db)
	if err != nil {
		return false, err
	}
	for managedHost := range settings.managedDownloadHosts(true) {
		canonical, err := canonicalCloakHost(managedHost)
		if err != nil {
			return false, err
		}
		if canonical == host {
			return true, nil
		}
	}
	var count int64
	// Domain creation accepts lowercase ASCII/punycode only, so the same
	// canonical ASCII hostname is the indexed representation in this table.
	if err := db.Model(&Domain{}).Where("hostname = ?", host).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// A disabled rule bypasses filtering; deleting it removes the entry entirely.
func (s *Server) routeCloak(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var c Cloak
	var owner User
	if err := s.DB.First(&c, "slug = ?", r.PathValue("slug")).Error; err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.DB.First(&owner, "id = ? AND disabled = ?", c.UserID, false).Error; err != nil {
		http.NotFound(w, r)
		return
	}
	if domainOwner, ok := r.Context().Value(domainOwnerKey).(string); ok && domainOwner != c.UserID {
		http.NotFound(w, r)
		return
	}
	if audience, managed := r.Context().Value(downloadAudienceKey).(string); managed && audience != "all" && owner.PlanID(s.Now()) != audience {
		http.NotFound(w, r)
		return
	}
	outcome := "blocked"
	defer func() { s.recordAccess(r, c.UserID, "cloaks", c.ID, outcome) }()
	if s.rejectBlockedUA(w, r, c.UserID, r.UserAgent()) {
		return
	}
	if err := s.chargeUsageUV(r, c.UserID, "cloak_uv", c.ID); err != nil {
		fail(w, err)
		return
	}
	target := c.SafeURL
	if !c.Enabled || !cloakBlocked(c, r, s.cloakRequestCountry(r)) {
		outcome = "allowed"
		candidate := c.TargetURL
		if c.TargetType == "traffic_pool" {
			// Execute the existing pool here; do not redirect to its /r/ entry
			// or replace the pool's own destination with an inline app page.
			var err error
			candidate, err = s.trafficTarget(r, c.TrafficPoolSlug, c.UserID)
			if err != nil {
				candidate = ""
			}
		}
		if c.TargetType == "app" || c.TargetType == "pwa" {
			if s.renderCloakApp(w, r, c, owner) {
				return
			}
			candidate = ""
		}
		if validCloakURL(candidate) {
			loop, err := s.cloakLoop(s.DB, candidate, requestHost(r))
			if err == nil && !loop {
				if merged, err := mergeCloakQuery(candidate, r.URL.RawQuery); err == nil {
					target = merged
				}
			}
		}
	}
	if strings.TrimSpace(target) == "" {
		s.cloakNotFound(w, r)
		return
	}
	loop, err := s.cloakLoop(s.DB, target, requestHost(r))
	if !validCloakURL(target) || err != nil || loop {
		http.Error(w, "访问链接配置不可用", http.StatusServiceUnavailable)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func mergeCloakQuery(raw, incoming string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", err
	}
	values, err := url.ParseQuery(incoming)
	if err != nil {
		return "", err
	}
	for key, v := range values {
		query[key] = v
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func cloakBlocked(c Cloak, r *http.Request, country string) bool {
	if containsCloak(c.BlockedCountries, country) {
		return true
	}
	ua := strings.ToLower(r.UserAgent())
	for _, word := range c.BlockedUAs {
		if strings.Contains(ua, strings.ToLower(word)) {
			return true
		}
	}
	for _, raw := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		parts := strings.Split(strings.TrimSpace(raw), ";")
		lang := strings.ToLower(strings.TrimSpace(parts[0]))
		quality := 1.0
		for _, parameter := range parts[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && strings.EqualFold(key, "q") {
				n, err := strconv.ParseFloat(value, 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1 {
					quality = 0
				} else {
					quality = n
				}
			}
		}
		if quality <= 0 || !cloakLanguage.MatchString(lang) {
			continue
		}
		for _, blocked := range c.BlockedLanguages {
			if lang == blocked || strings.HasPrefix(lang, blocked+"-") {
				return true
			}
		}
	}
	if len(c.AllowedPlatforms) > 0 {
		allowed := false
		params := r.URL.Query()
		for _, platform := range c.AllowedPlatforms {
			key := map[string]string{"tiktok": "ttclid", "facebook": "fbclid", "google": "gclid", "bigo": "bbg"}[platform]
			for _, value := range params[key] {
				if strings.TrimSpace(value) != "" {
					allowed = true
				}
			}
		}
		if !allowed {
			return true
		}
	}
	device, system := classifyCloakDevice(r.UserAgent())
	for _, rule := range c.BlockedDevices {
		if rule.Device == device && (containsCloak(rule.Systems, "all") || containsCloak(rule.Systems, system)) {
			return true
		}
	}
	return false
}

func classifyCloakDevice(ua string) (device, system string) {
	u := strings.ToLower(ua)
	device, system = "desktop", "other"
	switch {
	case strings.Contains(u, "harmonyos") || strings.Contains(u, "harmony os"):
		system = "harmonyos"
	case strings.Contains(u, "android"):
		system = "android"
	case strings.Contains(u, "iphone") || strings.Contains(u, "ipad") || strings.Contains(u, "ipod"):
		system = "ios"
	case strings.Contains(u, "windows"):
		system = "windows"
	case strings.Contains(u, "cros"):
		system = "chromeos"
	case strings.Contains(u, "macintosh") || strings.Contains(u, "mac os"):
		system = "macos"
	case strings.Contains(u, "linux"):
		system = "linux"
	}
	switch {
	case strings.Contains(u, "ipad") || strings.Contains(u, "tablet") || strings.Contains(u, "kindle") || strings.Contains(u, "silk/") || (strings.Contains(u, "android") && !strings.Contains(u, "mobile")) || (strings.Contains(u, "macintosh") && strings.Contains(u, "mobile/")):
		device = "tablet"
		if system == "macos" {
			system = "ios" // iPadOS can send a Macintosh desktop-mode UA.
		}
	case strings.Contains(u, "iphone") || strings.Contains(u, "ipod") || strings.Contains(u, "mobile") || strings.Contains(u, "windows phone"):
		device = "mobile"
	}
	return
}

// Cloudflare's published edge networks, checked 2026-10-24. Operators can
// override these using CLOAK_COUNTRY_PROXY_CIDRS, or disable with "none".
const defaultCloakCountryCIDRs = "173.245.48.0/20,103.21.244.0/22,103.22.200.0/22,103.31.4.0/22,141.101.64.0/18,108.162.192.0/18,190.93.240.0/20,188.114.96.0/20,197.234.240.0/22,198.41.128.0/17,162.158.0.0/15,104.16.0.0/13,104.24.0.0/14,172.64.0.0/13,131.0.72.0/22,2400:cb00::/32,2606:4700::/32,2803:f800::/32,2405:b500::/32,2405:8100::/32,2a06:98c0::/29,2c0f:f248::/32"

func parseCloakCountryNetworks(raw string) ([]*net.IPNet, error) {
	networks := []*net.IPNet{}
	if strings.EqualFold(strings.TrimSpace(raw), "none") {
		return networks, nil
	}
	if strings.TrimSpace(raw) == "" {
		raw = defaultCloakCountryCIDRs
	}
	for _, value := range strings.Split(raw, ",") {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("invalid CLOAK_COUNTRY_PROXY_CIDRS")
		}
		networks = append(networks, network)
	}
	return networks, nil
}

func (c Config) cloakCountryNetworks() []*net.IPNet {
	if c.CloakCountryProxies != nil {
		return c.CloakCountryProxies
	}
	networks, _ := parseCloakCountryNetworks("")
	return networks
}

func ipInCloakNetworks(ip net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) cloakRequestCountry(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "XX"
	}
	// Only consume XFF after the socket peer has been authenticated as a
	// configured internal proxy. The first untrusted hop must be the edge.
	if !ipInCloakNetworks(ip, s.Config.cloakCountryNetworks()) {
		if !ipInCloakNetworks(ip, s.Config.TrustedProxies) {
			return "XX"
		}
		chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		edgeFound := false
		for i := len(chain) - 1; i >= 0; i-- {
			candidate := net.ParseIP(strings.TrimSpace(chain[i]))
			if candidate == nil {
				return "XX"
			}
			if ipInCloakNetworks(candidate, s.Config.cloakCountryNetworks()) {
				edgeFound = true
				break
			}
			if !ipInCloakNetworks(candidate, s.Config.TrustedProxies) {
				return "XX"
			}
		}
		if !edgeFound {
			return "XX"
		}
	}
	country := strings.ToUpper(strings.TrimSpace(r.Header.Get("CF-IPCountry")))
	if !cloakCountry.MatchString(country) {
		return "XX"
	}
	return country
}
