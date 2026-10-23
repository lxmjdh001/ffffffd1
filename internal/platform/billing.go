package platform

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,7})(\.[0-9]{1,6})?$`)

func ParseMoney(v string) (int64, error) {
	if !decimalPattern.MatchString(v) {
		return 0, problem(400, "金额格式无效，最多支持 6 位小数")
	}
	parts := strings.SplitN(v, ".", 2)
	whole, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil {
		return 0, e
	}
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	frac += strings.Repeat("0", 6-len(frac))
	n, e := strconv.ParseInt(frac, 10, 64)
	return whole*Dollar + n, e
}
func formatMoney(n int64) string { return fmt.Sprintf("%d.%06d", n/Dollar, n%Dollar) }
func formatCents(n int64) string { return fmt.Sprintf("%d.%02d", n/Dollar, (n%Dollar)/10000) }
func idempotency(r *http.Request) (string, error) {
	k := r.Header.Get("Idempotency-Key")
	if !hexKey.MatchString(k) {
		return "", problem(400, "缺少有效的 Idempotency-Key")
	}
	return k, nil
}
func (s *Server) buySubscription(w http.ResponseWriter, r *http.Request) {
	key, e := idempotency(r)
	if e != nil {
		fail(w, e)
		return
	}
	if e = s.rate("subscribe:"+userOf(r).ID, 15, time.Hour); e != nil {
		fail(w, e)
		return
	}
	var out User
	e = s.DB.Transaction(func(tx *gorm.DB) error {
		u, e := lockUser(tx, userOf(r).ID)
		if e != nil {
			return e
		}
		reference := "subscription:" + u.ID + ":" + key
		var n int64
		if e = tx.Model(&Ledger{}).Where("reference = ?", reference).Count(&n).Error; e != nil {
			return e
		}
		if n > 0 {
			out = u
			return nil
		}
		var p Plan
		if e = tx.First(&p, "id = ?", "pro").Error; e != nil {
			return e
		}
		if p.PriceMicros <= 0 {
			return problem(503, "套餐价格尚未设置，请联系管理员")
		}
		if e = changeBalance(tx, &u, -p.PriceMicros, reference, "subscription", p.Name+" · 月度会员"); e != nil {
			return e
		}
		base := s.Now()
		if u.ProUntil != nil && u.ProUntil.After(base) {
			base = *u.ProUntil
		}
		if base.After(s.Now().AddDate(3, 0, 0)) {
			return problem(400, "最多可提前续费三年")
		}
		until := nextMonth(base)
		grant := DownloadGrant{ID: hashString(reference), UserID: u.ID, PlanID: p.ID, Limit: p.DownloadLimit, StartsAt: base, ExpiresAt: until}
		if e = tx.Create(&grant).Error; e != nil {
			return e
		}
		u.ProUntil = &until
		if e = tx.Model(&u).Update("pro_until", until).Error; e != nil {
			return e
		}
		out = u
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, out)
}
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	p, e := getPlan(s.DB, u, s.Now())
	if e != nil {
		fail(w, e)
		return
	}
	usage := Usage{UserID: u.ID, Period: period(s.Now())}
	if e = s.DB.Where("user_id = ? AND period = ?", u.ID, usage.Period).First(&usage).Error; e != nil && e != gorm.ErrRecordNotFound {
		fail(w, e)
		return
	}
	grant, e := activeDownloadGrant(s.DB, u, p, s.Now())
	if e != nil {
		fail(w, e)
		return
	}
	var quotaLimit, quotaUsed, quotaReserved int64
	var quotaExpiry *time.Time
	if grant != nil {
		quotaLimit, quotaUsed, quotaReserved = grant.Limit, grant.Used, grant.Reserved
		if grant.PlanID == "pro" {
			quotaExpiry = &grant.ExpiresAt
		}
	}
	var renewal DownloadGrant
	var renewsAt *time.Time
	if err := s.DB.Where("user_id = ? AND plan_id = ? AND starts_at > ?", u.ID, "pro", s.Now()).Order("starts_at ASC").First(&renewal).Error; err == nil {
		renewsAt = &renewal.StartsAt
	} else if err != gorm.ErrRecordNotFound {
		fail(w, err)
		return
	}
	var appCount, storage, downloads, domains int64
	checks := []error{s.DB.Model(&Application{}).Where("user_id = ?", u.ID).Count(&appCount).Error, s.DB.Model(&Application{}).Where("user_id = ?", u.ID).Select("COALESCE(SUM(size),0)").Scan(&storage).Error, s.DB.Model(&Download{}).Where("user_id = ?", u.ID).Count(&downloads).Error, s.DB.Model(&Domain{}).Where("user_id = ? AND verified_at > ?", u.ID, s.Now().Add(-48*time.Hour)).Count(&domains).Error}
	for _, e := range checks {
		if e != nil {
			fail(w, e)
			return
		}
	}
	now := s.Now().In(billingZone)
	const trendDays = 7
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, billingZone).AddDate(0, 0, -(trendDays - 1))
	type day struct {
		Date  string `json:"date"`
		Count int64  `json:"count"`
		Paid  int64  `json:"paid"`
	}
	series := make([]day, trendDays)
	// Seven bounded aggregate queries avoid loading millions of event rows.
	for i := range series {
		d := start.AddDate(0, 0, i)
		series[i].Date = d.Format("01-02")
		var totals struct {
			Count int64
			Paid  int64
		}
		e = s.DB.Model(&Download{}).Where("user_id = ? AND created_at >= ? AND created_at < ?", u.ID, d.UTC(), d.AddDate(0, 0, 1).UTC()).Select("COUNT(*) AS count, COALESCE(SUM(amount_micros),0) AS paid").Scan(&totals).Error
		if e != nil {
			fail(w, e)
			return
		}
		series[i].Count = totals.Count
		series[i].Paid = totals.Paid
	}
	jsonOut(w, 200, map[string]any{"user": u, "plan": p, "usage": usage, "app_count": appCount, "storage_bytes": storage, "total_downloads": downloads, "active_domains": domains, "series": series, "quota_resets_at": quotaExpiry, "quota_limit": quotaLimit, "quota_used": quotaUsed, "quota_reserved": quotaReserved, "quota_remaining": max(int64(0), quotaLimit-quotaUsed-quotaReserved), "quota_renews_at": renewsAt})
}
