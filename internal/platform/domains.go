package platform

import (
	"context"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var hostnamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

func validHostname(h string) bool {
	return len(h) <= 253 && hostnamePattern.MatchString(h) && net.ParseIP(h) == nil && !strings.HasSuffix(h, ".localhost") && !strings.HasSuffix(h, ".local")
}

// Explicit operator-controlled exceptions are for first-party integration tests.
// The website and CNAME target remain reserved even when listed here.
func (s *Server) customerHostnameAllowed(host string) bool {
	if !validHostname(host) || host == s.Config.Host() || host == s.Config.CNAMEHost {
		return false
	}
	if !strings.HasSuffix(host, "."+s.Config.Host()) {
		return true
	}
	for _, allowed := range strings.Split(os.Getenv("CUSTOM_DOMAIN_TEST_HOSTS"), ",") {
		if host == strings.TrimSpace(allowed) {
			return true
		}
	}
	return false
}
func (s *Server) listDomains(w http.ResponseWriter, r *http.Request) {
	listOut[Domain](w, r, s.DB.Model(&Domain{}).Where("user_id = ?", userOf(r).ID))
}
func (s *Server) createDomain(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Hostname string `json:"hostname"`
		Remark   string `json:"remark"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	in.Hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(in.Hostname), "."))
	in.Remark = strings.TrimSpace(in.Remark)
	if len([]rune(in.Remark)) > 120 {
		fail(w, problem(400, "域名备注最多 120 字"))
		return
	}
	if !s.customerHostnameAllowed(in.Hostname) {
		fail(w, problem(400, "请填写你拥有的完整下载子域名，例如 download.example.com"))
		return
	}
	var d Domain
	e := s.DB.Transaction(func(tx *gorm.DB) error {
		u, e := lockUser(tx, userOf(r).ID)
		if e != nil {
			return e
		}
		settings, e := getSettings(tx.Clauses(clause.Locking{Strength: "UPDATE"}))
		if e != nil {
			return e
		}
		if _, reserved := settings.managedDownloadHosts(false)[in.Hostname]; reserved {
			return problem(400, "此域名已用于公共下载入口")
		}
		var n int64
		if e = tx.Model(&Domain{}).Where("user_id = ?", u.ID).Count(&n).Error; e != nil {
			return e
		}
		if n >= settings.DomainLimit {
			return problem(403, "域名数量已达上限")
		}
		d = Domain{ID: randomHex(16), UserID: u.ID, Hostname: in.Hostname, Token: randomHex(24), Remark: in.Remark}
		if e = tx.Create(&d).Error; e != nil {
			if e == gorm.ErrDuplicatedKey {
				return problem(409, "该域名已被绑定，请联系管理员处理")
			}
			return e
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 201, d)
}
func (s *Server) checkDNS(ctx context.Context, d Domain) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	target, e := s.DNS.LookupCNAME(ctx, d.Hostname)
	if e != nil || !strings.EqualFold(strings.TrimSuffix(target, "."), s.Config.CNAMEHost) {
		return problem(400, "尚未检测到 CNAME → "+s.Config.CNAMEHost+"，请使用仅 DNS（灰云）解析")
	}
	return nil
}
func (s *Server) verifyDomain(w http.ResponseWriter, r *http.Request) {
	if e := s.rate("verify-domain:"+userOf(r).ID, 10, time.Minute); e != nil {
		fail(w, e)
		return
	}
	var d Domain
	if e := s.DB.First(&d, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Error; e != nil {
		fail(w, e)
		return
	}
	e := s.checkDNS(r.Context(), d)
	now := s.Now()
	updates := map[string]any{"last_error": "", "verified_at": now, "verification_method": "cname", "verification_requested_at": now, "ssl_retry_at": nil, "ssl_error": ""}
	if e != nil {
		// A proxy may hide the CNAME. Queue a public HTTP challenge instead of
		// treating an arbitrary 404 response as proof of a valid binding.
		updates["verified_at"] = nil
		updates["verification_method"] = "http"
		updates["ssl_status"] = "verifying"
	} else if d.SSLStatus != "ready" {
		updates["ssl_status"] = "pending"
	}
	res := s.DB.Model(&Domain{}).Where("id = ? AND token = ?", d.ID, d.Token).Updates(updates)
	if res.Error != nil {
		fail(w, res.Error)
		return
	}
	if res.RowsAffected != 1 {
		fail(w, problem(404, "域名已删除"))
		return
	}
	if e = s.DB.First(&d, "id = ?", d.ID).Error; e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, d)
}
func (s *Server) deleteDomain(w http.ResponseWriter, r *http.Request) {
	e := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, e := lockUser(tx, userOf(r).ID); e != nil {
			return e
		}
		res := tx.Delete(&Domain{}, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (s *Server) tlsAsk(w http.ResponseWriter, r *http.Request) {
	d := strings.ToLower(r.URL.Query().Get("domain"))
	if !validHostname(d) {
		http.NotFound(w, r)
		return
	}
	if d == s.Config.Host() {
		w.WriteHeader(204)
		return
	}
	settings, e := getSettings(s.DB)
	if e != nil {
		http.Error(w, "unavailable", 503)
		return
	}
	if _, managed := settings.managedDownloadHosts(true)[d]; managed {
		w.WriteHeader(204)
		return
	}
	var count int64
	e = s.DB.Model(&Domain{}).Joins("JOIN users ON users.id = domains.user_id").Where("hostname = ? AND verified_at > ? AND users.disabled = ?", d, s.Now().Add(-48*time.Hour), false).Count(&count).Error
	if e != nil || count != 1 {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(204)
}
