package platform

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"html/template"
	"net/http"
)

type APKLandingOptions struct {
	TextObfuscation      bool               `json:"text_obfuscation"`
	AgeRating            string             `json:"age_rating"`
	EnableGoogleBadge    bool               `json:"enable_google_badge"`
	EnableOfficialVerify bool               `json:"enable_official_verify"`
	EnableQRCode         bool               `json:"enable_qrcode"`
	StayPromptText       string             `json:"stay_prompt_text"`
	Reviews              []PWALandingReview `json:"reviews"`
	ButtonPosition       string             `json:"button_position"`
	ButtonBgColor        string             `json:"button_bg_color"`
	ButtonTextColor      string             `json:"button_text_color"`
}

func defaultAPKLanding() APKLandingOptions {
	return APKLandingOptions{AgeRating: "unrated", ButtonPosition: "bottom", ButtonBgColor: "#1976D2", ButtonTextColor: "#FFFFFF"}
}
func normalizedAPKLanding(value *APKLandingOptions) APKLandingOptions {
	c := defaultAPKLanding()
	if value != nil {
		c = *value
	}
	if c.AgeRating == "" {
		c.AgeRating = "unrated"
	}
	if c.ButtonPosition == "" {
		c.ButtonPosition = "bottom"
	}
	if c.ButtonBgColor == "" {
		c.ButtonBgColor = "#1976D2"
	}
	if c.ButtonTextColor == "" {
		c.ButtonTextColor = "#FFFFFF"
	}
	return c
}
func validateAPKLanding(value *APKLandingOptions) error {
	if value == nil {
		return nil
	}
	v := normalizedAPKLanding(value)
	if !pwaLandingContains([]string{"unrated", "3", "7", "12", "18"}, v.AgeRating) || !pwaLandingContains([]string{"top", "center", "middle", "bottom"}, v.ButtonPosition) || !pageColor.MatchString(v.ButtonBgColor) || !pageColor.MatchString(v.ButtonTextColor) || len([]rune(v.StayPromptText)) > 500 || len(v.Reviews) > 100 {
		return problem(400, "PWA 落地页配置无效")
	}
	for _, r := range v.Reviews {
		if len([]rune(r.Nickname)) > 50 || len(r.Comment) > 20000 || r.Rating < 1 || r.Rating > 5 {
			return problem(400, "PWA 落地页配置无效")
		}
	}
	return nil
}
func (s *Server) apkLandingRuntime(a Application, d PageDesign) map[string]any {
	c := normalizedAPKLanding(d.Landing)
	screens := []string{}
	if d.ShowScreenshots {
		screens = d.Screenshots
	}
	summary := ""
	if d.ShowDescription {
		summary = d.Description
		if d.Summary != "" {
			summary = d.Summary + "\n\n" + summary
		}
	}
	lang := PWALandingLanguage{Language: d.LanguageCode(), AppName: d.Title, DeveloperName: d.Developer, AppIcon: d.Icon, DownloadCount: d.DownloadsForDisplay(a.Downloads), AgeRating: c.AgeRating, ReviewCount: d.DisplayReviewCount, AppSummary: summary, StayPromptText: c.StayPromptText, DetailImages: screens, Tags: d.Tags, Reviews: c.Reviews, CustomImages: d.Screenshots, ShowInstallButton: true, ButtonText: d.DownloadButtonText(), ButtonPosition: c.ButtonPosition, ButtonBgColor: c.ButtonBgColor, ButtonTextColor: c.ButtonTextColor, ShowQRCode: c.EnableQRCode}
	settings := map[string]any{"packagePriority": "pwa", "enableIframeEmbed": true, "enableQrcode": c.EnableQRCode, "returnFlowConfig": d.ReturnFlowConfig}
	app := map[string]any{"id": a.ID, "appIcon": d.Icon, "appName": d.Title, "enableGoogleBadge": c.EnableGoogleBadge, "enableOfficialVerify": c.EnableOfficialVerify, "storeInfo": lang, "settings": settings}
	custom := map[string]any{"id": a.ID, "appIcon": d.Icon, "appName": d.Title, "setting": lang, "otherSettings": settings, "startupBgColor": d.BackgroundColor, "startupThemeColor": d.ThemeColor}
	return map[string]any{"app": app, "customApp": custom, "apiBaseUrl": "/a/" + a.Slug, "s3PublicDomain": s.Config.PublicURL, "d1APK": map[string]any{"textObfuscation": c.TextObfuscation, "buttonText": d.DownloadButtonText(), "rating": d.DisplayRating, "showDownloads": d.ShowDownloads, "showDescription": d.ShowDescription, "showScreenshots": d.ShowScreenshots, "showFileInfo": d.ShowFileInfo, "themeColor": d.ThemeColor, "backgroundColor": d.BackgroundColor}}
}
func (s *Server) apkLandingConfigData(w http.ResponseWriter, r *http.Request) (map[string]any, error) {
	a, e := s.publicApp(r)
	if e != nil {
		return nil, e
	}
	if s.rejectBlockedUA(w, r, a.UserID, r.UserAgent()) {
		return nil, nil
	}
	d, e := s.pageDesign(a)
	if e != nil {
		return nil, e
	}
	requested := r.URL.Query().Get("language")
	if requested == "" {
		requested = r.Header.Get("Accept-Language")
	}
	d = d.localized(requested)
	w.Header().Add("Vary", "Accept-Language")
	runtime := s.apkLandingRuntime(a, d)
	// Relative template/media paths must resolve against the visitor's chosen
	// download domain, including when the page was rendered by a cloak.
	runtime["s3PublicDomain"] = s.pwaRequestOrigin(r)
	return runtime, nil
}
func (s *Server) apkLandingConfig(w http.ResponseWriter, r *http.Request) {
	v, e := s.apkLandingConfigData(w, r)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	if v == nil {
		return
	}
	raw, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(append([]byte("window.__APP_CONFIG__ = "), append(raw, ';')...))
}
func (s *Server) apkLandingCustomConfig(w http.ResponseWriter, r *http.Request) {
	v, e := s.apkLandingConfigData(w, r)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	if v == nil {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonOut(w, 200, map[string]any{"code": 0, "data": v})
}
func (s *Server) renderAPKLanding(a Application, d PageDesign, nonce, attribution, viewToken string, deepLink template.URL, ads bool, out *bytes.Buffer) error {
	layout := d.Layout
	if layout == "google_play" {
		layout = "google_play"
	} else {
		layout = "custom"
	}
	raw, _ := pwaLandingFiles.ReadFile("templates/pwa-landing/.vite/manifest.json")
	var entries map[string]struct {
		File string   `json:"file"`
		CSS  []string `json:"css"`
	}
	if e := json.Unmarshal(raw, &entries); e != nil {
		return e
	}
	key := "google-play/src/main.ts"
	if layout == "custom" {
		key = "custom-app/src/main.ts"
	}
	entry := entries[key]
	decoration := s.newLandingDecoration("apk", a.UserID)
	v := map[string]any{"App": a, "Design": d, "JS": entry.File, "CSS": entry.CSS, "Nonce": nonce, "Attribution": attribution, "DeepLink": deepLink, "AdsEnabled": ads, "ReturnFlowToken": viewToken, "Size": pageDesignSize(a.Size), "Decoration": decoration, "APKStyle": apkReferenceStyles(decoration.Class)}
	v["JSURL"] = s.landingModuleURL(entry.File)
	v["ScriptType"] = s.landingScriptType()
	v["ClientJS"] = s.landingScriptURL("apk-client.js", "/a/"+a.Slug+"/client.js")
	return apkReferenceHTML.Execute(out, v)
}

//go:embed templates/apk-landing-bridge.js
var apkLandingBridgeJS string

//go:embed templates/apk-landing.css
var apkLandingCSS string

//go:embed templates/apk-text-obfuscation.css
var apkTextObfuscationCSS string

//go:embed templates/apk-landing.html
var apkLandingHTML string
var apkReferenceHTML = template.Must(template.New("apk-reference").Funcs(pageTemplateFunctions).Parse(apkLandingHTML))
