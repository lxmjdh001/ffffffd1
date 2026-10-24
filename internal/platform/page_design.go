package platform

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//go:embed templates/download.html
var downloadPageHTML string

//go:embed templates/google-play.html
var googlePlayHTML string

//go:embed templates/google-play.css
var googlePlayCSS string

//go:embed templates/download-button.css
var downloadButtonCSS string

type PageDesign struct {
	Layout               string   `json:"layout"`
	Language             string   `json:"language"`
	Title                string   `json:"title"`
	Developer            string   `json:"developer"`
	Summary              string   `json:"summary"`
	Description          string   `json:"description"`
	Icon                 string   `json:"icon"`
	Screenshots          []string `json:"screenshots"`
	Tags                 []string `json:"tags"`
	ButtonText           string   `json:"button_text"`
	ThemeColor           string   `json:"theme_color"`
	BackgroundColor      string   `json:"background_color"`
	ShowDescription      bool     `json:"show_description"`
	ShowScreenshots      bool     `json:"show_screenshots"`
	ShowFileInfo         bool     `json:"show_file_info"`
	ShowDownloads        bool     `json:"show_downloads"`
	DisplayRating        string   `json:"display_rating"`
	DisplayReviewCount   string   `json:"display_review_count"`
	DisplayDownloadCount string   `json:"display_download_count"`
}
type AppPageDesign struct {
	AppID     string     `json:"-" gorm:"primaryKey;size:32"`
	Design    PageDesign `json:"design" gorm:"serializer:json;type:text"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func defaultPageDesign(a Application) PageDesign {
	return PageDesign{Layout: "google_play", Language: "zh-CN", Title: a.Name, Description: a.Description, ButtonText: "下载应用", ThemeColor: "#01875f", BackgroundColor: "#ffffff", Screenshots: []string{}, Tags: []string{}, ShowDescription: true, ShowScreenshots: true, ShowFileInfo: true, ShowDownloads: true}
}
func (s *Server) pageDesign(a Application) (PageDesign, error) {
	var saved AppPageDesign
	err := s.DB.First(&saved, "app_id = ?", a.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return defaultPageDesign(a), nil
	}
	return saved.Design, err
}

var pageColor = regexp.MustCompile(`^#[a-fA-F0-9]{6}$`)
var pageRating = regexp.MustCompile(`^(?:[0-4](?:\.[0-9])?|5(?:\.0)?)$`)

// Marketing display values never change Application.Downloads or accounting.
// Empty display downloads retain the existing real-counter behavior.
func (d PageDesign) DownloadsForDisplay(real int64) string {
	if custom := strings.TrimSpace(d.DisplayDownloadCount); custom != "" {
		return custom
	}
	return strconv.FormatInt(real, 10)
}

func validPageImage(raw string) bool {
	if raw == "" {
		return true
	}
	if strings.HasPrefix(raw, "data:image/png;base64,") || strings.HasPrefix(raw, "data:image/jpeg;base64,") {
		parts := strings.SplitN(raw, ",", 2)
		b, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return false
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
		return err == nil && cfg.Width > 0 && cfg.Height > 0 && cfg.Width <= 4096 && cfg.Height <= 4096
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && len(raw) <= 2048
}
func validatePageDesign(d PageDesign) error {
	if _, supported := pageLanguages[d.Language]; d.Language != "" && !supported {
		return problem(400, "请选择支持的详情页语言")
	}
	if (d.Layout != "store" && d.Layout != "minimal" && d.Layout != "google_play") || !pageColor.MatchString(d.ThemeColor) || !pageColor.MatchString(d.BackgroundColor) {
		return problem(400, "请选择有效模板和十六进制主题颜色")
	}
	if strings.TrimSpace(d.Title) == "" || len([]rune(d.Title)) > 100 || len([]rune(d.Developer)) > 80 || len([]rune(d.Summary)) > 200 || len([]rune(d.Description)) > 4000 || strings.TrimSpace(d.ButtonText) == "" || len([]rune(d.ButtonText)) > 30 {
		return problem(400, "请检查标题、介绍和按钮文字的长度")
	}
	if d.DisplayRating != "" && !pageRating.MatchString(d.DisplayRating) {
		return problem(400, "展示评分须为 0–5 分，最多一位小数，留空则不显示")
	}
	if len([]rune(d.DisplayReviewCount)) > 20 || len([]rune(d.DisplayDownloadCount)) > 20 {
		return problem(400, "展示评价数和下载量最多 20 字")
	}
	if len(d.Screenshots) > 5 || len(d.Tags) > 6 || !validPageImage(d.Icon) {
		return problem(400, "最多 5 张截图、6 个标签，图片支持 HTTPS 地址或 PNG/JPEG 上传")
	}
	for _, src := range d.Screenshots {
		if src == "" || !validPageImage(src) {
			return problem(400, "截图格式无效，请使用 HTTPS 图片或 PNG/JPEG")
		}
	}
	for _, tag := range d.Tags {
		if len([]rune(tag)) > 20 {
			return problem(400, "每个标签最多 20 字")
		}
	}
	return nil
}
func (s *Server) getPageDesign(w http.ResponseWriter, r *http.Request) {
	a, err := s.ownedApp(r)
	if err != nil {
		fail(w, err)
		return
	}
	d, err := s.pageDesign(a)
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]any{"app": a, "design": d, "defaults": defaultPageDesign(a)})
}
func (s *Server) savePageDesign(w http.ResponseWriter, r *http.Request) {
	var d PageDesign
	if err := decode(w, r, &d); err != nil {
		fail(w, err)
		return
	}
	if err := validatePageDesign(d); err != nil {
		fail(w, err)
		return
	}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, userOf(r).ID); err != nil {
			return err
		}
		var a Application
		if err := tx.First(&a, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Error; err != nil {
			return err
		}
		if err := s.validatePageImageReferences(tx, a.ID, d); err != nil {
			return err
		}
		saved := AppPageDesign{AppID: a.ID, Design: d}
		if err := tx.Save(&saved).Error; err != nil {
			return err
		}
		return recordAudit(tx, userOf(r).ID, "app.page_design", a.ID, "保存应用详情页设计")
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]any{"design": d})
}
func (s *Server) previewPageDesign(w http.ResponseWriter, r *http.Request) {
	a, err := s.ownedApp(r)
	if err != nil {
		fail(w, err)
		return
	}
	var d PageDesign
	if err := decode(w, r, &d); err != nil {
		fail(w, err)
		return
	}
	if err := validatePageDesign(d); err != nil {
		fail(w, err)
		return
	}
	var out bytes.Buffer
	if err := downloadPageTemplate(d.Layout).Execute(&out, map[string]any{"App": a, "Design": d, "Preview": true, "Size": pageDesignSize(a.Size)}); err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]string{"html": out.String()})
}

func pageDesignSize(size int64) string {
	if size < 1<<20 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
}
