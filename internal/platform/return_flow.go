package platform

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

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

// Normalize button destinations exactly like the reference PWA. URL validation
// remains on the API boundary; no destination is fetched by the server.
func returnFlowButtonURL(raw, publicURL string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(raw), "https://") || strings.HasPrefix(strings.ToLower(raw), "http://") {
		return raw
	}
	origin, _ := url.Parse(publicURL)
	scheme := "https"
	if origin != nil && origin.Scheme != "" {
		scheme = origin.Scheme
	}
	if strings.HasPrefix(raw, "//") {
		return scheme + ":" + raw
	}
	if strings.HasPrefix(raw, "/") {
		if origin != nil && origin.Host != "" {
			u, e := url.Parse(raw)
			if e == nil {
				return origin.ResolveReference(u).String()
			}
		}
		return raw
	}
	return "https://" + strings.TrimLeft(raw, "/")
}
func validReturnFlowURL(raw, publicURL string) bool {
	if len(raw) > 2048 || strings.ContainsAny(raw, "\\\x00\r\n\t") {
		return false
	}
	candidate := returnFlowButtonURL(raw, publicURL)
	if strings.HasPrefix(candidate, "/") && !strings.HasPrefix(candidate, "//") {
		u, e := url.Parse(candidate)
		return e == nil && u.Host == "" && u.Scheme == ""
	}
	u, e := url.Parse(candidate)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Opaque == ""
}
func normalizeReturnFlow(c *ReturnFlowConfig, publicURL string) error {
	if c.TriggerMode != "back" && c.TriggerMode != "resume" && c.TriggerMode != "both" {
		c.TriggerMode = "back"
	}
	if c.ButtonAction != "continue" && c.ButtonAction != "download" && c.ButtonAction != "url" {
		c.ButtonAction = "continue"
	}
	if c.MediaAreaPercent == 0 {
		c.MediaAreaPercent = 50
	}
	c.MediaAreaPercent = min(100, max(50, c.MediaAreaPercent))
	if c.MediaURL != "" && !validReturnFlowURL(c.MediaURL, publicURL) {
		return problem(400, "回流素材地址无效")
	}
	if c.ButtonURL != "" && !validReturnFlowURL(c.ButtonURL, publicURL) {
		return problem(400, "回流按钮跳转地址无效")
	}
	// Empty labels, descriptions and URLs keep the reference's runtime defaults.
	// In particular a URL action without a URL simply closes the modal.
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
