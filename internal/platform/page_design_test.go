package platform

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"html"
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

func TestPageDesignDisplayMetricsDoNotChangeRealDownloadsOrAccounting(t *testing.T) {
	for _, layout := range []string{"google_play", "store", "minimal"} {
		t.Run(layout, func(t *testing.T) {
			s := testServer(t)
			u := seedUser(t, s, 5*Dollar)
			a := seedApp(t, s, u, 128)
			if err := s.DB.Model(&a).Update("downloads", 37).Error; err != nil {
				t.Fatal(err)
			}
			cookie, csrf := sessionFor(t, s, u)
			path := "/api/apps/" + a.ID + "/page-design"
			d := defaultPageDesign(a)
			d.Layout, d.DisplayRating, d.DisplayReviewCount, d.DisplayDownloadCount = layout, "4.8", "1.2万", "100K+"
			requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 200)
			preview := call(t, s, "POST", path+"/preview", d, cookie, csrf)
			requireCode(t, preview, 200)
			var p struct {
				HTML string `json:"html"`
			}
			if err := json.Unmarshal(preview.Body.Bytes(), &p); err != nil {
				t.Fatal(err)
			}
			public := call(t, s, "GET", "/a/"+a.Slug+"/info", nil, nil, "")
			requireCode(t, public, 200)
			for _, markup := range []string{p.HTML, public.Body.String()} {
				text := html.UnescapeString(markup)
				for _, display := range []string{"4.8", "1.2万 条评价", "100K+"} {
					if !strings.Contains(text, display) {
						t.Fatalf("%s missing display metric %q", layout, display)
					}
				}
			}
			var saved AppPageDesign
			if err := s.DB.First(&saved, "app_id = ?", a.ID).Error; err != nil || saved.Design.DisplayDownloadCount != "100K+" || saved.Design.DisplayRating != "4.8" {
				t.Fatal("display metrics were not persisted independently", err)
			}
			if err := s.DB.First(&a, "id = ?", a.ID).Error; err != nil || a.Downloads != 37 {
				t.Fatal("display metrics changed real downloads", err)
			}
			if err := s.DB.First(&u, "id = ?", u.ID).Error; err != nil || u.BalanceMicros != 5*Dollar {
				t.Fatal("display metrics changed account balance", err)
			}
			if countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Download{}) != 0 {
				t.Fatal("editing/previewing display metrics created real usage or billing data")
			}
			d.DisplayRating, d.DisplayReviewCount, d.DisplayDownloadCount = "", "", ""
			requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 200)
			public = call(t, s, "GET", "/a/"+a.Slug+"/info", nil, nil, "")
			if strings.Contains(html.UnescapeString(public.Body.String()), "100K+") || !strings.Contains(public.Body.String(), ">37</") {
				t.Fatal("clearing the custom display did not restore real downloads")
			}
			d.DisplayDownloadCount, d.ShowDownloads = "100K+", false
			requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 200)
			public = call(t, s, "GET", "/a/"+a.Slug+"/info", nil, nil, "")
			if strings.Contains(html.UnescapeString(public.Body.String()), "100K+") {
				t.Fatal("hidden download count was rendered")
			}
		})
	}
}

func TestPageDesignDisplayMetricValidationAndEscaping(t *testing.T) {
	d := defaultPageDesign(Application{Name: "测试应用"})
	for _, rating := range []string{"", "0", "0.0", "4.8", "5", "5.0"} {
		d.DisplayRating = rating
		if err := validatePageDesign(d); err != nil {
			t.Fatalf("valid rating %q rejected: %v", rating, err)
		}
	}
	for _, rating := range []string{"-1", "5.1", "6", "4.88", "NaN", "<script>"} {
		d.DisplayRating = rating
		if validatePageDesign(d) == nil {
			t.Fatalf("invalid rating %q accepted", rating)
		}
	}
	d.DisplayRating = "4.8"
	d.DisplayReviewCount = strings.Repeat("万", 21)
	if validatePageDesign(d) == nil {
		t.Fatal("oversized review count accepted")
	}
	d.DisplayReviewCount, d.DisplayDownloadCount = "12K", strings.Repeat("1", 21)
	if validatePageDesign(d) == nil {
		t.Fatal("oversized download display accepted")
	}
	d.DisplayDownloadCount = "<b>123</b>"
	for _, layout := range []string{"google_play", "store", "minimal"} {
		var html bytes.Buffer
		if err := downloadPageTemplate(layout).Execute(&html, map[string]any{"App": Application{Slug: "test", Downloads: 7}, "Design": d, "Preview": true}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(html.String(), "<b>123</b>") || !strings.Contains(html.String(), "&lt;b&gt;123&lt;/b&gt;") {
			t.Fatal("custom display bypassed template escaping")
		}
	}
}
