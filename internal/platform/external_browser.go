package platform

import (
	"crypto/hmac"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// This is app-wide behavior, not a per-language display setting.
type ExternalBrowserConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

var externalRelayPath = regexp.MustCompile(`^/(?:a|p)/[^/]+/external/open/?$`)

const externalReturnParam = "__d1_browser_return"

func normalizeExternalBrowser(c *ExternalBrowserConfig) error {
	if c == nil {
		return nil
	}
	c.URL = strings.TrimSpace(c.URL)
	if !c.Enabled && len(c.URL) <= 2048 {
		return nil
	}
	u, e := url.Parse(c.URL)
	if e != nil || len(c.URL) > 2048 || u.Hostname() == "" || u.User != nil || u.Opaque != "" ||
		(u.Scheme != "https" && u.Scheme != "http") || strings.Contains(c.URL, "\\") ||
		strings.IndexFunc(c.URL, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return problem(400, "请输入有效的外部浏览器目标网址（HTTP/HTTPS）")
	}
	if externalRelayPath.MatchString(u.Path) {
		return problem(400, "外部浏览器目标网址不能填写中转地址")
	}
	c.URL = u.String()
	return nil
}

type externalBrowserRuntime struct {
	Enabled     bool   `json:"enabled"`
	URL         string `json:"url"`
	OpenURL     string `json:"open_url"`
	Continue    bool   `json:"continue"`
	SelfTarget  bool   `json:"self_target"`
	ClickToken  string `json:"click_token"`
	ReturnToken string `json:"return_token"`
}

func (s *Server) externalLandingTarget(r *http.Request, kind, slug, owner string, u *url.URL) bool {
	p := strings.TrimRight(u.Path, "/")
	ownPath := "/p/" + slug
	if kind == "apk" {
		ownPath = "/a/" + slug
	}
	if p != ownPath && !(kind == "apk" && p == ownPath+"/info") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if strings.EqualFold(u.Host, r.Host) || host == s.Config.Host() {
		return true
	}
	settings, e := getSettings(s.DB)
	if e != nil {
		return false
	}
	if _, ok := settings.managedDownloadHosts(true)[host]; ok {
		return true
	}
	var count int64
	return s.DB.Model(&Domain{}).Where("hostname = ? AND user_id = ? AND verified_at > ?", host, owner, s.Now().Add(-48*time.Hour)).Count(&count).Error == nil && count > 0
}

func (s *Server) externalReturnToken(kind, id, target, eventID string) string {
	expiry := strconv.FormatInt(s.Now().Add(10*time.Minute).Unix(), 10)
	return expiry + "." + eventID + "." + s.mac("external-return:"+kind+":"+id+":"+hashString(target)+":"+expiry+":"+eventID)
}
func (s *Server) validExternalReturn(r *http.Request, kind, id, target string) bool {
	return s.validExternalReturnToken(kind, id, target, r.URL.Query().Get(externalReturnParam))
}
func (s *Server) validExternalReturnToken(kind, id, target, token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(parts[1]) != 64 {
		return false
	}
	expiry, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil || expiry <= s.Now().Unix() || expiry > s.Now().Add(10*time.Minute).Unix() {
		return false
	}
	expected := s.mac("external-return:" + kind + ":" + id + ":" + hashString(target) + ":" + parts[0] + ":" + parts[1])
	return hmac.Equal([]byte(expected), []byte(parts[2]))
}

func (s *Server) externalBrowserJSON(r *http.Request, kind, id, slug, owner string, c *ExternalBrowserConfig) string {
	if c == nil || !c.Enabled {
		return "{}"
	}
	copied := *c
	if normalizeExternalBrowser(&copied) != nil {
		return "{}"
	}
	prefix := "/p/" + slug + "/"
	if kind == "apk" {
		prefix = "/a/" + slug + "/"
	}
	target, _ := url.Parse(copied.URL)
	value := externalBrowserRuntime{Enabled: true, URL: copied.URL, OpenURL: s.pwaRequestOrigin(r) + prefix + "external/open", Continue: s.validExternalReturn(r, kind, id, copied.URL), SelfTarget: s.externalLandingTarget(r, kind, slug, owner, target), ClickToken: s.externalClickToken(kind, id, copied.URL), ReturnToken: r.URL.Query().Get(externalReturnParam)}
	raw, _ := json.Marshal(value)
	return string(raw)
}

// This endpoint never fetches the destination or accepts a caller-supplied URL.
// Click reporting and the relay settle the same event, without a download ticket.
func (s *Server) openExternalBrowser(w http.ResponseWriter, r *http.Request, kind string) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodPost && !sameUsageOrigin(s, r) {
		fail(w, problem(403, "请求来源不受信任"))
		return
	}
	if e := s.rate("external-open:"+s.clientIP(r), 1200, time.Minute); e != nil {
		fail(w, e)
		return
	}
	var id, slug, owner string
	var c *ExternalBrowserConfig
	if kind == "apk" {
		app, e := s.publicApp(r)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		id, slug, owner = app.ID, app.Slug, app.UserID
		design, e := s.pageDesign(app)
		if e != nil {
			fail(w, e)
			return
		}
		c = design.ExternalBrowser
	} else {
		app, e := s.loadPublicPWA(r)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		id, slug, owner = app.ID, app.Slug, app.UserID
		c = storedPWALanding(app.LandingJSON).Settings.ExternalBrowser
	}
	if s.rejectBlockedUA(w, r, owner, r.UserAgent()) {
		return
	}
	if c == nil || !c.Enabled {
		http.NotFound(w, r)
		return
	}
	copied := *c
	if e := normalizeExternalBrowser(&copied); e != nil {
		fail(w, e)
		return
	}
	var click struct {
		Key   string `json:"key"`
		Token string `json:"token"`
	}
	if r.Method == http.MethodPost {
		if e := decode(w, r, &click); e != nil {
			fail(w, e)
			return
		}
	} else {
		click.Key = r.URL.Query().Get("key")
		click.Token = r.URL.Query().Get("token")
	}
	eventID, err := s.chargeExternalClick(kind, id, owner, copied.URL, click.Key, click.Token)
	if err != nil {
		fail(w, err)
		return
	}
	if r.Method == http.MethodPost {
		jsonOut(w, 200, map[string]any{"allowed": true, "state": "charged"})
		return
	}
	target, _ := url.Parse(copied.URL)
	if s.externalLandingTarget(r, kind, slug, owner, target) {
		q := target.Query()
		q.Set(externalReturnParam, s.externalReturnToken(kind, id, copied.URL, eventID))
		target.RawQuery = q.Encode()
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	http.Redirect(w, r, target.String(), http.StatusFound)
}

//go:embed templates/external-browser.js
var externalBrowserJS string

//go:embed templates/external-browser.css
var externalBrowserCSS string
