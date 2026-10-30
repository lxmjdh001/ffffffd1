package platform

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

func adMode(c AdConfig) string {
	if c.Mode == "quick" {
		return "quick"
	}
	return "deep"
}
func quickAdConversion(c Conversion) bool {
	return c.VerifiedBy == "quick_download_server" || c.VerifiedBy == "quick_pwa_click"
}
func (s *Server) registerAdConfigurations(m *http.ServeMux) {
	m.Handle("GET /api/ad-configurations", s.auth(s.listAdConfigurations))
	m.Handle("POST /api/ad-configurations", s.auth(s.createAdConfiguration))
	m.Handle("GET /api/ad-app-options", s.auth(s.adApplicationOptions))
	m.Handle("POST /api/ad-configurations/{kind}/{id}/enabled", s.auth(s.toggleAdConfiguration))
}

type adConfigurationView struct {
	FacebookPixel string `json:"facebook_pixel"`
	TikTokPixel   string `json:"tiktok_pixel"`
	Kind          string `json:"resource_kind"`
	ID            string `json:"resource_id"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	Mode          string `json:"mode"`
	Enabled       bool   `json:"enabled"`
	Facebook      bool   `json:"facebook_enabled"`
	TikTok        bool   `json:"tiktok_enabled"`
	Configured    bool   `json:"configured"`
}

func (s *Server) adConfigurationViews(r *http.Request, onlyConfigured bool) ([]adConfigurationView, error) {
	var cfgs []AdConfig
	if e := s.DB.Where("user_id = ?", userOf(r).ID).Find(&cfgs).Error; e != nil {
		return nil, e
	}
	byID := map[string]AdConfig{}
	for _, c := range cfgs {
		byID[c.AppID] = c
	}
	views := []adConfigurationView{}
	add := func(kind, id, name, slug, key string) {
		c, ok := byID[key]
		if onlyConfigured && !ok {
			return
		}
		views = append(views, adConfigurationView{Kind: kind, ID: id, Name: name, Slug: slug, Mode: adMode(c), Enabled: !c.Disabled && (c.FacebookEnabled || c.TikTokEnabled), Facebook: c.FacebookEnabled, TikTok: c.TikTokEnabled, Configured: ok, FacebookPixel: c.FacebookPixel, TikTokPixel: c.TikTokPixel})
	}
	var apps []Application
	if e := s.DB.Select("id", "name", "slug").Where("user_id = ?", userOf(r).ID).Order("created_at DESC,id DESC").Find(&apps).Error; e != nil {
		return nil, e
	}
	for _, a := range apps {
		add("apps", a.ID, a.Name, a.Slug, a.ID)
	}
	var pwas []PWAApp
	if e := s.DB.Select("id", "name", "slug").Where("user_id = ?", userOf(r).ID).Order("created_at DESC,id DESC").Find(&pwas).Error; e != nil {
		return nil, e
	}
	for _, a := range pwas {
		add("pwas", a.ID, a.Name, a.Slug, pwaAdID(a.ID))
	}
	return views, nil
}
func (s *Server) listAdConfigurations(w http.ResponseWriter, r *http.Request) {
	v, e := s.adConfigurationViews(r, true)
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]any{"data": v})
}
func (s *Server) adApplicationOptions(w http.ResponseWriter, r *http.Request) {
	v, e := s.adConfigurationViews(r, false)
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]any{"data": v})
}
func (s *Server) createAdConfiguration(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind          string   `json:"resource_kind"`
		ID            string   `json:"resource_id"`
		Config        AdConfig `json:"config"`
		FacebookToken string   `json:"facebook_token"`
		TikTokToken   string   `json:"tiktok_token"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if in.Kind != "apps" && in.Kind != "pwas" {
		fail(w, problem(400, "请选择应用类型"))
		return
	}
	raw, _ := json.Marshal(map[string]any{"config": in.Config, "facebook_token": in.FacebookToken, "tiktok_token": in.TikTokToken})
	request := r.Clone(r.Context())
	u := *r.URL
	request.URL = &u
	request.URL.Path = "/api/" + in.Kind + "/" + in.ID + "/ads"
	request.SetPathValue("id", in.ID)
	request.SetPathValue("ad_create_only", "1")
	request.Body = http.NoBody
	request.Body = io.NopCloser(bytes.NewReader(raw))
	s.saveAds(w, request)
}

func (s *Server) toggleAdConfiguration(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if kind != "apps" && kind != "pwas" {
		http.NotFound(w, r)
		return
	}
	request := r.Clone(r.Context())
	u := *r.URL
	request.URL = &u
	request.URL.Path = "/api/" + kind + "/" + r.PathValue("id") + "/ads"
	app, e := s.ownedAdvertisingApp(request)
	if e != nil {
		fail(w, e)
		return
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if e = decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	var cfg AdConfig
	if e = s.DB.First(&cfg, "app_id = ? AND user_id = ?", app.ID, app.UserID).Error; e != nil {
		fail(w, e)
		return
	}
	if in.Enabled && !cfg.FacebookEnabled && !cfg.TikTokEnabled {
		fail(w, problem(400, "请先配置广告平台"))
		return
	}
	if e = s.DB.Model(&cfg).Update("disabled", !in.Enabled).Error; e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]any{"enabled": in.Enabled})
}
