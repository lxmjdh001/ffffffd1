package platform

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var hexKey = regexp.MustCompile(`^[a-f0-9]{32,64}$`)

func (s *Server) publicApp(r *http.Request) (Application, error) {
	var a Application
	q := s.DB.Where("slug = ? AND enabled = ? AND blocked = ?", r.PathValue("slug"), true, false)
	if owner := r.Context().Value(domainOwnerKey); owner != nil {
		q = q.Where("user_id = ?", owner)
	}
	if e := q.First(&a).Error; e != nil {
		return a, e
	}
	var owner User
	if e := s.DB.First(&owner, "id = ? AND disabled = ?", a.UserID, false).Error; e != nil {
		return a, gorm.ErrRecordNotFound
	}
	if audience, managed := r.Context().Value(downloadAudienceKey).(string); managed && audience != "all" && owner.PlanID(s.Now()) != audience {
		return a, gorm.ErrRecordNotFound
	}
	return a, nil
}
func (s *Server) visitor(w http.ResponseWriter, r *http.Request) string {
	c, e := r.Cookie("d1_visitor")
	if e == nil && hexKey.MatchString(c.Value) {
		return c.Value
	}
	v := randomHex(32)
	http.SetCookie(w, &http.Cookie{Name: "d1_visitor", Value: v, Path: "/", HttpOnly: true, Secure: s.Config.Production(), SameSite: http.SameSiteLaxMode, MaxAge: 30 * 86400})
	return v
}

// Public links start the download without a landing page or JavaScript. The
// redirect keeps resumable transfers on one browser-bound ticket URL.
func (s *Server) directDownload(w http.ResponseWriter, r *http.Request) {
	if e := s.rate("download-start:"+s.clientIP(r), 1200, time.Minute); e != nil {
		fail(w, e)
		return
	}
	a, e := s.publicApp(r)
	if e != nil {
		http.Error(w, "应用不存在、已暂停或已被下架", 404)
		return
	}
	if r.Method == http.MethodHead {
		// Metadata checks neither create tickets nor reserve download quota.
		s.serveDownloadFile(w, r, a, Ticket{})
		return
	}
	visitor := s.visitor(w, r)
	attr, _, e := s.prepareAttribution(w, r, a, visitor)
	if e != nil {
		fail(w, e)
		return
	}
	token, e := s.createDownloadTicket(r, a, visitor, randomHex(16), attr)
	if e != nil {
		fail(w, e)
		return
	}
	http.Redirect(w, r, "/d/"+token, http.StatusTemporaryRedirect)
}

func (s *Server) downloadPage(w http.ResponseWriter, r *http.Request) {
	a, e := s.publicApp(r)
	if e != nil {
		http.Error(w, "应用不存在、已暂停或已被下架", 404)
		return
	}
	if e = s.rate("landing:"+s.clientIP(r), 1200, time.Minute); e != nil {
		fail(w, e)
		return
	}
	visitor := s.visitor(w, r)
	attr, token, e := s.prepareAttribution(w, r, a, visitor)
	if e != nil {
		fail(w, e)
		return
	}
	var ads AdConfig
	_ = s.DB.First(&ads, "app_id = ?", a.ID).Error
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = downloadTemplate.Execute(w, map[string]any{"App": a, "Nonce": randomHex(16), "Size": fmt.Sprintf("%.1f MB", float64(a.Size)/(1<<20)), "Attribution": token, "Source": attr.Source, "DeepLink": deepLinkWithToken(ads, token), "AdsEnabled": ads.FacebookEnabled || ads.TikTokEnabled})
}

var downloadTemplate = template.Must(template.New("download").Parse(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.App.Name}} · 应用下载</title><style>*{box-sizing:border-box}body{margin:0;background:#f3f5f8;color:#18252f;font-family:system-ui,-apple-system,sans-serif;padding:8vh 20px}main{max-width:580px;margin:auto;background:white;border:1px solid #e3e7eb;border-radius:24px;padding:44px;text-align:center}.icon{display:grid;place-items:center;width:80px;height:80px;background:#e7eeff;color:#5266df;font-size:35px;border-radius:24px;margin:auto}h1{font-size:28px;margin:22px 0 10px}p{color:#697481;line-height:1.8}button{background:#5569e8;color:white;border:0;border-radius:12px;font-size:16px;padding:16px 40px;cursor:pointer;width:100%;margin:16px 0}.desc{text-align:left;white-space:pre-wrap;overflow-wrap:anywhere;padding:20px;background:#f7f8fa;border-radius:12px;font-size:14px}.hash{font-size:11px;word-break:break-all;color:#89929f;margin-top:24px}.meta{font-size:14px}.brand{text-align:center;margin:28px;color:#87919c;font-size:12px}</style><script defer src="/a/{{.App.Slug}}/client.js"></script></head><body><main><div class="icon">↓</div><h1>{{.App.Name}}</h1><p class="meta">{{.App.Platform}} · {{.App.Version}} · {{.Size}}</p><form method="post" action="/a/{{.App.Slug}}/download"><input type="hidden" name="request_key" value="{{.Nonce}}"><input type="hidden" name="attribution_token" value="{{.Attribution}}">{{if .AdsEnabled}}<label class="meta"><input type="checkbox" name="ad_consent" value="yes"> 我同意将下载及后续操作用于广告效果统计</label>{{end}}<button type="submit">下载应用 ↓</button></form><p id="download-status" class="meta" role="status" aria-live="polite"></p>{{if .DeepLink}}<p><a href="{{.DeepLink}}">已安装？打开应用并继续</a></p>{{end}}<p class="meta">{{.App.FileName}}<br>支持断点续传 · 重试不会重复计费</p>{{if .App.Description}}<div class="desc">{{.App.Description}}</div>{{end}}<p class="hash">SHA-256<br>{{.App.SHA256}}</p><p class="meta">请确认发布者可信后安装。IPA 需有效签名及对应设备授权。</p></main><div class="brand">应用分发服务 · D1 Apk</div></body></html>`))

func (s *Server) startDownload(w http.ResponseWriter, r *http.Request) {
	if e := s.rate("download-start:"+s.clientIP(r), 1200, time.Minute); e != nil {
		fail(w, e)
		return
	}
	a, e := s.publicApp(r)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if e = r.ParseForm(); e != nil {
		fail(w, problem(400, "请求无效"))
		return
	}
	key := r.PostForm.Get("request_key")
	if !hexKey.MatchString(key) {
		fail(w, problem(400, "请从下载页面开始下载"))
		return
	}
	v := s.visitor(w, r)
	var attr Attribution
	if raw := r.PostForm.Get("attribution_token"); raw != "" {
		attr, e = s.readAttribution(raw, a.ID)
		if e != nil || attr.VisitorHash != s.mac(v) {
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
	token, e := s.createDownloadTicket(r, a, v, key, attr)
	if e != nil {
		fail(w, e)
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		jsonOut(w, 200, map[string]string{"download_url": "/d/" + token})
		return
	}
	http.Redirect(w, r, "/d/"+token, http.StatusSeeOther)
}

func (s *Server) createDownloadTicket(r *http.Request, a Application, visitor, key string, attr Attribution) (string, error) {
	token := s.mac("download:" + a.ID + ":" + a.FileKey + ":" + visitor + ":" + key)
	device, system, browser := classifyDevice(r.UserAgent())
	t := Ticket{IP: s.clientIP(r), UserAgent: cleanText(r.UserAgent(), 2048), Source: attr.Source, Device: device, OS: system, Browser: browser, AttributionID: attr.ID, Hash: hashString(token), AppID: a.ID, UserID: a.UserID, FileKey: a.FileKey, VisitorHash: s.mac(visitor), Host: requestHost(r), ExpiresAt: s.Now().Add(24 * time.Hour)}
	e := s.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&t).Error
	return token, e
}

// Progressive enhancement keeps native form submission as a fallback, while
// WebViews that do not preserve POST navigation can request a ticket via fetch.
func (s *Server) downloadClient(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = io.WriteString(w, `"use strict";
const form=document.querySelector('form');
const button=form.querySelector('button');
const status=document.getElementById('download-status');
form.addEventListener('submit',async function(event){
 event.preventDefault();button.disabled=true;status.textContent='正在准备下载…';
 try {
  const response=await fetch(form.action,{method:'POST',credentials:'same-origin',headers:{Accept:'application/json'},body:new URLSearchParams(new FormData(form))});
  const data=await response.json();
  if(!response.ok)throw new Error(data.error||'暂时无法下载，请重试');
  if(!/^\/d\/[a-f0-9]{64}$/.test(data.download_url))throw new Error('下载地址无效');
  status.textContent='已准备下载，请查看浏览器下载进度。';
  window.location.assign(data.download_url);
 }catch(error){status.textContent=error.message||'网络连接失败，请重试';}
 finally{button.disabled=false;}
});`)
}

type billingWriter struct {
	http.ResponseWriter
	bill      func() error
	wrote     bool
	denied    bool
	status    int
	written   int64
	confirmed int64
	writeErr  error
}

func (b *billingWriter) WriteHeader(status int) {
	if b.wrote {
		return
	}
	b.wrote = true
	b.status = status
	if status == 200 || status == 206 {
		if e := b.bill(); e != nil {
			b.denied = true
			for _, k := range []string{"Content-Length", "Content-Range", "Content-Disposition", "Content-Type", "Accept-Ranges", "ETag", "Last-Modified"} {
				b.Header().Del(k)
			}
			fail(b.ResponseWriter, e)
			return
		}
	}
	b.ResponseWriter.WriteHeader(status)
}
func (b *billingWriter) Write(p []byte) (int, error) {
	if !b.wrote {
		b.WriteHeader(200)
	}
	if b.denied {
		return 0, errors.New("download not authorized")
	}
	n, err := b.ResponseWriter.Write(p)
	b.written += int64(n)
	if err == nil {
		b.confirmed += int64(n)
	}
	if err != nil {
		b.writeErr = err
	}
	return n, err
}
func (s *Server) serveDownload(w http.ResponseWriter, r *http.Request) {
	if !hexKey.MatchString(r.PathValue("token")) {
		http.NotFound(w, r)
		return
	}
	var t Ticket
	if e := s.DB.First(&t, "hash = ? AND expires_at > ?", hashString(r.PathValue("token")), s.Now()).Error; e != nil {
		http.Error(w, "下载凭证已过期，请重新打开应用下载链接", 410)
		return
	}
	v, e := r.Cookie("d1_visitor")
	if e != nil || s.mac(v.Value) != t.VisitorHash || t.Host != requestHost(r) {
		http.Error(w, "请使用应用下载链接开始下载", 403)
		return
	}
	if owner := r.Context().Value(domainOwnerKey); owner != nil && owner != t.UserID {
		http.NotFound(w, r)
		return
	}
	var a Application
	if e = s.DB.First(&a, "id = ? AND enabled = ? AND blocked = ?", t.AppID, true, false).Error; e != nil {
		http.NotFound(w, r)
		return
	}
	if a.FileKey != t.FileKey {
		http.Error(w, "应用已更新，请重新打开应用下载链接", 410)
		return
	}
	if e = s.rate("stream:"+s.clientIP(r), 12000, time.Minute); e != nil {
		fail(w, e)
		return
	}
	s.serveDownloadFile(w, r, a, t)
}

// HEAD can inspect an app directly; only GET needs a persisted ticket for billing.
func (s *Server) serveDownloadFile(w http.ResponseWriter, r *http.Request, a Application, t Ticket) {
	if strings.Contains(r.Header.Get("Range"), ",") {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", a.Size))
		http.Error(w, "单次请求仅支持一个字节区间，可使用多个并行请求", 416)
		return
	}
	f, size, e := s.Blobs.Open(r.Context(), a.FileKey)
	if e != nil || size != a.Size {
		if f != nil {
			_ = f.Close()
		}
		http.Error(w, "文件暂时不可用，请联系发布者", 503)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": a.FileName}))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("ETag", `"`+a.SHA256+`"`)
	bw := &billingWriter{ResponseWriter: w, bill: func() error {
		if r.Method == "HEAD" {
			return nil
		}
		return s.reserveDownload(t)
	}}
	http.ServeContent(bw, r, a.FileName, a.UpdatedAt, f)
	if r.Method == "HEAD" || bw.denied || (bw.status != 200 && bw.status != 206) {
		return
	}
	start, end := int64(0), a.Size-1
	if bw.status == 206 {
		var total int64
		if _, e := fmt.Sscanf(bw.Header().Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); e != nil || total != a.Size {
			return
		}
	}
	if bw.confirmed <= 0 {
		return
	}
	if bw.writeErr != nil || bw.confirmed < end-start+1 {
		// Preserve successfully written prefixes so a later resumed request can
		// complete coverage. Never certify the final byte of an errored response.
		end = min(end-1, start+bw.confirmed-1)
	} else if err := http.NewResponseController(w).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		end--
	}
	if end >= start {
		s.persistCompletion(t, start, end)
	}
}
func (s *Server) listDownloads(w http.ResponseWriter, r *http.Request) {
	start, end, err := downloadDateRange(r, s.Now())
	if err != nil {
		fail(w, err)
		return
	}
	// Bound the owner/time index scan before counting and paginating the
	// successful download records, which are created at completion.
	q := s.DB.Model(&Download{}).Where("user_id = ? AND created_at >= ? AND created_at < ?", userOf(r).ID, start, end)
	if appID := r.URL.Query().Get("app_id"); appID != "" {
		q = q.Where("app_id = ?", appID)
	}
	listOut[Download](w, r, q)
}

func downloadDateRange(r *http.Request, now time.Time) (time.Time, time.Time, error) {
	now = now.In(billingZone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, billingZone)
	start, end := today, today
	from, to := r.URL.Query().Get("start_date"), r.URL.Query().Get("end_date")
	if from != "" || to != "" {
		var err error
		start, err = time.ParseInLocation("2006-01-02", from, billingZone)
		if err != nil {
			return time.Time{}, time.Time{}, problem(400, "请选择有效的开始日期，格式为 YYYY-MM-DD")
		}
		end, err = time.ParseInLocation("2006-01-02", to, billingZone)
		if err != nil {
			return time.Time{}, time.Time{}, problem(400, "请选择有效的结束日期，格式为 YYYY-MM-DD")
		}
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, problem(400, "结束日期不能早于开始日期")
	}
	if end.After(today) {
		return time.Time{}, time.Time{}, problem(400, "只能查询今天及以前的下载记录")
	}
	if end.Sub(start) >= 30*24*time.Hour {
		return time.Time{}, time.Time{}, problem(400, "单次最多查询 30 天下载记录（含起止日期）")
	}
	return start.UTC(), end.AddDate(0, 0, 1).UTC(), nil
}
func (s *Server) listLedger(w http.ResponseWriter, r *http.Request) {
	listOut[Ledger](w, r, s.DB.Model(&Ledger{}).Where("user_id = ?", userOf(r).ID))
}

var _ io.Writer = (*billingWriter)(nil)

func validUTF8(s string) bool { return utf8.ValidString(s) }
