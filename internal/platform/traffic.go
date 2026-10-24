package platform

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type TrafficPool struct {
	StatisticsID string    `json:"-" gorm:"size:32;not null;default:''"`
	UserID       string    `json:"-" gorm:"index;size:32"`
	Slug         string    `json:"slug" gorm:"primaryKey;size:32"`
	Enabled      bool      `json:"enabled"`
	RiskEnabled  bool      `json:"risk_enabled" gorm:"not null;default:false"`
	Name         string    `json:"name"`
	Mode         string    `json:"mode" gorm:"not null;default:random"`
	Cursor       int64     `json:"-"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (TrafficPool) TableName() string { return "traffic_groups" }
func (p *TrafficPool) BeforeCreate(_ *gorm.DB) error {
	if p.StatisticsID == "" {
		p.StatisticsID = randomHex(16)
	}
	return nil
}

type TrafficLink struct {
	PoolSlug  string     `json:"-" gorm:"index;size:32"`
	Position  int        `json:"position"`
	ID        string     `json:"id" gorm:"primaryKey;size:32"`
	UserID    string     `json:"-" gorm:"index;size:32"`
	URL       string     `json:"url"`
	Status    string     `json:"status"`
	Error     string     `json:"error"`
	CheckedAt *time.Time `json:"checked_at"`
}

func (s *Server) getTrafficPool(w http.ResponseWriter, r *http.Request) {
	var pools []TrafficPool
	if err := s.DB.Where("user_id = ?", userOf(r).ID).Order("updated_at DESC, slug").Find(&pools).Error; err != nil {
		fail(w, err)
		return
	}
	domains, err := s.downloadDomainsFor(userOf(r))
	if err != nil {
		fail(w, err)
		return
	}
	base := ""
	if len(domains) > 0 {
		base = domains[0].URL
	}
	data := []map[string]any{}
	for _, p := range pools {
		links := []TrafficLink{}
		if err := s.DB.Where("pool_slug = ? AND user_id = ?", p.Slug, userOf(r).ID).Order("position, id").Find(&links).Error; err != nil {
			fail(w, err)
			return
		}
		entryURL := ""
		if base != "" {
			entryURL = base + "/r/" + p.Slug
		}
		data = append(data, map[string]any{"pool": p, "links": links, "entry_url": entryURL})
	}
	eligible, e := s.riskEligible(s.DB, userOf(r))
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]any{"data": data, "paid": eligible, "risk_eligible": eligible, "configured": os.Getenv("GOOGLE_WEB_RISK_API_KEY") != ""})
}

func (s *Server) deleteTrafficPool(w http.ResponseWriter, r *http.Request) {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, userOf(r).ID); err != nil {
			return err
		}
		var p TrafficPool
		if err := tx.First(&p, "slug = ? AND user_id = ?", r.PathValue("slug"), userOf(r).ID).Error; err != nil {
			return err
		}
		if err := tx.Where("pool_slug = ?", p.Slug).Delete(&TrafficLink{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ? AND kind = ? AND resource_id = ?", p.UserID, "traffic", p.StatisticsID).Delete(&AccessVisit{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&p).Error; err != nil {
			return err
		}
		return recordAudit(tx, p.UserID, "traffic.delete", p.Slug, "删除分流")
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func (s *Server) saveTrafficPool(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Slug        *string  `json:"slug"`
		Enabled     bool     `json:"enabled"`
		RiskEnabled bool     `json:"risk_enabled"`
		Name        string   `json:"name"`
		Mode        string   `json:"mode"`
		URLs        []string `json:"urls"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Slug != nil {
		slug, err := normalizeAppSlug(*in.Slug)
		if err != nil {
			fail(w, problem(400, "分流标识须为 1–32 位字母、数字、短横线或下划线，且以字母或数字开头"))
			return
		}
		in.Slug = &slug
	}
	if in.Mode == "" {
		in.Mode = "random"
	}
	if in.Mode != "random" && in.Mode != "sequential" {
		fail(w, problem(400, "请选择随机或顺序展示"))
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if len([]rune(in.Name)) > 60 {
		fail(w, problem(400, "分流名称最多 60 字"))
		return
	}
	if len(in.URLs) > 30 || len(in.URLs) == 0 {
		fail(w, problem(400, "请配置 1–30 条下载链接"))
		return
	}
	seen := map[string]bool{}
	for i, raw := range in.URLs {
		raw = strings.TrimSpace(raw)
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && (s.Config.Production() || u.Scheme != "http")) || strings.EqualFold(u.Hostname(), s.Config.Host()) && strings.HasPrefix(u.Path, "/r/") || len(raw) > 4096 {
			fail(w, problem(400, "请输入完整的 HTTP/HTTPS 下载链接，不能填写分流入口本身"))
			return
		}
		if seen[raw] {
			fail(w, problem(400, "请删除重复的下载链接"))
			return
		}
		seen[raw] = true
		in.URLs[i] = raw
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		u, err := lockUser(tx, userOf(r).ID)
		if err != nil {
			return err
		}
		if in.RiskEnabled {
			eligible, e := s.riskEligible(tx, u)
			if e != nil {
				return e
			}
			if !eligible {
				return problem(403, "需要可用余额或赠送额度才能开启安全检测")
			}
		}
		if in.RiskEnabled && os.Getenv("GOOGLE_WEB_RISK_API_KEY") == "" {
			return problem(503, "管理员尚未配置 Google Web Risk API")
		}
		var p TrafficPool
		oldSlug := r.PathValue("slug")
		if slug := r.PathValue("slug"); slug != "" {
			if err := tx.First(&p, "slug = ? AND user_id = ?", slug, u.ID).Error; err != nil {
				return err
			}
		} else {
			p = TrafficPool{UserID: u.ID}
			if in.Slug == nil {
				for attempt := 0; attempt < 8; attempt++ {
					candidate := randomHex(4)
					var count int64
					if err := tx.Model(&TrafficPool{}).Where("slug = ?", candidate).Count(&count).Error; err != nil {
						return err
					}
					if count == 0 {
						p.Slug = candidate
						break
					}
				}
				if p.Slug == "" {
					return problem(503, "暂时无法生成分流标识，请重试")
				}
			}
		}
		if in.Slug != nil {
			p.Slug = *in.Slug
		}
		p.Name = in.Name
		if p.Name == "" {
			p.Name = "APK 分流"
		}
		p.Mode = in.Mode
		p.Cursor = 0
		p.Enabled = in.Enabled
		p.RiskEnabled = in.RiskEnabled
		var saveErr error
		if oldSlug == "" {
			saveErr = tx.Create(&p).Error
		} else {
			saveErr = tx.Model(&TrafficPool{}).Where("slug = ? AND user_id = ?", oldSlug, u.ID).Updates(map[string]any{"slug": p.Slug, "name": p.Name, "mode": p.Mode, "cursor": p.Cursor, "enabled": p.Enabled, "risk_enabled": p.RiskEnabled}).Error
		}
		if errors.Is(saveErr, gorm.ErrDuplicatedKey) {
			return problem(409, "分流标识已被使用，请换一个")
		}
		if saveErr != nil {
			return saveErr
		}
		var previous []TrafficLink
		if oldSlug != "" {
			if err := tx.Where("user_id = ? AND pool_slug = ?", u.ID, oldSlug).Find(&previous).Error; err != nil {
				return err
			}
			if err := tx.Where("user_id = ? AND pool_slug = ?", u.ID, oldSlug).Delete(&TrafficLink{}).Error; err != nil {
				return err
			}
		}
		for position, raw := range in.URLs {
			link := TrafficLink{ID: randomHex(16), UserID: u.ID, URL: raw, Status: "pending"}
			for _, old := range previous {
				if old.URL == raw {
					link = old
					break
				}
			}
			link.PoolSlug = p.Slug
			link.Position = position
			if err := tx.Create(&link).Error; err != nil {
				return err
			}
		}
		return recordAudit(tx, u.ID, "traffic.update", p.Slug, "更新链接池及自动检测分流设置")
	})
	if err != nil {
		fail(w, err)
		return
	}
	s.getTrafficPool(w, r)
}

func eligibleTrafficLinks(links []TrafficLink, now time.Time) []TrafficLink {
	out := []TrafficLink{}
	blocked := map[string]bool{}
	for _, l := range links {
		if l.Status == "unsafe" {
			u, err := url.Parse(l.URL)
			if err == nil {
				blocked[strings.ToLower(u.Hostname())] = true
			}
		}
	}
	for _, l := range links {
		u, err := url.Parse(l.URL)
		if err == nil && !blocked[strings.ToLower(u.Hostname())] && l.Status == "clean" && l.CheckedAt != nil && l.CheckedAt.After(now.Add(-120*time.Minute)) {
			out = append(out, l)
		}
	}
	return out
}

func (s *Server) routeTraffic(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var p TrafficPool
	var u User
	if s.DB.First(&p, "slug = ? AND enabled = ?", r.PathValue("slug"), true).Error != nil || s.DB.First(&u, "id = ?", p.UserID).Error != nil || u.Disabled {
		http.NotFound(w, r)
		return
	}
	if owner, ok := r.Context().Value(domainOwnerKey).(string); ok && owner != p.UserID {
		http.NotFound(w, r)
		return
	}
	if audience, managed := r.Context().Value(downloadAudienceKey).(string); managed && audience != "all" && u.PlanID(s.Now()) != audience {
		http.NotFound(w, r)
		return
	}
	if s.rejectBlockedUA(w, r, p.UserID, r.UserAgent()) {
		s.recordAccess(r, p.UserID, "traffic", p.StatisticsID, "blocked")
		return
	}
	target, err := s.trafficTarget(r, p.Slug, p.UserID)
	if err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// Traffic and cloaks share eligibility and cursor updates under the owner lock.
func (s *Server) trafficTarget(r *http.Request, slug, ownerID string) (string, error) {
	target := ""
	visited := false
	statisticsID := ""
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		owner, err := lockUser(tx, ownerID)
		if err != nil {
			return err
		}
		if owner.Disabled {
			return problem(404, "分流未启用")
		}
		if audience, managed := r.Context().Value(downloadAudienceKey).(string); managed && audience != "all" && owner.PlanID(s.Now()) != audience {
			return problem(404, "分流未启用")
		}
		var p TrafficPool
		if err := tx.First(&p, "slug = ? AND user_id = ? AND enabled = ?", slug, owner.ID, true).Error; err != nil {
			return err
		}
		visited = true
		statisticsID = p.StatisticsID
		var links []TrafficLink
		if err := tx.Where("pool_slug = ? AND user_id = ?", p.Slug, owner.ID).Order("position, id").Find(&links).Error; err != nil {
			return err
		}
		candidates := links
		eligible, e := s.riskEligible(tx, owner)
		if e != nil {
			return e
		}
		// Running out of credit pauses fresh lookups, never restores a URL that
		// has already been flagged as unsafe.
		if p.RiskEnabled && (eligible || meteredEnabled(tx)) {
			candidates = eligibleTrafficLinks(links, s.Now())
		}
		if len(candidates) == 0 {
			return problem(503, "暂无通过风险检测的可用下载链接，请联系应用发布者")
		}
		index := 0
		if p.Mode == "sequential" {
			// Cursor is a position in the original URL list, so excluded rows are skipped.
			index = 0
			for i, c := range candidates {
				if int64(c.Position) >= p.Cursor {
					index = i
					break
				}
			}
			if err := tx.Model(&p).Update("cursor", int64(candidates[index].Position+1)).Error; err != nil {
				return err
			}
		} else {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(candidates))))
			if err != nil {
				return err
			}
			index = int(n.Int64())
		}
		target = candidates[index].URL
		return nil
	})
	if visited {
		outcome := "allowed"
		if err != nil {
			outcome = "unavailable"
		}
		s.recordAccess(r, ownerID, "traffic", statisticsID, outcome)
	}
	return target, err
}

func lookupWebRisk(ctx context.Context, client *http.Client, endpoint, key, uri string) (bool, error) {
	q := url.Values{"uri": {uri}, "threatTypes": {"MALWARE", "SOCIAL_ENGINEERING", "UNWANTED_SOFTWARE"}}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return false, errors.New("无法创建风险查询")
	}
	req.Header.Set("X-Goog-Api-Key", key)
	resp, err := client.Do(req)
	if err != nil {
		return false, errors.New("Google 风险检测连接失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return false, fmt.Errorf("Google 风险检测暂不可用（HTTP %d）", resp.StatusCode)
	}
	var out struct {
		Threat *struct {
			ThreatTypes []string `json:"threatTypes"`
		} `json:"threat"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return false, errors.New("Google 风险检测响应无效")
	}
	return out.Threat != nil && len(out.Threat.ThreatTypes) > 0, nil
}

func (s *Server) runTrafficRiskChecks(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		s.checkTrafficRisk(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Server) checkTrafficRisk(ctx context.Context) {
	key := os.Getenv("GOOGLE_WEB_RISK_API_KEY")
	if key == "" || s.rate("traffic-risk-lease", 1, 120*time.Minute) != nil {
		return
	}
	var pools []TrafficPool
	if s.DB.WithContext(ctx).Where("enabled = ? AND risk_enabled = ?", true, true).Find(&pools).Error != nil {
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	for _, p := range pools {
		var owner User
		if s.DB.First(&owner, "id = ?", p.UserID).Error != nil || owner.Disabled {
			continue
		}
		eligible, e := s.riskEligible(s.DB, owner)
		if e != nil || !eligible {
			continue
		}
		var links []TrafficLink
		if s.DB.Where("user_id = ? AND pool_slug = ?", p.UserID, p.Slug).Find(&links).Error != nil {
			continue
		}
		results := map[string]string{}
		messages := map[string]string{}
		for i := range links {
			l := &links[i]
			u, err := url.Parse(l.URL)
			if err != nil {
				continue
			}
			host := strings.ToLower(u.Hostname())
			status := "clean"
			message := ""
			// Only inspect the customer-configured traffic target. Do not probe the
			// host root: the customer's main site is outside this feature's scope.
			unsafe, err := s.lookupTrackedWebRisk(ctx, client, "https://webrisk.googleapis.com/v1/uris:search", key, l.URL)
			if err != nil {
				status = "unknown"
				message = err.Error()
			} else if unsafe {
				status = "unsafe"
			}
			l.Status = status
			l.Error = message
			if results[host] != "unsafe" && (status == "unsafe" || results[host] == "" || status == "unknown") {
				results[host] = status
				messages[host] = message
			}
		}
		s.DB.Transaction(func(tx *gorm.DB) error {
			for _, l := range links {
				u, err := url.Parse(l.URL)
				if err != nil {
					continue
				}
				host := strings.ToLower(u.Hostname())
				if err := tx.Model(&TrafficLink{}).Where("id = ? AND user_id = ? AND url = ?", l.ID, p.UserID, l.URL).Updates(map[string]any{"status": results[host], "error": messages[host], "checked_at": s.Now()}).Error; err != nil {
					return err
				}
			}
			return nil
		})
	}
}
