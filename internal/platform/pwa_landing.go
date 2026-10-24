package platform

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"html/template"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

// These field names follow the source PWA creation forms and runtime config.
type PWALandingReview struct {
	Nickname        string `json:"nickname"`
	Comment         string `json:"comment"`
	CommentLanguage string `json:"commentLanguage"`
	Rating          int    `json:"rating"`
	SortOrder       int    `json:"sortOrder"`
}
type PWALandingLanguage struct {
	Language          string             `json:"language"`
	AppName           string             `json:"appName"`
	DeveloperName     string             `json:"developerName"`
	AppIcon           string             `json:"appIcon"`
	DownloadCount     string             `json:"downloadCount"`
	AgeRating         string             `json:"ageRating"`
	ReviewCount       string             `json:"reviewCount"`
	AppSummary        string             `json:"appSummary"`
	StayPromptText    string             `json:"stayPromptText"`
	DetailImages      []string           `json:"detailImages"`
	Tags              []string           `json:"tags"`
	Reviews           []PWALandingReview `json:"reviews"`
	CustomImages      []string           `json:"customImages"`
	ShowInstallButton bool               `json:"showInstallButton"`
	ButtonText        string             `json:"buttonText"`
	ButtonPosition    string             `json:"buttonPosition"`
	ButtonBgColor     string             `json:"buttonBgColor"`
	ButtonTextColor   string             `json:"buttonTextColor"`
	ShowQRCode        bool               `json:"showQrCode"`
}
type PWABrowserGuide struct {
	Enabled bool   `json:"enabled"`
	Message string `json:"message"`
}
type PWALandingSettings struct {
	APKURL             string          `json:"apkUrl"`
	PackagePriority    string          `json:"packagePriority"`
	EnableQRCode       bool            `json:"enableQrcode"`
	IOSCompatible      bool            `json:"iosCompatible"`
	W2AAutoDownload    bool            `json:"w2aAutoDownload"`
	EnableIframeEmbed  bool            `json:"enableIframeEmbed"`
	BrowserGuideConfig PWABrowserGuide `json:"browserGuideConfig"`
	EnableComplaint    bool            `json:"enableComplaint"`
	ComplaintShowOn    []string        `json:"complaintShowOn"`
}
type PWALandingConfig struct {
	Template             string               `json:"template"`
	AppRemark            string               `json:"appRemark"`
	StartupBgColor       string               `json:"startupBgColor"`
	StartupThemeColor    string               `json:"startupThemeColor"`
	DisplayMode          string               `json:"displayMode"`
	PWAOrientation       string               `json:"pwaOrientation"`
	BottomMenu           string               `json:"bottomMenu"`
	EnableGoogleBadge    bool                 `json:"enableGoogleBadge"`
	EnableOfficialVerify bool                 `json:"enableOfficialVerify"`
	IOSRedirectURL       string               `json:"iosRedirectUrl"`
	LanguagePriority     []string             `json:"languagePriority"`
	StoreInfos           []PWALandingLanguage `json:"storeInfos"`
	Settings             PWALandingSettings   `json:"settings"`
}

func storedPWALanding(raw string) PWALandingConfig {
	var c PWALandingConfig
	_ = json.Unmarshal([]byte(raw), &c)
	return c
}
func (s *Server) normalizePWALanding(c *PWALandingConfig) error {
	if c.Template != "legacy" && c.Template != "google_play" && c.Template != "custom" {
		return problem(400, "PWA 落地页配置无效")
	}
	if len(c.StoreInfos) > 32 || len(c.LanguagePriority) > 32 || (c.Template != "legacy" && len(c.StoreInfos) == 0) {
		return problem(400, "PWA 落地页配置无效")
	}
	for _, v := range []*string{&c.StartupBgColor, &c.StartupThemeColor} {
		if *v == "" {
			*v = "#ffffff"
		}
		if !pwaColorPattern.MatchString(*v) {
			return problem(400, "PWA 落地页配置无效")
		}
	}
	if c.DisplayMode == "" {
		c.DisplayMode = "standalone"
	}
	if c.PWAOrientation == "" {
		c.PWAOrientation = "any"
	}
	if c.BottomMenu == "" {
		c.BottomMenu = "apps"
	}
	if !pwaLandingContains([]string{"standalone", "fullscreen", "minimal-ui", "browser"}, c.DisplayMode) || !pwaLandingContains([]string{"any", "portrait", "landscape"}, c.PWAOrientation) || !pwaLandingContains([]string{"apps", "games", "films", "books", "children"}, c.BottomMenu) {
		return problem(400, "PWA 落地页配置无效")
	}
	if c.Settings.PackagePriority == "" {
		c.Settings.PackagePriority = "pwa"
	}
	if !pwaLandingContains([]string{"pwa", "w2a_only"}, c.Settings.PackagePriority) {
		return problem(400, "PWA 落地页配置无效")
	}
	if len([]rune(c.Settings.BrowserGuideConfig.Message)) > 300 || (c.Settings.BrowserGuideConfig.Enabled && strings.TrimSpace(c.Settings.BrowserGuideConfig.Message) == "") {
		return problem(400, "PWA 落地页配置无效")
	}
	for _, u := range []string{c.Settings.APKURL, c.IOSRedirectURL} {
		if u != "" && !validReturnFlowURL(u, s.Config.PublicURL) {
			return problem(400, "PWA 落地页素材或链接无效")
		}
	}
	if c.Settings.PackagePriority == "w2a_only" && c.Settings.APKURL == "" {
		return problem(400, "请填写 APK 下载地址")
	}
	seen := map[string]bool{}
	for i := range c.StoreInfos {
		v := &c.StoreInfos[i]
		if !pwaLandingLanguagePattern.MatchString(v.Language) || seen[strings.ToLower(v.Language)] || strings.TrimSpace(v.AppName) == "" || len([]rune(v.AppName)) > 100 || len(v.DetailImages) > 5 || len(v.CustomImages) > 5 || len(v.Reviews) > 100 || len(v.Tags) > 30 || len([]rune(v.StayPromptText)) > 500 {
			return problem(400, "PWA 落地页配置无效")
		}
		seen[strings.ToLower(v.Language)] = true
		urls := append(append([]string{v.AppIcon}, v.DetailImages...), v.CustomImages...)
		for _, u := range urls {
			if u != "" && !validReturnFlowURL(u, s.Config.PublicURL) {
				return problem(400, "PWA 落地页素材或链接无效")
			}
		}
		if v.ButtonPosition == "" {
			v.ButtonPosition = "bottom"
		}
		if !pwaLandingContains([]string{"top", "bottom", "center", "middle"}, v.ButtonPosition) {
			return problem(400, "PWA 落地页配置无效")
		}
		for _, color := range []*string{&v.ButtonBgColor, &v.ButtonTextColor} {
			if *color == "" {
				*color = "#1976D2"
				if color == &v.ButtonTextColor {
					*color = "#FFFFFF"
				}
			}
			if !pwaColorPattern.MatchString(*color) {
				return problem(400, "PWA 落地页配置无效")
			}
		}
		for _, review := range v.Reviews {
			if review.Rating < 1 || review.Rating > 5 || len([]rune(review.Nickname)) > 50 || len(review.Comment) > 20000 {
				return problem(400, "PWA 落地页配置无效")
			}
		}
	}
	for _, l := range c.LanguagePriority {
		if !seen[strings.ToLower(l)] {
			return problem(400, "PWA 落地页配置无效")
		}
	}
	return nil
}
func (s *Server) validatePWALandingAssets(tx *gorm.DB, owner string, c *PWALandingConfig) error {
	if c == nil {
		return nil
	}
	for _, v := range c.StoreInfos {
		for _, u := range append(append([]string{v.AppIcon}, v.DetailImages...), v.CustomImages...) {
			if err := s.validateReturnFlowAsset(tx, owner, &ReturnFlowConfig{MediaURL: u}); err != nil {
				return err
			}
		}
	}
	return nil
}
func pwaLandingLanguage(c PWALandingConfig, requested string) PWALandingLanguage {
	// Exact match, language prefix, configured priority, then first configured locale.
	requested = strings.ToLower(strings.Split(strings.Split(requested, ",")[0], ";")[0])
	for _, v := range c.StoreInfos {
		if strings.EqualFold(v.Language, requested) {
			return v
		}
	}
	for _, v := range c.StoreInfos {
		if strings.EqualFold(strings.Split(v.Language, "-")[0], strings.Split(requested, "-")[0]) {
			return v
		}
	}
	for _, l := range c.LanguagePriority {
		for _, v := range c.StoreInfos {
			if strings.EqualFold(l, v.Language) {
				return v
			}
		}
	}
	if len(c.StoreInfos) > 0 {
		return c.StoreInfos[0]
	}
	return PWALandingLanguage{}
}
func (s *Server) pwaLandingRuntime(a *PWAApp, r *http.Request) map[string]any {
	c := storedPWALanding(a.LandingJSON)
	requested := r.URL.Query().Get("language")
	if requested == "" {
		requested = r.Header.Get("Accept-Language")
	}
	v := pwaLandingLanguage(c, requested)
	base := s.Config.PublicURL + "/p/" + a.Slug + "/"
	if v.AppIcon == "" {
		v.AppIcon = base + "icon/512.png"
	}
	for _, p := range []*[]string{&v.DetailImages, &v.CustomImages} {
		if *p == nil {
			*p = []string{}
		}
	}
	settingsJSON, _ := json.Marshal(c.Settings)
	settings := map[string]any{}
	_ = json.Unmarshal(settingsJSON, &settings)
	settings["returnFlowConfig"] = storedReturnFlow(a.ReturnFlowJSON)
	settings["enableApkUpload"] = c.Settings.APKURL != ""
	if c.Settings.APKURL != "" {
		settings["apkUrl"] = returnFlowButtonURL(c.Settings.APKURL, s.Config.PublicURL)
	}
	out := map[string]any{"apiBaseUrl": strings.TrimSuffix(base, "/"), "s3PublicDomain": s.Config.PublicURL, "productH5Url": a.TargetURL, "iosRedirectUrl": c.IOSRedirectURL}
	common := map[string]any{"id": a.ID, "appName": a.Name, "appIcon": base + "icon/512.png", "startupBgColor": c.StartupBgColor, "startupThemeColor": c.StartupThemeColor, "displayMode": c.DisplayMode, "pwaOrientation": c.PWAOrientation, "bottomMenu": c.BottomMenu, "enableGoogleBadge": c.EnableGoogleBadge, "enableOfficialVerify": c.EnableOfficialVerify, "settings": settings, "storeInfo": v}
	out["app"] = common
	custom := map[string]any{}
	for k, value := range common {
		custom[k] = value
	}
	custom["setting"] = v
	custom["otherSettings"] = settings
	out["customApp"] = custom
	return out
}

//go:embed templates/pwa-landing/* templates/pwa-landing/.vite/manifest.json
var pwaLandingFiles embed.FS
var pwaLandingLanguagePattern = regexp.MustCompile(`^[a-zA-Z]{2,3}(?:-[a-zA-Z0-9]{2,8})?$`)

func (s *Server) registerPWALandingRoutes(m *http.ServeMux) {
	m.Handle("GET /api/pwa-landing/preview-adapter.js", s.auth(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Write([]byte(apkLandingBridgeJS))
	}))
	m.Handle("GET /api/pwa-landing/preview/{layout}", s.auth(func(w http.ResponseWriter, r *http.Request) {
		layout := r.PathValue("layout")
		if layout != "google_play" && layout != "custom" {
			http.NotFound(w, r)
			return
		}
		raw, _ := json.Marshal(PWALandingConfig{Template: layout})
		a := PWAApp{Name: "PWA preview", Slug: "preview", LandingJSON: string(raw)}
		var buf bytes.Buffer
		if err := s.renderPWALanding(&a, r, &buf); err != nil {
			http.Error(w, "Preview unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' https: data:; media-src 'self' https:; frame-ancestors 'self'; object-src 'none'; base-uri 'none'")
		w.Write(buf.Bytes())
	}))

	m.HandleFunc("GET /pwa-template-assets/{file...}", func(w http.ResponseWriter, r *http.Request) {
		file := r.PathValue("file")
		if !fs.ValidPath(file) || strings.HasPrefix(file, ".") {
			http.NotFound(w, r)
			return
		}
		data, err := pwaLandingFiles.ReadFile("templates/pwa-landing/" + file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(file)))
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeContent(w, r, file, time.Time{}, bytes.NewReader(data))
	})
	m.HandleFunc("GET /p/{slug}/landing-config.js", func(w http.ResponseWriter, r *http.Request) {
		a, e := s.loadPublicPWA(r)
		if e != nil {
			pwaPublicFailure(w, r, e)
			return
		}
		raw, _ := json.Marshal(s.pwaLandingRuntime(a, r))
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, "window.__APP_CONFIG__ = %s;", raw)
	})
	m.HandleFunc("GET /p/{slug}/api/promotion-links/by-domain", func(w http.ResponseWriter, r *http.Request) {
		a, e := s.loadPublicPWA(r)
		if e != nil {
			pwaPublicFailure(w, r, e)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		jsonOut(w, 200, map[string]any{"code": 0, "data": s.pwaLandingRuntime(a, r)})
	})
}
func (s *Server) renderPWALanding(a *PWAApp, r *http.Request, buf *bytes.Buffer) error {
	c := storedPWALanding(a.LandingJSON)
	manifest, _ := pwaLandingFiles.ReadFile("templates/pwa-landing/.vite/manifest.json")
	var entries map[string]struct {
		File string   `json:"file"`
		CSS  []string `json:"css"`
	}
	if err := json.Unmarshal(manifest, &entries); err != nil {
		return err
	}
	entry := entries["google-play/src/main.ts"]
	if c.Template == "custom" {
		entry = entries["custom-app/src/main.ts"]
	}
	if entry.File == "" {
		return fmt.Errorf("landing asset missing")
	}
	type view struct {
		App                          *PWAApp
		Base, JS                     string
		MetaName, Icon               string
		ReturnFlowToken              string
		APKStyle, BaseStyle          template.CSS
		CSS                          []string
		Chinese, Preview, APKPreview bool
	}
	localized := pwaLandingLanguage(c, r.Header.Get("Accept-Language"))
	name, icon := localized.AppName, localized.AppIcon
	if name == "" {
		name = a.Name
	}
	if icon == "" {
		icon = "/p/" + a.Slug + "/icon/192.png"
	}
	v := view{ReturnFlowToken: s.returnFlowViewToken("pwa", a.ID, r), App: a, Base: "/p/" + a.Slug + "/", JS: entry.File, CSS: entry.CSS, Chinese: pwaWantsChinese(r), Preview: strings.HasPrefix(r.URL.Path, "/api/pwa-landing/preview/"), APKPreview: strings.HasPrefix(r.URL.Path, "/api/pwa-landing/preview/") && r.URL.Query().Get("apk") == "1", MetaName: name, Icon: icon, APKStyle: template.CSS(apkLandingCSS), BaseStyle: template.CSS(pwaLandingBaseCSS)}
	return pwaLandingHTML.Execute(buf, v)
}

var pwaLandingHTML = template.Must(template.New("pwa-landing").Parse(`<!doctype html><html lang="{{if .Chinese}}zh-CN{{else}}en{{end}}"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover"><meta name="theme-color" content="{{.App.ThemeColor}}"><title>{{.MetaName}}</title><meta name="apple-mobile-web-app-capable" content="yes"><meta name="apple-mobile-web-app-title" content="{{.MetaName}}"><meta name="description" content="{{.App.Description}}"><link rel="manifest" href="{{.Base}}manifest.webmanifest"><link rel="icon" href="{{.Base}}icon/192.png"><link rel="apple-touch-icon" href="{{.Icon}}">{{range .CSS}}<link rel="stylesheet" href="/pwa-template-assets/{{.}}">{{end}}{{if .Preview}}<style id="pwa-return-flow-style">{{.BaseStyle}}</style>{{else}}<link id="pwa-return-flow-style" rel="stylesheet" href="{{.Base}}style.css">{{end}}{{if not .Preview}}<script src="{{.Base}}landing-config.js" defer></script><script src="{{.Base}}client.js" defer></script>{{end}}{{if .APKPreview}}<style>{{.APKStyle}}</style><script defer src="/api/pwa-landing/preview-adapter.js"></script>{{end}}<script type="module" src="/pwa-template-assets/{{.JS}}"></script></head><body class="pwa-reference-page {{if .APKPreview}}d1-apk-reference d1-apk-preview{{end}}" data-base="{{.Base}}" data-locale="{{if .Chinese}}zh{{else}}en{{end}}" {{if not .Preview}}data-return-flow="{{.App.ReturnFlowJSON}}" data-return-flow-token="{{.ReturnFlowToken}}" data-return-flow-endpoint="{{.Base}}return-flow/view"{{end}} data-return-flow-app="{{.App.ID}}" data-push-enabled="{{.App.PushEnabled}}"><div id="app"></div><aside id="d1-pwa-status" hidden><p id="install-status" role="status"></p><section id="install-guide" hidden><p id="install-guide-text"></p><button type="button" id="install-guide-close">Close</button></section></aside><noscript>Enable JavaScript to install this app.</noscript></body></html>`))

func pwaLandingContains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

const pwaLandingBaseCSS = `*{box-sizing:border-box}html,body{margin:0;width:100%;min-height:100%}body{overflow-x:hidden}#app{width:100%;min-height:100vh}[hidden]{display:none!important}#d1-pwa-status:not([hidden]){position:fixed;inset:0;z-index:100003;display:flex;align-items:center;justify-content:center;background:#0009;padding:24px}#d1-pwa-status>p{display:none}#d1-pwa-status section{padding:28px;max-width:440px;background:#fff;color:#202124;border-radius:20px;font:16px/1.7 Arial}#install-guide-close{display:block;border:0;background:#01875f;color:#fff;padding:10px 24px;border-radius:20px;margin:24px auto 0}`

//go:embed templates/pwa-browser-guide.js
var pwaBrowserGuideJS string

//go:embed templates/pwa-browser-guide.css
var pwaBrowserGuideCSS string
