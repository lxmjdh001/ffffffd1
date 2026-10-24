package platform

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// Exercise language selection and downloading as one flow rather than testing
// the APK page and cloak independently. Both configured hosts belong to the
// same customer; a file ticket must still work only on the host actually chosen.
func TestCloakAPKLocalesAndDownloadStayOnCopiedDomain(t *testing.T) {
	for _, layout := range []string{"google_play", "custom"} {
		for _, host := range []string{"domain-a.example", "domain-b.example"} {
			for _, language := range []struct{ header, code, title, button string }{
				{"en-GB,en;q=0.9", "en-US", "English APK", "Download app"},
				{"zh-CN,zh;q=0.9", "zh-CN", "简体应用", "下载应用"},
				{"zh-TW,zh;q=0.9", "zh-TW", "繁體應用", "下載應用"},
				{"fr-CA,fr;q=0.9", "fr-FR", "Application française", apkLanguageButtons["fr-FR"]},
				{"ja-JP,ja;q=0.9", "en-US", "English APK", "Download app"},
			} {
				t.Run(layout+"/"+host+"/"+language.header, func(t *testing.T) {
					s := testServer(t)
					s.Config.Env = "production"
					owner := seedUser(t, s, Dollar)
					app := seedApp(t, s, owner, 120)
					for _, domain := range []string{"domain-a.example", "domain-b.example"} {
						addChosenDomain(t, s, owner, domain)
					}
					base := defaultPageDesign(app)
					base.Layout = layout
					base.Landing = &APKLandingOptions{TextObfuscation: true}
					var locales []PageDesign
					for _, value := range []struct{ code, title string }{{"zh-CN", "简体应用"}, {"zh-TW", "繁體應用"}, {"en-US", "English APK"}, {"fr-FR", "Application française"}} {
						d := base
						d.Language, d.Title, d.ButtonText = value.code, value.title, apkLanguageButtons[value.code]
						locales = append(locales, d)
					}
					base.Localizations, base.LanguagePriority = locales, []string{"en-US", "zh-CN", "zh-TW", "fr-FR"}
					cookie, csrf := sessionFor(t, s, owner)
					requireCode(t, call(t, s, "PUT", "/api/apps/"+app.ID+"/page-design", base, cookie, csrf), 200)
					body := cloakFixture()
					body["target_type"], body["target_id"], body["app_target_mode"], body["safe_url"] = "app", app.ID, "info", ""
					body["blocked_languages"] = []string{}
					cloak := createTestCloak(t, s, owner, body)
					request := func(method, target, path string, cookies []*http.Cookie, data url.Values) *httptest.ResponseRecorder {
						r := httptest.NewRequest(method, "https://"+target+path, strings.NewReader(data.Encode()))
						r.RemoteAddr = "203.0.113.90:1234"
						r.Header.Set("Accept-Language", language.header)
						if method == "POST" {
							r.Header.Set("Origin", "https://"+target)
							r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
							r.Header.Set("Accept", "application/json")
						}
						for _, c := range cookies {
							r.AddCookie(c)
						}
						w := httptest.NewRecorder()
						s.Handler().ServeHTTP(w, r)
						return w
					}
					page := request("GET", host, "/c/"+cloak.Slug, nil, nil)
					requireCode(t, page, 200)
					visible := regexp.MustCompile(`<span class="d1-text-noise" aria-hidden="true">[a-f0-9]{2}</span>`).ReplaceAllString(page.Body.String(), "")
					if !strings.Contains(page.Body.String(), `class="d1-text-noise"`) || page.Header().Get("Location") != "" || !strings.Contains(page.Header().Get("Vary"), "Accept-Language") || !strings.Contains(visible, language.title) || !strings.Contains(visible, language.button) {
						t.Fatal("cloak did not render the browser's localized APK page in place")
					}
					if !strings.Contains(page.Body.String(), `action="/a/`+app.Slug+`/download"`) {
						t.Fatal("download form no longer uses the current domain")
					}
					if countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Download{}) != 0 {
						t.Fatal("displaying a translated page started a download")
					}
					for _, suffix := range []string{"/landing-config.js", "/api/promotion-links/by-domain"} {
						config := request("GET", host, "/a/"+app.Slug+suffix, nil, nil)
						requireCode(t, config, 200)
						text := config.Body.String()
						if strings.Contains(text, s.Config.PublicURL) || !strings.Contains(text, `"textObfuscation":true`) || !strings.Contains(text, `"s3PublicDomain":"https://`+host+`"`) || !strings.Contains(text, `"language":"`+language.code+`"`) || !strings.Contains(text, language.title) || !strings.Contains(text, language.button) {
							t.Fatal("template runtime changed the selected language or domain", suffix)
						}
					}
					form := url.Values{}
					for _, name := range []string{"request_key", "attribution_token"} {
						match := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(html.UnescapeString(page.Body.String()))
						if len(match) != 2 {
							t.Fatal("download form field missing", name)
						}
						form.Set(name, match[1])
					}
					start := request("POST", host, "/a/"+app.Slug+"/download", page.Result().Cookies(), form)
					requireCode(t, start, 200)
					var answer map[string]string
					if err := json.Unmarshal(start.Body.Bytes(), &answer); err != nil || !strings.HasPrefix(answer["download_url"], "/d/") {
						t.Fatal("download API did not return a relative file URL", err)
					}
					var ticket Ticket
					if err := s.DB.First(&ticket).Error; err != nil || ticket.Host != host {
						t.Fatal("file ticket was not bound to the chosen cloak domain", err)
					}
					other := "domain-a.example"
					if other == host {
						other = "domain-b.example"
					}
					requireCode(t, request("GET", other, answer["download_url"], page.Result().Cookies(), nil), 403)
					file := request("GET", host, answer["download_url"], page.Result().Cookies(), nil)
					requireCode(t, file, 200)
					if file.Header().Get("Location") != "" || file.Body.Len() != int(app.Size) || countRows(t, s, &Download{}) != 1 {
						t.Fatal("localized cloak download left the selected domain or failed completion")
					}
				})
			}
		}
	}
}
