package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func riskFixtureHTTP(fn func(*http.Request) int) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		code := fn(r)
		body := "{}"
		if code == 299 {
			code = 200
			body = `{"threat":{"threatTypes":["SOCIAL_ENGINEERING"]}}`
		}
		return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
}

func TestTrafficFallbackAvailabilityAndTruthfulStatus(t *testing.T) {
	now := time.Now()
	old := now.Add(-121 * time.Minute)
	for _, status := range []string{"pending", "unknown", "unsafe", "clean"} {
		t.Run(status, func(t *testing.T) {
			links := []TrafficLink{{ID: "second", Position: 1, URL: "https://b.example/a", Status: status, CheckedAt: &old}, {ID: "first", Position: 0, URL: "https://a.example/a", Status: status, CheckedAt: &old}}
			before := append([]TrafficLink(nil), links...)
			out, fallback := trafficCandidates(links, now)
			if !reflect.DeepEqual(before, links) || status == "unsafe" && (len(out) != 1 || fallback != "first" || out[0].ID != "first") || status != "unsafe" && (len(out) != 2 || fallback != "") {
				t.Fatalf("wrong fallback or mutated verdict: %v %s", out, fallback)
			}
		})
	}
	links := []TrafficLink{{ID: "unsafe", Position: 0, URL: "https://bad.example/a", Status: "unsafe", CheckedAt: &now}, {ID: "pending", Position: 2, URL: "https://pending.example/a", Status: "pending"}, {ID: "old", Position: 3, URL: "https://old.example/a", Status: "clean", CheckedAt: &old}}
	if out, id := trafficCandidates(links, now); len(out) != 2 || id != "" {
		t.Fatal("pending or expired checks blocked normal routing")
	}
	links = append(links, TrafficLink{ID: "safe", Position: 4, URL: "https://safe.example/a", Status: "clean", CheckedAt: &now})
	if out, id := trafficCandidates(links, now); len(out) != 3 || id != "" {
		t.Fatal("safe target did not join normal routing")
	}
	if out, id := trafficCandidates([]TrafficLink{{ID: "invalid", URL: "javascript:alert(1)"}}, now); len(out) != 0 || id != "" {
		t.Fatal("invalid URL became a fallback")
	}
}

func TestTrafficFallbackRoutingAndAPIStayConsistent(t *testing.T) {
	s := meteredServer(t, 0)
	owner := seedUser(t, s, Dollar)
	cookie, csrf := sessionFor(t, s, owner)
	for _, mode := range []string{"sequential", "random"} {
		p := TrafficPool{Slug: randomHex(4), UserID: owner.ID, Enabled: true, RiskEnabled: true, Mode: mode}
		if e := s.DB.Create(&p).Error; e != nil {
			t.Fatal(e)
		}
		first := ""
		for i := 0; i < 3; i++ {
			l := TrafficLink{ID: randomHex(16), UserID: owner.ID, PoolSlug: p.Slug, Position: i, URL: "https://target.example/" + string(rune('a'+i)), Status: "unsafe"}
			if i == 0 {
				first = l.ID
			}
			if e := s.DB.Create(&l).Error; e != nil {
				t.Fatal(e)
			}
		}
		for range 4 {
			w := call(t, s, "GET", "/r/"+p.Slug, nil, nil, "")
			requireCode(t, w, 302)
			if w.Header().Get("Location") != "https://target.example/a" {
				t.Fatal("all-risk pool did not retain stable single fallback")
			}
		}
		w := call(t, s, "GET", "/api/traffic-pools", nil, cookie, csrf)
		requireCode(t, w, 200)
		var response struct {
			Data []struct {
				Pool     TrafficPool   `json:"pool"`
				IDs      []string      `json:"eligible_link_ids"`
				Fallback string        `json:"fallback_link_id"`
				Links    []TrafficLink `json:"links"`
			} `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &response); e != nil {
			t.Fatal(e)
		}
		for _, item := range response.Data {
			if item.Pool.Slug == p.Slug {
				if !reflect.DeepEqual(item.IDs, []string{first}) || item.Fallback != first {
					t.Fatal("API eligible list differs from routing")
				}
				for _, l := range item.Links {
					if l.Status != "unsafe" {
						t.Fatal("risk verdict was whitened")
					}
				}
			}
		}
		s.DB.Model(&p).Update("enabled", false)
		requireCode(t, call(t, s, "GET", "/r/"+p.Slug, nil, nil, ""), 404)
	}
	if got := usageOwner(t, s, owner.ID); got.BalanceMicros != Dollar || got.GiftBalanceMicros != 0 {
		t.Fatal("free routing billed the user")
	}
}

func TestTrafficRiskFirstCheckWakeAndEmptyScanDoesNotDelay(t *testing.T) {
	s := meteredServer(t, 0)
	t.Setenv("GOOGLE_WEB_RISK_API_KEY", "fixture")
	var requests atomic.Int64
	s.trafficRiskHTTP = riskFixtureHTTP(func(r *http.Request) int {
		requests.Add(1)
		if r.URL.Host != "webrisk.googleapis.com" || r.Header.Get("X-Goog-Api-Key") != "fixture" {
			t.Error("unexpected risk endpoint")
		}
		return 200
	})
	s.checkTrafficRisk(context.Background()) // An empty scan cannot consume a later first check.
	for range 2 {
		u := seedUser(t, s, Dollar)
		cookie, csrf := sessionFor(t, s, u)
		body := map[string]any{"enabled": true, "risk_enabled": true, "urls": []string{"https://example.com/full/app.apk?source=target"}}
		requireCode(t, call(t, s, "POST", "/api/traffic-pools", body, cookie, csrf), 200)
		select {
		case <-s.trafficRiskWake:
		default:
			t.Fatal("saving pool did not wake background checks")
		}
		s.checkTrafficRisk(context.Background())
		var link TrafficLink
		s.DB.First(&link, "user_id = ?", u.ID)
		if link.Status != "clean" || link.CheckedAt == nil {
			t.Fatal("new account stayed pending in the same former global window")
		}
	}
	if requests.Load() != 2 {
		t.Fatalf("fresh pool was rechecked or new pool blocked: %d", requests.Load())
	}
	s.checkTrafficRisk(context.Background())
	if requests.Load() != 2 {
		t.Fatal("fresh check was repeated")
	}
}

func TestTrafficRiskWorkerWakeAndLookupNeverBlockVisitors(t *testing.T) {
	s := meteredServer(t, 0)
	t.Setenv("GOOGLE_WEB_RISK_API_KEY", "fixture")
	u := seedUser(t, s, Dollar)
	cookie, csrf := sessionFor(t, s, u)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var requests atomic.Int64
	s.trafficRiskHTTP = riskFixtureHTTP(func(r *http.Request) int {
		requests.Add(1)
		once.Do(func() { close(started) })
		select {
		case <-release:
		case <-r.Context().Done():
		}
		return 200
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.runTrafficRiskChecks(ctx); close(done) }()
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		cancel()
		<-done
	})
	requireCode(t, call(t, s, "POST", "/api/traffic-pools", map[string]any{"enabled": true, "risk_enabled": true, "urls": []string{"https://pending.example/a.apk"}}, cookie, csrf), 200)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("save did not trigger immediate first check")
	}
	var pool TrafficPool
	s.DB.First(&pool, "user_id = ?", u.ID)
	second := make(chan struct{})
	go func() { s.checkTrafficRisk(context.Background()); close(second) }()
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("per-pool lease did not skip concurrent checker")
	}
	visit := make(chan int, 1)
	go func() { visit <- call(t, s, "GET", "/r/"+pool.Slug, nil, nil, "").Code }()
	select {
	case code := <-visit:
		if code != 302 {
			t.Fatalf("visitor blocked during lookup: %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("visitor waited for Google")
	}
	if requests.Load() != 1 {
		t.Fatal("concurrent workers duplicated lookup")
	}
	close(release)
}

func TestTrafficRiskRetryBackoffAndExpiredPoolLease(t *testing.T) {
	s := meteredServer(t, 0)
	t.Setenv("GOOGLE_WEB_RISK_API_KEY", "fixture")
	u := seedUser(t, s, Dollar)
	clock := s.Now()
	s.Now = func() time.Time { return clock }
	p := TrafficPool{Slug: randomHex(4), UserID: u.ID, Enabled: true, RiskEnabled: true}
	s.DB.Create(&p)
	l := TrafficLink{ID: randomHex(16), UserID: u.ID, PoolSlug: p.Slug, URL: "https://retry.example/a.apk", Status: "pending"}
	s.DB.Create(&l)
	var requests atomic.Int64
	s.trafficRiskHTTP = riskFixtureHTTP(func(*http.Request) int { requests.Add(1); return 503 })
	s.checkTrafficRisk(context.Background())
	s.checkTrafficRisk(context.Background())
	if requests.Load() != 1 {
		t.Fatal("failure retry hammered provider")
	}
	clock = clock.Add(5 * time.Minute)
	s.checkTrafficRisk(context.Background())
	if requests.Load() != 2 {
		t.Fatal("failed lookup was not retried")
	}
	s.trafficRiskHTTP = riskFixtureHTTP(func(*http.Request) int { requests.Add(1); return 200 })
	clock = clock.Add(5 * time.Minute)
	lease := RateBucket{Key: s.mac("traffic-risk-pool:" + p.UserID + ":" + p.StatisticsID), Count: 1, ExpiresAt: clock.Add(-time.Second)}
	s.DB.Create(&lease)
	s.checkTrafficRisk(context.Background())
	s.DB.First(&l, "id = ?", l.ID)
	if l.Status != "clean" || requests.Load() != 3 {
		t.Fatal("expired claim blocked recovery")
	}
	clock = clock.Add(120 * time.Minute)
	s.checkTrafficRisk(context.Background())
	if requests.Load() != 4 {
		t.Fatal("clean verdict was not refreshed")
	}
}

func TestTrafficRiskFailureDoesNotEraseConfirmedRisk(t *testing.T) {
	s := meteredServer(t, 0)
	t.Setenv("GOOGLE_WEB_RISK_API_KEY", "fixture")
	u := seedUser(t, s, Dollar)
	p := TrafficPool{Slug: randomHex(4), UserID: u.ID, Enabled: true, RiskEnabled: true}
	s.DB.Create(&p)
	old := s.Now().Add(-121 * time.Minute)
	l := TrafficLink{ID: randomHex(16), UserID: u.ID, PoolSlug: p.Slug, URL: "https://unsafe.example/a.apk", Status: "unsafe", CheckedAt: &old}
	s.DB.Create(&l)
	s.trafficRiskHTTP = riskFixtureHTTP(func(*http.Request) int { return 503 })
	s.checkTrafficRisk(context.Background())
	s.DB.First(&l, "id = ?", l.ID)
	if l.Status != "unsafe" || l.Error == "" {
		t.Fatal("upstream failure cleared known unsafe verdict")
	}
	available, id := trafficCandidates([]TrafficLink{l}, s.Now())
	if len(available) != 1 || id != l.ID {
		t.Fatal("risk-preserving failure lost sole fallback")
	}
}

func TestTrafficRiskPendingFailedAndExpiredRemainNormalSequentialTargets(t *testing.T) {
	s := meteredServer(t, 0)
	u := seedUser(t, s, Dollar)
	p := TrafficPool{Slug: randomHex(4), UserID: u.ID, Enabled: true, RiskEnabled: true, Mode: "sequential"}
	s.DB.Create(&p)
	old := s.Now().Add(-24 * time.Hour)
	for i, status := range []string{"pending", "unknown", "clean"} {
		l := TrafficLink{ID: randomHex(16), UserID: u.ID, PoolSlug: p.Slug, Position: i, URL: "https://normal.example/" + string(rune('a'+i)), Status: status}
		if status != "pending" {
			l.CheckedAt = &old
		}
		s.DB.Create(&l)
	}
	for _, suffix := range []string{"a", "b", "c", "a"} {
		w := call(t, s, "GET", "/r/"+p.Slug, nil, nil, "")
		requireCode(t, w, 302)
		if w.Header().Get("Location") != "https://normal.example/"+suffix {
			t.Fatal("detection changed normal sequential routing")
		}
	}
}
