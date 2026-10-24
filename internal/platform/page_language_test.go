package platform

import (
	"bytes"
	"encoding/json"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var detailPageLanguages = []string{"en", "zh-CN", "zh-TW", "ja", "ko", "de"}

func TestPageLanguagesRenderAcrossTemplatesWithoutPublisherContentChanges(t *testing.T) {
	for _, language := range detailPageLanguages {
		for _, layout := range []string{"google_play", "store", "minimal"} {
			t.Run(language+"/"+layout, func(t *testing.T) {
				a := Application{Name: "客户自定义名称", Slug: "app-preview", FileName: "customer.apk", Platform: "Android", Version: "1.2.3", Downloads: 37}
				d := defaultPageDesign(a)
				d.Language, d.Layout = language, layout
				d.Description = "客户填写的介绍，保留原文。"
				d.Screenshots = []string{"https://example.com/screenshot.png"}
				d.DisplayRating, d.DisplayReviewCount = "4.8", "12K"
				for _, preview := range []bool{false, true} {
					var out bytes.Buffer
					if err := downloadPageTemplate(layout).Execute(&out, map[string]any{"App": a, "Design": d, "Preview": preview, "Size": "1.1 MB", "Nonce": "unchanged-request", "Attribution": "unchanged-token", "AdsEnabled": true, "DeepLink": "myapp://open"}); err != nil {
						t.Fatal(err)
					}
					body := html.UnescapeString(out.String())
					for _, want := range []string{`lang="` + language + `"`, d.Copy().DownloadButton, d.Copy().AboutApp, d.Copy().PackageSize, d.Copy().Downloads, d.Copy().AdConsent, d.Copy().OpenApp, "12K " + d.Copy().Reviews, d.Title, d.Description, "customer.apk", "unchanged-request", "unchanged-token", "/a/app-preview/download", "d1-download-shine", "prefers-reduced-motion:reduce"} {
						if !strings.Contains(body, want) {
							t.Fatalf("rendered page missing %q", want)
						}
					}
					for _, removed := range []string{"页面样式参考 Google Play", "安装包由应用发布者提供", "请确认发布者可信后安装", "IPA 需有效签名", "<footer"} {
						if strings.Contains(body, removed) {
							t.Fatalf("obsolete footer still rendered: %s", removed)
						}
					}
					if preview == strings.Contains(body, "/client.js") || preview != strings.Contains(body, "d1-download-button download-preview") {
						t.Fatal("preview/download script isolation changed")
					}
				}
			})
		}
	}
}

func TestPageLanguagePersistsAndPreviewDoesNotPublishOrBill(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 5*Dollar)
	a := seedApp(t, s, u, 128)
	cookie, csrf := sessionFor(t, s, u)
	path := "/api/apps/" + a.ID + "/page-design"
	for _, language := range detailPageLanguages {
		d := defaultPageDesign(a)
		d.Language, d.Description = language, "客户描述保留原文"
		requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 200)
		response := call(t, s, "GET", path, nil, cookie, "")
		requireCode(t, response, 200)
		var saved struct{ Design PageDesign }
		if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil || saved.Design.Language != language {
			t.Fatalf("language not persisted: %s, %v", language, err)
		}
		public := call(t, s, "GET", "/a/"+a.Slug+"/info", nil, nil, "")
		requireCode(t, public, 200)
		if !strings.Contains(html.UnescapeString(public.Body.String()), d.Copy().DownloadButton) {
			t.Fatal("public page does not follow saved language")
		}
		d.Language = "de"
		if language == "de" {
			d.Language = "ja"
		}
		requireCode(t, call(t, s, "POST", path+"/preview", d, cookie, csrf), 200)
		public = call(t, s, "GET", "/a/"+a.Slug+"/info", nil, nil, "")
		if !strings.Contains(public.Body.String(), `lang="`+language+`"`) {
			t.Fatal("preview changed published language")
		}
	}
	for _, language := range []string{"fr", "EN", `en" onload="alert(1)`} {
		d := defaultPageDesign(a)
		d.Language = language
		requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 400)
		requireCode(t, call(t, s, "POST", path+"/preview", d, cookie, csrf), 400)
	}
	if countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Download{}) != 0 || countRows(t, s, &Ledger{}) != 0 {
		t.Fatal("language editing or preview created downloads, reservations or billing")
	}
	if err := s.DB.First(&u, "id = ?", u.ID).Error; err != nil || u.BalanceMicros != 5*Dollar {
		t.Fatal("language editing changed the account balance", err)
	}
}

func TestPageLanguageDefaultsAndCustomButtonEscaping(t *testing.T) {
	d := defaultPageDesign(Application{Name: "Test"})
	d.Language = ""
	if err := validatePageDesign(d); err != nil || d.LanguageCode() != "zh-CN" || d.DownloadButtonText() != "下载应用" {
		t.Fatal("legacy designs without a language must remain compatible", err)
	}
	for _, language := range detailPageLanguages {
		d.Language = language
		d.ButtonText = `<b>客户自定义按钮</b>`
		if d.DownloadButtonText() != d.ButtonText {
			t.Fatal("custom button text was overwritten")
		}
		var out bytes.Buffer
		if err := googlePlayTemplate.Execute(&out, map[string]any{"App": Application{Slug: "test"}, "Design": d, "Preview": true}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "<b>客户") || !strings.Contains(out.String(), "&lt;b&gt;客户") {
			t.Fatal("custom button text bypassed HTML escaping")
		}
	}
}

func TestDownloadButtonStylesMatchCenteredPWAContainer(t *testing.T) {
	for _, required := range []string{"justify-content:center", "margin:20px auto 8px", "min-height:60px", "min-height:56px", "d1-download-shine", "pointer-events:none", "prefers-reduced-motion:reduce"} {
		if !strings.Contains(downloadButtonCSS, required) {
			t.Fatalf("shared button style missing %s", required)
		}
	}
	if strings.Contains(googlePlayCSS, ".install-button{max-width:320px}") || !strings.Contains(googlePlayCSS, "max-width: 480px") {
		t.Fatal("Google Play template no longer matches the PWA card width")
	}
}

// Optional fixtures use the actual server templates at phone and desktop widths.
// Run with D1_PAGE_PREVIEW_DIR pointing to a task-owned temporary directory.
func TestPageLanguageVisualFixtures(t *testing.T) {
	dir := os.Getenv("D1_PAGE_PREVIEW_DIR")
	if dir == "" {
		t.Skip("visual fixture export not requested")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a := Application{Name: "D1 App Preview", Slug: "visual-fixture", Platform: "Android", Version: "1.0.0", FileName: "preview.apk", Downloads: 128}
	for _, language := range detailPageLanguages {
		for _, layout := range []string{"google_play", "store", "minimal"} {
			d := defaultPageDesign(a)
			d.Language, d.Layout = language, layout
			d.Developer, d.Description = "Example Studio", "Your app description stays unchanged when the template language changes."
			var out bytes.Buffer
			if err := downloadPageTemplate(layout).Execute(&out, map[string]any{"App": a, "Design": d, "Preview": true, "Size": "12.8 MB"}); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, layout+"-"+language+".html"), out.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
