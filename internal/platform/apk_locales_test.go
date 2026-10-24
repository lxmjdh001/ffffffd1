package platform

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPKMultipleLanguagesMatchPWAAndPreserveContent(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	app := seedApp(t, s, owner, 100)
	cookie, csrf := sessionFor(t, s, owner)
	zh := defaultPageDesign(app)
	zh.Language = "zh-CN"
	zh.Title = "中文应用"
	zh.Description = "中文说明"
	en := zh
	en.Language = "en-US"
	en.Title = "English app"
	en.Description = "English description"
	en.ButtonText = "Download app"
	fr := en
	fr.Language = "fr-FR"
	fr.Title = "Application française"
	fr.Description = "Description française"
	fr.ButtonText = apkLanguageButtons["fr-FR"]
	d := zh
	d.Localizations = []PageDesign{zh, en, fr}
	d.LanguagePriority = []string{"en-US", "fr-FR", "zh-CN"}
	flow := defaultReturnFlowConfig()
	flow.Enabled = true
	d.ReturnFlowConfig = &flow
	if localized := d.localized("en-GB"); localized.ReturnFlowConfig == nil || !localized.ReturnFlowConfig.Enabled {
		t.Fatal("language selection changed the shared return-flow switch")
	}
	path := "/api/apps/" + app.ID + "/page-design"
	requireCode(t, call(t, s, "PUT", path, d, cookie, csrf), 200)
	read := call(t, s, "GET", path, nil, cookie, "")
	requireCode(t, read, 200)
	var got struct {
		Design PageDesign `json:"design"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Design.Localizations) != 3 || got.Design.Localizations[0].Title != zh.Title || got.Design.Localizations[1].Title != en.Title {
		t.Fatal("localized content lost")
	}
	for _, tc := range []struct{ language, title string }{{"zh-CN", zh.Title}, {"en-GB", en.Title}, {"fr-CA", fr.Title}, {"ja-JP", en.Title}} {
		for _, suffix := range []string{"/info", "/landing-config.js"} {
			r := httptest.NewRequest("GET", s.Config.PublicURL+"/a/"+app.Slug+suffix, nil)
			r.Header.Set("Accept-Language", tc.language)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			requireCode(t, w, 200)
			if !strings.Contains(w.Body.String(), tc.title) || !strings.Contains(w.Header().Get("Vary"), "Accept-Language") {
				t.Fatalf("wrong language %s: %s", tc.language, w.Body.String())
			}
		}
	}
	bad := d
	bad.Localizations = append(append([]PageDesign{}, d.Localizations...), en)
	requireCode(t, call(t, s, "PUT", path, bad, cookie, csrf), 400)
	bad = d
	bad.Localizations = append([]PageDesign{}, d.Localizations...)
	bad.Localizations[1].Localizations = []PageDesign{en}
	requireCode(t, call(t, s, "PUT", path, bad, cookie, csrf), 400)
	if countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Download{}) != 0 {
		t.Fatal("language render changed download accounting")
	}
}
