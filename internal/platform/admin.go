package platform

import (
	"encoding/json"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	listOut[User](w, r, addQuery(s.DB.Model(&User{}), r, "name", "email"))
}
func (s *Server) adminApps(w http.ResponseWriter, r *http.Request) {
	listOut[Application](w, r, addQuery(s.DB.Model(&Application{}), r, "name", "file_name"))
}
func (s *Server) adminOrders(w http.ResponseWriter, r *http.Request) {
	q := s.DB.Model(&Order{})
	if status := r.URL.Query().Get("status"); status != "" {
		q = q.Where("status = ?", status)
	}
	listOut[Order](w, r, q)
}
func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request) {
	listOut[Audit](w, r, s.DB.Model(&Audit{}))
}
func (s *Server) adminUpdateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Disabled bool `json:"disabled"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if r.PathValue("id") == userOf(r).ID {
		fail(w, problem(400, "不可停用当前管理员"))
		return
	}
	var user User
	e := s.DB.Transaction(func(tx *gorm.DB) error {
		u, e := lockUser(tx, r.PathValue("id"))
		if e != nil {
			return e
		}
		if u.Role == "admin" {
			return problem(400, "不可通过此入口变更管理员状态")
		}
		if e = tx.Model(&u).Update("disabled", in.Disabled).Error; e != nil {
			return e
		}
		if in.Disabled {
			if e = tx.Delete(&Session{}, "user_id = ?", u.ID).Error; e != nil {
				return e
			}
		}
		user = u
		raw, _ := json.Marshal(in)
		return recordAudit(tx, userOf(r).ID, "user.status", u.ID, string(raw))
	})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, user)
}
func (s *Server) adminBalance(w http.ResponseWriter, r *http.Request) {
	key, e := idempotency(r)
	if e != nil {
		fail(w, e)
		return
	}
	var in struct {
		Amount string `json:"amount"`
		Note   string `json:"note"`
	}
	if e = decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	negative := strings.HasPrefix(in.Amount, "-")
	amount, e := ParseMoney(strings.TrimPrefix(in.Amount, "-"))
	if e != nil || amount == 0 || amount > 100000*Dollar || len([]rune(strings.TrimSpace(in.Note))) < 4 || len(in.Note) > 1000 {
		fail(w, problem(400, "请输入有效金额（不超过 10 万元）及至少 4 字的调整原因"))
		return
	}
	if negative {
		amount = -amount
	}
	var out User
	e = s.DB.Transaction(func(tx *gorm.DB) error {
		u, e := lockUser(tx, r.PathValue("id"))
		if e != nil {
			return e
		}
		ref := "admin:" + userOf(r).ID + ":" + key
		var existing Ledger
		e = tx.First(&existing, "reference = ?", ref).Error
		if e == nil {
			if existing.UserID != u.ID || existing.AmountMicros != amount || existing.Note != strings.TrimSpace(in.Note) {
				return problem(409, "幂等键对应的操作内容不一致")
			}
			out = u
			return nil
		}
		if e != gorm.ErrRecordNotFound {
			return e
		}
		if e = changeBalance(tx, &u, amount, ref, "adjustment", in.Note); e != nil {
			return e
		}
		out = u
		return recordAudit(tx, userOf(r).ID, "balance.adjust", u.ID, formatMoneySigned(amount)+" · "+in.Note)
	})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, out)
}
func formatMoneySigned(n int64) string {
	if n < 0 {
		return "-" + formatMoney(-n)
	}
	return formatMoney(n)
}
func (s *Server) adminModerate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Blocked bool   `json:"blocked"`
		Note    string `json:"note"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if len([]rune(strings.TrimSpace(in.Note))) < 4 || len(in.Note) > 1000 {
		fail(w, problem(400, "请填写至少 4 字的处理原因"))
		return
	}
	var app Application
	if e := s.DB.First(&app, "id = ?", r.PathValue("id")).Error; e != nil {
		fail(w, e)
		return
	}
	e := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, e := lockUser(tx, app.UserID); e != nil {
			return e
		}
		if e := tx.Model(&app).Update("blocked", in.Blocked).Error; e != nil {
			return e
		}
		raw, _ := json.Marshal(in)
		return recordAudit(tx, userOf(r).ID, "app.moderate", app.ID, string(raw))
	})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, app)
}
func (s *Server) adminSettle(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TradeID string `json:"trade_id"`
		Note    string `json:"note"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	in.TradeID = strings.TrimSpace(in.TradeID)
	in.Note = strings.TrimSpace(in.Note)
	if len([]rune(in.TradeID)) > 100 || len([]rune(in.Note)) > 500 {
		fail(w, problem(400, "交易号最多 100 字，备注最多 500 字"))
		return
	}
	var o Order
	if e := s.DB.First(&o, "id = ?", r.PathValue("id")).Error; e != nil {
		fail(w, e)
		return
	}
	// A stable order-specific reference makes empty-form confirmations safe to
	// retry. Supplied references retain the payment channel's replay protection.
	tradeID := "admin:" + o.ID
	if in.TradeID != "" {
		prefix := "manual:"
		switch o.Method {
		case "alipay", "wechat":
			prefix = "futoon:"
		case "usdt":
			prefix = "tron:"
			in.TradeID = strings.ToLower(in.TradeID)
		}
		tradeID = prefix + in.TradeID
	}
	if in.Note == "" {
		in.Note = "管理员确认入账"
	}
	if e := s.settleAudited(o.ID, tradeID, o.Method, o.AmountMicros, userOf(r).ID, in.Note); e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	settings, e := getSettings(s.DB)
	if e != nil {
		fail(w, e)
		return
	}
	var plans []Plan
	if e = s.DB.Order("price_micros").Find(&plans).Error; e != nil {
		fail(w, e)
		return
	}
	usage, err := s.riskUsageSummary()
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]any{"risk_usage": usage, "settings": settings, "plans": plans, "runtime": map[string]any{"public_url": s.Config.PublicURL, "cname_target": s.Config.CNAMEHost, "turnstile_configured": s.Config.TurnstileSecret != "", "scanner_configured": s.Config.ClamAVAddr != "", "futoon_configured": s.Config.FutoonURL != "", "usdt_configured": s.Config.USDTWallet != "", "production": s.Config.Production()}})
}
func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Settings Settings `json:"settings"`
		Plans    []Plan   `json:"plans"`
		Section  string   `json:"section"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	x := in.Settings
	sectionFields := map[string][]string{
		"basic":     {"site_name", "support_email", "registration_open"},
		"domains":   {"download_domain_rules"},
		"billing":   {"download_price_micros", "max_upload_bytes", "domain_limit", "usdt_rate_micros"},
		"recharge":  {"recharge_bonus_enabled", "recharge_bonus_tiers"},
		"plan_free": {}, "plan_pro": {},
	}
	if in.Section != "" {
		fields, valid := sectionFields[in.Section]
		if !valid {
			fail(w, problem(400, "未知的设置模块"))
			return
		}
		current, err := getSettings(s.DB)
		if err != nil {
			fail(w, err)
			return
		}
		incomingJSON, _ := json.Marshal(x)
		currentJSON, _ := json.Marshal(current)
		var incoming, merged map[string]json.RawMessage
		_ = json.Unmarshal(incomingJSON, &incoming)
		_ = json.Unmarshal(currentJSON, &merged)
		for _, key := range fields {
			merged[key] = incoming[key]
		}
		mergedJSON, _ := json.Marshal(merged)
		_ = json.Unmarshal(mergedJSON, &x)
		var plans []Plan
		if err := s.DB.Find(&plans).Error; err != nil {
			fail(w, err)
			return
		}
		for i := range plans {
			if in.Section == "plan_"+plans[i].ID {
				for _, p := range in.Plans {
					if p.ID == plans[i].ID {
						plans[i] = p
						break
					}
				}
			}
		}
		in.Plans = plans
	}
	x.SiteName = strings.TrimSpace(x.SiteName)
	if x.DownloadDomainMode == "" {
		x.DownloadDomainMode = "unified"
	}
	if x.DownloadDomainMode != "unified" && x.DownloadDomainMode != "separate" && x.DownloadDomainMode != "public_paid" {
		fail(w, problem(400, "请选择统一分配或按套餐分配下载域名"))
		return
	}
	for _, value := range []*string{&x.DownloadBaseURL, &x.FreeDownloadBaseURL, &x.PaidDownloadBaseURL} {
		normalized, err := s.normalizeDownloadURL(*value)
		if err != nil {
			fail(w, err)
			return
		}
		*value = normalized
	}
	for i := range x.DownloadDomainRules {
		rule := &x.DownloadDomainRules[i]
		rule.Remark = strings.TrimSpace(rule.Remark)
		if len([]rune(rule.Remark)) > 120 {
			fail(w, problem(400, "域名备注最多 120 字"))
			return
		}
		if rule.Audience != "all" && rule.Audience != "paid" {
			fail(w, problem(400, "域名权限必须是所有用户或付费用户"))
			return
		}
		normalized, err := s.normalizeDownloadURL(rule.URL)
		if err != nil {
			fail(w, err)
			return
		}
		rule.URL = normalized
	}
	if len(x.RechargeBonusTiers) == 0 || len(x.RechargeBonusTiers) > 10 {
		fail(w, problem(400, "请设置 1–10 个充值赠送档位"))
		return
	}
	var previous int64
	for _, tier := range x.RechargeBonusTiers {
		if tier.ThresholdMicros < Dollar || tier.ThresholdMicros > 10000*Dollar || tier.ThresholdMicros%10000 != 0 || tier.ThresholdMicros <= previous || tier.Percent < 1 || tier.Percent > 100 {
			fail(w, problem(400, "充值档位须按金额递增，门槛为 1–10,000 美元（最多两位小数），赠送比例为 1–100%"))
			return
		}
		previous = tier.ThresholdMicros
	}
	if x.SiteName == "" || len([]rune(x.SiteName)) > 60 || x.DownloadPriceMicros < 0 || x.DownloadPriceMicros > Dollar || x.MaxUploadBytes < 1<<20 || x.MaxUploadBytes > 2<<30 || x.DomainLimit < 0 || x.DomainLimit > 100 || x.USDTRateMicros < Dollar/100 || x.USDTRateMicros > 100*Dollar || len(in.Plans) != 2 {
		fail(w, problem(400, "配置数值无效，请检查填写范围"))
		return
	}
	if x.SupportEmail != "" {
		if _, e := normalizeEmail(x.SupportEmail); e != nil {
			fail(w, e)
			return
		}
	}
	seen := map[string]bool{}
	for _, p := range in.Plans {
		if (p.ID != "free" && p.ID != "pro") || seen[p.ID] || p.Name == "" || len([]rune(p.Name)) > 30 || p.AppLimit < 1 || p.AppLimit > 100000 || p.DownloadLimit < 0 || p.DownloadLimit > 100000000 || p.PriceMicros < 0 || p.PriceMicros > 100000*Dollar || p.StorageBytes < 1<<20 || p.StorageBytes > 100<<40 || (p.ID == "free" && p.PriceMicros != 0) {
			fail(w, problem(400, "套餐配置无效"))
			return
		}
		seen[p.ID] = true
	}
	e := s.DB.Transaction(func(tx *gorm.DB) error {
		// Serialize platform hostname changes against customer domain claims.
		if e := tx.Exec("UPDATE settings SET id = id WHERE id = 1").Error; e != nil {
			return e
		}
		for host := range x.managedDownloadHosts(false) {
			if in.Section != "" && in.Section != "domains" {
				break
			}
			var n int64
			if e := tx.Model(&Domain{}).Where("hostname = ?", host).Count(&n).Error; e != nil {
				return e
			}
			if n > 0 {
				return problem(409, "该域名已被客户绑定，不能设为公共下载域名")
			}
		}
		x.ID = 1
		if in.Section == "" {
			if e := tx.Save(&x).Error; e != nil {
				return e
			}
		} else if fields := sectionFields[in.Section]; len(fields) > 0 {
			if e := tx.Model(&Settings{}).Where("id = 1").Select(fields).Updates(&x).Error; e != nil {
				return e
			}
		}
		for _, p := range in.Plans {
			if in.Section != "" && in.Section != "plan_"+p.ID {
				continue
			}
			if e := tx.Save(&p).Error; e != nil {
				return e
			}
		}
		raw, _ := json.Marshal(in)
		return recordAudit(tx, userOf(r).ID, "settings.update", "platform", string(raw))
	})
	if e != nil {
		fail(w, e)
		return
	}
	s.adminSettings(w, r)
}

func (s *Server) adminResetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if len(in.Password) < 6 || len(in.Password) > 72 {
		fail(w, problem(400, "密码须为 6–72 字节"))
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(in.Password), 12)
	if e != nil {
		fail(w, e)
		return
	}
	e = s.DB.Transaction(func(tx *gorm.DB) error {
		u, e := lockUser(tx, r.PathValue("id"))
		if e != nil {
			return e
		}
		if u.Role == "admin" {
			return problem(403, "不可通过此入口重置管理员密码")
		}
		if e = tx.Model(&u).Update("password_hash", string(hash)).Error; e != nil {
			return e
		}
		if e = tx.Delete(&Session{}, "user_id = ?", u.ID).Error; e != nil {
			return e
		}
		return recordAudit(tx, userOf(r).ID, "user.reset_password", u.ID, "重置密码并撤销所有登录会话")
	})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
