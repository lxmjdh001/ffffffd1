package platform

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The configured order is the stable tie-breaker. A fallback is availability,
// not a cleared verdict: risk status and checked_at remain untouched.
func trafficCandidates(links []TrafficLink, now time.Time) ([]TrafficLink, string) {
	if available := eligibleTrafficLinks(links, now); len(available) > 0 {
		return available, ""
	}
	best := -1
	for i, l := range links {
		u, err := url.Parse(l.URL)
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
			continue
		}
		if best < 0 || l.Position < links[best].Position || l.Position == links[best].Position && l.ID < links[best].ID {
			best = i
		}
	}
	if best < 0 {
		return nil, ""
	}
	return []TrafficLink{links[best]}, links[best].ID
}

func (s *Server) wakeTrafficRiskChecks() {
	select {
	case s.trafficRiskWake <- struct{}{}:
	default:
	}
}

func trafficRiskDue(links []TrafficLink, now time.Time) bool {
	for _, l := range links {
		interval := 120 * time.Minute
		if l.Status == "unknown" || l.Error != "" {
			interval = 5 * time.Minute
		}
		if l.Status == "pending" || l.CheckedAt == nil || !l.CheckedAt.After(now.Add(-interval)) {
			return true
		}
	}
	return false
}

// A per-pool expiring lease only exists while work is running. Empty scans do
// not consume a global window, and one account cannot delay another's first scan.
func (s *Server) checkTrafficRiskPool(parent context.Context, candidate TrafficPool, key string) error {
	var p TrafficPool
	err := s.DB.WithContext(parent).Where("slug = ? AND user_id = ? AND enabled = ? AND risk_enabled = ?", candidate.Slug, candidate.UserID, true, true).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var owner User
	if err = s.DB.WithContext(parent).First(&owner, "id = ?", p.UserID).Error; err != nil {
		return err
	}
	eligible, err := s.riskEligible(s.DB.WithContext(parent), owner)
	if err != nil || !eligible {
		return err
	}
	var links []TrafficLink
	if err = s.DB.WithContext(parent).Where("user_id = ? AND pool_slug = ?", p.UserID, p.Slug).Order("position, id").Find(&links).Error; err != nil {
		return err
	}
	if !trafficRiskDue(links, s.Now()) {
		return nil
	}
	lease := RateBucket{Key: s.mac("traffic-risk-pool:" + p.UserID + ":" + p.StatisticsID), Count: 1, ExpiresAt: s.Now().Add(2 * time.Minute)}
	claim := s.DB.WithContext(parent).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.Assignments(map[string]any{"count": 1, "expires_at": lease.ExpiresAt}),
		Where:     clause.Where{Exprs: []clause.Expression{clause.Lte{Column: clause.Column{Table: "rate_buckets", Name: "expires_at"}, Value: s.Now()}}},
	}).Create(&lease)
	if claim.Error != nil {
		return claim.Error
	}
	if claim.RowsAffected == 0 {
		return nil
	}
	defer s.DB.Where("key = ? AND expires_at = ?", lease.Key, lease.ExpiresAt).Delete(&RateBucket{})
	// Re-read after claiming: another worker may have completed since discovery.
	if err = s.DB.WithContext(parent).Where("user_id = ? AND pool_slug = ?", p.UserID, p.Slug).Order("position, id").Find(&links).Error; err != nil {
		return err
	}
	if !trafficRiskDue(links, s.Now()) {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	client := s.trafficRiskHTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	// Up to 30 URLs; four bounded lookups prevent one slow URL blocking the
	// whole pool. No database/user lock is held during upstream HTTP requests.
	jobs := make(chan int)
	var workers sync.WaitGroup
	for i := 0; i < min(4, len(links)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				previous := links[i].Status
				unsafe, e := s.lookupTrackedWebRisk(ctx, client, "https://webrisk.googleapis.com/v1/uris:search", key, links[i].URL)
				links[i].Status, links[i].Error = "clean", ""
				if e != nil {
					links[i].Status, links[i].Error = "unknown", e.Error()
					if previous == "unsafe" {
						links[i].Status = "unsafe"
					}
				} else if unsafe {
					links[i].Status = "unsafe"
				}
			}
		}()
	}
sendJobs:
	for i := range links {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break sendJobs
		}
	}
	close(jobs)
	workers.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	statuses, messages := map[string]string{}, map[string]string{}
	for _, l := range links {
		u, e := url.Parse(l.URL)
		if e != nil {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if statuses[host] != "unsafe" && (l.Status == "unsafe" || statuses[host] == "" || l.Status == "unknown") {
			statuses[host], messages[host] = l.Status, l.Error
		}
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, l := range links {
			u, e := url.Parse(l.URL)
			if e != nil {
				continue
			}
			host := strings.ToLower(u.Hostname())
			// Editing/deleting a URL during a lookup must not receive an old result.
			q := tx.Model(&TrafficLink{}).Where("id = ? AND user_id = ? AND pool_slug = ? AND url = ?", l.ID, p.UserID, p.Slug, l.URL)
			if l.CheckedAt == nil {
				q = q.Where("checked_at IS NULL")
			} else {
				q = q.Where("checked_at = ?", l.CheckedAt)
			}
			if e = q.Updates(map[string]any{"status": statuses[host], "error": messages[host], "checked_at": s.Now()}).Error; e != nil {
				return e
			}
		}
		return nil
	})
}
