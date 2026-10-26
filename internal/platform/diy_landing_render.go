package platform

import (
	"bytes"
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

type DIYLandingPreview struct {
	ID        string    `gorm:"primaryKey;size:32"`
	UserID    string    `gorm:"index;size:32;not null"`
	HTML      string    `gorm:"type:text;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
}

//go:embed templates/diy-parent.js
var diyParentJS string

func (s *Server) previewDIYLanding(w http.ResponseWriter, r *http.Request) {
	var in struct {
		HTML      string               `json:"html"`
		Config    DIYLandingOptions    `json:"config"`
		Mode      string               `json:"mode"`
		Reference *DIYLandingReference `json:"reference,omitempty"`
	}
	if err := decodeDIY(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Mode != "apk" && in.Mode != "pwa" {
		fail(w, problem(400, "请选择 APK 或 PWA 预览"))
		return
	}
	if err := s.rate("diy-preview:"+userOf(r).ID, 120, time.Minute); err != nil {
		fail(w, err)
		return
	}
	var prepared string
	issues := []DIYHTMLIssue{}
	var err error
	if in.Reference != nil {
		var revision *DIYLandingRevision
		revision, err = loadDIYRevision(s.DB, userOf(r).ID, in.Mode, in.Reference)
		if err == nil {
			in.Config = revision.Config
			err = validateDIYManagedImageReferences(s.DB, userOf(r).ID, in.Reference.Content)
			if err == nil {
				prepared, err = applyDIYContent(revision.RenderHTML, revision.Config, in.Reference.Content)
			}
		}
	} else {
		prepared, issues, err = prepareDIYHTML(in.HTML, in.Config)
	}
	if err != nil {
		fail(w, err)
		return
	}
	p := DIYLandingPreview{ID: randomHex(16), UserID: userOf(r).ID, ExpiresAt: s.Now().Add(10 * time.Minute)}
	base := "/api/diy-landing-preview/" + p.ID + "/asset"
	label := "安装应用"
	if in.Mode == "apk" {
		label = "下载应用"
	}
	var content DIYLandingContent
	if in.Reference != nil {
		content = in.Reference.Content
	}
	label = diyApplicationButtonLabel(prepared, in.Config, content, label)
	channel := randomHex(16)
	p.HTML, err = diyContentDocument(prepared, in.Config, channel, in.Mode, label, "应用预览", base, true, func(raw, kind string) string {
		return s.diyOwnedImageAssetURL(base, raw, kind, p.ExpiresAt, userOf(r).ID)
	})
	if err != nil {
		fail(w, err)
		return
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, p.UserID); err != nil {
			return err
		}
		if err := tx.Where("expires_at < ?", s.Now()).Delete(&DIYLandingPreview{}).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&DIYLandingPreview{}).Where("user_id = ?", p.UserID).Count(&count).Error; err != nil {
			return err
		}
		if count >= 12 {
			var old []DIYLandingPreview
			if err := tx.Select("id").Where("user_id = ?", p.UserID).Order("expires_at ASC, id ASC").Limit(int(count) - 11).Find(&old).Error; err != nil {
				return err
			}
			for _, snapshot := range old {
				if err := tx.Delete(&snapshot).Error; err != nil {
					return err
				}
			}
		}
		return tx.Create(&p).Error
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]any{"preview_url": "/api/diy-landing-preview/" + p.ID, "channel": channel, "issues": issues})
}

func diyContentCSP(assetBase string) string {
	return "default-src 'none'; script-src 'unsafe-inline' " + assetBase + "; style-src 'unsafe-inline' " + assetBase + "; img-src data: blob: " + assetBase + "; media-src " + assetBase + "; font-src data: " + assetBase + "; connect-src 'none'; frame-src 'none'; worker-src 'none'; manifest-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'; sandbox allow-scripts"
}
func (s *Server) serveDIYPreview(w http.ResponseWriter, r *http.Request) {
	var p DIYLandingPreview
	if err := s.DB.First(&p, "id = ? AND user_id = ? AND expires_at > ?", r.PathValue("preview"), userOf(r).ID, s.Now()).Error; err != nil {
		fail(w, err)
		return
	}
	base := s.pwaRequestOrigin(r) + "/api/diy-landing-preview/" + p.ID + "/asset"
	s.writeDIYContent(w, r, p.HTML, base)
}
func (s *Server) writeDIYContent(w http.ResponseWriter, r *http.Request, body, assetBase string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", diyContentCSP(assetBase))
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodHead {
		io.WriteString(w, body)
	}
}

func (s *Server) registerDIYRuntimeRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/diy-landing-preview/{preview}/asset", s.serveDIYAsset)
	for _, kind := range []string{"a", "p"} {
		m.HandleFunc("GET /"+kind+"/{slug}/diy-content", s.serveDIYContent)
		m.HandleFunc("GET /"+kind+"/{slug}/diy-asset", s.serveDIYAsset)
		m.HandleFunc("GET /"+kind+"/{slug}/diy-parent.js", s.serveDIYParent)
	}
}
func (s *Server) diyPublicResource(r *http.Request) (kind, id, owner, slug string, err error) {
	if strings.HasPrefix(r.URL.Path, "/a/") {
		var a Application
		a, err = s.publicApp(r)
		return "apk", a.ID, a.UserID, a.Slug, err
	}
	a, e := s.loadPublicPWA(r)
	if e != nil {
		return "", "", "", "", e
	}
	return "pwa", a.ID, a.UserID, a.Slug, nil
}
func (s *Server) serveDIYParent(w http.ResponseWriter, r *http.Request) {
	if _, _, _, _, err := s.diyPublicResource(r); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method != http.MethodHead {
		io.WriteString(w, diyParentJS)
	}
}
func (s *Server) diyContentSignature(kind, id, revision, host, expires, channel string) string {
	return s.mac(strings.Join([]string{"diy-content", kind, id, revision, host, expires, channel}, ":"))
}
func (s *Server) serveDIYContent(w http.ResponseWriter, r *http.Request) {
	if s.Config.DisableDIYLanding {
		http.NotFound(w, r)
		return
	}
	kind, id, owner, slug, err := s.diyPublicResource(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ref, err := diyBindingReference(s.DB, kind, id)
	if err != nil || ref == nil {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	expires, e := strconv.ParseInt(q.Get("expires"), 10, 64)
	if e != nil || expires < s.Now().Unix() || expires > s.Now().Add(2*time.Hour).Unix() || q.Get("revision") != ref.RevisionID || !hexKey.MatchString(q.Get("channel")) {
		http.NotFound(w, r)
		return
	}
	expected := s.diyContentSignature(kind, id, ref.RevisionID, requestHost(r), q.Get("expires"), q.Get("channel"))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(q.Get("signature"))) != 1 {
		http.NotFound(w, r)
		return
	}
	rev, err := loadDIYRevision(s.DB, owner, kind, ref)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	prefix := "/a/"
	if kind == "pwa" {
		prefix = "/p/"
	}
	base := prefix + slug + "/diy-asset"
	name, label := "", "下载应用"
	if kind == "pwa" {
		label = "安装应用"
		var a PWAApp
		s.DB.Select("name").First(&a, "id = ?", id)
		name = a.Name
	} else {
		var a Application
		s.DB.Select("name").First(&a, "id = ?", id)
		name = a.Name
	}
	prepared, err := applyDIYContent(rev.RenderHTML, rev.Config, ref.Content)
	if err != nil {
		http.Error(w, "Page unavailable", 503)
		return
	}
	label = diyApplicationButtonLabel(rev.RenderHTML, rev.Config, ref.Content, label)
	body, err := diyContentDocument(prepared, rev.Config, q.Get("channel"), kind, label, name, base, false, func(raw, t string) string {
		return s.diyOwnedImageAssetURL(base, raw, t, time.Unix(expires, 0), owner)
	})
	if err != nil {
		http.Error(w, "Page unavailable", 503)
		return
	}
	s.writeDIYContent(w, r, body, s.pwaRequestOrigin(r)+base)
}

type diyShellData struct {
	Name, Description, Kind, Base, Frame, Channel, Label, ExternalBrowser, ReturnFlow, ReturnFlowToken, ResourceID, Attribution, Nonce, ClientJS string
	DocumentTitle                                                                                                                                string
	HasDocumentTitle                                                                                                                             bool
	DeepLink                                                                                                                                     template.URL
	CSS                                                                                                                                          template.CSS
	PWA, Chinese, Push, PlatformButton, AdsEnabled, Fallback                                                                                     bool
}

var diyShellTemplate = template.Must(template.New("diy-shell").Parse(`<!doctype html><html lang="{{if .Chinese}}zh-CN{{else}}en{{end}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover"><title>{{if .HasDocumentTitle}}{{.DocumentTitle}}{{else}}{{.Name}}{{end}}</title><meta name="description" content="{{.Description}}">{{if .PWA}}<link rel="manifest" href="{{.Base}}manifest.webmanifest"><link rel="icon" href="{{.Base}}icon/192.png"><link rel="apple-touch-icon" href="{{.Base}}icon/192.png"><script src="{{.Base}}landing-config.js" defer></script>{{end}}<style>{{.CSS}}</style><script src="{{.ClientJS}}" defer></script><script src="{{.Base}}diy-parent.js" defer></script></head><body data-d1-diy data-kind="{{.Kind}}" data-channel="{{.Channel}}" data-base="{{.Base}}" data-locale="{{if .Chinese}}zh{{else}}en{{end}}" data-push-enabled="{{.Push}}" {{if .PWA}}data-d1-pwa-reference{{end}} data-external-browser="{{.ExternalBrowser}}" data-return-flow="{{.ReturnFlow}}" data-return-flow-app="{{.ResourceID}}" data-return-flow-token="{{.ReturnFlowToken}}" data-return-flow-endpoint="{{.Base}}return-flow/view">{{if .Fallback}}<main class="diy-fallback"><h1>{{.Name}}</h1><p>{{.Description}}</p></main>{{else}}<iframe id="d1-diy-content" title="{{.Name}}" src="{{.Frame}}" sandbox="allow-scripts" referrerpolicy="no-referrer"></iframe>{{end}}<aside id="d1-diy-actions" {{if not .PlatformButton}}hidden{{end}}><span id="d1-diy-confirm" hidden>{{if .Chinese}}请点击继续{{else}}Tap to continue{{end}}</span>{{if .PWA}}<button id="install-button" type="button">{{.Label}}</button>{{else}}<form id="d1-native-download" class="d1-download-form" action="{{.Base}}download" method="post"><input type="hidden" name="request_key" value="{{.Nonce}}"><input type="hidden" name="attribution_token" value="{{.Attribution}}"><button type="submit">{{.Label}}</button></form>{{end}}<button id="d1-diy-cancel" type="button" hidden>{{if .Chinese}}取消{{else}}Cancel{{end}}</button>{{if .AdsEnabled}}<label><input type="checkbox" form="d1-native-download" name="ad_consent" value="yes">{{if .Chinese}}同意广告归因{{else}}Allow ad attribution{{end}}</label>{{end}}{{if .DeepLink}}<a href="{{.DeepLink}}">{{if .Chinese}}打开应用{{else}}Open app{{end}}</a>{{end}}<p id="{{if .PWA}}install-status{{else}}download-status{{end}}" role="status" aria-live="polite"></p>{{if .PWA}}{{if .Push}}<button type="button" id="notification-button">{{if .Chinese}}开启通知{{else}}Enable notifications{{end}}</button><button type="button" id="unsubscribe-button" hidden>{{if .Chinese}}关闭通知{{else}}Turn off notifications{{end}}</button><p id="notification-status" role="status"></p>{{end}}{{end}}</aside>{{if .PWA}}<aside id="d1-pwa-status" hidden><section id="install-guide" hidden><p id="install-guide-text"></p><button type="button" id="install-guide-close">{{if .Chinese}}关闭{{else}}Close{{end}}</button></section></aside>{{end}}<noscript>{{if .Chinese}}请启用 JavaScript 以下载或安装应用。{{else}}Enable JavaScript to download or install this app.{{end}}</noscript></body></html>`))

const diyShellCSS = `html,body{margin:0;height:100%;overflow:hidden;font-family:-apple-system,BlinkMacSystemFont,"PingFang SC",sans-serif;background:#fff;color:#24374b}body{display:flex;flex-direction:column;height:100dvh}.diy-fallback{flex:1;display:grid;place-content:center;padding:24px;text-align:center}#d1-diy-content{border:0;width:100%;flex:1;min-height:0;background:#fff}#d1-diy-actions{flex:none;display:flex;flex-wrap:wrap;align-items:center;justify-content:center;gap:10px;padding:12px 16px max(12px,env(safe-area-inset-bottom));background:white;border-top:1px solid #dce5ed;z-index:20}#d1-diy-actions[hidden],[hidden]{display:none!important}#d1-diy-actions button{border:0;border-radius:8px;padding:12px 24px;background:#167d9a;color:white;font:inherit;cursor:pointer}#d1-diy-actions button:disabled{opacity:.55;cursor:wait}#d1-diy-actions form{margin:0}#d1-diy-actions p{margin:0;font-size:13px;flex-basis:100%;text-align:center}#d1-diy-actions #d1-diy-cancel{background:#edf2f6;color:#24374b}#d1-diy-actions label{font-size:12px}#d1-pwa-status{position:fixed;bottom:120px;left:16px;right:16px;padding:20px;background:white;box-shadow:0 6px 30px #182d4333;border-radius:12px;z-index:100}button:focus-visible,a:focus-visible{outline:3px solid #167d9a;outline-offset:3px}`

func (s *Server) renderDIYShell(w http.ResponseWriter, r *http.Request, data diyShellData, ref *DIYLandingReference, owner string) error {
	rev, err := loadDIYRevision(s.DB, owner, data.Kind, ref)
	if err == nil {
		err = validateDIYContent(rev.RenderHTML, rev.Config, ref.Content)
	}
	if err == nil {
		err = validateDIYManagedImageReferences(s.DB, owner, ref.Content)
	}
	if err != nil || s.Config.DisableDIYLanding {
		// Missing/corrupt bindings retain the original trusted installation CTA.
		data.PlatformButton = true
		data.Fallback = true
	} else {
		data.PlatformButton = rev.Config.PlatformButton
		data.DocumentTitle, data.HasDocumentTitle = diyContentDocumentTitle(rev.RenderHTML, rev.Config, ref.Content)
		data.Label = diyApplicationButtonLabel(rev.RenderHTML, rev.Config, ref.Content, data.Label)
		data.Channel = randomHex(32)
		expires := strconv.FormatInt(s.Now().Add(time.Hour).Unix(), 10)
		q := url.Values{"revision": {rev.ID}, "channel": {data.Channel}, "expires": {expires}, "signature": {s.diyContentSignature(data.Kind, data.ResourceID, rev.ID, requestHost(r), expires, data.Channel)}}
		data.Frame = data.Base + "diy-content?" + q.Encode()
	}
	data.CSS = template.CSS(diyShellCSS + "\n" + returnFlowCSS + "\n" + externalBrowserCSS + "\n" + pwaBrowserGuideCSS)
	var buf bytes.Buffer
	if err = diyShellTemplate.Execute(&buf, data); err != nil {
		return err
	}
	policy := "default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; img-src 'self' https: data:; media-src 'self' https:; connect-src 'self'; worker-src 'self'; manifest-src 'self'; frame-src " + s.pwaRequestOrigin(r) + data.Base + "diy-content; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"
	w.Header().Set("Content-Security-Policy", s.landingScriptPolicy(policy))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	if r.Method != http.MethodHead {
		_, err = w.Write(buf.Bytes())
	}
	return err
}

func (s *Server) diyAssetURL(base, raw, kind string, expires time.Time) string {
	stamp := strconv.FormatInt(expires.Unix(), 10)
	q := url.Values{"url": {raw}, "kind": {kind}, "expires": {stamp}, "signature": {s.mac("diy-asset:" + base + ":" + raw + ":" + kind + ":" + stamp)}}
	return base + "?" + q.Encode()
}

var diyNonPublicNetworks = func() []*net.IPNet {
	var networks []*net.IPNet
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16"} {
		_, network, _ := net.ParseCIDR(raw)
		networks = append(networks, network)
	}
	return networks
}()

func diyPublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, network := range diyNonPublicNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	return true
}
func (s *Server) serveDIYAsset(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	raw, kind, stamp := q.Get("url"), q.Get("kind"), q.Get("expires")
	expires, e := strconv.ParseInt(stamp, 10, 64)
	expected := s.mac("diy-asset:" + r.URL.Path + ":" + raw + ":" + kind + ":" + stamp)
	if e != nil || expires < s.Now().Unix() || expires > s.Now().Add(2*time.Hour).Unix() || subtle.ConstantTimeCompare([]byte(expected), []byte(q.Get("signature"))) != 1 {
		http.NotFound(w, r)
		return
	}
	if kind == "managed-image" {
		if err := s.rate("diy-asset:"+s.clientIP(r), 600, time.Minute); err != nil {
			fail(w, err)
			return
		}
		s.serveDIYManagedImage(w, r, raw)
		return
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(raw) > 4096 || (u.Port() != "" && u.Port() != "443") {
		http.NotFound(w, r)
		return
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if s.PageImages != nil && strings.HasPrefix(raw, s.PageImages.PublicURL+"/"+s.PageImages.Prefix) {
		http.NotFound(w, r)
		return
	}
	if host == s.Config.Host() || host == requestHost(r) {
		http.NotFound(w, r)
		return
	}
	var count int64
	s.DB.Model(&Domain{}).Where("hostname = ?", host).Count(&count)
	if count > 0 {
		http.NotFound(w, r)
		return
	}
	if settings, err := getSettings(s.DB); err == nil {
		if _, ok := settings.managedDownloadHosts(true)[host]; ok {
			http.NotFound(w, r)
			return
		}
	}
	if kind != "image" && kind != "script" && kind != "style" && kind != "font" && kind != "media" {
		http.NotFound(w, r)
		return
	}
	if err := s.rate("diy-asset:"+s.clientIP(r), 600, time.Minute); err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	// Resolve once and pin the dialled address. No private destinations, cookies,
	// redirects or ambient proxy are used, including after DNS rebinding.
	transport := &http.Transport{TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 8 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		h, p, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, h)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if !diyPublicIP(ip.IP) {
				return nil, fmt.Errorf("non-public resource")
			}
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("resource not found")
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), p))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	req.Header.Set("User-Agent", "D1-static-asset/1.0")
	res, err := client.Do(req)
	if err != nil {
		http.Error(w, "Resource unavailable", 502)
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		http.Error(w, "Resource unavailable", 502)
		return
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20+1))
	if err != nil || len(data) > 8<<20 {
		http.Error(w, "Resource too large", 413)
		return
	}
	ct := strings.ToLower(strings.Split(res.Header.Get("Content-Type"), ";")[0])
	detected := http.DetectContentType(data)
	valid := false
	switch kind {
	case "image":
		valid = strings.HasPrefix(detected, "image/")
		ct = strings.Split(detected, ";")[0]
	case "script":
		valid = (ct == "application/javascript" || ct == "text/javascript" || ct == "application/x-javascript") && !strings.Contains(strings.ToLower(string(data[:min(len(data), 256)])), "<html")
		ct = "application/javascript"
	case "style":
		valid = ct == "text/css"
		data = []byte(diyRewriteCSS(string(data), raw, func(v, k string) string { return s.diyAssetURL(r.URL.Path, v, k, time.Unix(expires, 0)) }))
		ct = "text/css"
	case "font":
		valid = strings.HasPrefix(ct, "font/") || ct == "application/font-woff" || ct == "application/vnd.ms-fontobject"
		if !valid {
			valid = bytes.HasPrefix(data, []byte("wOFF")) || bytes.HasPrefix(data, []byte("wOF2"))
		}
	case "media":
		valid = strings.HasPrefix(detected, "video/") || strings.HasPrefix(detected, "audio/")
		ct = strings.Split(detected, ";")[0]
	}
	if !valid {
		http.Error(w, "Unsupported resource type", 415)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if r.Method != http.MethodHead {
		w.Write(data)
	}
}

// Used by application saves while holding the owner's transaction lock.
func (s *Server) saveAPKLandingSource(tx *gorm.DB, a *Application, source string, ref *DIYLandingReference, selectedTemplate ...string) error {
	if source != "diy" && source != "system" {
		return problem(400, "请选择有效的落地页来源")
	}
	requested := ""
	if len(selectedTemplate) > 0 {
		requested = selectedTemplate[0]
	}
	if requested != "" && (requested != "google_play" || source != "system") {
		return problem(400, "请选择有效的落地页来源")
	}
	var saved AppPageDesign
	err := tx.First(&saved, "app_id = ?", a.ID).Error
	if err == gorm.ErrRecordNotFound {
		saved = AppPageDesign{AppID: a.ID, Design: defaultPageDesign(*a)}
	} else if err != nil {
		return err
	}
	if source == "diy" {
		if ref == nil {
			return problem(400, "请选择已发布的 DIY 落地页")
		}
		prior, err := diyBindingReference(tx, "apk", a.ID)
		if err != nil {
			return err
		}
		if saved.Design.Layout != "diy" {
			saved.Design.SystemLayout = saved.Design.Layout
		}
		if saved.Design.Layout != "diy" || !diySameRevision(prior, ref) {
			a.DownloadMode = "info"
		}
		saved.Design.Layout = "diy"
	} else if saved.Design.Layout == "diy" {
		saved.Design.Layout = saved.Design.SystemLayout
		if saved.Design.Layout == "" || saved.Design.Layout == "diy" {
			saved.Design.Layout = "google_play"
		}
	}
	if source == "system" {
		ref = nil
		if requested == "google_play" {
			var useGoogle func(*PageDesign)
			useGoogle = func(d *PageDesign) {
				d.Layout = "google_play"
				d.SystemLayout = "google_play"
				d.DIYLanding = nil
				for i := range d.Localizations {
					useGoogle(&d.Localizations[i])
				}
			}
			useGoogle(&saved.Design)
		}
	}
	saved.Design.DIYLanding = nil
	if err = s.bindDIYLanding(tx, a.UserID, "apk", a.ID, ref); err != nil {
		return err
	}
	return tx.Save(&saved).Error
}

func (s *Server) projectAPKLanding(a *Application) error {
	ref, err := diyBindingReference(s.DB, "apk", a.ID)
	if err != nil {
		return err
	}
	a.LandingSource = "system"
	a.DIYLanding = ref
	if ref != nil {
		a.LandingSource = "diy"
	}
	design, err := s.pageDesign(*a)
	if err != nil {
		return err
	}
	a.LandingTemplate = design.Layout
	return nil
}

func (s *Server) savePWADIYBinding(tx *gorm.DB, a PWAApp, c *PWALandingConfig) error {
	if c == nil {
		return nil
	}
	var ref *DIYLandingReference
	if c.Template == "diy" {
		ref = c.DIYLanding
		if ref == nil {
			var err error
			ref, err = diyBindingReference(tx, "pwa", a.ID)
			if err != nil {
				return err
			}
			if ref == nil {
				return problem(400, "请选择已发布的 DIY 落地页")
			}
		}
	}
	return s.bindDIYLanding(tx, a.UserID, "pwa", a.ID, ref)
}

func (s *Server) bindDIYLanding(tx *gorm.DB, owner, kind, id string, ref *DIYLandingReference) error {
	if s.Config.DisableDIYLanding && ref != nil {
		prior, err := diyBindingReference(tx, kind, id)
		if err != nil {
			return err
		}
		if !diySameReference(prior, ref) {
			return problem(503, "DIY 落地页暂时停用，请稍后重试")
		}
	}
	return setDIYBinding(tx, owner, kind, id, ref)
}

func (s *Server) pwaLandingWithReference(a PWAApp) PWALandingConfig {
	c := storedPWALanding(a.LandingJSON)
	if c.Template == "diy" {
		c.DIYLanding, _ = diyBindingReference(s.DB, "pwa", a.ID)
	}
	return c
}

// Keep a small JSON helper for tests and runtime-contract fixtures.
func diyJSON(v any) string { raw, _ := json.Marshal(v); return string(raw) }

// The editor's reset value, isolated preview and trusted ready-state button
// share the source CTA wording. Installation state may still replace it with
// Play/processing text through the original APK/PWA client runtime.
func diyApplicationButtonLabel(prepared string, options DIYLandingOptions, content DIYLandingContent, fallback string) string {
	if custom := diyContentPrimaryLabel(content); custom != "" {
		return custom
	}
	fields, err := diyContentFields(prepared, options)
	if err == nil {
		for _, field := range fields {
			if field.ID == "primary-label" && strings.TrimSpace(field.Value) != "" {
				return strings.TrimSpace(field.Value)
			}
		}
	}
	return fallback
}
