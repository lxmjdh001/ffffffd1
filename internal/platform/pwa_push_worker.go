package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Queue ownership is stored in PostgreSQL/SQLite, so restarting the server does
// not lose schedules. Web Push has no provider-side idempotency API: a crash
// after provider acceptance can resend. The stable delivery tag lets the service
// worker display only one notification, and events remain idempotent.
func (s *Server) RunPWAPushWorker(ctx context.Context) {
	if s.pwaPushHTTP == nil {
		s.pwaPushHTTP = s.securePWAPushClient()
	}
	defer s.pwaPushHTTP.CloseIdleConnections()
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
				for range 10 {
					if ctx.Err() != nil {
						return
					}
					ok, e := s.expandOnePWAPushTask(ctx)
					if e != nil {
						if ctx.Err() == nil {
							slog.Error("PWA push audience queue failed")
						}
						break
					}
					if !ok {
						break
					}
				}
				for range 30 {
					if ctx.Err() != nil {
						return
					}
					if !s.deliverOnePWAPush(ctx) {
						break
					}
				}
				if e := s.finishPWAPushTasks(ctx); e != nil && ctx.Err() == nil {
					slog.Error("PWA push task completion failed")
				}
			}
		}()
	}
	<-ctx.Done()
	wg.Wait()
}

func (s *Server) expandOnePWAPushTask(ctx context.Context) (bool, error) {
	found := false
	e := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task PWAPushTask
		query := tx.Where("status IN ? AND expanded = ? AND scheduled_at <= ?", []string{"queued", "sending"}, false, s.Now()).Order("scheduled_at ASC, id ASC")
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		e := query.First(&task).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		found = true
		enabled, err := s.pwaPushAppEnabled(tx, task.PWAID, task.UserID)
		if err != nil {
			return err
		}
		if !enabled {
			now := s.Now()
			return tx.Model(&task).Updates(map[string]any{"status": "cancelled", "completed_at": now, "expanded": true}).Error
		}
		if task.AudienceAt == nil {
			now := s.Now()
			task.AudienceAt = &now
		}
		task.Status = "sending"
		var subs []PWAPushSubscription
		q := tx.Where("pwa_id = ? AND status = ? AND created_at <= ? AND id > ?", task.PWAID, "active", *task.AudienceAt, task.AudienceCursor)
		var countries []string
		if e = json.Unmarshal([]byte(task.CountriesJSON), &countries); e != nil {
			return e
		}
		if len(countries) > 0 {
			q = q.Where("country IN ?", countries)
		}
		if e = q.Order("id ASC").Limit(200).Find(&subs).Error; e != nil {
			return e
		}
		now := s.Now()
		for _, sub := range subs {
			if e = createPWAPushDelivery(tx, task, sub, now, now); e != nil {
				return e
			}
			task.AudienceCursor = sub.ID
		}
		if len(subs) < 200 {
			task.Expanded = true
		}
		return tx.Save(&task).Error
	})
	return found, e
}
func (s *Server) pwaPushAppEnabled(tx *gorm.DB, appID, userID string) (bool, error) {
	var n int64
	e := tx.Model(&PWAApp{}).Joins("JOIN users ON users.id = pwa_apps.user_id").Where("pwa_apps.id = ? AND pwa_apps.user_id = ? AND pwa_apps.enabled = ? AND pwa_apps.push_enabled = ? AND users.disabled = ?", appID, userID, true, true, false).Count(&n).Error
	return n == 1, e
}
func (s *Server) claimPWAPushDelivery(ctx context.Context) (PWAPushDelivery, error) {
	var delivery PWAPushDelivery
	e := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Task then delivery is the same lock order used by cancellation. Different
		// workers may send different tasks concurrently; a lease fences stale results.
		var task PWAPushTask
		query := tx.Where("status IN ? AND EXISTS (SELECT 1 FROM pwa_push_deliveries d WHERE d.task_id = pwa_push_tasks.id AND ((d.status = ? AND d.next_attempt_at <= ?) OR (d.status = ? AND d.lease_until <= ?)))", []string{"queued", "sending", "active"}, "pending", s.Now(), "processing", s.Now()).Order("created_at ASC, id ASC")
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if e := query.First(&task).Error; e != nil {
			return e
		}
		q := tx.Where("task_id = ? AND ((status = ? AND next_attempt_at <= ?) OR (status = ? AND lease_until <= ?))", task.ID, "pending", s.Now(), "processing", s.Now()).Order("next_attempt_at ASC, id ASC")
		if tx.Dialector.Name() == "postgres" {
			q = q.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if e := q.First(&delivery).Error; e != nil {
			return e
		}
		delivery.Attempts++
		until := s.Now().Add(time.Minute)
		delivery.LeaseUntil = &until
		delivery.LeaseToken = randomHex(16)
		delivery.Status = "processing"
		return tx.Save(&delivery).Error
	})
	return delivery, e
}

// Fixed, non-sensitive error codes are suitable for the owner report. Never
// propagate HTTP transport errors, which often contain an endpoint capability.
type pwaPushOutcome struct {
	subscriptionCiphertext       string
	accepted, expired, retryable bool
	message                      string
	retryAfter                   time.Duration
}

func (s *Server) deliverOnePWAPush(ctx context.Context) bool {
	delivery, e := s.claimPWAPushDelivery(ctx)
	if e != nil {
		if !errors.Is(e, gorm.ErrRecordNotFound) && ctx.Err() == nil {
			slog.Error("PWA push delivery claim failed")
		}
		return false
	}
	outcome := s.sendPWAPush(ctx, delivery)
	if e = s.finishPWAPushDelivery(ctx, delivery, outcome); e != nil && ctx.Err() == nil {
		slog.Error("PWA push result write failed", "delivery_id", delivery.ID)
	}
	return true
}
func (s *Server) sendPWAPush(ctx context.Context, d PWAPushDelivery) pwaPushOutcome {
	var task PWAPushTask
	if e := s.DB.WithContext(ctx).First(&task, "id = ?", d.TaskID).Error; e != nil {
		return pwaPushOutcome{retryable: true, message: "推送任务暂时不可用"}
	}
	if task.Status == "cancelled" {
		return pwaPushOutcome{message: "推送已取消"}
	}
	enabled, e := s.pwaPushAppEnabled(s.DB.WithContext(ctx), d.PWAID, d.UserID)
	if e != nil {
		return pwaPushOutcome{retryable: true, message: "应用状态暂时不可用"}
	}
	if !enabled {
		return pwaPushOutcome{message: "应用或账户已停用通知"}
	}
	var sub PWAPushSubscription
	if e := s.DB.WithContext(ctx).First(&sub, "id = ? AND pwa_id = ? AND status = ?", d.SubscriptionID, d.PWAID, "active").Error; e != nil {
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return pwaPushOutcome{retryable: true, message: "订阅状态暂时不可用"}
		}
		return pwaPushOutcome{message: "订阅已停用"}
	}
	// Keep retries bounded even if the process is repeatedly killed mid-request.
	if d.Attempts > 5 || !d.ExpiresAt.After(s.Now()) {
		return pwaPushOutcome{message: "通知重试期限已结束"}
	}
	var app PWAApp
	if e := s.DB.WithContext(ctx).First(&app, "id = ?", d.PWAID).Error; e != nil {
		return pwaPushOutcome{message: "应用已移除"}
	}
	raw, e := s.decryptSecret(sub.EncryptedSubscription)
	if e != nil {
		return pwaPushOutcome{message: "订阅密钥不可用"}
	}
	var stored pwaSubscriptionInput
	if e = json.Unmarshal([]byte(raw), &stored); e != nil {
		return pwaPushOutcome{message: "订阅信息无效"}
	}
	if e = validatePWASubscription(&stored); e != nil {
		return pwaPushOutcome{message: "订阅信息无效"}
	}
	privateKey, e := s.decryptSecret(app.VAPIDPrivateKey)
	if e != nil {
		return pwaPushOutcome{message: "推送身份密钥不可用"}
	}
	payload, e := json.Marshal(map[string]any{"title": task.Title, "body": task.Body, "icon": task.Icon, "url": task.URL, "delivery_id": d.ID, "event_token": s.pwaEventToken(d)})
	if e != nil {
		return pwaPushOutcome{message: "通知内容无效"}
	}
	subscription := &webpush.Subscription{Endpoint: stored.Endpoint, Keys: webpush.Keys{P256dh: stored.Keys.P256DH, Auth: stored.Keys.Auth}}
	client := s.pwaPushHTTP
	if client == nil {
		client = s.securePWAPushClient()
	}
	reqctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	response, e := webpush.SendNotificationWithContext(reqctx, payload, subscription, &webpush.Options{HTTPClient: client, Subscriber: s.Config.PublicURL, VAPIDPublicKey: app.VAPIDPublicKey, VAPIDPrivateKey: privateKey, TTL: 24 * 60 * 60, Topic: d.ID, Urgency: webpush.UrgencyNormal})
	if e != nil {
		return pwaPushOutcome{retryable: true, message: "推送服务暂时无法连接"}
	}
	if response.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return pwaPushOutcome{accepted: true}
	}
	if response.StatusCode == 404 || response.StatusCode == 410 {
		return pwaPushOutcome{expired: true, message: "浏览器订阅已失效", subscriptionCiphertext: sub.EncryptedSubscription}
	}
	if response.StatusCode == 429 || response.StatusCode >= 500 {
		retry := time.Duration(0)
		if sec, e := strconv.Atoi(response.Header.Get("Retry-After")); e == nil && sec > 0 {
			retry = time.Duration(min(sec, 6*60*60)) * time.Second
		} else if t, e := http.ParseTime(response.Header.Get("Retry-After")); e == nil {
			retry = min(max(t.Sub(s.Now()), 0), 6*time.Hour)
		}
		return pwaPushOutcome{retryable: true, message: "推送服务繁忙，等待重试", retryAfter: retry}
	}
	return pwaPushOutcome{message: "推送服务拒绝此通知 (HTTP " + strconv.Itoa(response.StatusCode) + ")"}
}
func (s *Server) finishPWAPushDelivery(ctx context.Context, d PWAPushDelivery, outcome pwaPushOutcome) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task PWAPushTask
		if e := pwaPushLock(tx).First(&task, "id = ?", d.TaskID).Error; e != nil {
			return e
		}
		var current PWAPushDelivery
		if e := pwaPushLock(tx).Where("id = ? AND status = ? AND lease_token = ?", d.ID, "processing", d.LeaseToken).First(&current).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return nil
			}
			return e
		}
		update := map[string]any{"lease_until": nil, "lease_token": "", "last_error": outcome.message}
		switch {
		case outcome.accepted:
			update["status"] = "sent"
			update["sent_at"] = s.Now()
			update["last_error"] = ""
		case task.Status == "cancelled":
			update["status"] = "cancelled"
		case outcome.retryable && d.Attempts < 5 && d.ExpiresAt.After(s.Now()):
			update["status"] = "pending"
			delay := time.Minute * time.Duration(1<<min(d.Attempts-1, 6))
			delay = max(delay, outcome.retryAfter)
			update["next_attempt_at"] = s.Now().Add(delay)
		default:
			update["status"] = "failed"
		}
		if e := tx.Model(&current).Updates(update).Error; e != nil {
			return e
		}
		if outcome.expired {
			return tx.Model(&PWAPushSubscription{}).Where("id = ? AND status = ? AND encrypted_subscription = ?", d.SubscriptionID, "active", outcome.subscriptionCiphertext).Update("status", "expired").Error
		}
		return nil
	})
}
func (s *Server) finishPWAPushTasks(ctx context.Context) error {
	db := s.DB.WithContext(ctx)
	// A cancelled task may have had a crashed worker. Active leases may still
	// report provider acceptance, while expired ones can be safely cancelled.
	if e := db.Model(&PWAPushDelivery{}).Where("(status = ? OR (status = ? AND lease_until <= ?)) AND EXISTS (SELECT 1 FROM pwa_push_tasks t WHERE t.id = pwa_push_deliveries.task_id AND t.status = ?)", "pending", "processing", s.Now(), "cancelled").Updates(map[string]any{"status": "cancelled", "lease_until": nil, "lease_token": ""}).Error; e != nil {
		return e
	}
	return db.Model(&PWAPushTask{}).Where("status IN ? AND expanded = ? AND NOT EXISTS (SELECT 1 FROM pwa_push_deliveries d WHERE d.task_id = pwa_push_tasks.id AND d.status IN ?)", []string{"queued", "sending"}, true, []string{"pending", "processing"}).Updates(map[string]any{"status": "completed", "completed_at": s.Now()}).Error
}

func pwaPublicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if addr.Is6() && !netip.MustParsePrefix("2000::/3").Contains(addr) {
		return false
	}
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/32", "2002::/16", "2001:10::/28", "2001:20::/28"} {
		if netip.MustParsePrefix(cidr).Contains(addr) {
			return false
		}
	}
	return true
}
func (s *Server) securePWAPushClient() *http.Client {
	resolver := s.DNS
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 4}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil || port != "443" || !pwaPushProvider("https://"+host+"/push") {
			return nil, errors.New("push destination rejected")
		}
		ips, e := resolver.LookupIPAddr(ctx, host)
		if e != nil || len(ips) == 0 {
			return nil, errors.New("push DNS unavailable")
		}
		// Fail the entire mixed DNS answer; never fall back to a private address.
		for _, ip := range ips {
			if !pwaPublicIP(ip.IP) {
				return nil, errors.New("push destination rejected")
			}
		}
		dialer := net.Dialer{Timeout: 10 * time.Second}
		for _, ip := range ips {
			conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, errors.New("push connection unavailable")
	}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
