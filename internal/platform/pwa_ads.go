package platform

import (
	"bytes"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
)

// A non-hex namespace prevents APK and PWA identifiers from sharing credentials,
// attribution tokens, event identities, or logs, even when their IDs are equal.
func pwaAdID(id string) string { return "pwa:" + id }
func pwaAdApplication(a PWAApp) Application {
	return Application{ID: pwaAdID(a.ID), UserID: a.UserID, Slug: a.Slug, Name: a.Name, Enabled: a.Enabled}
}
func isPWAAdvertising(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/pwas/") || strings.HasPrefix(r.URL.Path, "/api/sdk/pwas/")
}
func (s *Server) ownedAdvertisingApp(r *http.Request) (Application, error) {
	if isPWAAdvertising(r) {
		a, e := s.ownedPWA(r)
		return pwaAdApplication(a), e
	}
	return s.ownedApp(r)
}
func (s *Server) advertisingSDKApp(r *http.Request) (Application, error) {
	if isPWAAdvertising(r) {
		var a PWAApp
		e := s.DB.First(&a, "id = ? AND enabled = ?", r.PathValue("app"), true).Error
		return pwaAdApplication(a), e
	}
	var a Application
	e := s.DB.First(&a, "id = ? AND blocked = ? AND enabled = ?", r.PathValue("app"), false, true).Error
	return a, e
}
func migratePWAAds(tx *gorm.DB) error {
	var n int64
	if e := tx.Model(&Migration{}).Where("version = ?", 32).Count(&n).Error; e != nil || n > 0 {
		return e
	}
	// Only widen the resource identifiers; existing APK rows and credentials stay intact.
	if e := tx.AutoMigrate(&AdConfig{}, &Attribution{}, &Conversion{}); e != nil {
		return e
	}
	// GORM intentionally skips resizing primary keys during AutoMigrate.
	// Existing PostgreSQL installs still have a 32-character config key.
	if tx.Dialector.Name() == "postgres" {
		if err := tx.Exec("ALTER TABLE ad_configs ALTER COLUMN app_id TYPE varchar(40)").Error; err != nil {
			return err
		}
	}
	return tx.Create(&Migration{Version: 32, AppliedAt: time.Now().UTC()}).Error
}
func (s *Server) registerPWAAds(m *http.ServeMux) {
	m.Handle("GET /api/pwas/{id}/ads", s.auth(s.getAds))
	m.Handle("PUT /api/pwas/{id}/ads", s.auth(s.saveAds))
	m.Handle("POST /api/pwas/{id}/ads/rotate-key", s.auth(s.rotateAPISecret))
	m.Handle("GET /api/pwas/{id}/events", s.auth(s.listConversions))
	m.HandleFunc("POST /api/sdk/pwas/{app}/events", s.sdkEvent)
	m.HandleFunc("OPTIONS /api/sdk/pwas/{app}/events", s.sdkEvent)
	m.HandleFunc("POST /p/{slug}/ad-event", s.pwaAdEvent)
	m.HandleFunc("GET /p/{slug}/ads.css", func(w http.ResponseWriter, r *http.Request) { s.pwaAsset(w, r, pwaAdsCSS, "text/css; charset=utf-8") })
}

type pwaAdWriter struct {
	http.ResponseWriter
	markup, style []byte
}

func (w *pwaAdWriter) Write(raw []byte) (int, error) {
	original := len(raw)
	raw = bytes.Replace(raw, []byte("</head>"), append(w.style, []byte("</head>")...), 1)
	raw = bytes.Replace(raw, []byte("</body>"), append(w.markup, []byte("</body>")...), 1)
	_, e := w.ResponseWriter.Write(raw)
	if e != nil {
		return 0, e
	}
	return original, nil
}

var pwaAdMarkup = template.Must(template.New("pwa-ads").Parse(`<section id="d1-pwa-ads" data-mode="{{.Mode}}" {{if .Quick}}hidden{{end}} data-token="{{.Token}}" data-base="{{.Base}}" data-proof="{{.Proof}}" data-origin="{{.Origin}}" data-app="{{.App}}" data-sdk="{{.SDK}}" data-launch="{{.Launch}}" data-consented="{{.Consent}}"><label><input type="checkbox" id="d1-pwa-ad-consent" {{if .Consent}}checked{{end}}>{{if .Chinese}}同意广告归因{{else}}Allow ad attribution{{end}}</label></section>`))

func (s *Server) pwaAdvertisingWriter(w http.ResponseWriter, r *http.Request, a PWAApp, launch bool) http.ResponseWriter {
	if r.Method != http.MethodGet {
		return w
	}
	var cfg AdConfig
	if e := s.DB.First(&cfg, "app_id = ? AND user_id = ?", pwaAdID(a.ID), a.UserID).Error; e != nil || cfg.Disabled || (!cfg.FacebookEnabled && !cfg.TikTokEnabled) {
		return w
	}
	attr, token, e := s.prepareAttribution(w, r, pwaAdApplication(a), s.visitor(w, r))
	if e != nil {
		slog.Warn("PWA advertising context unavailable")
		return w
	}
	base := "/p/" + a.Slug + "/"
	var markup bytes.Buffer
	e = pwaAdMarkup.Execute(&markup, map[string]any{"Mode": adMode(cfg), "Quick": adMode(cfg) == "quick", "Token": token, "Base": base, "Proof": s.returnFlowViewToken("pwa", a.ID, r), "Origin": cfg.AllowedOrigin, "App": a.ID, "SDK": s.Config.PublicURL + "/api/sdk/pwas/" + a.ID + "/events", "Launch": launch, "Consent": attr.Consent, "Chinese": pwaWantsChinese(r)})
	if e != nil {
		return w
	}
	style := []byte(`<link rel="stylesheet" href="` + base + `ads.css">`)
	return &pwaAdWriter{w, markup.Bytes(), style}
}
func (s *Server) pwaAdEvent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !sameUsageOrigin(s, r) {
		fail(w, problem(403, "请求来源不受信任"))
		return
	}
	app, e := s.loadPublicPWA(r)
	if e != nil {
		pwaPublicFailure(w, r, e)
		return
	}
	if s.rejectBlockedUA(w, r, app.UserID, r.UserAgent()) {
		return
	}
	var in struct {
		Token   string `json:"attribution_token"`
		Proof   string `json:"proof"`
		Consent bool   `json:"consent"`
		Event   string `json:"event"`
	}
	if e = decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if !s.validReturnFlowViewToken("pwa", app.ID, in.Proof, r) || (in.Event != "pwa_install" && in.Event != "app_open" && in.Event != "consent" && in.Event != "install_click") {
		fail(w, problem(400, "事件凭证无效"))
		return
	}
	attr, e := s.readAttribution(in.Token, pwaAdID(app.ID))
	cookie, ce := r.Cookie("d1_visitor")
	if e != nil || ce != nil || attr.VisitorHash != s.mac(cookie.Value) {
		fail(w, problem(403, "归因标识无效或过期"))
		return
	}
	if e = s.rate("pwa-ads:"+app.ID+":"+s.clientIP(r), 120, time.Minute); e != nil {
		fail(w, e)
		return
	}
	var config AdConfig
	if e = s.DB.First(&config, "app_id = ? AND user_id = ?", attr.AppID, app.UserID).Error; e != nil {
		http.NotFound(w, r)
		return
	}
	if config.Disabled {
		jsonOut(w, 202, map[string]any{"queued_platforms": 0})
		return
	}
	if adMode(config) == "quick" {
		if in.Event != "install_click" {
			fail(w, problem(400, "快速模式仅回传下载或安装点击事件"))
			return
		}
		event := Conversion{ID: randomHex(16), AppID: attr.AppID, UserID: app.UserID, AttributionID: attr.ID, Event: "pwa_click", EventID: hashString(attr.AppID + ":" + attr.ID + ":pwa_click"), IP: s.clientIP(r), UserAgent: cleanText(r.UserAgent(), 2048), SourceURL: attr.PageURL, Consent: attr.Consent, VerifiedBy: "quick_pwa_click", EventTime: s.Now(), CreatedAt: s.Now()}
		if e = s.DB.Transaction(func(tx *gorm.DB) error { return s.createConversion(tx, &event, attr) }); e != nil {
			fail(w, e)
			return
		}
		jsonOut(w, 202, map[string]any{"event_id": event.EventID, "queued_platforms": len(event.Deliveries)})
		return
	}
	if in.Event == "install_click" {
		fail(w, problem(400, "深度模式需要安装确认"))
		return
	}
	if in.Event == "consent" {
		if e = s.DB.Model(&attr).Update("consent", in.Consent).Error; e != nil {
			fail(w, e)
			return
		}
		jsonOut(w, 202, map[string]any{"queued_platforms": 0})
		return
	}
	if !in.Consent {
		jsonOut(w, 202, map[string]any{"queued_platforms": 0})
		return
	}
	event := Conversion{ID: randomHex(16), AppID: attr.AppID, UserID: app.UserID, AttributionID: attr.ID, Event: in.Event, EventID: hashString(attr.AppID + ":" + attr.ID + ":" + in.Event), IP: s.clientIP(r), UserAgent: cleanText(r.UserAgent(), 2048), SourceURL: attr.PageURL, Consent: true, VerifiedBy: "pwa_browser", EventTime: s.Now(), CreatedAt: s.Now()}
	e = s.DB.Transaction(func(tx *gorm.DB) error {
		if in.Event == "app_open" {
			var n int64
			if e := tx.Table("conversions c").Joins("JOIN attributions a ON a.id=c.attribution_id").Where("c.app_id = ? AND a.app_id = ? AND a.visitor_hash = ? AND c.event = ?", attr.AppID, attr.AppID, attr.VisitorHash, "pwa_install").Count(&n).Error; e != nil {
				return e
			}
			if n == 0 {
				return problem(409, "尚未确认本次点击的 PWA 安装")
			}
		}
		if e := tx.Model(&attr).Update("consent", true).Error; e != nil {
			return e
		}
		attr.Consent = true
		return s.createConversion(tx, &event, attr)
	})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 202, map[string]any{"queued_platforms": len(event.Deliveries), "event_id": event.EventID})
}
func pwaAdOrigin(a PWAApp) string {
	u, e := url.Parse(a.TargetURL)
	if e != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
func (s *Server) pwaAdvertisingSDKReady(appID, attrID string) (bool, error) {
	var attr Attribution
	if e := s.DB.First(&attr, "id = ? AND app_id = ?", attrID, appID).Error; e != nil {
		return false, e
	}
	var n int64
	e := s.DB.Table("conversions c").Joins("JOIN attributions a ON a.id=c.attribution_id").Where("c.app_id = ? AND a.app_id = ? AND a.visitor_hash = ? AND c.event = ?", appID, appID, attr.VisitorHash, "pwa_install").Count(&n).Error
	return n > 0, e
}
func (s *Server) pwaAdvertisingActive(id, owner string) bool {
	var p PWAApp
	return s.DB.First(&p, "id = ? AND user_id = ? AND enabled = ?", strings.TrimPrefix(id, "pwa:"), owner, true).Error == nil
}

const pwaAdsCSS = `#d1-pwa-ads{position:fixed;bottom:12px;left:50%;transform:translateX(-50%);max-width:calc(100vw - 24px);border-radius:10px;box-shadow:0 4px 24px #0001;z-index:10001;padding:12px 16px;background:#fff;color:#40566b;border-top:1px solid #e0e7ef;font:13px/1.6 system-ui,sans-serif;text-align:center}#d1-pwa-ads label{display:inline-flex;align-items:center;gap:9px;min-height:44px;cursor:pointer}#d1-pwa-ads input{width:18px;height:18px;accent-color:#01875f}#d1-pwa-ads input:focus-visible{outline:3px solid #93c5fd;outline-offset:3px}body:has(#d1-pwa-ads[data-consented="true"]){--d1-ads-consent:true}`
