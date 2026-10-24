package platform

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
)

// Field names intentionally match the reference PWA's returnFlowConfig.
type ReturnFlowConfig struct {
	Enabled                  bool   `json:"enabled"`
	TriggerMode              string `json:"triggerMode"`
	ShowOnce                 bool   `json:"showOnce"`
	ShowOnDownloadRiskCancel bool   `json:"showOnDownloadRiskCancel"`
	MediaURL                 string `json:"mediaUrl"`
	MediaAreaPercent         int    `json:"mediaAreaPercent"`
	BadgeText                string `json:"badgeText"`
	Title                    string `json:"title"`
	Description              string `json:"description"`
	ButtonText               string `json:"buttonText"`
	DismissText              string `json:"dismissText"`
	ButtonAction             string `json:"buttonAction"`
	ButtonURL                string `json:"buttonUrl"`
}

func defaultReturnFlowConfig() ReturnFlowConfig {
	return ReturnFlowConfig{TriggerMode: "back", ShowOnce: true, MediaAreaPercent: 50, ButtonAction: "continue"}
}
func (c *ReturnFlowConfig) UnmarshalJSON(data []byte) error {
	type config ReturnFlowConfig
	next := config(defaultReturnFlowConfig())
	if err := json.Unmarshal(data, &next); err != nil {
		return err
	}
	*c = ReturnFlowConfig(next)
	return nil
}
func storedReturnFlow(raw string) ReturnFlowConfig {
	c := defaultReturnFlowConfig()
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	return c
}
func returnFlowJSON(c *ReturnFlowConfig) string {
	if c == nil {
		return "{}"
	}
	data, _ := json.Marshal(c)
	return string(data)
}
func (d PageDesign) ReturnFlowJSON() string { return returnFlowJSON(d.ReturnFlowConfig) }
func validReturnFlowURL(raw, publicURL string) bool {
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		u, e := url.Parse(raw)
		return e == nil && u.Host == "" && u.Scheme == "" && len(raw) <= 2048 && !strings.ContainsAny(raw, "\\\x00\r\n")
	}
	return validPWAPushContentURL(raw, publicURL) && !strings.Contains(raw, "\\")
}
func normalizeReturnFlow(c *ReturnFlowConfig, publicURL string) error {
	if c.TriggerMode == "" {
		c.TriggerMode = "back"
	}
	if c.ButtonAction == "" {
		c.ButtonAction = "continue"
	}
	if c.MediaAreaPercent == 0 {
		c.MediaAreaPercent = 50
	}
	if c.TriggerMode != "back" && c.TriggerMode != "resume" && c.TriggerMode != "both" {
		return problem(400, "请选择有效的回流触发方式")
	}
	if c.ButtonAction != "continue" && c.ButtonAction != "download" && c.ButtonAction != "url" {
		return problem(400, "请选择有效的回流按钮动作")
	}
	if c.MediaAreaPercent < 50 || c.MediaAreaPercent > 100 {
		return problem(400, "回流素材区域占比须为 50–100")
	}
	fields := []struct {
		value *string
		limit int
	}{{&c.BadgeText, 80}, {&c.Title, 100}, {&c.Description, 1000}, {&c.ButtonText, 40}, {&c.DismissText, 40}}
	for _, f := range fields {
		*f.value = strings.TrimSpace(*f.value)
		if utf8.RuneCountInString(*f.value) > f.limit || strings.ContainsRune(*f.value, '\x00') {
			return problem(400, "请检查回流弹窗文案长度")
		}
	}
	c.MediaURL = strings.TrimSpace(c.MediaURL)
	c.ButtonURL = strings.TrimSpace(c.ButtonURL)
	if c.MediaURL != "" && !validReturnFlowURL(c.MediaURL, publicURL) {
		return problem(400, "回流素材须为有效的 HTTPS 地址")
	}
	if c.ButtonURL != "" && !validReturnFlowURL(c.ButtonURL, publicURL) {
		return problem(400, "回流按钮须填写有效的 HTTPS 地址")
	}
	for _, value := range []*string{&c.MediaURL, &c.ButtonURL} {
		if *value != "" {
			u, _ := url.Parse(*value)
			*value = u.String()
		}
	}
	if c.Enabled && c.ButtonAction == "url" && c.ButtonURL == "" {
		return problem(400, "请填写回流按钮跳转地址")
	}
	return nil
}
func (s *Server) validateReturnFlowAsset(tx *gorm.DB, owner string, c *ReturnFlowConfig) error {
	if c == nil || c.MediaURL == "" {
		return nil
	}
	u, err := url.Parse(c.MediaURL)
	if err != nil {
		return problem(400, "回流素材地址无效")
	}
	own := u.Host == "" || strings.EqualFold(u.Hostname(), s.Config.Host())
	if !own || !strings.HasPrefix(u.Path, "/return-flow-assets/") {
		return nil
	}
	var asset ReturnFlowAsset
	if err = tx.First(&asset, "user_id = ? AND public_path = ?", owner, u.Path).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return problem(400, "回流素材不存在或不属于当前账户")
		}
		return err
	}
	return nil
}
func (s *Server) registerReturnFlowRoutes(m *http.ServeMux) {
	m.Handle("POST /api/return-flow/assets", s.auth(s.uploadReturnFlowAsset))
	m.HandleFunc("GET /return-flow-assets/{file}", s.publicReturnFlowAsset)
}

//go:embed templates/return-flow.js
var returnFlowJS string

//go:embed templates/return-flow.css
var returnFlowCSS string
