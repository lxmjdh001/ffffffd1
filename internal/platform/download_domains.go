package platform

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
)

func (x Settings) assignedDownloadURL(planID string) string {
	for _, rule := range x.DownloadDomainRules {
		if !rule.LegacyOnly && (x.MeteredBilling || rule.Audience == "all" || (rule.Audience == "paid" && planID == "pro")) {
			return rule.URL
		}
	}
	// Configured rules are authoritative. Never fall back to a retired URL.
	if len(x.DownloadDomainRules) > 0 {
		return ""
	}
	if x.DownloadDomainMode == "separate" {
		if planID == "pro" {
			return x.PaidDownloadBaseURL
		}
		return x.FreeDownloadBaseURL
	}
	if x.DownloadDomainMode == "public_paid" && planID == "pro" {
		return x.PaidDownloadBaseURL
	}
	if x.DownloadDomainMode == "public_paid" {
		return x.FreeDownloadBaseURL
	}
	return x.DownloadBaseURL
}

// Active domains authorize download traffic. All saved domains, including the
// inactive mode, stay reserved against customer domain claims.
func (x Settings) managedDownloadHosts(activeOnly bool) map[string]string {
	hosts := map[string]string{}
	add := func(raw, audience string) {
		if x.MeteredBilling {
			audience = "all"
		}
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return
		}
		host := strings.ToLower(u.Hostname())
		if existing := hosts[host]; existing != "" && existing != audience {
			audience = "all"
		}
		hosts[host] = audience
	}
	if !activeOnly || x.DownloadDomainMode != "separate" {
		if !activeOnly || len(x.DownloadDomainRules) == 0 {
			add(x.DownloadBaseURL, "all")
		}
	}
	if !activeOnly || x.DownloadDomainMode == "separate" || x.DownloadDomainMode == "public_paid" {
		if !activeOnly || len(x.DownloadDomainRules) == 0 {
			add(x.FreeDownloadBaseURL, "free")
			add(x.PaidDownloadBaseURL, "pro")
		}
	}
	for _, rule := range x.DownloadDomainRules {
		audience := rule.Audience
		if audience == "paid" {
			audience = "pro"
		}
		add(rule.URL, audience)
	}
	return hosts
}

func (s *Server) normalizeDownloadURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if s.Config.Env == "development" && err == nil && (strings.EqualFold(u.Hostname(), "localhost") || strings.HasPrefix(u.Hostname(), "127.")) {
		return strings.TrimRight(raw, "/"), nil
	}
	if err != nil || !validHTTPS(raw) || !validHostname(strings.ToLower(u.Hostname())) || u.Host != u.Hostname() || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil || strings.EqualFold(u.Hostname(), s.Config.Host()) || strings.EqualFold(u.Hostname(), "www."+s.Config.Host()) {
		return "", problem(400, "下载域名须为独立 HTTPS 域名，不含路径、端口、账号或参数，且不能使用官网域名")
	}
	return "https://" + strings.ToLower(u.Hostname()), nil
}

func (s *Server) assignedDownloadDomain(w http.ResponseWriter, r *http.Request) {
	x, err := getSettings(s.DB)
	if err != nil {
		fail(w, err)
		return
	}
	plan := userOf(r).PlanID(s.Now())
	base := x.assignedDownloadURL(plan)
	// Never let a legacy or directly edited configuration expose the control host.
	if normalized, err := s.normalizeDownloadURL(base); err != nil {
		base = ""
	} else {
		base = normalized
	}
	jsonOut(w, 200, map[string]string{"mode": x.DownloadDomainMode, "plan_id": plan, "download_base_url": base})
}

func migrateDownloadDomainRules(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = 7").Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if err := tx.AutoMigrate(&Settings{}); err != nil {
		return err
	}
	return tx.Create(&Migration{Version: 7, AppliedAt: time.Now().UTC()}).Error
}

func migrateDownloadDomains(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = 5").Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if err := tx.AutoMigrate(&Settings{}); err != nil {
		return err
	}
	return tx.Create(&Migration{Version: 5, AppliedAt: time.Now().UTC()}).Error
}
