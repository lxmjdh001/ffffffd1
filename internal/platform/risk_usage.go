package platform

import (
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/http"
)

// Counts this installation's attempted Lookup requests, not Google's invoice.
type RiskUsage struct {
	Month    string `gorm:"primaryKey;size:7"`
	Requests int64
}

func (s *Server) lookupTrackedWebRisk(ctx context.Context, client *http.Client, endpoint, key, uri string) (bool, error) {
	month := s.Now().UTC().Format("2006-01")
	err := s.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "month"}},
		DoUpdates: clause.Assignments(map[string]any{"requests": gorm.Expr("requests + 1")}),
	}).Create(&RiskUsage{Month: month, Requests: 1}).Error
	if err != nil {
		return false, err
	}
	return lookupWebRisk(ctx, client, endpoint, key, uri)
}

func (s *Server) riskUsageSummary() (map[string]any, error) {
	month := s.Now().UTC().Format("2006-01")
	var usage RiskUsage
	if err := s.DB.Where("month = ?", month).Find(&usage).Error; err != nil {
		return nil, err
	}
	const allowance int64 = 100000
	remaining := allowance - usage.Requests
	if remaining < 0 {
		remaining = 0
	}
	return map[string]any{"month": month, "requests": usage.Requests, "free_limit": allowance, "free_remaining": remaining}, nil
}
