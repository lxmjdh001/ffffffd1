package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPWAPushProviderDiagnosticsAndRetries(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
		state  string
	}{
		{201, "", "sent"}, {400, "invalid_request", "failed"},
		{401, "auth_failed", "failed"}, {403, "auth_failed", "failed"},
		{404, "subscription_expired", "failed"}, {410, "subscription_expired", "failed"},
		{413, "payload_too_large", "failed"}, {429, "rate_limited", "pending"},
		{503, "provider_unavailable", "pending"},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			s := testServer(t)
			owner := seedUser(t, s, 0)
			app := pushTestApp(t, s, owner)
			in, _, _ := pushTestInput(t, "https://web.push.apple.com/private-subscription-endpoint")
			pushTestSubscribe(t, s, app, in)
			task := pushTestTask(t, s, owner, app, "now", nil)
			pushExpand(t, s)
			s.pwaPushHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Urgency") != "high" || r.Header.Get("Content-Encoding") != "aes128gcm" {
					t.Fatal("incompatible Web Push headers")
				}
				response := pushFakeResponse(tc.status)
				response.Body = http.NoBody
				response.Header.Set("Retry-After", "120")
				return response, nil
			})}
			if !s.deliverOnePWAPush(context.Background()) {
				t.Fatal("delivery not processed")
			}
			d := pushDeliveries(t, s, task)[0]
			if d.Provider != "apple" || d.ProviderStatus != tc.status || d.ErrorCode != tc.code || d.Status != tc.state || d.LastAttemptAt == nil {
				t.Fatalf("missing diagnostic: %+v", d)
			}
			if d.DisplayedAt != nil || d.ClickedAt != nil {
				t.Fatal("provider response fabricated device receipts")
			}
			w := pushTaskCall(t, s, "GET", "/api/pwa-push/tasks/"+task.ID+"/deliveries", nil, owner, "", "")
			requireCode(t, w, 200)
			if strings.Contains(w.Body.String(), "private-subscription-endpoint") || strings.Contains(w.Body.String(), "encrypted_subscription") {
				t.Fatal("private subscription leaked")
			}
			if tc.state == "pending" && newPWAPushDeliveryView(d).NextAttemptAt == nil {
				t.Fatal("retry time missing from owner report")
			}
		})
	}
}

func TestPWAPushNetworkDiagnosticsAndRecovery(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	app := pushTestApp(t, s, owner)
	in, _, _ := pushTestInput(t, "https://fcm.googleapis.com/private-capability")
	pushTestSubscribe(t, s, app, in)
	task := pushTestTask(t, s, owner, app, "now", nil)
	pushExpand(t, s)
	s.pwaPushHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "POST", URL: in.Endpoint, Err: &net.DNSError{Err: "private DNS details", Name: "fcm.googleapis.com"}}
	})}
	s.deliverOnePWAPush(context.Background())
	d := pushDeliveries(t, s, task)[0]
	if d.ErrorCode != "dns_failed" || d.ProviderStatus != 0 || d.Status != "pending" || d.Provider != "google" {
		t.Fatalf("network failure not diagnosed: %+v", d)
	}
	if strings.Contains(d.LastError, "private") {
		t.Fatal("transport details persisted in public error")
	}
	now := d.NextAttemptAt
	s.Now = func() time.Time { return now }
	s.pwaPushHTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return pushFakeResponse(201), nil
	})}
	s.deliverOnePWAPush(context.Background())
	d = pushDeliveries(t, s, task)[0]
	if d.Status != "sent" || d.ErrorCode != "" || d.ProviderStatus != 201 || d.Attempts != 2 || d.LastError != "" || !d.LastAttemptAt.Equal(now) {
		t.Fatalf("successful retry retained stale diagnostic: %+v", d)
	}
	if pwaPushNetworkCode(context.DeadlineExceeded) != "network_timeout" || pwaPushNetworkCode(errors.New("private transport message")) != "network_failed" || pwaPushNetworkCode(errPWAPushDNS) != "dns_failed" {
		t.Fatal("network codes are not stable")
	}
}

func TestPWAPushStagesPreserveIndependentReceipts(t *testing.T) {
	now := time.Now().UTC()
	d := PWAPushDelivery{Status: "sent", SentAt: &now, LastError: "legacy private endpoint", ErrorCode: ""}
	if newPWAPushDeliveryView(d).Stage != "accepted" {
		t.Fatal("provider acceptance missing")
	}
	d.ClickedAt = &now
	v := newPWAPushDeliveryView(d)
	if v.Stage != "clicked" || v.DisplayedAt != nil || v.LastError != "" || v.NextAttemptAt != nil {
		t.Fatal("click fabricated a display or exposed legacy errors")
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "legacy private") {
		t.Fatal("legacy diagnostic leaked")
	}
	d.ClickedAt, d.DisplayedAt = nil, &now
	if newPWAPushDeliveryView(d).Stage != "displayed" {
		t.Fatal("display receipt missing")
	}
	for _, endpoint := range []string{"https://web.push.apple.com/private", "https://eu.push.apple.com/private"} {
		if !pwaPushProvider(endpoint) || pwaPushProviderName(endpoint) != "apple" {
			t.Fatal("Apple endpoint rejected")
		}
	}
	for _, endpoint := range []string{"https://push.apple.com.evil.example/private", "https://fakepush.apple.com/private", "https://web.push.apple.com:8443/private"} {
		if pwaPushProvider(endpoint) || pwaPushProviderName(endpoint) != "" {
			t.Fatal("untrusted endpoint accepted")
		}
	}
}

func TestPWAPushDiagnosticMigrationPreservesExistingData(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	app := pushTestApp(t, s, owner)
	in, _, _ := pushTestInput(t, "https://fcm.googleapis.com/migration-private")
	sub, _ := pushTestSubscribe(t, s, app, in)
	task := pushTestTask(t, s, owner, app, "now", nil)
	pushExpand(t, s)
	d := pushDeliveries(t, s, task)[0]
	now := s.Now().Truncate(time.Second)
	if err := s.DB.Model(&d).Updates(map[string]any{"status": "sent", "sent_at": now, "displayed_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	if s.DB.Dialector.Name() == "postgres" {
		// Simulate restarting on the old schema. A reused pgx SELECT * plan
		// cannot survive dropping/re-adding columns in this fixture process.
		pool, err := s.DB.DB()
		if err != nil {
			t.Fatal(err)
		}
		pool.SetMaxIdleConns(0)
	}
	for _, field := range []string{"Provider", "ProviderStatus", "ErrorCode", "LastAttemptAt"} {
		if err := s.DB.Migrator().DropColumn(&PWAPushDelivery{}, field); err != nil {
			t.Fatal(err)
		}
	}
	s.DB.Where("version = ?", 29).Delete(&Migration{})
	for range 2 {
		if err := s.DB.Transaction(migratePWAPushDiagnostics); err != nil {
			t.Fatal(err)
		}
	}
	fresh := pushDeliveries(t, s, task)[0]
	var saved PWAPushSubscription
	s.DB.First(&saved, "id = ?", sub.ID)
	if fresh.ID != d.ID || fresh.Status != "sent" || fresh.SentAt == nil || !fresh.SentAt.Equal(now) || fresh.DisplayedAt == nil || !fresh.DisplayedAt.Equal(now) || saved.EncryptedSubscription != sub.EncryptedSubscription || fresh.ProviderStatus != 0 {
		t.Fatal("migration rewrote existing push data")
	}
}
