package platform

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDownloadDateFiltersAndPagination(t *testing.T) {
	s := testServer(t)
	// UTC is still October 6; the platform's calendar is already October 7.
	now, _ := time.Parse(time.RFC3339, "2026-10-06T17:00:00Z")
	s.Now = func() time.Time { return now }
	u, outsider := seedUser(t, s, 0), seedUser(t, s, 0)
	a, b := seedApp(t, s, u, 100), seedApp(t, s, u, 100)
	cookie, _ := sessionFor(t, s, u)
	for _, row := range []struct{ id, date, owner, app string }{
		{"outside-30", "2026-09-07T23:59:59+08:00", u.ID, a.ID},
		{"start-30", "2026-09-08T00:00:00+08:00", u.ID, a.ID},
		{"yesterday", "2026-10-06T23:59:59+08:00", u.ID, a.ID},
		{"today-start", "2026-10-07T00:00:00+08:00", u.ID, a.ID},
		{"today-end", "2026-10-07T23:59:59+08:00", u.ID, b.ID},
		{"tomorrow", "2026-10-08T00:00:00+08:00", u.ID, a.ID},
		{"another-user", "2026-10-07T00:00:00+08:00", outsider.ID, a.ID},
	} {
		at, err := time.Parse(time.RFC3339, row.date)
		if err != nil {
			t.Fatal(err)
		}
		at = at.UTC()
		record := Download{ID: row.id, TicketHash: randomHex(32), UserID: row.owner, AppID: row.app, CreatedAt: at, CompletedAt: at, StartedAt: at.Add(-time.Second)}
		if err := s.DB.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, query string
		total       int
		ids         []string
	}{
		{"default today", "", 2, []string{"today-end", "today-start"}},
		{"first page", "?per_page=1&page=1", 2, []string{"today-end"}},
		{"second page", "?per_page=1&page=2", 2, []string{"today-start"}},
		{"application", "?app_id=" + a.ID, 1, []string{"today-start"}},
		{"yesterday", "?start_date=2026-10-06&end_date=2026-10-06", 1, []string{"yesterday"}},
		{"30 inclusive days", "?start_date=2026-09-08&end_date=2026-10-07", 4, []string{"today-end", "today-start", "yesterday", "start-30"}},
		{"historical month", "?start_date=2026-09-01&end_date=2026-09-30", 2, []string{"start-30", "outside-30"}},
		{"empty range", "?start_date=2026-10-01&end_date=2026-10-02", 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := call(t, s, "GET", "/api/downloads"+tc.query, nil, cookie, "")
			requireCode(t, w, 200)
			var result struct {
				Data  []Download `json:"data"`
				Total int        `json:"total"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Total != tc.total || len(result.Data) != len(tc.ids) {
				t.Fatalf("unexpected total/page: total=%d rows=%d", result.Total, len(result.Data))
			}
			for i, row := range result.Data {
				if row.ID != tc.ids[i] || row.UserID != u.ID {
					t.Fatal("wrong date, pagination order or tenant in download results")
				}
			}
		})
	}
}

func TestDownloadDateFiltersRejectInvalidRanges(t *testing.T) {
	s := testServer(t)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, billingZone)
	s.Now = func() time.Time { return now }
	cookie, _ := sessionFor(t, s, seedUser(t, s, 0))
	for _, query := range []string{
		"start_date=2026-09-07&end_date=2026-10-07", // 31 inclusive days
		"start_date=2026-10-07&end_date=2026-10-06",
		"start_date=2026-10-08&end_date=2026-10-08",
		"start_date=2026-10-01",
		"end_date=2026-10-07",
		"start_date=2026-02-30&end_date=2026-03-01",
		"start_date=invalid&end_date=2026-10-07",
	} {
		t.Run(query, func(t *testing.T) { requireCode(t, call(t, s, "GET", "/api/downloads?"+query, nil, cookie, ""), 400) })
	}
}
