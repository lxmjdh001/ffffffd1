package platform

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Server struct {
	LandingScripts *LandingScriptRelease
	Media          *R2MediaStore
	PageImages     *R2PageImageStore
	Blobs          BlobStore
	DB             *gorm.DB
	Config         Config
	HTTP           *http.Client
	DNS            *net.Resolver
	Now            func() time.Time
	uploadSlots    chan struct{}
	// Push delivery uses a separate restricted client; tests may replace its
	// transport without changing payment, DNS, or advertisement HTTP traffic.
	pwaPushHTTP *http.Client
	// Production captures with Chromium; tests can inject deterministic results.
	landingCoverCapture func(context.Context, LandingCoverRenderInput) ([]byte, error)
}

func NewServer(db *gorm.DB, c Config) *Server {
	return &Server{Blobs: LocalStore{Root: filepath.Join(c.DataDir, "files")}, DB: db, Config: c, HTTP: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, DNS: net.DefaultResolver, Now: func() time.Time { return time.Now().UTC() }, uploadSlots: make(chan struct{}, 4)}
}

type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string              { return e.Message }
func problem(status int, message string) error { return &apiError{status, message} }
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) {
	var e *apiError
	if errors.As(err, &e) {
		jsonOut(w, e.Status, map[string]string{"error": e.Message})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		jsonOut(w, 404, map[string]string{"error": "记录不存在"})
		return
	}
	slog.Error("request failed", "error", err)
	jsonOut(w, 500, map[string]string{"error": "服务暂时不可用，请稍后重试"})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return problem(400, "请求格式不正确")
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		return problem(400, "请求格式不正确")
	}
	return nil
}
func randomHex(n int) string {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func hashString(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func (s *Server) mac(v string) string {
	h := hmac.New(sha256.New, []byte(s.Config.Secret))
	_, _ = h.Write([]byte(v))
	return hex.EncodeToString(h.Sum(nil))
}

type contextKey string

const userKey contextKey = "user"
const sessionKey contextKey = "session"
const domainOwnerKey contextKey = "domain-owner"
const downloadAudienceKey contextKey = "download-audience"

func userOf(r *http.Request) User { return r.Context().Value(userKey).(User) }
func (s *Server) clientIP(r *http.Request) string {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	trusted := func(ip net.IP) bool {
		for _, n := range s.Config.TrustedProxies {
			if n.Contains(ip) {
				return true
			}
		}
		return false
	}
	if trusted(ip) {
		chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		for i := len(chain) - 1; i >= 0; i-- {
			p := net.ParseIP(strings.TrimSpace(chain[i]))
			if p == nil {
				return host
			}
			host = p.String()
			if !trusted(p) {
				break
			}
		}
	}
	return host
}

// Shared database counters work across replicas. The key contains a fixed window.
func (s *Server) rate(key string, limit int64, window time.Duration) error {
	now := s.Now()
	slot := now.UnixNano() / window.Nanoseconds()
	k := s.mac(fmt.Sprintf("rate:%s:%d", key, slot))
	b := RateBucket{Key: k, Count: 1, ExpiresAt: now.Add(2 * window)}
	if e := s.DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.Assignments(map[string]any{"count": gorm.Expr("rate_buckets.count + 1")})}).Create(&b).Error; e != nil {
		return e
	}
	if e := s.DB.First(&b, "key = ?", k).Error; e != nil {
		return e
	}
	if b.Count > limit {
		return problem(429, "操作过于频繁，请稍后重试")
	}
	return nil
}
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.Handle("GET /api/visit-statistics/{kind}/{id}", s.auth(s.visitStatistics))
	s.registerPWARoutes(m)
	s.registerDIYRuntimeRoutes(m)
	s.registerDIYLandingRoutes(m)
	s.registerLandingLibraryRoutes(m)
	s.registerReturnFlowRoutes(m)
	s.registerPWAPushRoutes(m)
	m.HandleFunc("GET /robots.txt", s.robots)
	m.HandleFunc("GET /sitemap.xml", s.sitemap)
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		db, e := s.DB.DB()
		if e != nil {
			fail(w, e)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if e = db.PingContext(ctx); e != nil {
			fail(w, e)
			return
		}
		jsonOut(w, 200, map[string]string{"status": "ok"})
	})
	m.HandleFunc("GET /internal/tls/ask", s.tlsAsk)
	m.HandleFunc("PUT /internal/storage/{key}", s.internalStorage)
	m.HandleFunc("DELETE /internal/storage/{key}", s.internalStorage)
	m.HandleFunc("GET /api/config", s.publicConfig)
	m.HandleFunc("GET /api/auth/captcha", s.captcha)
	m.HandleFunc("POST /api/auth/register", s.register)
	m.HandleFunc("POST /api/auth/login", s.login)
	m.Handle("POST /api/auth/logout", s.auth(s.logout))
	m.Handle("GET /api/me", s.auth(s.me))
	m.Handle("PATCH /api/me", s.auth(s.updateProfile))
	m.Handle("POST /api/me/password", s.auth(s.changePassword))
	m.Handle("GET /api/dashboard", s.auth(s.dashboard))
	m.Handle("GET /api/apps", s.auth(s.listApps))
	m.Handle("POST /api/apps", s.auth(s.uploadApp))
	m.Handle("GET /api/apps/{id}", s.auth(s.getApp))
	m.Handle("GET /api/apps/{id}/ads", s.auth(s.getAds))
	m.Handle("PUT /api/apps/{id}/ads", s.auth(s.saveAds))
	m.Handle("POST /api/apps/{id}/ads/rotate-key", s.auth(s.rotateAPISecret))
	m.Handle("GET /api/apps/{id}/events", s.auth(s.listConversions))
	m.Handle("POST /api/deliveries/{delivery}/retry", s.auth(s.retryDelivery))
	m.HandleFunc("POST /api/sdk/apps/{app}/events", s.sdkEvent)
	m.HandleFunc("OPTIONS /api/sdk/apps/{app}/events", s.sdkEvent)
	m.Handle("PATCH /api/apps/{id}", s.auth(s.updateApp))
	m.Handle("POST /api/apps/{id}/file", s.auth(s.uploadApp))
	m.Handle("DELETE /api/apps/{id}", s.auth(s.deleteApp))
	m.Handle("GET /api/download-domain", s.auth(s.assignedDownloadDomain))
	m.Handle("GET /api/download-domains", s.auth(s.availableDownloadDomains))
	m.Handle("PATCH /api/download-domains/note", s.auth(s.saveDownloadDomainNote))
	m.Handle("GET /api/apps/{id}/page-design", s.auth(s.getPageDesign))
	m.Handle("POST /api/apps/{id}/page-images", s.auth(s.uploadPageImage))
	m.Handle("PATCH /api/apps/{id}/download-settings", s.auth(s.saveDownloadSettings))
	m.Handle("PUT /api/apps/{id}/page-design", s.auth(s.savePageDesign))
	m.Handle("POST /api/apps/{id}/page-design/preview", s.auth(s.previewPageDesign))
	m.Handle("GET /api/domains", s.auth(s.listDomains))
	m.Handle("GET /api/traffic-pool", s.auth(s.getTrafficPool))
	m.Handle("PUT /api/traffic-pool", s.auth(s.saveTrafficPool))
	m.Handle("GET /api/traffic-pools", s.auth(s.getTrafficPool))
	m.Handle("POST /api/traffic-pools", s.auth(s.saveTrafficPool))
	m.Handle("PUT /api/traffic-pools/{slug}", s.auth(s.saveTrafficPool))
	m.Handle("DELETE /api/traffic-pools/{slug}", s.auth(s.deleteTrafficPool))
	m.HandleFunc("GET /r/{slug}", s.routeTraffic)
	m.Handle("GET /api/cloaks", s.auth(s.listCloaks))
	m.Handle("GET /api/cloak-targets", s.auth(s.cloakTargetChoices))
	m.Handle("POST /api/cloaks", s.auth(s.saveCloak))
	m.Handle("PUT /api/cloaks/{id}", s.auth(s.saveCloak))
	m.Handle("DELETE /api/cloaks/{id}", s.auth(s.deleteCloak))
	m.HandleFunc("GET /c/{slug}", s.routeCloak)
	m.Handle("POST /api/domains", s.auth(s.createDomain))
	m.Handle("PATCH /api/domains/{id}", s.auth(s.saveDomainRemark))
	m.Handle("POST /api/domains/{id}/verify", s.auth(s.verifyDomain))
	m.Handle("DELETE /api/domains/{id}", s.auth(s.deleteDomain))
	m.Handle("GET /api/downloads", s.auth(s.listDownloads))
	m.Handle("PUT /api/downloads/{id}/ua-block", s.auth(s.setDownloadUABlock))
	m.Handle("GET /api/downloads/apps", s.auth(s.listDownloadFilterApps))
	m.Handle("GET /api/ledger", s.auth(s.listLedger))
	m.Handle("GET /api/referrals", s.auth(s.referrals))
	m.Handle("GET /api/referral-withdrawals", s.auth(s.listReferralWithdrawals))
	m.Handle("POST /api/referral-withdrawals", s.auth(s.requestReferralWithdrawal))
	m.Handle("GET /api/orders", s.auth(s.listOrders))
	m.Handle("POST /api/orders", s.auth(s.createOrder))
	m.Handle("GET /api/orders/{id}", s.auth(s.getOrder))
	m.Handle("POST /api/orders/{id}/check", s.auth(s.checkOrder))
	m.Handle("POST /api/subscription", s.auth(s.buySubscription))
	m.HandleFunc("GET /api/payments/futoon/notify", s.futoonNotify)
	m.HandleFunc("POST /api/payments/futoon/notify", s.futoonNotify)
	m.HandleFunc("GET /api/payments/futoon/return", s.futoonReturn)
	m.Handle("GET /api/admin/users", s.admin(s.adminUsers))
	m.Handle("PUT /api/admin/users/{id}/referral-rates", s.admin(s.saveUserReferralRates))
	m.Handle("DELETE /api/admin/users/{id}/referral-rates", s.admin(s.clearUserReferralRates))
	m.Handle("GET /api/admin/commission-settings", s.admin(s.getCommissionSettings))
	m.Handle("PUT /api/admin/commission-settings", s.admin(s.saveCommissionSettings))
	m.Handle("PATCH /api/admin/users/{id}", s.admin(s.adminUpdateUser))
	m.Handle("POST /api/admin/users/{id}/password", s.admin(s.adminResetPassword))
	m.Handle("POST /api/admin/users/{id}/balance", s.admin(s.adminBalance))
	m.Handle("GET /api/admin/apps", s.admin(s.adminApps))
	m.Handle("PATCH /api/admin/apps/{id}", s.admin(s.adminModerate))
	m.Handle("GET /api/admin/orders", s.admin(s.adminOrders))
	m.Handle("GET /api/admin/referral-withdrawals", s.admin(s.adminReferralWithdrawals))
	m.Handle("POST /api/admin/referral-withdrawals/{id}/review", s.admin(s.reviewReferralWithdrawal))
	m.Handle("POST /api/admin/orders/{id}/settle", s.admin(s.adminSettle))
	m.Handle("GET /api/admin/audit", s.admin(s.adminAudit))
	m.Handle("GET /api/admin/settings", s.admin(s.adminSettings))
	m.Handle("PUT /api/admin/settings", s.admin(s.saveSettings))
	m.Handle("PUT /api/admin/usdt-wallet", s.admin(s.updateUSDTWallet))
	m.HandleFunc("GET /a/{slug}", s.directDownload)
	m.HandleFunc("GET /a/{slug}/direct", s.directAppDownload)
	m.HandleFunc("GET /a/{slug}/info", s.downloadPage)
	m.HandleFunc("POST /a/{slug}/external/click", func(w http.ResponseWriter, r *http.Request) { s.openExternalBrowser(w, r, "apk") })
	m.HandleFunc("POST /p/{slug}/external/click", func(w http.ResponseWriter, r *http.Request) { s.openExternalBrowser(w, r, "pwa") })
	m.HandleFunc("GET /a/{slug}/external/open", func(w http.ResponseWriter, r *http.Request) { s.openExternalBrowser(w, r, "apk") })
	m.HandleFunc("GET /p/{slug}/external/open", func(w http.ResponseWriter, r *http.Request) { s.openExternalBrowser(w, r, "pwa") })
	m.HandleFunc("POST /a/{slug}/return-flow/view", func(w http.ResponseWriter, r *http.Request) { s.returnFlowView(w, r, "apk") })
	m.HandleFunc("POST /p/{slug}/return-flow/view", func(w http.ResponseWriter, r *http.Request) { s.returnFlowView(w, r, "pwa") })
	m.HandleFunc("GET /a/{slug}/client.js", s.downloadClient)
	m.HandleFunc("GET /a/{slug}/landing-config.js", s.apkLandingConfig)
	m.HandleFunc("GET /a/{slug}/api/promotion-links/by-domain", s.apkLandingCustomConfig)
	m.HandleFunc("POST /a/{slug}/download", s.startDownload)
	m.HandleFunc("GET /d/{token}", s.serveDownload)
	m.HandleFunc("/", s.frontend)
	return s.middleware(m)
}
func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		policy := "default-src 'self'; script-src 'self'; frame-src 'none'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; media-src 'self' https:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"
		if strings.HasPrefix(r.URL.Path, "/a/") || strings.HasPrefix(r.URL.Path, "/p/") || strings.HasPrefix(r.URL.Path, "/c/") {
			policy = s.landingScriptPolicy(policy)
		}
		w.Header().Set("Content-Security-Policy", policy)
		w.Header().Set("Cache-Control", "no-store")
		defer func() {
			if p := recover(); p != nil {
				slog.Error("panic", "error", p)
				jsonOut(w, 500, map[string]string{"error": "服务器内部错误"})
			}
		}()
		if strings.HasPrefix(r.URL.Path, "/internal/") {
			host, _, _ := net.SplitHostPort(r.RemoteAddr)
			ip := net.ParseIP(host)
			allowed := ip != nil && ip.IsLoopback()
			for _, n := range s.Config.TrustedProxies {
				if n.Contains(ip) {
					allowed = true
				}
			}
			if !allowed {
				http.NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if s.Config.Role == "download" && !strings.HasPrefix(r.URL.Path, "/a/") && !strings.HasPrefix(r.URL.Path, "/d/") && !strings.HasPrefix(r.URL.Path, "/p/") && !strings.HasPrefix(r.URL.Path, "/pwa-template-assets/") && !strings.HasPrefix(r.URL.Path, "/c/") {
			http.NotFound(w, r)
			return
		}
		if s.Config.Role == "control" && (strings.HasPrefix(r.URL.Path, "/a/") || strings.HasPrefix(r.URL.Path, "/d/")) {
			http.NotFound(w, r)
			return
		}
		host := requestHost(r)
		if s.Config.Production() && host == s.Config.Host() && (strings.HasPrefix(r.URL.Path, "/a/") || strings.HasPrefix(r.URL.Path, "/d/")) {
			http.NotFound(w, r)
			return
		}
		if host != s.Config.Host() {
			if !s.Config.Production() && (host == "localhost" || host == "127.0.0.1") {
			} else {
				if !strings.HasPrefix(r.URL.Path, "/a/") && !strings.HasPrefix(r.URL.Path, "/d/") && !strings.HasPrefix(r.URL.Path, "/r/") && !strings.HasPrefix(r.URL.Path, "/c/") && !strings.HasPrefix(r.URL.Path, "/p/") && !strings.HasPrefix(r.URL.Path, "/pwa-template-assets/") {
					http.NotFound(w, r)
					return
				}
				x, e := getSettings(s.DB)
				if e != nil {
					fail(w, e)
					return
				}
				if audience, managed := x.managedDownloadHosts(true)[host]; managed {
					r = r.WithContext(context.WithValue(r.Context(), downloadAudienceKey, audience))
				} else {
					var d Domain
					if e := s.DB.Where("hostname = ? AND verified_at > ?", host, s.Now().Add(-48*time.Hour)).First(&d).Error; e != nil {
						http.NotFound(w, r)
						return
					}
					r = r.WithContext(context.WithValue(r.Context(), domainOwnerKey, d.UserID))
				}
			}
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.URL.Path != "/api/payments/futoon/notify" && !strings.HasPrefix(r.URL.Path, "/api/sdk/") {
			origin := r.Header.Get("Origin")
			expected := s.Config.PublicURL
			if strings.HasPrefix(r.URL.Path, "/a/") || strings.HasPrefix(r.URL.Path, "/p/") {
				expected = "https://" + r.Host
				if !s.Config.Production() {
					expected = "http://" + r.Host
				}
			}
			if origin != "" && origin != expected {
				fail(w, problem(403, "请求来源不受信任"))
				return
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, problem(403, "请求来源不受信任"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func requestHost(r *http.Request) string {
	h := r.Host
	if host, _, e := net.SplitHostPort(h); e == nil {
		h = host
	}
	return strings.ToLower(strings.TrimSuffix(h, "."))
}
func (s *Server) frontend(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		// The console may embed only the authenticated, same-origin template preview.
		policy := strings.Replace(w.Header().Get("Content-Security-Policy"), "frame-src 'none'", "frame-src "+strings.TrimRight(s.Config.PublicURL, "/")+"/api/pwa-landing/preview/ "+strings.TrimRight(s.Config.PublicURL, "/")+"/api/diy-landing-preview/", 1)
		w.Header().Set("Content-Security-Policy", policy)
	}

	if r.Method != "GET" && r.Method != "HEAD" {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/internal/") {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/index.html" && !strings.HasPrefix(r.URL.Path, "/assets/") && r.URL.Path != "/favicon.svg" && r.URL.Path != "/sdk/d1apk.js" {
		http.NotFound(w, r)
		return
	}
	p := filepath.Join(s.Config.WebDir, filepath.Clean("/"+r.URL.Path))
	if r.URL.Path == "/" {
		p = filepath.Join(s.Config.WebDir, "index.html")
	}
	if _, e := os.Stat(p); e != nil {
		http.Error(w, "前端尚未构建，请运行 npm --prefix web run build", 503)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFile(w, r, p)
}
func (s *Server) publicConfig(w http.ResponseWriter, r *http.Request) {
	// Country hints are visitor-specific, never share them through a CDN cache.
	w.Header().Set("Cache-Control", "private, no-store")
	x, e := getSettings(s.DB)
	if e != nil {
		fail(w, e)
		return
	}
	var plans []Plan
	if e = s.DB.Order("price_micros").Find(&plans).Error; e != nil {
		fail(w, e)
		return
	}
	channelConfig := s.Config
	channelConfig.USDTWallet = s.Config.receivingWallet(x)
	methods := channelConfig.paymentMethods()
	downloadBase := ""
	if len(x.DownloadDomainRules) > 0 {
		if normalized, err := s.normalizeDownloadURL(x.assignedDownloadURL("free")); err == nil {
			downloadBase = normalized
		}
	} else if x.DownloadDomainMode != "separate" {
		if x.DownloadDomainMode == "public_paid" {
			if normalized, err := s.normalizeDownloadURL(x.FreeDownloadBaseURL); err == nil {
				downloadBase = normalized
			}
		}
		if normalized, err := s.normalizeDownloadURL(x.DownloadBaseURL); err == nil {
			if downloadBase == "" {
				downloadBase = normalized
			}
		}
	}
	if x.MeteredBilling {
		plans = publicMeteredPlans(x)
	}
	jsonOut(w, 200, map[string]any{"country_code": s.cloakRequestCountry(r), "currency": "USD", "site_name": x.SiteName, "registration_open": x.RegistrationOpen, "turnstile_site_key": "", "development": !s.Config.Production(), "public_url": s.Config.PublicURL, "download_domain_mode": x.DownloadDomainMode, "download_base_url": downloadBase, "cname_target": s.Config.CNAMEHost, "download_price_micros": x.DownloadPriceMicros, "max_upload_bytes": x.MaxUploadBytes, "domain_limit": x.DomainLimit, "plans": plans, "payment_methods": methods, "support_email": x.SupportEmail, "usdt_rate_micros": x.USDTRateMicros, "alipay_cny_rate_micros": x.AlipayCNYRateMicros, "recharge_bonus_enabled": x.RechargeBonusEnabled, "recharge_bonus_tiers": x.RechargeBonusTiers,
		"metered_billing": x.MeteredBilling, "pwa_install_price_micros": x.PWAInstallPriceMicros, "cloak_uv_price_micros": 0, "return_flow_price_micros": 0, "pwa_push_price_micros": 0, "registration_gift_limit": 0, "registration_gift_micros": x.RegistrationGiftMicros, "app_limit": x.AppLimit, "storage_limit_bytes": x.StorageLimitBytes})
}
func pageParams(r *http.Request) (int, int) {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if p < 1 {
		p = 1
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if n < 1 {
		n = 20
	}
	if n > 100 {
		n = 100
	}
	if p > 100000 {
		p = 100000
	}
	return (p - 1) * n, n
}
func listOut[T any](w http.ResponseWriter, r *http.Request, q *gorm.DB) {
	var n int64
	if e := q.Count(&n).Error; e != nil {
		fail(w, e)
		return
	}
	offset, limit := pageParams(r)
	items := []T{}
	if e := q.Order("created_at DESC").Offset(offset).Limit(limit).Find(&items).Error; e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]any{"data": items, "total": n})
}
func addQuery(q *gorm.DB, r *http.Request, columns ...string) *gorm.DB {
	term := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(term) > 100 {
		term = term[:100]
	}
	if term == "" {
		return q
	}
	parts := []string{}
	args := []any{}
	for _, col := range columns {
		parts = append(parts, "LOWER("+col+") LIKE ?")
		args = append(args, "%"+strings.ToLower(term)+"%")
	}
	return q.Where(strings.Join(parts, " OR "), args...)
}
func validHTTPS(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}
