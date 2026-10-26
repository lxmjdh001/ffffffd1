package platform

import (
	"crypto/ecdh"
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Subscription endpoints and encryption keys are browser capabilities. Keep the
// complete subscription encrypted at rest and never expose it in owner reports.
// Tombstones remain after opt-out so re-subscribing cannot replay welcome rules.
type PWAPushSubscription struct {
	ID                    string    `json:"id" gorm:"primaryKey;size:32"`
	UserID                string    `json:"-" gorm:"index;size:32;not null"`
	PWAID                 string    `json:"pwa_id" gorm:"uniqueIndex:idx_pwa_push_endpoint,priority:1;index:idx_pwa_push_audience,priority:1;size:32;not null"`
	EndpointHash          string    `json:"-" gorm:"uniqueIndex:idx_pwa_push_endpoint,priority:2;size:64;not null"`
	EncryptedSubscription string    `json:"-" gorm:"not null"`
	Status                string    `json:"status" gorm:"index:idx_pwa_push_audience,priority:2;size:20;not null"`
	Country               string    `json:"country" gorm:"size:2"`
	Browser               string    `json:"browser" gorm:"size:40"`
	Device                string    `json:"device" gorm:"size:40"`
	CreatedAt             time.Time `json:"created_at" gorm:"index:idx_pwa_push_audience,priority:3"`
	LastSeenAt            time.Time `json:"last_seen_at"`
}

type PWAPushTask struct {
	ID                 string     `json:"id" gorm:"primaryKey;size:32"`
	UserID             string     `json:"-" gorm:"uniqueIndex:idx_pwa_push_request,priority:1;index;size:32;not null"`
	RequestKeyHash     string     `json:"-" gorm:"uniqueIndex:idx_pwa_push_request,priority:2;size:64;not null"`
	RequestFingerprint string     `json:"-" gorm:"size:64;not null"`
	PWAID              string     `json:"pwa_id" gorm:"index;size:32;not null"`
	PWAName            string     `json:"pwa_name" gorm:"size:100"`
	Title              string     `json:"title" gorm:"size:400;not null"`
	Body               string     `json:"body" gorm:"size:2000;not null"`
	URL                string     `json:"url" gorm:"size:2048;not null"`
	Icon               string     `json:"icon" gorm:"size:2048"`
	TemplateID         *string    `json:"template_id,omitempty" gorm:"size:32;index"`
	Image              string     `json:"image" gorm:"size:2048;not null;default:''"`
	ActionsJSON        string     `json:"-" gorm:"type:text;not null;default:'[]'"`
	ScheduleType       string     `json:"schedule_type" gorm:"size:20;not null"`
	ScheduledAt        *time.Time `json:"scheduled_at" gorm:"index:idx_pwa_push_due,priority:2"`
	DelayMinutes       int        `json:"delay_minutes"`
	CountriesJSON      string     `json:"-" gorm:"not null"`
	Status             string     `json:"status" gorm:"index:idx_pwa_push_due,priority:1;size:20;not null"`
	AudienceAt         *time.Time `json:"-"`
	AudienceCursor     string     `json:"-" gorm:"size:32"`
	Expanded           bool       `json:"-"`
	CreatedAt          time.Time  `json:"created_at"`
	CompletedAt        *time.Time `json:"completed_at"`
}

type PWAPushDelivery struct {
	ID             string     `json:"id" gorm:"primaryKey;size:32"`
	TaskID         string     `json:"task_id" gorm:"uniqueIndex:idx_pwa_push_delivery_once,priority:1;size:32;not null"`
	SubscriptionID string     `json:"subscription_id" gorm:"uniqueIndex:idx_pwa_push_delivery_once,priority:2;index;size:32;not null"`
	UserID         string     `json:"-" gorm:"index;size:32;not null"`
	PWAID          string     `json:"-" gorm:"index;size:32;not null"`
	Country        string     `json:"country" gorm:"size:2"`
	Status         string     `json:"status" gorm:"index:idx_pwa_push_delivery_due,priority:1;size:20;not null"`
	Attempts       int        `json:"attempts"`
	NextAttemptAt  time.Time  `json:"-" gorm:"index:idx_pwa_push_delivery_due,priority:2"`
	ExpiresAt      time.Time  `json:"-"`
	LeaseUntil     *time.Time `json:"-"`
	LeaseToken     string     `json:"-" gorm:"size:32"`
	LastError      string     `json:"last_error" gorm:"size:200"`
	Provider       string     `json:"provider" gorm:"size:20;not null;default:''"`
	ProviderStatus int        `json:"provider_status" gorm:"not null;default:0"`
	ErrorCode      string     `json:"error_code" gorm:"size:40;not null;default:''"`
	LastAttemptAt  *time.Time `json:"last_attempt_at"`
	CreatedAt      time.Time  `json:"created_at"`
	SentAt         *time.Time `json:"sent_at"`
	DisplayedAt    *time.Time `json:"displayed_at"`
	ClickedAt      *time.Time `json:"clicked_at"`
}

type pwaSubscriptionInput struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Token          string   `json:"token,omitempty"`
	ExpirationTime *float64 `json:"expirationTime,omitempty"`
	Automatic      bool     `json:"automatic,omitempty"`
}

var errPWASubscriptionUnsubscribed = errors.New("subscription_unsubscribed")

type pwaPushTaskInput struct {
	PWAID           string          `json:"pwa_id"`
	Title           string          `json:"title"`
	Body            string          `json:"body"`
	URL             string          `json:"url"`
	ScheduleType    string          `json:"schedule_type"`
	ScheduledAt     *time.Time      `json:"scheduled_at"`
	DelayMinutes    int             `json:"delay_minutes"`
	TargetCountries []string        `json:"target_countries"`
	TemplateID      string          `json:"template_id,omitempty"`
	Icon            string          `json:"icon,omitempty"`
	Image           string          `json:"image,omitempty"`
	Actions         []PWAPushAction `json:"actions,omitempty"`
}

type pwaPushTaskView struct {
	PWAPushTask
	TargetCountries []string        `json:"target_countries"`
	Actions         []PWAPushAction `json:"actions"`
	TotalCount      int64           `json:"total_count"`
	SentCount       int64           `json:"sent_count"`
	FailedCount     int64           `json:"failed_count"`
	DisplayedCount  int64           `json:"displayed_count"`
	ClickedCount    int64           `json:"clicked_count"`
}

func (s *Server) registerPWAPushRoutes(m *http.ServeMux) {
	s.registerPWAPushTemplateRoutes(m)
	m.Handle("GET /api/pwa-push/subscriptions", s.auth(s.listPWAPushSubscriptions))
	m.Handle("GET /api/pwa-push/tasks", s.auth(s.listPWAPushTasks))
	m.Handle("POST /api/pwa-push/tasks", s.auth(s.createPWAPushTask))
	m.Handle("GET /api/pwa-push/tasks/{id}", s.auth(s.getPWAPushTask))
	m.Handle("DELETE /api/pwa-push/tasks/{id}", s.auth(s.cancelPWAPushTask))
	m.Handle("GET /api/pwa-push/tasks/{id}/deliveries", s.auth(s.listPWAPushDeliveries))
	m.HandleFunc("POST /p/{slug}/subscribe", s.subscribePWAPush)
	m.HandleFunc("POST /p/{slug}/unsubscribe", s.unsubscribePWAPush)
	m.HandleFunc("POST /p/{slug}/event", s.recordPWAPushEvent)
}

func pwaActiveSubscriptionCounts(db *gorm.DB, ids []string) (map[string]int64, error) {
	result := map[string]int64{}
	if len(ids) == 0 {
		return result, nil
	}
	var rows []struct {
		PWAID string
		N     int64
	}
	if err := db.Model(&PWAPushSubscription{}).Select("pwa_id, count(*) AS n").Where("pwa_id IN ? AND status = ?", ids, "active").Group("pwa_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.PWAID] = row.N
	}
	return result, nil
}

// The only network targets accepted here are the public endpoints used by major
// browser push services. DNS/IP validation is repeated by the worker at dial time.
func pwaPushProvider(raw string) bool {
	if len(raw) > 2048 || strings.ContainsAny(raw, "\r\n") {
		return false
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Hostname() == "" || (u.Port() != "" && u.Port() != "443") || u.Path == "" || u.Path == "/" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if strings.HasSuffix(h, ".") {
		return false
	}
	switch h {
	case "fcm.googleapis.com", "android.googleapis.com", "updates.push.services.mozilla.com", "push.services.mozilla.com", "web.push.apple.com":
		return true
	}
	return (strings.HasSuffix(h, ".push.apple.com") || strings.HasSuffix(h, ".push.services.mozilla.com") || strings.HasSuffix(h, ".notify.windows.com")) && !strings.ContainsAny(h, "%\\")
}
func pwaDecodeKey(raw string) ([]byte, error) {
	if strings.ContainsAny(raw, " \r\n\t") {
		return nil, errors.New("invalid key")
	}
	b, e := base64.RawURLEncoding.DecodeString(strings.TrimRight(raw, "="))
	return b, e
}
func validatePWASubscription(in *pwaSubscriptionInput) error {
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	if !pwaPushProvider(in.Endpoint) {
		return problem(400, "通知订阅地址不受支持")
	}
	p, e := pwaDecodeKey(in.Keys.P256DH)
	if e != nil || len(p) != 65 {
		return problem(400, "通知订阅密钥无效")
	}
	if _, e = ecdh.P256().NewPublicKey(p); e != nil {
		return problem(400, "通知订阅密钥无效")
	}
	a, e := pwaDecodeKey(in.Keys.Auth)
	if e != nil || len(a) != 16 {
		return problem(400, "通知订阅密钥无效")
	}
	in.Keys.P256DH = base64.RawURLEncoding.EncodeToString(p)
	in.Keys.Auth = base64.RawURLEncoding.EncodeToString(a)
	return nil
}
func (s *Server) pwaSubscriptionToken(sub PWAPushSubscription) string {
	return sub.ID + "." + s.mac("pwa-subscription:"+sub.PWAID+":"+sub.ID)
}
func (s *Server) validPWASubscriptionToken(sub PWAPushSubscription, token string) bool {
	return hmac.Equal([]byte(s.pwaSubscriptionToken(sub)), []byte(token))
}
func (s *Server) pwaEventToken(d PWAPushDelivery) string {
	return s.mac("pwa-push-event:" + d.PWAID + ":" + d.ID)
}
func (s *Server) pwaPublicPOST(r *http.Request) error {
	if r.Header.Get("Origin") != s.pwaRequestOrigin(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return problem(403, "请求来源不受信任")
	}
	return nil
}
func pwaPushLock(db *gorm.DB) *gorm.DB { return db.Clauses(clause.Locking{Strength: "UPDATE"}) }

func (s *Server) subscribePWAPush(w http.ResponseWriter, r *http.Request) {
	if e := s.pwaPublicPOST(r); e != nil {
		fail(w, e)
		return
	}
	app, e := s.loadPublicPWA(r)
	if e != nil {
		fail(w, e)
		return
	}
	if !app.PushEnabled {
		fail(w, problem(409, "此应用尚未启用通知"))
		return
	}
	if e = s.rate("pwa-subscribe:"+s.clientIP(r), 60, 10*time.Minute); e != nil {
		fail(w, e)
		return
	}
	var in pwaSubscriptionInput
	if e = decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if e = validatePWASubscription(&in); e != nil {
		fail(w, e)
		return
	}
	var sub PWAPushSubscription
	e = s.DB.Transaction(func(tx *gorm.DB) error {
		var current PWAApp
		if err := pwaPushLock(tx).First(&current, "id = ? AND enabled = ? AND push_enabled = ?", app.ID, true, true).Error; err != nil {
			return err
		}
		endpointHash := hashString(in.Endpoint)
		err := pwaPushLock(tx).Where("pwa_id = ? AND endpoint_hash = ?", app.ID, endpointHash).First(&sub).Error
		isNew := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !isNew {
			return err
		}
		if isNew && in.Token != "" {
			parts := strings.Split(in.Token, ".")
			if len(parts) != 2 {
				return problem(403, "通知订阅凭证无效")
			}
			if err = pwaPushLock(tx).Where("id = ? AND pwa_id = ?", parts[0], app.ID).First(&sub).Error; err != nil {
				return problem(403, "通知订阅凭证无效")
			}
			if !s.validPWASubscriptionToken(sub, in.Token) {
				return problem(403, "通知订阅凭证无效")
			}
			isNew = false
		} else if !isNew && !s.validPWASubscriptionToken(sub, in.Token) {
			// Losing localStorage can be recovered with the browser-held auth secret;
			// knowing an endpoint alone never grants a subscription capability.
			raw, err := s.decryptSecret(sub.EncryptedSubscription)
			if err != nil {
				return err
			}
			var old pwaSubscriptionInput
			if json.Unmarshal([]byte(raw), &old) != nil || !hmac.Equal([]byte(old.Keys.Auth), []byte(in.Keys.Auth)) || !hmac.Equal([]byte(old.Keys.P256DH), []byte(in.Keys.P256DH)) {
				return problem(403, "通知订阅凭证无效")
			}
		}
		// A delayed automatic sync from another window must not undo opt-out.
		// The row lock also orders this check against unsubscribe's UPDATE.
		if in.Automatic && !isNew && sub.Status == "unsubscribed" {
			return errPWASubscriptionUnsubscribed
		}
		now := s.Now()
		if isNew {
			var n int64
			if err = tx.Model(&PWAPushSubscription{}).Where("pwa_id = ?", app.ID).Count(&n).Error; err != nil {
				return err
			}
			if n >= 100000 {
				return problem(409, "此应用的通知订阅数量已达上限")
			}
			sub = PWAPushSubscription{ID: randomHex(16), UserID: app.UserID, PWAID: app.ID, CreatedAt: now}
		}
		stored := in
		stored.Token = ""
		stored.ExpirationTime = nil
		stored.Automatic = false
		raw, _ := json.Marshal(stored)
		encrypted, err := s.encryptSecret(string(raw))
		if err != nil {
			return err
		}
		sub.EndpointHash = endpointHash
		sub.EncryptedSubscription = encrypted
		sub.Status = "active"
		sub.Country = s.cloakRequestCountry(r)
		sub.LastSeenAt = now
		sub.Device, _, sub.Browser = classifyDevice(r.UserAgent())
		if err = tx.Save(&sub).Error; err != nil {
			return err
		}
		if isNew {
			var tasks []PWAPushTask
			if err = pwaPushLock(tx).Where("pwa_id = ? AND schedule_type = ? AND status = ?", app.ID, "first_subscribe", "active").Order("id ASC").Find(&tasks).Error; err != nil {
				return err
			}
			for _, task := range tasks {
				if !pwaTaskCountryMatches(task, sub.Country) {
					continue
				}
				if err = createPWAPushDelivery(tx, task, sub, now.Add(time.Duration(task.DelayMinutes)*time.Minute), now); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if e != nil {
		if errors.Is(e, errPWASubscriptionUnsubscribed) {
			jsonOut(w, http.StatusConflict, map[string]string{"error": "通知已关闭，请手动开启以重新订阅", "code": "subscription_unsubscribed"})
			return
		}
		fail(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonOut(w, 200, map[string]any{"subscription_id": sub.ID, "token": s.pwaSubscriptionToken(sub), "status": sub.Status})
}
func (s *Server) unsubscribePWAPush(w http.ResponseWriter, r *http.Request) {
	if e := s.pwaPublicPOST(r); e != nil {
		fail(w, e)
		return
	}
	app, e := s.loadPublicPWA(r)
	if e != nil {
		fail(w, e)
		return
	}
	var in struct {
		SubscriptionID string `json:"subscription_id"`
		Token          string `json:"token"`
	}
	if e = decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	var sub PWAPushSubscription
	if e = s.DB.Where("id = ? AND pwa_id = ?", in.SubscriptionID, app.ID).First(&sub).Error; e != nil {
		fail(w, problem(404, "订阅不存在"))
		return
	}
	if !s.validPWASubscriptionToken(sub, in.Token) {
		fail(w, problem(403, "通知订阅凭证无效"))
		return
	}
	if e = s.DB.Model(&sub).Update("status", "unsubscribed").Error; e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (s *Server) recordPWAPushEvent(w http.ResponseWriter, r *http.Request) {
	if e := s.pwaPublicPOST(r); e != nil {
		fail(w, e)
		return
	}
	app, e := s.loadPublicPWA(r)
	if e != nil {
		fail(w, e)
		return
	}
	var in struct {
		DeliveryID string `json:"delivery_id"`
		Token      string `json:"token"`
		Event      string `json:"event"`
	}
	if e = decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if in.Event != "displayed" && in.Event != "clicked" {
		fail(w, problem(400, "通知事件无效"))
		return
	}
	var d PWAPushDelivery
	if e = s.DB.Where("id = ? AND pwa_id = ?", in.DeliveryID, app.ID).First(&d).Error; e != nil {
		fail(w, problem(404, "通知记录不存在"))
		return
	}
	if !hmac.Equal([]byte(in.Token), []byte(s.pwaEventToken(d))) {
		fail(w, problem(403, "通知事件凭证无效"))
		return
	}
	// The browser can acknowledge before the provider HTTP response is committed.
	// A valid per-delivery capability is only sent inside the encrypted payload.
	column := "displayed_at"
	if in.Event == "clicked" {
		column = "clicked_at"
	}
	if e = s.DB.Model(&PWAPushDelivery{}).Where("id = ? AND "+column+" IS NULL", d.ID).Update(column, s.Now()).Error; e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func pwaTaskCountryMatches(task PWAPushTask, country string) bool {
	var countries []string
	if json.Unmarshal([]byte(task.CountriesJSON), &countries) != nil {
		return false
	}
	if len(countries) == 0 {
		return true
	}
	for _, c := range countries {
		if c == country {
			return true
		}
	}
	return false
}
func createPWAPushDelivery(tx *gorm.DB, t PWAPushTask, sub PWAPushSubscription, due, now time.Time) error {
	d := PWAPushDelivery{ID: randomHex(16), TaskID: t.ID, SubscriptionID: sub.ID, UserID: t.UserID, PWAID: t.PWAID, Country: sub.Country, Status: "pending", NextAttemptAt: due, ExpiresAt: due.Add(24 * time.Hour), CreatedAt: now}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "task_id"}, {Name: "subscription_id"}}, DoNothing: true}).Create(&d).Error
}
func pwaPushPage(r *http.Request) (int, int) {
	offset, n := pageParams(r)
	p := offset/n + 1
	if n > 100 {
		n = 100
	}
	return p, n
}
func (s *Server) listPWAPushSubscriptions(w http.ResponseWriter, r *http.Request) {
	p, n := pwaPushPage(r)
	q := s.DB.Model(&PWAPushSubscription{}).Where("pwa_push_subscriptions.user_id = ?", userOf(r).ID)
	if app := r.URL.Query().Get("pwa_id"); app != "" {
		q = q.Where("pwa_id = ?", app)
	}
	if keyword := strings.TrimSpace(r.URL.Query().Get("q")); keyword != "" {
		q = q.Where("browser LIKE ? OR device LIKE ? OR country LIKE ? OR status LIKE ?", "%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
	}
	var total int64
	if e := q.Count(&total).Error; e != nil {
		fail(w, e)
		return
	}
	var rows []struct {
		PWAPushSubscription
		PWAName string `json:"pwa_name"`
	}
	e := q.Select("pwa_push_subscriptions.*, pwa_apps.name AS pwa_name").Joins("LEFT JOIN pwa_apps ON pwa_apps.id = pwa_push_subscriptions.pwa_id").Order("pwa_push_subscriptions.created_at DESC, pwa_push_subscriptions.id DESC").Offset((p - 1) * n).Limit(n).Scan(&rows).Error
	if e != nil {
		fail(w, e)
		return
	}
	if rows == nil {
		rows = make([]struct {
			PWAPushSubscription
			PWAName string `json:"pwa_name"`
		}, 0)
	}
	jsonOut(w, 200, map[string]any{"data": rows, "total": total, "page": p, "per_page": n})
}
func normalizedPWATaskInput(in *pwaPushTaskInput, app PWAApp, now time.Time, publicURL string) error {
	in.URL = strings.TrimSpace(in.URL)
	if in.URL == "" {
		in.URL = publicURL + "/p/" + app.Slug + "/launch"
	}
	if err := normalizePWAPushContent(&in.Title, &in.Body, &in.URL, &in.Icon, &in.Image, &in.Actions, publicURL, false); err != nil {
		return err
	}
	switch in.ScheduleType {
	case "now":
		if in.DelayMinutes != 0 || in.ScheduledAt != nil {
			return problem(400, "立即推送不能设置延时")
		}
	case "scheduled":
		if in.DelayMinutes != 0 || in.ScheduledAt == nil || !in.ScheduledAt.After(now) || in.ScheduledAt.After(now.Add(365*24*time.Hour)) {
			return problem(400, "请选择一年内的将来推送时间")
		}
		utc := in.ScheduledAt.UTC()
		in.ScheduledAt = &utc
	case "first_subscribe":
		if in.ScheduledAt != nil || in.DelayMinutes < 0 || in.DelayMinutes > 43200 {
			return problem(400, "首次订阅延时须在 0–43200 分钟之间")
		}
	default:
		return problem(400, "推送方式无效")
	}
	seen := map[string]bool{}
	countries := make([]string, 0)
	for _, country := range in.TargetCountries {
		country = strings.ToUpper(strings.TrimSpace(country))
		if len(country) != 2 || country == "XX" || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
			return problem(400, "目标国家须为两位国家代码")
		}
		if !seen[country] {
			seen[country] = true
			countries = append(countries, country)
		}
	}
	if len(countries) > 250 {
		return problem(400, "目标国家数量过多")
	}
	sort.Strings(countries)
	in.TargetCountries = countries
	// Reserve room for per-delivery IDs and Web Push encryption overhead.
	icon := in.Icon
	if icon == "" {
		icon = publicURL + "/p/" + app.Slug + "/icon/192.png"
	}
	payload, _ := json.Marshal(pwaPushContentPayload(in.Title, in.Body, in.URL, icon, in.Image, in.Actions))
	if len(payload) > 3400 {
		return problem(400, "通知内容过长，请缩短正文或链接")
	}
	return nil
}
func (s *Server) createPWAPushTask(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	if e := s.rate("pwa-push-task:"+u.ID, 100, 24*time.Hour); e != nil {
		fail(w, e)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) < 16 || len(key) > 128 || strings.ContainsAny(key, "\r\n\t ") {
		fail(w, problem(400, "请提供有效的请求标识"))
		return
	}
	var in pwaPushTaskInput
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	requested := in
	var task PWAPushTask
	status := 201
	e := s.DB.Transaction(func(tx *gorm.DB) error {
		// Serialize owner idempotency even when retrying with another application.
		var owner User
		if err := pwaPushLock(tx).First(&owner, "id = ? AND disabled = ?", u.ID, false).Error; err != nil {
			return err
		}
		var app PWAApp
		if err := pwaPushLock(tx).Where("id = ? AND user_id = ?", in.PWAID, u.ID).First(&app).Error; err != nil {
			return err
		}
		// Normalize before hashing. For an identical retry, compare scheduling against
		// the original creation time, so a delayed retry of a scheduled task succeeds.
		existingErr := tx.Where("user_id = ? AND request_key_hash = ?", u.ID, hashString(key)).First(&task).Error
		reference := s.Now()
		if existingErr == nil {
			reference = task.CreatedAt
		}
		in.TemplateID = strings.TrimSpace(in.TemplateID)
		implicitTemplate := in.TemplateID != "" && strings.TrimSpace(requested.Title) == "" && strings.TrimSpace(requested.Body) == ""
		if in.TemplateID != "" {
			// Retries use the task's immutable content, including after a source
			// template was edited or removed. New tasks validate template ownership.
			var title, body, target, icon, image string
			var actions []PWAPushAction
			if existingErr == nil && task.TemplateID != nil && *task.TemplateID == in.TemplateID {
				title, body, target, icon, image = task.Title, task.Body, task.URL, task.Icon, task.Image
				actions = pwaTaskActions(task)
			} else {
				var template PWAPushTemplate
				if err := tx.Where("id = ? AND user_id = ?", in.TemplateID, u.ID).First(&template).Error; err != nil {
					return err
				}
				title, body, target, icon, image = template.Title, template.Body, template.URL, template.Icon, template.Image
				if err := json.Unmarshal([]byte(template.ActionsJSON), &actions); err != nil {
					return err
				}
			}
			if implicitTemplate {
				in.Title, in.Body = title, body
				if strings.TrimSpace(in.URL) == "" {
					in.URL = target
				}
				if strings.TrimSpace(in.Icon) == "" {
					in.Icon = icon
				}
				if strings.TrimSpace(in.Image) == "" {
					in.Image = image
				}
				if in.Actions == nil {
					in.Actions = actions
				}
			}
		}
		if err := normalizedPWATaskInput(&in, app, reference, s.Config.PublicURL); err != nil {
			return err
		}
		fingerprintInput := in
		if implicitTemplate {
			// Fingerprint the caller's choices, not mutable template content.
			fingerprintInput.Title, fingerprintInput.Body = "", ""
			fingerprintInput.URL, fingerprintInput.Icon, fingerprintInput.Image = strings.TrimSpace(requested.URL), strings.TrimSpace(requested.Icon), strings.TrimSpace(requested.Image)
			if requested.Actions == nil {
				fingerprintInput.Actions = nil
			}
		}
		fingerprintRaw, _ := json.Marshal(fingerprintInput)
		if implicitTemplate {
			fingerprintRaw, _ = json.Marshal(struct {
				Input           pwaPushTaskInput `json:"input"`
				ActionsOverride bool             `json:"actions_override"`
			}{fingerprintInput, requested.Actions != nil})
		}
		fingerprint := hashString(string(fingerprintRaw))
		if existingErr == nil {
			if task.RequestFingerprint != fingerprint {
				return problem(409, "此请求标识已用于另一条推送")
			}
			status = 200
			return nil
		}
		if !errors.Is(existingErr, gorm.ErrRecordNotFound) {
			return existingErr
		}
		if !app.Enabled || !app.PushEnabled {
			return problem(409, "请先启用应用和通知推送")
		}
		var active int64
		if err := tx.Model(&PWAPushTask{}).Where("user_id = ? AND status IN ?", u.ID, []string{"queued", "sending", "active"}).Count(&active).Error; err != nil {
			return err
		}
		if active >= 100 {
			return problem(409, "进行中的推送任务已达 100 条，请先取消旧任务")
		}
		if in.ScheduleType == "first_subscribe" {
			var rules int64
			if err := tx.Model(&PWAPushTask{}).Where("user_id = ? AND status = ? AND schedule_type = ?", u.ID, "active", "first_subscribe").Count(&rules).Error; err != nil {
				return err
			}
			if rules >= 25 {
				return problem(409, "首次订阅推送规则已达 25 条")
			}
		}
		now := s.Now()
		countries, _ := json.Marshal(in.TargetCountries)
		actions, _ := json.Marshal(in.Actions)
		icon := in.Icon
		if icon == "" {
			icon = s.Config.PublicURL + "/p/" + app.Slug + "/icon/192.png"
		}
		task = PWAPushTask{ID: randomHex(16), UserID: u.ID, RequestKeyHash: hashString(key), RequestFingerprint: fingerprint, PWAID: app.ID, PWAName: app.Name, Title: in.Title, Body: in.Body, URL: in.URL, Icon: icon, Image: in.Image, ActionsJSON: string(actions), ScheduleType: in.ScheduleType, ScheduledAt: in.ScheduledAt, DelayMinutes: in.DelayMinutes, CountriesJSON: string(countries), Status: "queued", CreatedAt: now}
		if in.TemplateID != "" {
			task.TemplateID = &in.TemplateID
		}
		if task.ScheduleType == "now" {
			task.ScheduledAt = &now
		}
		if task.ScheduleType == "first_subscribe" {
			task.Status = "active"
			task.Expanded = true
		}
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		return recordAudit(tx, u.ID, "pwa.push.create", task.ID, task.ScheduleType)
	})
	if e != nil {
		fail(w, e)
		return
	}
	views, e := s.pwaPushTaskViews([]PWAPushTask{task})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, status, views[0])
}
func (s *Server) pwaPushTaskViews(tasks []PWAPushTask) ([]pwaPushTaskView, error) {
	result := make([]pwaPushTaskView, 0, len(tasks))
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		ids = append(ids, t.ID)
	}
	type counts struct {
		TaskID                                                           string
		TotalCount, SentCount, FailedCount, DisplayedCount, ClickedCount int64
	}
	rows := []counts{}
	if len(ids) > 0 {
		e := s.DB.Model(&PWAPushDelivery{}).Select("task_id, count(*) AS total_count, SUM(CASE WHEN status = 'sent' THEN 1 ELSE 0 END) AS sent_count, SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) AS failed_count, SUM(CASE WHEN displayed_at IS NOT NULL THEN 1 ELSE 0 END) AS displayed_count, SUM(CASE WHEN clicked_at IS NOT NULL THEN 1 ELSE 0 END) AS clicked_count").Where("task_id IN ?", ids).Group("task_id").Scan(&rows).Error
		if e != nil {
			return nil, e
		}
	}
	byID := map[string]counts{}
	for _, r := range rows {
		byID[r.TaskID] = r
	}
	for _, t := range tasks {
		countries := []string{}
		_ = json.Unmarshal([]byte(t.CountriesJSON), &countries)
		c := byID[t.ID]
		result = append(result, pwaPushTaskView{PWAPushTask: t, TargetCountries: countries, Actions: pwaTaskActions(t), TotalCount: c.TotalCount, SentCount: c.SentCount, FailedCount: c.FailedCount, DisplayedCount: c.DisplayedCount, ClickedCount: c.ClickedCount})
	}
	return result, nil
}
func (s *Server) listPWAPushTasks(w http.ResponseWriter, r *http.Request) {
	p, n := pwaPushPage(r)
	q := s.DB.Where("user_id = ?", userOf(r).ID)
	if app := r.URL.Query().Get("pwa_id"); app != "" {
		q = q.Where("pwa_id = ?", app)
	}
	var total int64
	if e := q.Model(&PWAPushTask{}).Count(&total).Error; e != nil {
		fail(w, e)
		return
	}
	var tasks []PWAPushTask
	if e := q.Order("created_at DESC, id DESC").Offset((p - 1) * n).Limit(n).Find(&tasks).Error; e != nil {
		fail(w, e)
		return
	}
	views, e := s.pwaPushTaskViews(tasks)
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]any{"data": views, "total": total, "page": p, "per_page": n})
}
func (s *Server) getPWAPushTask(w http.ResponseWriter, r *http.Request) {
	var task PWAPushTask
	if e := s.DB.First(&task, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Error; e != nil {
		fail(w, e)
		return
	}
	views, e := s.pwaPushTaskViews([]PWAPushTask{task})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, views[0])
}
func (s *Server) cancelPWAPushTask(w http.ResponseWriter, r *http.Request) {
	var task PWAPushTask
	e := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := pwaPushLock(tx).First(&task, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Error; err != nil {
			return err
		}
		if task.Status == "completed" || task.Status == "cancelled" {
			return nil
		}
		now := s.Now()
		task.Status = "cancelled"
		task.CompletedAt = &now
		if err := tx.Save(&task).Error; err != nil {
			return err
		}
		// An already in-flight provider request may complete; pending work stops.
		if err := tx.Model(&PWAPushDelivery{}).Where("task_id = ? AND status IN ?", task.ID, []string{"pending"}).Updates(map[string]any{"status": "cancelled", "lease_token": "", "lease_until": nil}).Error; err != nil {
			return err
		}
		return recordAudit(tx, userOf(r).ID, "pwa.push.cancel", task.ID, "")
	})
	if e != nil {
		fail(w, e)
		return
	}
	views, e := s.pwaPushTaskViews([]PWAPushTask{task})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, views[0])
}
func (s *Server) listPWAPushDeliveries(w http.ResponseWriter, r *http.Request) {
	var task PWAPushTask
	if e := s.DB.First(&task, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Error; e != nil {
		fail(w, e)
		return
	}
	p, n := pwaPushPage(r)
	q := s.DB.Model(&PWAPushDelivery{}).Where("task_id = ? AND user_id = ?", task.ID, userOf(r).ID)
	var total int64
	if e := q.Count(&total).Error; e != nil {
		fail(w, e)
		return
	}
	rows := []PWAPushDelivery{}
	if e := q.Order("created_at DESC, id DESC").Offset((p - 1) * n).Limit(n).Find(&rows).Error; e != nil {
		fail(w, e)
		return
	}
	views := make([]pwaPushDeliveryView, 0, len(rows))
	for _, row := range rows {
		views = append(views, newPWAPushDeliveryView(row))
	}
	jsonOut(w, 200, map[string]any{"data": views, "total": total, "page": p, "per_page": n})
}
