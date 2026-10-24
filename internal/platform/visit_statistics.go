package platform

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"gorm.io/gorm"
)

// Visit records are separate from download tickets, financial ledgers and push
// events. The owner/resource/time index bounds every customer statistics query.
type AccessVisit struct {
	ID         string    `json:"id" gorm:"primaryKey;size:32"`
	UserID     string    `json:"-" gorm:"size:32;not null;index:idx_visit_owner_resource_time,priority:1"`
	Kind       string    `json:"-" gorm:"size:8;not null;index:idx_visit_owner_resource_time,priority:2"`
	ResourceID string    `json:"-" gorm:"size:32;not null;index:idx_visit_owner_resource_time,priority:3"`
	IP         string    `json:"ip" gorm:"size:45"`
	UserAgent  string    `json:"user_agent" gorm:"size:2048"`
	Country    string    `json:"country" gorm:"size:2"`
	Device     string    `json:"device" gorm:"size:32"`
	OS         string    `json:"os" gorm:"size:32"`
	Browser    string    `json:"browser" gorm:"size:32"`
	Outcome    string    `json:"outcome" gorm:"size:16"`
	CreatedAt  time.Time `json:"visited_at" gorm:"not null;index:idx_visit_owner_resource_time,priority:4;index:idx_visit_retention"`
}

func migrateVisitStatistics(tx *gorm.DB) error {
	var n int64
	if e := tx.Model(&Migration{}).Where("version = ?", 18).Count(&n).Error; e != nil {
		return e
	}
	if n != 0 {
		return nil
	}
	if e := tx.AutoMigrate(&AccessVisit{}); e != nil {
		return e
	}
	var pools []TrafficPool
	if err := tx.Where("statistics_id = ?", "").Find(&pools).Error; err != nil {
		return err
	}
	for _, pool := range pools {
		if err := tx.Model(&TrafficPool{}).Where("slug = ?", pool.Slug).UpdateColumn("statistics_id", randomHex(16)).Error; err != nil {
			return err
		}
	}
	return tx.Create(&Migration{Version: 18, AppliedAt: time.Now().UTC()}).Error
}
func (s *Server) recordAccess(r *http.Request, owner, kind, id, outcome string) {
	if r.Method != http.MethodGet {
		return
	}
	ip := ""
	if parsed := net.ParseIP(s.clientIP(r)); parsed != nil {
		ip = parsed.String()
	}
	device, system, browser := classifyDevice(r.UserAgent())
	row := AccessVisit{ID: randomHex(16), UserID: owner, Kind: kind, ResourceID: id, IP: ip, UserAgent: cleanText(r.UserAgent(), 2048), Country: s.cloakRequestCountry(r), Device: device, OS: system, Browser: browser, Outcome: outcome, CreatedAt: s.Now().UTC()}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Second)
	defer cancel()
	if e := s.DB.WithContext(ctx).Create(&row).Error; e != nil {
		slog.Error("visit statistics write failed", "kind", kind, "error", e)
	}
}

type visitSummary struct {
	Views         int64      `json:"views"`
	UniqueIPs     int64      `json:"unique_ips"`
	Allowed       int64      `json:"allowed"`
	Blocked       int64      `json:"blocked"`
	Unavailable   int64      `json:"unavailable"`
	LastVisitedAt *time.Time `json:"last_visited_at" gorm:"-"`
}
type visitSeriesPoint struct {
	Label     string `json:"label"`
	Views     int64  `json:"views"`
	UniqueIPs int64  `json:"unique_ips"`
}
type visitBreakdown struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

func (s *Server) visitStatistics(w http.ResponseWriter, r *http.Request) {
	kind, id := r.PathValue("kind"), r.PathValue("id")
	owner := userOf(r)
	// Resolve ownership before returning either data or a membership decision.
	var count int64
	var q *gorm.DB
	switch kind {
	case "apps":
		q = s.DB.Model(&Application{}).Where("id = ? AND user_id = ?", id, owner.ID)
	case "pwas":
		q = s.DB.Model(&PWAApp{}).Where("id = ? AND user_id = ?", id, owner.ID)
	case "cloaks":
		q = s.DB.Model(&Cloak{}).Where("id = ? AND user_id = ?", id, owner.ID)
	case "traffic":
		q = s.DB.Model(&TrafficPool{}).Where("slug = ? AND user_id = ?", id, owner.ID)
	default:
		fail(w, problem(404, "记录不存在"))
		return
	}
	if e := q.Count(&count).Error; e != nil {
		fail(w, e)
		return
	}
	if count != 1 {
		fail(w, problem(404, "记录不存在"))
		return
	}
	if kind == "traffic" {
		var pool TrafficPool
		if err := s.DB.First(&pool, "slug = ? AND user_id = ?", id, owner.ID).Error; err != nil {
			fail(w, err)
			return
		}
		id = pool.StatisticsID
	}
	ranges := []string{"today", "7d"}
	paid := owner.PlanID(s.Now()) == "pro"
	if paid {
		ranges = append(ranges, "30d")
	}
	selected := r.URL.Query().Get("range")
	if selected == "" {
		selected = "today"
	}
	days := 1
	switch selected {
	case "today":
	case "7d":
		days = 7
	case "30d":
		if !paid {
			fail(w, problem(403, "近一月访问统计仅限有效付费会员使用"))
			return
		}
		days = 30
	default:
		fail(w, problem(400, "请选择今日、7天或近一月统计"))
		return
	}
	now := s.Now().In(billingZone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, billingZone)
	start := today.AddDate(0, 0, -days+1)
	query := func() *gorm.DB {
		return s.DB.Model(&AccessVisit{}).Where("user_id = ? AND kind = ? AND resource_id = ? AND created_at >= ? AND created_at <= ?", owner.ID, kind, id, start.UTC(), now.UTC())
	}
	summary := visitSummary{}
	if e := query().Select("COUNT(*) AS views, COUNT(DISTINCT NULLIF(ip, '')) AS unique_ips, COALESCE(SUM(CASE WHEN outcome = 'allowed' THEN 1 ELSE 0 END),0) AS allowed, COALESCE(SUM(CASE WHEN outcome = 'blocked' THEN 1 ELSE 0 END),0) AS blocked, COALESCE(SUM(CASE WHEN outcome = 'unavailable' THEN 1 ELSE 0 END),0) AS unavailable").Scan(&summary).Error; e != nil {
		fail(w, e)
		return
	}
	var last []AccessVisit
	if e := query().Select("created_at").Order("created_at DESC").Limit(1).Find(&last).Error; e != nil {
		fail(w, e)
		return
	}
	if len(last) > 0 {
		summary.LastVisitedAt = &last[0].CreatedAt
	}
	bucketSQL := "strftime('%Y-%m-%d',created_at,'+8 hours')"
	if days == 1 {
		bucketSQL = "strftime('%H',created_at,'+8 hours')"
	}
	if s.DB.Dialector.Name() == "postgres" {
		format := "YYYY-MM-DD"
		if days == 1 {
			format = "HH24"
		}
		bucketSQL = fmt.Sprintf("to_char(created_at AT TIME ZONE 'Asia/Shanghai', '%s')", format)
	}
	var grouped []struct {
		Bucket           string
		Views, UniqueIPs int64
	}
	if e := query().Select(bucketSQL + " AS bucket, COUNT(*) AS views, COUNT(DISTINCT NULLIF(ip, '')) AS unique_ips").Group("bucket").Order("bucket").Scan(&grouped).Error; e != nil {
		fail(w, e)
		return
	}
	points := map[string]visitSeriesPoint{}
	for _, g := range grouped {
		points[g.Bucket] = visitSeriesPoint{Views: g.Views, UniqueIPs: g.UniqueIPs}
	}
	series := []visitSeriesPoint{}
	length := days
	if days == 1 {
		length = 24
	}
	for i := 0; i < length; i++ {
		key := start.AddDate(0, 0, i).Format("2006-01-02")
		label := key
		if days == 1 {
			key = fmt.Sprintf("%02d", i)
			label = key + ":00"
		}
		point := points[key]
		point.Label = label
		series = append(series, point)
	}
	countries, devices := []visitBreakdown{}, []visitBreakdown{}
	if e := query().Select("country AS name, COUNT(*) AS count").Group("country").Order("count DESC, country").Limit(8).Scan(&countries).Error; e != nil {
		fail(w, e)
		return
	}
	if e := query().Select("device AS name, COUNT(*) AS count").Group("device").Order("count DESC, device").Limit(8).Scan(&devices).Error; e != nil {
		fail(w, e)
		return
	}
	offset, limit := pageParams(r)
	records := []AccessVisit{}
	if e := query().Order("created_at DESC, id DESC").Offset(offset).Limit(limit).Find(&records).Error; e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]any{"range": selected, "allowed_ranges": ranges, "timezone": "Asia/Shanghai", "start_at": start.UTC(), "end_at": now.UTC(), "summary": summary, "series": series, "countries": countries, "devices": devices, "records": map[string]any{"data": records, "total": summary.Views}})
}
func (s *Server) cleanupVisitStatistics(ctx context.Context) {
	if e := s.rate("visit-statistics-cleanup", 1, time.Hour); e != nil {
		return
	}
	stale := s.DB.Model(&AccessVisit{}).Select("id").Where("created_at < ?", s.Now().AddDate(0, 0, -35)).Order("created_at").Limit(5000)
	if e := s.DB.WithContext(ctx).Where("id IN (?)", stale).Delete(&AccessVisit{}).Error; e != nil {
		slog.Error("visit statistics cleanup failed", "error", e)
	}
}
