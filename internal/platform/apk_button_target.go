package platform

import (
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
)

func (d PageDesign) UsesTraffic() bool { return d.ButtonAction == "traffic" }
func (d PageDesign) ButtonActionPath(slug string) string {
	if d.UsesTraffic() {
		return "/a/" + slug + "/button"
	}
	return "/a/" + slug + "/download"
}
func validateAPKButtonTarget(d PageDesign) error {
	if d.UsesTraffic() && d.ExternalBrowser != nil && d.ExternalBrowser.Enabled {
		return problem(400, "使用应用分流时，请关闭自定义外部浏览器跳转")
	}
	if d.ButtonAction != "" && d.ButtonAction != "download" && d.ButtonAction != "traffic" {
		return problem(400, "请选择有效的按钮目标")
	}
	if d.UsesTraffic() && (len(d.TrafficPoolID) != 32 || !hexKey.MatchString(d.TrafficPoolID)) {
		return problem(400, "请选择要绑定的应用分流")
	}
	return nil
}
func validateAPKTrafficBinding(db *gorm.DB, owner string, d PageDesign) error {
	if !d.UsesTraffic() {
		return nil
	}
	var count int64
	if err := db.Model(&TrafficPool{}).Where("user_id = ? AND statistics_id = ?", owner, d.TrafficPoolID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return problem(400, "所选应用分流不存在或无权使用，请重新选择")
	}
	return nil
}

// A separate action keeps explicit direct-download links unchanged. The stable
// statistics ID binds to the owned pool even when its public slug is edited.
func (s *Server) apkPageButton(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a, err := s.publicApp(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if s.rejectBlockedUA(w, r, a.UserID, r.UserAgent()) {
		return
	}
	d, err := s.pageDesign(a)
	if err != nil {
		fail(w, err)
		return
	}
	if !d.UsesTraffic() {
		s.startDownload(w, r)
		return
	}
	if err = s.rate("apk-button:"+s.clientIP(r), 1200, time.Minute); err != nil {
		fail(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err = r.ParseForm(); err != nil {
		fail(w, problem(400, "请求无效"))
		return
	}
	if !hexKey.MatchString(r.PostForm.Get("request_key")) {
		fail(w, problem(400, "请从下载页面开始下载"))
		return
	}
	// Validate the same optional attribution capability as a normal page click.
	// The source APK must not create a download ticket when routing elsewhere.
	if raw := r.PostForm.Get("attribution_token"); raw != "" {
		attr, e := s.readAttribution(raw, a.ID)
		if e != nil || attr.VisitorHash != s.mac(s.visitor(w, r)) {
			fail(w, problem(400, "归因页面已过期，请重新访问"))
			return
		}
		if r.PostForm.Get("ad_consent") == "yes" {
			if e = s.DB.Model(&attr).Update("consent", true).Error; e != nil {
				fail(w, e)
				return
			}
		}
	}
	target, err := s.selectTrafficTarget(r, "", d.TrafficPoolID, a.UserID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = problem(503, "绑定的应用分流不可用，请联系应用发布者")
	}
	if err != nil {
		fail(w, err)
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		jsonOut(w, 200, map[string]string{"redirect_url": target})
		return
	}
	// Chrome applies the source page's form-action policy to HTTP redirects.
	// A script-free handoff document also works when JavaScript is disabled,
	// without widening the landing page's CSP to arbitrary external origins.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	_ = apkTrafficHandoff.Execute(w, map[string]string{"URL": target, "Label": d.localized(r.Header.Get("Accept-Language")).DownloadButtonText()})
}

var apkTrafficHandoff = template.Must(template.New("apk-traffic-handoff").Parse(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="refresh" content="0;url={{.URL}}"><title>Continue</title></head><body><a href="{{.URL}}">{{.Label}}</a></body></html>`))
