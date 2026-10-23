package platform

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestPageDesignPersistsAndRendersWithoutChangingDownloads(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	a := seedApp(t, s, u, 10)
	cookie, csrf := sessionFor(t, s, u)
	path := "/api/apps/" + a.ID + "/page-design"
	d := defaultPageDesign(a)
	d.Title = "自定义应用标题"
	d.Summary = "为你的每一天而设计"
	d.Developer = "Example Studio"
	d.Tags = []string{"效率", "工具"}
	d.Description = "<script>alert('xss')</script>"
	d.ButtonText = "立即获取"
	d.ThemeColor = "#125533"
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	d.Icon = "data:image/png;base64," + base64.StdEncoding.EncodeToString(picture.Bytes())
	d.Screenshots = []string{d.Icon}
	requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 200)
	w := call(t, s, "GET", path, nil, cookie, "")
	requireCode(t, w, 200)
	var saved struct {
		Design PageDesign `json:"design"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Design.Title != d.Title {
		t.Fatal("design not persisted")
	}
	preview := call(t, s, "POST", path+"/preview", d, cookie, csrf)
	requireCode(t, preview, 200)
	var p struct {
		HTML string `json:"html"`
	}
	json.Unmarshal(preview.Body.Bytes(), &p)
	if !strings.Contains(p.HTML, "自定义应用标题") || !strings.Contains(p.HTML, d.Icon) || strings.Contains(p.HTML, "#ZgotmplZ") || strings.Contains(p.HTML, "<script>alert") || strings.Contains(p.HTML, "client.js") {
		t.Fatal("preview image, content or sandbox rendering invalid")
	}
	// Preview must not reserve quota or create download tickets.
	var count int64
	s.DB.Model(&Ticket{}).Count(&count)
	if count != 0 {
		t.Fatal("preview created download tickets")
	}
	public := call(t, s, "GET", "/a/"+a.Slug+"/info", nil, nil, "")
	requireCode(t, public, 200)
	if !strings.Contains(public.Body.String(), "立即获取") || !strings.Contains(public.Body.String(), "/a/"+a.Slug+"/download") || !strings.Contains(public.Body.String(), "client.js") {
		t.Fatal("public page did not use saved design and download flow")
	}
	d.Title = "仅预览的新标题"
	requireCode(t, call(t, s, "POST", path+"/preview", d, cookie, csrf), 200)
	public = call(t, s, "GET", "/a/"+a.Slug+"/info", nil, nil, "")
	if strings.Contains(public.Body.String(), d.Title) {
		t.Fatal("preview overwrote published design")
	}
	other := seedUser(t, s, 0)
	otherCookie, otherCSRF := sessionFor(t, s, other)
	requireCode(t, call(t, s, "GET", path, nil, otherCookie, ""), 404)
	requireCode(t, call(t, s, "PUT", path, d, otherCookie, otherCSRF), 404)
	requireCode(t, call(t, s, "POST", path+"/preview", d, otherCookie, otherCSRF), 404)
}

func TestPageDesignRejectsUnsafeImagesAndCSS(t *testing.T) {
	d := defaultPageDesign(Application{Name: "测试应用"})
	for _, src := range []string{"javascript:alert(1)", "data:image/svg+xml,<svg onload=alert(1)>", "data:image/png;base64,bm90LWFuLWltYWdl", "http://external.example.com/icon.png"} {
		d.Icon = src
		if validatePageDesign(d) == nil {
			t.Errorf("unsafe image accepted: %s", src)
		}
	}
	d.Icon = ""
	d.ThemeColor = "red;background:url(javascript:alert(1))"
	if validatePageDesign(d) == nil {
		t.Fatal("invalid color accepted")
	}
}
