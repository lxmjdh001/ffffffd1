package platform

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"html/template"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

//go:embed templates/pwa-page.html
var pwaPageHTML string

//go:embed templates/pwa-client.js
var pwaClientJS string

//go:embed templates/pwa-sw.js
var pwaSWJS string

//go:embed templates/pwa-page.css
var pwaPageCSS string

var pwaPageTemplate = template.Must(template.New("pwa-page").Parse(pwaPageHTML))

type pwaPageData struct {
	App     *PWAApp
	Base    string
	Launch  bool
	Chinese bool
}

func pwaWantsChinese(r *http.Request) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Accept-Language"))), "zh")
}
func (s *Server) pwaInstallPage(w http.ResponseWriter, r *http.Request) { s.renderPWAPage(w, r, false) }
func (s *Server) pwaLaunchPage(w http.ResponseWriter, r *http.Request)  { s.renderPWAPage(w, r, true) }
func (s *Server) renderPWAPage(w http.ResponseWriter, r *http.Request, launch bool) {
	a, err := s.loadPublicPWA(r)
	if err != nil {
		pwaPublicFailure(w, r, err)
		return
	}
	target, _ := url.Parse(a.TargetURL)
	frameSources := "https:"
	if !s.Config.Production() && target.Scheme == "http" {
		// Normalized development targets can use one exact loopback origin.
		// Never allow arbitrary insecure frames, including their redirects.
		frameSources += " " + target.Scheme + "://" + target.Host
	}
	data := pwaPageData{App: a, Base: "/p/" + a.Slug + "/", Launch: launch, Chinese: pwaWantsChinese(r)}
	var buf bytes.Buffer
	if err = pwaPageTemplate.Execute(&buf, data); err != nil {
		http.Error(w, "Page unavailable", 503)
		return
	}
	// H5 flows can redirect to another HTTPS login, payment or www origin.
	// Every D1 response retains frame-ancestors 'none' and X-Frame-Options DENY,
	// so redirects back to this origin cannot load a same-origin child document.
	// Only public PWA wrappers relax frame-src; the console stays restrictive.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; worker-src 'self'; manifest-src 'self'; frame-src "+frameSources+"; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodHead {
		_, _ = w.Write(buf.Bytes())
	}
}
func pwaPublicFailure(w http.ResponseWriter, r *http.Request, err error) { http.NotFound(w, r) }
func (s *Server) pwaManifest(w http.ResponseWriter, r *http.Request) {
	a, err := s.loadPublicPWA(r)
	if err != nil {
		pwaPublicFailure(w, r, err)
		return
	}
	base := "/p/" + a.Slug + "/"
	short := []rune(a.Name)
	if len(short) > 12 {
		short = short[:12]
	}
	w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Robots-Tag", "noindex")
	jsonOutContent(w, map[string]any{"id": base, "name": a.Name, "short_name": string(short), "description": a.Description, "start_url": base + "launch", "scope": base, "display": "standalone", "orientation": "any", "background_color": "#f5f8ff", "theme_color": a.ThemeColor, "icons": []map[string]string{{"src": base + "icon/192.png", "sizes": "192x192", "type": "image/png", "purpose": "any"}, {"src": base + "icon/512.png", "sizes": "512x512", "type": "image/png", "purpose": "any"}}})
}
func jsonOutContent(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "Page unavailable", 503)
		return
	}
	_, _ = w.Write(b)
}
func (s *Server) pwaAsset(w http.ResponseWriter, r *http.Request, content, contentType string) {
	if _, err := s.loadPublicPWA(r); err != nil {
		pwaPublicFailure(w, r, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Robots-Tag", "noindex")
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(content))
	}
}
func (s *Server) pwaClientScript(w http.ResponseWriter, r *http.Request) {
	s.pwaAsset(w, r, pwaClientJS, "application/javascript; charset=utf-8")
}
func (s *Server) pwaStyles(w http.ResponseWriter, r *http.Request) {
	s.pwaAsset(w, r, pwaPageCSS, "text/css; charset=utf-8")
}
func (s *Server) pwaServiceWorker(w http.ResponseWriter, r *http.Request) {
	// The browser's default maximum scope is the worker's /p/{slug}/ directory.
	// Deliberately do not emit a broad Service-Worker-Allowed header.
	s.pwaAsset(w, r, pwaSWJS, "application/javascript; charset=utf-8")
}
func (s *Server) pwaPushConfig(w http.ResponseWriter, r *http.Request) {
	a, err := s.loadPublicPWA(r)
	if err != nil {
		pwaPublicFailure(w, r, err)
		return
	}
	base := "/p/" + a.Slug + "/"
	key := ""
	if a.PushEnabled {
		key = a.VAPIDPublicKey
	}
	jsonOut(w, 200, map[string]any{"enabled": a.PushEnabled, "public_key": key, "subscribe_url": base + "subscribe", "unsubscribe_url": base + "unsubscribe", "event_url": base + "event"})
}
func (s *Server) pwaIcon(w http.ResponseWriter, r *http.Request) {
	a, err := s.loadPublicPWA(r)
	if err != nil {
		pwaPublicFailure(w, r, err)
		return
	}
	raw := r.PathValue("size")
	size := 0
	if raw == "192.png" {
		size = 192
	}
	if raw == "512.png" {
		size = 512
	}
	if size == 0 {
		http.NotFound(w, r)
		return
	}
	var blob struct{ IconPNG []byte }
	if err := s.DB.Model(&PWAApp{}).Select("icon_png").Where("id = ?", a.ID).Take(&blob).Error; err != nil {
		http.Error(w, "Icon unavailable", 503)
		return
	}
	a.IconPNG = blob.IconPNG
	s.writePWAIcon(w, r, *a, size, false)
}

// The owner can still manage and recognize a paused app while every public
// endpoint remains unavailable. This image is authenticated and never shared.
func (s *Server) ownedPWAIcon(w http.ResponseWriter, r *http.Request) {
	a, err := s.ownedPWA(r)
	if err != nil {
		fail(w, err)
		return
	}
	s.writePWAIcon(w, r, a, 512, true)
}

func (s *Server) writePWAIcon(w http.ResponseWriter, r *http.Request, a PWAApp, size int, private bool) {
	var pngBytes []byte
	if len(a.IconPNG) > 0 {
		if size == 512 {
			pngBytes = a.IconPNG
		} else {
			im, err := png.Decode(bytes.NewReader(a.IconPNG))
			if err != nil {
				http.Error(w, "Icon unavailable", 503)
				return
			}
			var out bytes.Buffer
			_ = png.Encode(&out, resizePWAIcon(im, size))
			pngBytes = out.Bytes()
		}
	}
	if len(pngBytes) == 0 {
		var buf bytes.Buffer
		_ = png.Encode(&buf, defaultPWAIcon(a.Name, a.ThemeColor, size))
		pngBytes = buf.Bytes()
	}
	etag := `"` + hashString(string(pngBytes)) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if private {
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Add("Vary", "Cookie")
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Robots-Tag", "noindex")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(304)
		return
	}
	if r.Method != http.MethodHead {
		_, _ = w.Write(pngBytes)
	}
}
func defaultPWAIcon(name, theme string, size int) *image.NRGBA {
	base := color.NRGBA{99, 115, 232, 255}
	if len(theme) == 7 {
		v, _ := strconv.ParseUint(theme[1:], 16, 32)
		base = color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
	}
	im := image.NewNRGBA(image.Rect(0, 0, size, size))
	radius := size / 5
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := 0, 0
			if x < radius {
				dx = radius - x
			}
			if x >= size-radius {
				dx = x - (size - radius - 1)
			}
			if y < radius {
				dy = radius - y
			}
			if y >= size-radius {
				dy = y - (size - radius - 1)
			}
			if dx*dx+dy*dy > radius*radius {
				im.SetNRGBA(x, y, color.NRGBA{245, 248, 255, 255})
				continue
			}
			lift := (size - y) * 22 / size
			im.SetNRGBA(x, y, color.NRGBA{uint8(min(255, int(base.R)+lift)), uint8(min(255, int(base.G)+lift)), uint8(min(255, int(base.B)+lift)), 255})
		}
	}
	letter := 'D'
	for _, r := range name {
		r = unicode.ToUpper(r)
		if r >= 'A' && r <= 'Z' {
			letter = r
			break
		}
	}
	rows := pwaLetterGlyphs[letter]
	unit := size / 12
	left := (size - 5*unit) / 2
	top := (size - 7*unit) / 2
	for y, row := range rows {
		for x := 0; x < 5; x++ {
			if row&(1<<uint(4-x)) == 0 {
				continue
			}
			for py := 0; py < unit; py++ {
				for px := 0; px < unit; px++ {
					im.SetNRGBA(left+x*unit+px, top+y*unit+py, color.NRGBA{255, 255, 255, 255})
				}
			}
		}
	}
	return im
}

var pwaLetterGlyphs = map[rune][7]byte{
	'A': {14, 17, 17, 31, 17, 17, 17}, 'B': {30, 17, 17, 30, 17, 17, 30}, 'C': {14, 17, 16, 16, 16, 17, 14}, 'D': {30, 17, 17, 17, 17, 17, 30}, 'E': {31, 16, 16, 30, 16, 16, 31}, 'F': {31, 16, 16, 30, 16, 16, 16}, 'G': {14, 17, 16, 23, 17, 17, 15}, 'H': {17, 17, 17, 31, 17, 17, 17}, 'I': {31, 4, 4, 4, 4, 4, 31}, 'J': {7, 2, 2, 2, 18, 18, 12}, 'K': {17, 18, 20, 24, 20, 18, 17}, 'L': {16, 16, 16, 16, 16, 16, 31}, 'M': {17, 27, 21, 21, 17, 17, 17}, 'N': {17, 25, 25, 21, 19, 19, 17}, 'O': {14, 17, 17, 17, 17, 17, 14}, 'P': {30, 17, 17, 30, 16, 16, 16}, 'Q': {14, 17, 17, 17, 21, 18, 13}, 'R': {30, 17, 17, 30, 20, 18, 17}, 'S': {15, 16, 16, 14, 1, 1, 30}, 'T': {31, 4, 4, 4, 4, 4, 4}, 'U': {17, 17, 17, 17, 17, 17, 14}, 'V': {17, 17, 17, 17, 17, 10, 4}, 'W': {17, 17, 17, 21, 21, 21, 10}, 'X': {17, 17, 10, 4, 10, 17, 17}, 'Y': {17, 17, 10, 4, 4, 4, 4}, 'Z': {31, 1, 2, 4, 8, 16, 31},
}
