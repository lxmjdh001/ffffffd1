package platform

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Server) encryptSecret(plain string) (string, error) {
	key := sha256.Sum256([]byte("d1apk-ad-credentials:" + s.Config.Secret))
	block, e := aes.NewCipher(key[:])
	if e != nil {
		return "", e
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		return "", e
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	return base64.RawStdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}
func (s *Server) decryptSecret(value string) (string, error) {
	key := sha256.Sum256([]byte("d1apk-ad-credentials:" + s.Config.Secret))
	block, e := aes.NewCipher(key[:])
	if e != nil {
		return "", e
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		return "", e
	}
	b, e := base64.RawStdEncoding.DecodeString(value)
	if e != nil || len(b) < gcm.NonceSize() {
		return "", errors.New("credential decryption failed")
	}
	out, e := gcm.Open(nil, b[:gcm.NonceSize()], b[gcm.NonceSize():], nil)
	return string(out), e
}
func defaultAds(a Application) AdConfig {
	return AdConfig{AppID: a.ID, UserID: a.UserID, OpenFacebookEvent: "ViewContent", RegisterFacebookEvent: "CompleteRegistration", DownloadFacebookEvent: "D1DownloadComplete", OpenTikTokEvent: "ViewContent", RegisterTikTokEvent: "CompleteRegistration", DownloadTikTokEvent: "Download"}
}

// Called only after ownedApp has verified the application's owner.
func (s *Server) writeAdConfig(w http.ResponseWriter, c AdConfig) {
	secret := ""
	if c.APISecret != "" {
		var err error
		secret, err = s.decryptSecret(c.APISecret)
		if err != nil {
			fail(w, err)
			return
		}
	}
	jsonOut(w, 200, map[string]any{"config": c, "facebook_token_set": c.FacebookToken != "", "tiktok_token_set": c.TikTokToken != "", "api_secret_set": c.APISecret != "", "api_secret": secret})
}
func (s *Server) getAds(w http.ResponseWriter, r *http.Request) {
	a, e := s.ownedApp(r)
	if e != nil {
		fail(w, e)
		return
	}
	c := defaultAds(a)
	e = s.DB.First(&c, "app_id = ?", a.ID).Error
	if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
		fail(w, e)
		return
	}
	s.writeAdConfig(w, c)
}

var adIdentifier = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,100}$`)

func validDeepLink(raw string) bool {
	if raw == "" {
		return true
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme == "" || u.User != nil || len(raw) > 512 {
		return false
	}
	if u.Scheme == "https" {
		return validHostname(u.Hostname())
	}
	return regexp.MustCompile(`^[a-z][a-z0-9]{1,25}$`).MatchString(u.Scheme) && u.Host != "" && u.Scheme != "http" && u.Scheme != "javascript" && u.Scheme != "data" && u.Scheme != "file"
}
func (s *Server) saveAds(w http.ResponseWriter, r *http.Request) {
	a, e := s.ownedApp(r)
	if e != nil {
		fail(w, e)
		return
	}
	var in struct {
		Config        AdConfig `json:"config"`
		FacebookToken string   `json:"facebook_token"`
		TikTokToken   string   `json:"tiktok_token"`
	}
	if e = decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	c := in.Config
	c.AppID = a.ID
	c.UserID = a.UserID
	for _, name := range []string{c.OpenFacebookEvent, c.RegisterFacebookEvent, c.DownloadFacebookEvent, c.OpenTikTokEvent, c.RegisterTikTokEvent, c.DownloadTikTokEvent} {
		if !adIdentifier.MatchString(name) {
			fail(w, problem(400, "回传事件名格式无效"))
			return
		}
	}
	if c.FacebookEnabled && !regexp.MustCompile(`^[0-9]{5,32}$`).MatchString(c.FacebookPixel) || c.TikTokEnabled && !adIdentifier.MatchString(c.TikTokPixel) || !validDeepLink(c.DeepLink) || len(c.FacebookTestCode) > 100 || len(c.TikTokTestCode) > 100 || len(in.FacebookToken) > 4096 || len(in.TikTokToken) > 4096 {
		fail(w, problem(400, "请检查广告账户、事件或 App Link 配置"))
		return
	}
	if c.AllowedOrigin != "" {
		u, e := url.Parse(c.AllowedOrigin)
		if e != nil || !validHTTPS(c.AllowedOrigin) || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			fail(w, problem(400, "WebView 网站来源须为 HTTPS Origin，例如 https://customer.com"))
			return
		}
	}
	e = s.DB.Transaction(func(tx *gorm.DB) error {
		if _, e := lockUser(tx, a.UserID); e != nil {
			return e
		}
		var old AdConfig
		e := tx.First(&old, "app_id = ?", a.ID).Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		c.APISecret = old.APISecret
		c.FacebookToken = old.FacebookToken
		c.TikTokToken = old.TikTokToken
		if in.FacebookToken != "" {
			c.FacebookToken, e = s.encryptSecret(strings.TrimSpace(in.FacebookToken))
			if e != nil {
				return e
			}
		}
		if in.TikTokToken != "" {
			c.TikTokToken, e = s.encryptSecret(strings.TrimSpace(in.TikTokToken))
			if e != nil {
				return e
			}
		}
		if c.FacebookEnabled && c.FacebookToken == "" || c.TikTokEnabled && c.TikTokToken == "" {
			return problem(400, "启用回传前须填写对应的 Access Token")
		}
		if e = tx.Save(&c).Error; e != nil {
			return e
		}
		return recordAudit(tx, a.UserID, "ads.configure", a.ID, "更新广告回传设置（不记录密钥）")
	})
	if e != nil {
		fail(w, e)
		return
	}
	s.writeAdConfig(w, c)
}
func (s *Server) rotateAPISecret(w http.ResponseWriter, r *http.Request) {
	a, e := s.ownedApp(r)
	if e != nil {
		fail(w, e)
		return
	}
	secret := randomHex(32)
	encrypted, e := s.encryptSecret(secret)
	if e != nil {
		fail(w, e)
		return
	}
	e = s.DB.Transaction(func(tx *gorm.DB) error {
		if _, e := lockUser(tx, a.UserID); e != nil {
			return e
		}
		c := defaultAds(a)
		if e := tx.Where("app_id = ?", a.ID).FirstOrCreate(&c).Error; e != nil {
			return e
		}
		if e := tx.Model(&c).Update("api_secret", encrypted).Error; e != nil {
			return e
		}
		return recordAudit(tx, a.UserID, "ads.rotate_api_key", a.ID, "旧 API 签名密钥已失效")
	})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]string{"api_secret": secret, "app_id": a.ID, "endpoint": s.Config.PublicURL + "/api/sdk/apps/" + a.ID + "/events"})
}
func (s *Server) attributionToken(a Attribution) string {
	return a.ID + "." + s.mac("attribution:"+a.AppID+":"+a.ID)
}
func (s *Server) readAttribution(token, appID string) (Attribution, error) {
	var a Attribution
	parts := strings.Split(token, ".")
	if len(parts) != 2 || len(parts[0]) != 32 || !hexKey.MatchString(parts[0]) || subtle.ConstantTimeCompare([]byte(parts[1]), []byte(s.mac("attribution:"+appID+":"+parts[0]))) != 1 {
		return a, problem(400, "归因标识无效")
	}
	e := s.DB.First(&a, "id = ? AND app_id = ? AND expires_at > ?", parts[0], appID, s.Now()).Error
	return a, e
}
func (s *Server) prepareAttribution(w http.ResponseWriter, r *http.Request, app Application, visitor string) (Attribution, string, error) {
	if routed, ok, err := s.prepareRoutedAttribution(w, r, app, visitor); ok || err != nil {
		return routed, s.attributionToken(routed), err
	}
	fb := cleanText(r.URL.Query().Get("fbclid"), 512)
	tt := cleanText(r.URL.Query().Get("ttclid"), 1024)
	cookieName := "d1_attr_" + app.Slug
	if cookie, e := r.Cookie(cookieName); e == nil {
		if a, e := s.readAttribution(cookie.Value, app.ID); e == nil && a.VisitorHash == s.mac(visitor) && ((fb == "" && tt == "") || (a.FBClid == fb && a.TTClid == tt)) {
			return a, cookie.Value, nil
		}
	}
	scheme := "https"
	if !s.Config.Production() {
		scheme = "http"
	}
	a := Attribution{ID: randomHex(16), AppID: app.ID, VisitorHash: s.mac(visitor), FBClid: fb, TTClid: tt, IP: s.clientIP(r), UserAgent: cleanText(r.UserAgent(), 2048), Source: safeSource(r.Referer()), PageURL: safeSource(scheme + "://" + r.Host + r.URL.RequestURI()), CreatedAt: s.Now(), ExpiresAt: s.Now().Add(7 * 24 * time.Hour)}
	if fb != "" {
		a.FBC = fmt.Sprintf("fb.1.%d.%s", s.Now().UnixMilli(), fb)
	}
	for _, pair := range []struct {
		name string
		out  *string
	}{{"_fbp", &a.FBP}, {"_ttp", &a.TTP}} {
		if c, e := r.Cookie(pair.name); e == nil {
			*pair.out = cleanText(c.Value, 512)
		}
	}
	if e := s.DB.Create(&a).Error; e != nil {
		return a, "", e
	}
	token := s.attributionToken(a)
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/a/" + app.Slug, HttpOnly: true, Secure: s.Config.Production(), SameSite: http.SameSiteLaxMode, MaxAge: 7 * 86400})
	return a, token, nil
}
func deepLinkWithToken(c AdConfig, token string) template.URL {
	if c.DeepLink == "" || !validDeepLink(c.DeepLink) {
		return ""
	}
	u, e := url.Parse(c.DeepLink)
	if e != nil {
		return ""
	}
	q := u.Query()
	q.Set("d1_token", token)
	u.RawQuery = q.Encode()
	return template.URL(u.String())
}
func (s *Server) queueDownloadConversion(tx *gorm.DB, t Ticket) error {
	if t.AttributionID == "" {
		return nil
	}
	var attr Attribution
	if e := tx.First(&attr, "id = ?", t.AttributionID).Error; e != nil {
		return e
	}
	if !attr.Consent {
		return nil
	}
	eventName, verified := "download_complete", "download_server"
	if attr.SourceAttributionID != "" {
		var root Attribution
		if err := tx.First(&root, "id = ? AND source_attribution_id = ?", attr.SourceAttributionID, "").Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		var source Application
		if err := tx.Unscoped().First(&source, "id = ? AND user_id = ?", root.AppID, t.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if !root.Consent {
			return nil
		}
		attr = root
		eventName, verified = "download_start", "download_server_start"
	}
	// Keep the existing canonical ID so start, completion, retries, and a route
	// back to the source APK cannot produce two download advertising events.
	event := Conversion{ID: randomHex(16), AppID: attr.AppID, DownloadedAppID: t.AppID, UserID: t.UserID, AttributionID: attr.ID, Event: eventName, EventID: hashString(attr.AppID + ":" + attr.ID + ":download_complete"), IP: t.IP, UserAgent: t.UserAgent, SourceURL: attr.PageURL, Consent: true, VerifiedBy: verified, EventTime: s.Now(), CreatedAt: s.Now()}
	return s.createConversion(tx, &event, attr)
}
func (s *Server) createConversion(tx *gorm.DB, event *Conversion, attr Attribution) error {
	res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(event)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var found Conversion
		q := tx.Preload("Deliveries").Where("attribution_id = ?", attr.ID)
		if event.Event == "download_start" || event.Event == "download_complete" {
			q = q.Where("event IN ?", []string{"download_start", "download_complete"})
		} else {
			q = q.Where("event = ?", event.Event)
		}
		e := q.First(&found).Error
		if e != nil {
			return problem(409, "event_id 已用于另一归因事件")
		}
		*event = found
		return nil
	}
	if !event.Consent {
		return nil
	}
	var c AdConfig
	if e := tx.First(&c, "app_id = ?", event.AppID).Error; e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil
		}
		return e
	}
	platforms := []string{}
	if c.FacebookEnabled && attr.FBC != "" {
		platforms = append(platforms, "facebook")
	}
	if c.TikTokEnabled && attr.TTClid != "" {
		platforms = append(platforms, "tiktok")
	}
	for _, p := range platforms {
		name, pixel := c.OpenFacebookEvent, c.FacebookPixel
		if p == "facebook" {
			if event.Event == "register" {
				name = c.RegisterFacebookEvent
			}
			if event.Event == "download_complete" || event.Event == "download_start" {
				name = c.DownloadFacebookEvent
			}
		} else {
			pixel = c.TikTokPixel
			name = c.OpenTikTokEvent
			if event.Event == "register" {
				name = c.RegisterTikTokEvent
			}
			if event.Event == "download_complete" || event.Event == "download_start" {
				name = c.DownloadTikTokEvent
			}
		}
		d := Delivery{ID: randomHex(16), ConversionID: event.ID, Platform: p, EventName: name, PixelID: pixel, Status: "pending", NextAttemptAt: s.Now(), CreatedAt: s.Now()}
		if e := tx.Create(&d).Error; e != nil {
			return e
		}
		event.Deliveries = append(event.Deliveries, d)
	}
	return nil
}

type SDKEvent struct {
	AttributionToken string `json:"attribution_token"`
	Event            string `json:"event"`
	EventID          string `json:"event_id"`
	EventTime        int64  `json:"event_time"`
	Consent          bool   `json:"consent"`
	PageURL          string `json:"page_url"`
	ExternalID       string `json:"external_id"`
	ClientIP         string `json:"client_ip"`
	UserAgent        string `json:"user_agent"`
}

func sdkSignature(secret, timestamp string, body []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	_, _ = io.WriteString(h, timestamp+"\n")
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
func (s *Server) sdkEvent(w http.ResponseWriter, r *http.Request) {
	appID := r.PathValue("app")
	var app Application
	if e := s.DB.First(&app, "id = ? AND blocked = ? AND enabled = ?", appID, false, true).Error; e != nil {
		http.NotFound(w, r)
		return
	}
	var owner User
	if e := s.DB.First(&owner, "id = ? AND disabled = ?", app.UserID, false).Error; e != nil {
		http.NotFound(w, r)
		return
	}
	var cfg AdConfig
	if e := s.DB.First(&cfg, "app_id = ?", appID).Error; e != nil {
		http.NotFound(w, r)
		return
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		if origin != cfg.AllowedOrigin || origin == "null" {
			fail(w, problem(403, "网站来源未获授权"))
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-D1-Time, X-D1-Signature")
		w.Header().Set("Access-Control-Max-Age", "600")
	}
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if e := s.rate("sdk:"+appID+":"+s.clientIP(r), 6000, time.Minute); e != nil {
		fail(w, e)
		return
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 16384))
	if e != nil {
		fail(w, problem(400, "事件过大"))
		return
	}
	var in SDKEvent
	if e = json.Unmarshal(body, &in); e != nil || !adIdentifier.MatchString(in.EventID) || len(in.EventID) < 8 || (in.Event != "app_open" && in.Event != "register") || len(in.ExternalID) > 256 {
		fail(w, problem(400, "支持 app_open / register，请提供至少 8 位稳定 event_id"))
		return
	}
	attr, e := s.readAttribution(in.AttributionToken, appID)
	if e != nil {
		fail(w, problem(400, "归因标识无效或过期"))
		return
	}
	verified := "client_token"
	ip := s.clientIP(r)
	ua := cleanText(r.UserAgent(), 2048)
	if signature := r.Header.Get("X-D1-Signature"); signature != "" {
		ts := r.Header.Get("X-D1-Time")
		stamp, e := strconv.ParseInt(ts, 10, 64)
		secret, de := s.decryptSecret(cfg.APISecret)
		if e != nil || de != nil || stamp < s.Now().Add(-5*time.Minute).Unix() || stamp > s.Now().Add(time.Minute).Unix() || subtle.ConstantTimeCompare([]byte(signature), []byte(sdkSignature(secret, ts, body))) != 1 {
			fail(w, problem(401, "API 签名错误或已过期"))
			return
		}
		verified = "server_signature"
		if in.ClientIP != "" {
			if net.ParseIP(in.ClientIP) == nil {
				fail(w, problem(400, "client_ip 无效"))
				return
			}
			ip = in.ClientIP
		}
		if in.UserAgent != "" {
			ua = cleanText(in.UserAgent, 2048)
		}
	} else if !cfg.AllowClientEvents {
		fail(w, problem(401, "此应用仅接受客户服务器签名事件"))
		return
	}
	var downloaded int64
	if e = s.DB.Model(&Download{}).Where("attribution_id = ? AND app_id = ?", attr.ID, appID).Count(&downloaded).Error; e != nil {
		fail(w, e)
		return
	}
	if downloaded == 0 {
		fail(w, problem(409, "尚未确认本次点击的完整下载，请稍后重试"))
		return
	}
	eventTime := s.Now()
	if in.EventTime != 0 {
		eventTime = time.Unix(in.EventTime, 0).UTC()
	}
	if eventTime.Before(attr.CreatedAt.Add(-time.Minute)) || eventTime.Before(s.Now().Add(-7*24*time.Hour)) || eventTime.After(s.Now().Add(time.Minute)) {
		fail(w, problem(400, "事件时间不在归因窗口内"))
		return
	}
	pageURL := safeSource(in.PageURL)
	if pageURL != "" {
		u, _ := url.Parse(pageURL)
		if u.Scheme+"://"+u.Host != cfg.AllowedOrigin {
			fail(w, problem(400, "page_url 与配置的网站来源不一致"))
			return
		}
	} else {
		pageURL = attr.PageURL
	}
	event := Conversion{ID: randomHex(16), AppID: app.ID, UserID: app.UserID, AttributionID: attr.ID, Event: in.Event, EventID: hashString(appID + ":" + in.EventID), IP: ip, UserAgent: ua, SourceURL: pageURL, Consent: in.Consent, VerifiedBy: verified, EventTime: eventTime, CreatedAt: s.Now()}
	if in.ExternalID != "" {
		event.ExternalIDHash = hashString(strings.TrimSpace(in.ExternalID))
	}
	e = s.DB.Transaction(func(tx *gorm.DB) error { return s.createConversion(tx, &event, attr) })
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 202, map[string]any{"event_id": event.EventID, "id": event.ID, "queued_platforms": len(event.Deliveries), "message": "事件已接收，广告平台回传结果请查看日志"})
}
func (s *Server) listConversions(w http.ResponseWriter, r *http.Request) {
	if _, e := s.ownedApp(r); e != nil {
		fail(w, e)
		return
	}
	listOut[Conversion](w, r, s.DB.Model(&Conversion{}).Preload("Deliveries").Where("app_id = ?", r.PathValue("id")))
}
func (s *Server) retryDelivery(w http.ResponseWriter, r *http.Request) {
	var d Delivery
	e := s.DB.Joins("JOIN conversions ON conversions.id = deliveries.conversion_id").Where("deliveries.id = ? AND conversions.user_id = ?", r.PathValue("delivery"), userOf(r).ID).Select("deliveries.*").First(&d).Error
	if e != nil {
		fail(w, e)
		return
	}
	if d.Status != "failed" {
		fail(w, problem(400, "仅失败任务可重试"))
		return
	}
	res := s.DB.Model(&Delivery{}).Where("id = ? AND status = ?", d.ID, "failed").Updates(map[string]any{"status": "pending", "attempts": 0, "next_attempt_at": s.Now(), "last_error": ""})
	if res.Error != nil {
		fail(w, res.Error)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
