package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRiskUsageCountsFailuresAndMonthlyRemaining(t *testing.T) {
	s := testServer(t)
	var attempts atomic.Int64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { attempts.Add(1); w.WriteHeader(503) }))
	defer endpoint.Close()
	for i := 0; i < 2; i++ {
		if _, err := s.lookupTrackedWebRisk(context.Background(), endpoint.Client(), endpoint.URL, "fixture", "https://example.com/app.apk"); err == nil {
			t.Fatal("expected lookup failure")
		}
	}
	if attempts.Load() != 2 {
		t.Fatal("tracked requests failed before reaching the upstream API")
	}
	usage, err := s.riskUsageSummary()
	if err != nil {
		t.Fatal(err)
	}
	if usage["requests"] != int64(2) || usage["free_remaining"] != int64(99998) {
		t.Fatalf("incorrect usage: %v", usage)
	}
	month := s.Now().UTC().Format("2006-01")
	if err := s.DB.Model(&RiskUsage{}).Where("month = ?", month).Update("requests", 100001).Error; err != nil {
		t.Fatal(err)
	}
	usage, err = s.riskUsageSummary()
	if err != nil || usage["free_remaining"] != int64(0) {
		t.Fatalf("remaining must be clamped: %v %v", usage, err)
	}
	previous := s.Now
	s.Now = func() time.Time { return previous().AddDate(0, 1, 0) }
	usage, err = s.riskUsageSummary()
	if err != nil || usage["requests"] != int64(0) || usage["free_remaining"] != int64(100000) {
		t.Fatalf("new month must reset: %v %v", usage, err)
	}
}
