package platform

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"golang.org/x/net/idna"
	"gorm.io/gorm"
)

// PWAApp deliberately keeps credentials and binary icons out of JSON responses.
// Each app gets a distinct VAPID identity and a narrowly scoped service worker.
type PWAApp struct {
	LandingJSON       string         `json:"-" gorm:"type:text;not null;default:'{}'"`
	ReturnFlowJSON    string         `json:"-" gorm:"type:text;not null;default:'{}'"`
	ID                string         `json:"id" gorm:"primaryKey;size:32"`
	Slug              string         `json:"slug" gorm:"uniqueIndex;size:16;not null"`
	UserID            string         `json:"-" gorm:"index;size:32;not null"`
	Name              string         `json:"name" gorm:"size:100;not null"`
	TargetURL         string         `json:"target_url" gorm:"size:2048;not null"`
	Description       string         `json:"description" gorm:"size:2000"`
	Enabled           bool           `json:"enabled" gorm:"not null"`
	PushEnabled       bool           `json:"push_enabled" gorm:"not null"`
	ThemeColor        string         `json:"theme_color" gorm:"size:7;not null"`
	VAPIDPublicKey    string         `json:"-"`
	VAPIDPrivateKey   string         `json:"-"`
	IconPNG           []byte         `json:"-"`
	IconObjectKey     string         `json:"-" gorm:"size:250;not null;default:''"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	CreateKey         *string        `json:"-" gorm:"uniqueIndex;size:64"`
	CreateFingerprint string         `json:"-" gorm:"size:64"`
	DeletedAt         gorm.DeletedAt `json:"-" gorm:"index"`
}

type pwaAppView struct {
	LandingConfig     PWALandingConfig `json:"landing_config"`
	ReturnFlowConfig  ReturnFlowConfig `json:"return_flow_config"`
	ID                string           `json:"id"`
	Slug              string           `json:"slug"`
	Name              string           `json:"name"`
	TargetURL         string           `json:"target_url"`
	Description       string           `json:"description"`
	Enabled           bool             `json:"enabled"`
	PushEnabled       bool             `json:"push_enabled"`
	ThemeColor        string           `json:"theme_color"`
	IconURL           string           `json:"icon_url"`
	EntryURL          string           `json:"entry_url"`
	LaunchURL         string           `json:"launch_url"`
	SubscriptionCount int64            `json:"subscription_count"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

var pwaSlugPattern = regexp.MustCompile(`^[a-f0-9]{16}$`)
var pwaColorPattern = regexp.MustCompile(`^#[a-fA-F0-9]{6}$`)

func (s *Server) registerPWARoutes(m *http.ServeMux) {
	m.Handle("GET /api/pwas", s.auth(s.listPWAs))
	s.registerPWALandingRoutes(m)
	m.Handle("POST /api/pwas", s.auth(s.createPWA))
	m.Handle("GET /api/pwas/{id}", s.auth(s.getPWA))
	m.Handle("PUT /api/pwas/{id}", s.auth(s.updatePWA))
	m.Handle("DELETE /api/pwas/{id}", s.auth(s.deletePWA))
	m.Handle("POST /api/pwas/{id}/icon", s.auth(s.uploadPWAIcon))
	m.Handle("GET /api/pwas/{id}/icon", s.auth(s.ownedPWAIcon))
	m.HandleFunc("GET /p/{slug}/{$}", s.pwaInstallPage)
	m.HandleFunc("GET /p/{slug}/launch", s.pwaLaunchPage)
	m.HandleFunc("GET /p/{slug}/manifest.webmanifest", s.pwaManifest)
	m.HandleFunc("GET /p/{slug}/sw.js", s.pwaServiceWorker)
	m.HandleFunc("GET /p/{slug}/client.js", s.pwaClientScript)
	m.HandleFunc("GET /p/{slug}/style.css", s.pwaStyles)
	m.HandleFunc("GET /p/{slug}/icon/{size}", s.pwaIcon)
	m.HandleFunc("GET /p/{slug}/push-config", s.pwaPushConfig)
	m.HandleFunc("POST /p/{slug}/install/{action}", s.pwaInstallUsage)
}

func (s *Server) pwaView(a PWAApp, count int64) pwaAppView {
	return s.pwaViewWithLanding(a, count, s.pwaLandingWithReference(a))
}

func (s *Server) pwaListView(a PWAApp, count int64) pwaAppView {
	landing := storedPWALanding(a.LandingJSON)
	if landing.Template == "diy" {
		landing.DIYLanding, _ = diyBindingRevisionReference(s.DB, "pwa", a.ID)
	}
	return s.pwaViewWithLanding(a, count, landing)
}

func (s *Server) pwaViewWithLanding(a PWAApp, count int64, landing PWALandingConfig) pwaAppView {
	base := strings.TrimRight(s.Config.PublicURL, "/") + "/p/" + a.Slug + "/"
	return pwaAppView{LandingConfig: landing, ReturnFlowConfig: storedReturnFlow(a.ReturnFlowJSON), ID: a.ID, Slug: a.Slug, Name: a.Name, TargetURL: a.TargetURL, Description: a.Description, Enabled: a.Enabled, PushEnabled: a.PushEnabled, ThemeColor: a.ThemeColor, IconURL: s.Config.PublicURL + "/api/pwas/" + a.ID + "/icon", EntryURL: base, LaunchURL: base + "launch", SubscriptionCount: count, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}
func (s *Server) ownedPWA(r *http.Request) (PWAApp, error) {
	var a PWAApp
	e := s.DB.First(&a, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Error
	return a, e
}
func (s *Server) listPWAs(w http.ResponseWriter, r *http.Request) {
	var apps []PWAApp
	if err := s.DB.Omit("icon_png", "vapid_private_key", "vapid_public_key").Where("user_id = ?", userOf(r).ID).Order("created_at DESC, id DESC").Limit(1000).Find(&apps).Error; err != nil {
		fail(w, err)
		return
	}
	ids := make([]string, 0, len(apps))
	for _, a := range apps {
		ids = append(ids, a.ID)
	}
	counts, err := pwaActiveSubscriptionCounts(s.DB, ids)
	if err != nil {
		fail(w, err)
		return
	}
	views := make([]pwaAppView, 0, len(apps))
	for _, a := range apps {
		views = append(views, s.pwaListView(a, counts[a.ID]))
	}
	jsonOut(w, 200, map[string]any{"data": views})
}
func (s *Server) getPWA(w http.ResponseWriter, r *http.Request) {
	a, err := s.ownedPWA(r)
	if err != nil {
		fail(w, err)
		return
	}
	counts, err := pwaActiveSubscriptionCounts(s.DB, []string{a.ID})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, s.pwaView(a, counts[a.ID]))
}

type pwaAppInput struct {
	ImportFrom       string            `json:"import_from"`
	LandingConfig    *PWALandingConfig `json:"landing_config"`
	ReturnFlowConfig *ReturnFlowConfig `json:"return_flow_config"`
	Name             *string           `json:"name"`
	TargetURL        *string           `json:"target_url"`
	Description      *string           `json:"description"`
	Enabled          *bool             `json:"enabled"`
	PushEnabled      *bool             `json:"push_enabled"`
	ThemeColor       *string           `json:"theme_color"`
}

// No target is fetched by the server. Reject private destinations anyway, and
// keep the customer page on a different origin from the privileged PWA shell.
func (s *Server) normalizePWATarget(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Host == "" || u.User != nil || u.Opaque != "" || strings.ContainsAny(raw, "\x00\r\n") {
		return "", problem(400, "请输入有效的 HTTPS 网页 URL")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil || ascii == "" {
		return "", problem(400, "网页域名无效")
	}
	host = ascii
	ip := net.ParseIP(host)
	loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && !s.Config.Production() && loopback) {
		return "", problem(400, "网页 URL 必须使用 HTTPS")
	}
	if !(!s.Config.Production() && loopback) {
		if ip != nil {
			if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				return "", problem(400, "网页 URL 必须为公开地址")
			}
		} else if !strings.Contains(host, ".") || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".home.arpa") || pwaNumericHost(host) {
			return "", problem(400, "网页 URL 必须为公开地址")
		}
	}
	if port := u.Port(); port != "" && port != "443" && !(loopback && !s.Config.Production()) {
		return "", problem(400, "公开网页 URL 请使用标准 HTTPS 端口")
	}
	origin, _ := url.Parse(s.Config.PublicURL)
	if origin != nil && strings.EqualFold(host, strings.TrimSuffix(origin.Hostname(), ".")) {
		return "", problem(400, "请填写独立网站地址，不能嵌入当前平台")
	}
	if ip != nil && strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	if port := urlPort(raw); port != "" {
		u.Host = net.JoinHostPort(host, port)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	return u.String(), nil
}
func pwaNumericHost(host string) bool {
	// Browsers also accept shortened, octal, and hexadecimal IPv4 spellings.
	// A numeric final label is never a public DNS suffix for app targets.
	last := host[strings.LastIndex(host, ".")+1:]
	if strings.HasPrefix(last, "0x") {
		return true
	}
	for _, r := range last {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func urlPort(raw string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return ""
	}
	return u.Port()
}
func (s *Server) applyPWAInput(a *PWAApp, in pwaAppInput, _ bool) error {
	if in.LandingConfig != nil {
		if err := s.normalizePWALanding(in.LandingConfig); err != nil {
			return err
		}
		stored := *in.LandingConfig
		stored.DIYLanding = nil
		raw, _ := json.Marshal(stored)
		a.LandingJSON = string(raw)
	}
	if in.ReturnFlowConfig != nil {
		if err := normalizeReturnFlow(in.ReturnFlowConfig, s.Config.PublicURL); err != nil {
			return err
		}
		a.ReturnFlowJSON = returnFlowJSON(in.ReturnFlowConfig)
	}
	if in.Name != nil {
		a.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		a.Description = strings.TrimSpace(*in.Description)
	}
	if len([]rune(a.Name)) < 1 || len([]rune(a.Name)) > 100 || strings.ContainsAny(a.Name, "\x00\r\n") {
		return problem(400, "应用名称须为 1–100 字")
	}
	if len([]rune(a.Description)) > 500 || strings.ContainsRune(a.Description, '\x00') {
		return problem(400, "应用简介最多 500 字")
	}
	if in.TargetURL != nil {
		target, err := s.normalizePWATarget(*in.TargetURL)
		if err != nil {
			return err
		}
		a.TargetURL = target
	}
	if a.TargetURL == "" {
		return problem(400, "请填写网页 URL")
	}
	if in.Enabled != nil {
		a.Enabled = *in.Enabled
	}
	if in.PushEnabled != nil {
		a.PushEnabled = *in.PushEnabled
	}
	if in.ThemeColor != nil {
		a.ThemeColor = strings.ToLower(strings.TrimSpace(*in.ThemeColor))
	}
	if !pwaColorPattern.MatchString(a.ThemeColor) {
		return problem(400, "主题色须为 #RRGGBB 格式")
	}
	return nil
}
func (s *Server) createPWA(w http.ResponseWriter, r *http.Request) {
	var in pwaAppInput
	if err := decodeDIYApplication(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	a := PWAApp{ID: randomHex(16), Slug: randomHex(8), UserID: userOf(r).ID, Enabled: true, PushEnabled: true, ThemeColor: "#6373e8", CreatedAt: s.Now(), UpdatedAt: s.Now()}
	if err := s.applyPWAInput(&a, in, true); err != nil {
		fail(w, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key != "" {
		if len(key) < 8 || len(key) > 128 || strings.ContainsAny(key, "\r\n\x00") {
			fail(w, problem(400, "请求标识无效"))
			return
		}
		hashed := s.mac("pwa-create:" + a.UserID + ":" + key)
		a.CreateKey = &hashed
	}
	canonical, _ := json.Marshal(struct {
		Name, TargetURL, Description, ThemeColor string
		Enabled, PushEnabled                     bool
		ReturnFlowConfig                         *ReturnFlowConfig `json:",omitempty"`
		LandingConfig                            *PWALandingConfig `json:",omitempty"`
		ImportFrom                               string            `json:",omitempty"`
	}{a.Name, a.TargetURL, a.Description, a.ThemeColor, a.Enabled, a.PushEnabled, in.ReturnFlowConfig, in.LandingConfig, in.ImportFrom})
	a.CreateFingerprint = hashString(string(canonical))
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		fail(w, err)
		return
	}
	a.VAPIDPublicKey = public
	a.VAPIDPrivateKey, err = s.encryptSecret(private)
	if err != nil {
		fail(w, err)
		return
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		owner, e := lockUser(tx, a.UserID)
		if e != nil {
			return e
		}
		if owner.Disabled {
			return problem(403, "账户已停用")
		}
		if a.CreateKey != nil {
			var existing PWAApp
			e = tx.Unscoped().First(&existing, "create_key = ?", *a.CreateKey).Error
			if e == nil {
				if existing.DeletedAt.Valid || existing.CreateFingerprint != a.CreateFingerprint {
					return problem(409, "请求标识已用于其他应用配置")
				}
				a = existing
				return nil
			}
			if e != gorm.ErrRecordNotFound {
				return e
			}
		}
		if in.ImportFrom != "" {
			var source PWAApp
			if e = tx.First(&source, "id = ? AND user_id = ?", in.ImportFrom, owner.ID).Error; e != nil {
				return e
			}
			a.IconPNG = append([]byte(nil), source.IconPNG...)
			a.IconObjectKey = source.IconObjectKey
			if in.LandingConfig == nil {
				config := storedPWALanding(source.LandingJSON)
				if config.Template == "diy" {
					config.DIYLanding, e = diyBindingReference(tx, "pwa", source.ID)
					if e != nil {
						return e
					}
				}
				if config.Template != "" {
					in.LandingConfig = &config
					if e = s.applyPWAInput(&a, pwaAppInput{LandingConfig: &config}, false); e != nil {
						return e
					}
				}
			}
		}
		plan, e := getPlan(tx, owner, s.Now())
		if e != nil {
			return e
		}
		limit := plan.AppLimit
		if limit > 1000 {
			limit = 1000
		}
		var count int64
		if e = tx.Model(&PWAApp{}).Where("user_id = ?", owner.ID).Count(&count).Error; e != nil {
			return e
		}
		if count >= limit {
			return problem(403, "PWA 应用数量已达套餐上限，请升级会员或删除应用")
		}
		if e = s.validatePWALandingAssets(tx, owner.ID, in.LandingConfig); e != nil {
			return e
		}
		if e = s.validateReturnFlowAsset(tx, owner.ID, in.ReturnFlowConfig); e != nil {
			return e
		}
		if e = tx.Create(&a).Error; e != nil {
			return e
		}
		if e = s.savePWADIYBinding(tx, a, in.LandingConfig); e != nil {
			return e
		}
		return recordAudit(tx, owner.ID, "pwa.create", a.ID, "")
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 201, s.pwaView(a, 0))
}
func (s *Server) updatePWA(w http.ResponseWriter, r *http.Request) {
	var in pwaAppInput
	if err := decodeDIYApplication(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	var a PWAApp
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		owner, e := lockUser(tx, userOf(r).ID)
		if e != nil {
			return e
		}
		if owner.Disabled {
			return problem(403, "账户已停用")
		}
		if e = tx.First(&a, "id = ? AND user_id = ?", r.PathValue("id"), owner.ID).Error; e != nil {
			return e
		}
		if e = s.applyPWAInput(&a, in, false); e != nil {
			return e
		}
		if e = s.validatePWALandingAssets(tx, owner.ID, in.LandingConfig); e != nil {
			return e
		}
		if e = s.validateReturnFlowAsset(tx, owner.ID, in.ReturnFlowConfig); e != nil {
			return e
		}
		a.UpdatedAt = s.Now()
		if e = tx.Save(&a).Error; e != nil {
			return e
		}
		if e = s.savePWADIYBinding(tx, a, in.LandingConfig); e != nil {
			return e
		}
		return recordAudit(tx, owner.ID, "pwa.update", a.ID, "")
	})
	if err != nil {
		fail(w, err)
		return
	}
	counts, err := pwaActiveSubscriptionCounts(s.DB, []string{a.ID})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, s.pwaView(a, counts[a.ID]))
}
func (s *Server) deletePWA(w http.ResponseWriter, r *http.Request) {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		owner, e := lockUser(tx, userOf(r).ID)
		if e != nil {
			return e
		}
		if owner.Disabled {
			return problem(403, "账户已停用")
		}
		var a PWAApp
		if e = tx.First(&a, "id = ? AND user_id = ?", r.PathValue("id"), owner.ID).Error; e != nil {
			return e
		}
		if e = setDIYBinding(tx, owner.ID, "pwa", a.ID, nil); e != nil {
			return e
		}
		if e = tx.Delete(&a).Error; e != nil {
			return e
		}
		return recordAudit(tx, owner.ID, "pwa.delete", a.ID, "")
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

const pwaMaxIconBytes = 2 << 20

func (s *Server) uploadPWAIcon(w http.ResponseWriter, r *http.Request) {
	a, err := s.ownedPWA(r)
	if err != nil {
		fail(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, pwaMaxIconBytes+(64<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		fail(w, problem(400, "请使用 multipart 上传 PNG 或 JPEG 图标"))
		return
	}
	var raw []byte
	for n := 0; ; n++ {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil || n > 2 {
			fail(w, problem(400, "图标上传格式无效"))
			return
		}
		if part.FormName() != "file" || part.FileName() == "" || raw != nil {
			part.Close()
			fail(w, problem(400, "每次仅上传一个图标文件"))
			return
		}
		raw, e = io.ReadAll(io.LimitReader(part, pwaMaxIconBytes+1))
		part.Close()
		if e != nil || len(raw) > pwaMaxIconBytes {
			fail(w, problem(413, "图标不能超过 2 MiB"))
			return
		}
	}
	normalized, err := normalizePWAIcon(raw)
	if err != nil {
		fail(w, err)
		return
	}
	objectKey := ""
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		owner, e := lockUser(tx, userOf(r).ID)
		if e != nil {
			return e
		}
		if owner.Disabled {
			return problem(403, "账户已停用")
		}
		if e := tx.First(&a, "id = ? AND user_id = ?", a.ID, userOf(r).ID).Error; e != nil {
			return e
		}
		updates := map[string]any{"icon_png": normalized, "icon_object_key": "", "updated_at": s.Now()}
		if s.Media != nil {
			objectKey, e = s.Media.putPNG(r.Context(), tx, "pwa-icons", normalized)
			if e != nil {
				return e
			}
			updates["icon_png"], updates["icon_object_key"] = []byte{}, objectKey
		}
		if e := tx.Model(&a).Updates(updates).Error; e != nil {
			return e
		}
		return recordAudit(tx, userOf(r).ID, "pwa.icon", a.ID, "")
	})
	if err != nil {
		s.Media.discard(objectKey)
		fail(w, err)
		return
	}
	counts, err := pwaActiveSubscriptionCounts(s.DB, []string{a.ID})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, s.pwaView(a, counts[a.ID]))
}
func normalizePWAIcon(raw []byte) ([]byte, error) {
	invalid := problem(400, "请选择有效的 PNG 或 JPEG 图标，尺寸不超过 4096 × 4096")
	if len(raw) == 0 {
		return nil, invalid
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "png" && format != "jpeg") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 16_000_000 {
		return nil, invalid
	}
	im, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, invalid
	}
	out := resizePWAIcon(im, 512)
	var buf bytes.Buffer
	if err = png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
func resizePWAIcon(src image.Image, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	bounds := src.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()
	dw, dh := size, size
	if sw > sh {
		dh = sh * size / sw
	} else {
		dw = sw * size / sh
	}
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	ox, oy := (size-dw)/2, (size-dh)/2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dst.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
		}
	}
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			r, g, b, a := src.At(bounds.Min.X+x*sw/dw, bounds.Min.Y+y*sh/dh).RGBA()
			alpha := uint32(a)
			dst.SetNRGBA(x+ox, y+oy, color.NRGBA{uint8((r + (65535 - alpha)) >> 8), uint8((g + (65535 - alpha)) >> 8), uint8((b + (65535 - alpha)) >> 8), 255})
		}
	}
	return dst
}

// Every public endpoint shares the same tenant/owner and domain-audience checks.
func (s *Server) loadPublicPWA(r *http.Request) (*PWAApp, error) {
	host := requestHost(r)
	if host != s.Config.Host() && !(!s.Config.Production() && (host == "localhost" || host == "127.0.0.1")) {
		_, custom := r.Context().Value(domainOwnerKey).(string)
		_, managed := r.Context().Value(downloadAudienceKey).(string)
		if !custom && !managed {
			return nil, gorm.ErrRecordNotFound
		}
	}
	slug := r.PathValue("slug")
	if !pwaSlugPattern.MatchString(slug) {
		return nil, gorm.ErrRecordNotFound
	}
	var app PWAApp
	err := s.DB.Omit("icon_png").Joins("JOIN users ON users.id = pwa_apps.user_id AND users.disabled = ?", false).Where("pwa_apps.slug = ? AND pwa_apps.enabled = ?", slug, true).First(&app).Error
	if err != nil {
		return nil, err
	}
	if owner, ok := r.Context().Value(domainOwnerKey).(string); ok && owner != app.UserID {
		return nil, gorm.ErrRecordNotFound
	}
	if audience, managed := r.Context().Value(downloadAudienceKey).(string); managed && audience != "all" {
		var owner User
		if s.DB.First(&owner, "id = ?", app.UserID).Error != nil || owner.PlanID(s.Now()) != audience {
			return nil, gorm.ErrRecordNotFound
		}
	}
	if _, err = s.normalizePWATarget(app.TargetURL); err != nil {
		return nil, gorm.ErrRecordNotFound
	}
	return &app, nil
}
