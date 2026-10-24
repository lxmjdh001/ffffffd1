package platform

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
	"gorm.io/gorm"
)

// Templates are reusable drafts. Tasks copy their content and never depend on a
// mutable template when a scheduled delivery is sent.
type PWAPushTemplate struct {
	ID                string         `json:"id" gorm:"primaryKey;size:32"`
	UserID            string         `json:"-" gorm:"uniqueIndex:idx_pwa_push_template_create,priority:1;index;size:32;not null"`
	Name              string         `json:"name" gorm:"size:100;not null"`
	Title             string         `json:"title" gorm:"size:100;not null"`
	Body              string         `json:"body" gorm:"size:2000;not null"`
	URL               string         `json:"url" gorm:"size:2048;not null"`
	Icon              string         `json:"icon" gorm:"size:2048;not null"`
	Image             string         `json:"image" gorm:"size:2048;not null"`
	ActionsJSON       string         `json:"-" gorm:"not null;default:'[]'"`
	CreateKeyHash     string         `json:"-" gorm:"uniqueIndex:idx_pwa_push_template_create,priority:2;size:64;not null"`
	CreateFingerprint string         `json:"-" gorm:"size:64;not null"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	DeletedAt         gorm.DeletedAt `json:"-" gorm:"index"`
}

type pwaPushTemplateInput struct {
	Name    string          `json:"name"`
	Title   string          `json:"title"`
	Body    string          `json:"body"`
	URL     string          `json:"url"`
	Icon    string          `json:"icon"`
	Image   string          `json:"image"`
	Actions []PWAPushAction `json:"actions"`
}

type pwaPushTemplateView struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Title     string          `json:"title"`
	Body      string          `json:"body"`
	URL       string          `json:"url"`
	Icon      string          `json:"icon"`
	Image     string          `json:"image"`
	Actions   []PWAPushAction `json:"actions"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Public image URLs contain random IDs, never an owner identifier or file name.
// Store only normalized PNG data and deduplicate independently for each owner.
type PWAPushAsset struct {
	ObjectKey string    `json:"-" gorm:"size:250;not null;default:''"`
	ID        string    `json:"-" gorm:"primaryKey;size:32"`
	UserID    string    `json:"-" gorm:"uniqueIndex:idx_pwa_push_asset_content,priority:1;index;size:32;not null"`
	SHA256    string    `json:"-" gorm:"uniqueIndex:idx_pwa_push_asset_content,priority:2;size:64;not null"`
	PNG       []byte    `json:"-" gorm:"not null"`
	Width     int       `json:"-" gorm:"not null"`
	Height    int       `json:"-" gorm:"not null"`
	CreatedAt time.Time `json:"-"`
}

func (s *Server) registerPWAPushTemplateRoutes(m *http.ServeMux) {
	m.Handle("GET /api/pwa-push/templates", s.auth(s.listPWAPushTemplates))
	m.Handle("POST /api/pwa-push/templates", s.auth(s.createPWAPushTemplate))
	m.Handle("GET /api/pwa-push/templates/{id}", s.auth(s.getPWAPushTemplate))
	m.Handle("PUT /api/pwa-push/templates/{id}", s.auth(s.updatePWAPushTemplate))
	m.Handle("DELETE /api/pwa-push/templates/{id}", s.auth(s.deletePWAPushTemplate))
	m.Handle("POST /api/pwa-push/assets", s.auth(s.uploadPWAPushAsset))
	m.HandleFunc("GET /pwa-push-assets/{file}", s.publicPWAPushAsset)
}

func pwaPushTemplateViewOf(row PWAPushTemplate) pwaPushTemplateView {
	actions := []PWAPushAction{}
	_ = json.Unmarshal([]byte(row.ActionsJSON), &actions)
	if actions == nil {
		actions = []PWAPushAction{}
	}
	return pwaPushTemplateView{ID: row.ID, Name: row.Name, Title: row.Title, Body: row.Body, URL: row.URL, Icon: row.Icon, Image: row.Image, Actions: actions, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func (s *Server) normalizePWAPushTemplate(in *pwaPushTemplateInput) error {
	in.Name = strings.TrimSpace(in.Name)
	if n := len([]rune(in.Name)); n < 1 || n > 100 || strings.ContainsAny(in.Name, "\x00\r\n") {
		return problem(400, "模板名称须为 1–100 个字符")
	}
	return normalizePWAPushContent(&in.Title, &in.Body, &in.URL, &in.Icon, &in.Image, &in.Actions, s.Config.PublicURL, true)
}

func applyPWAPushTemplate(row *PWAPushTemplate, in pwaPushTemplateInput) {
	row.Name, row.Title, row.Body, row.URL, row.Icon, row.Image = in.Name, in.Title, in.Body, in.URL, in.Icon, in.Image
	actions, _ := json.Marshal(in.Actions)
	row.ActionsJSON = string(actions)
}

func (s *Server) listPWAPushTemplates(w http.ResponseWriter, r *http.Request) {
	page, perPage := pwaPushPage(r)
	q := s.DB.Model(&PWAPushTemplate{}).Where("user_id = ?", userOf(r).ID)
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(search)) > 100 {
		fail(w, problem(400, "搜索内容不能超过 100 个字符"))
		return
	}
	if search != "" {
		// Search text is bound data. Escape SQL wildcards so a literal '%' does
		// not accidentally select every template in a large collection.
		search = strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.ToLower(search))
		pattern := "%" + search + "%"
		q = q.Where("(LOWER(name) LIKE ? ESCAPE '\\' OR LOWER(title) LIKE ? ESCAPE '\\' OR LOWER(body) LIKE ? ESCAPE '\\')", pattern, pattern, pattern)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		fail(w, err)
		return
	}
	rows := []PWAPushTemplate{}
	if err := q.Order("created_at DESC, id DESC").Offset((page - 1) * perPage).Limit(perPage).Find(&rows).Error; err != nil {
		fail(w, err)
		return
	}
	data := make([]pwaPushTemplateView, 0, len(rows))
	for _, row := range rows {
		data = append(data, pwaPushTemplateViewOf(row))
	}
	jsonOut(w, 200, map[string]any{"data": data, "total": total, "page": page, "per_page": perPage})
}

func (s *Server) getPWAPushTemplate(w http.ResponseWriter, r *http.Request) {
	var row PWAPushTemplate
	if err := s.DB.First(&row, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Error; err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, pwaPushTemplateViewOf(row))
}

func (s *Server) createPWAPushTemplate(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) < 16 || len(key) > 128 || strings.ContainsAny(key, "\r\n\t ") {
		fail(w, problem(400, "请提供有效的请求标识"))
		return
	}
	var in pwaPushTemplateInput
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if err := s.normalizePWAPushTemplate(&in); err != nil {
		fail(w, err)
		return
	}
	raw, _ := json.Marshal(in)
	fingerprint := hashString(string(raw))
	var row PWAPushTemplate
	status := http.StatusCreated
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		owner, err := lockUser(tx, userOf(r).ID)
		if err != nil {
			return err
		}
		if owner.Disabled {
			return problem(403, "账户已停用")
		}
		err = tx.Unscoped().First(&row, "user_id = ? AND create_key_hash = ?", owner.ID, hashString(key)).Error
		if err == nil {
			if row.DeletedAt.Valid {
				return problem(409, "原模板已删除，请使用新的请求标识")
			}
			if row.CreateFingerprint != fingerprint {
				return problem(409, "此请求标识已用于另一份模板")
			}
			status = http.StatusOK
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var count int64
		if err = tx.Model(&PWAPushTemplate{}).Where("user_id = ?", owner.ID).Count(&count).Error; err != nil {
			return err
		}
		if count >= 200 {
			return problem(409, "通知模板已达 200 份，请先删除旧模板")
		}
		row = PWAPushTemplate{ID: randomHex(16), UserID: owner.ID, CreateKeyHash: hashString(key), CreateFingerprint: fingerprint, CreatedAt: s.Now(), UpdatedAt: s.Now()}
		applyPWAPushTemplate(&row, in)
		if err = tx.Create(&row).Error; err != nil {
			return err
		}
		return recordAudit(tx, owner.ID, "pwa.push.template.create", row.ID, "")
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, status, pwaPushTemplateViewOf(row))
}

func (s *Server) updatePWAPushTemplate(w http.ResponseWriter, r *http.Request) {
	var in pwaPushTemplateInput
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if err := s.normalizePWAPushTemplate(&in); err != nil {
		fail(w, err)
		return
	}
	var row PWAPushTemplate
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		owner, err := lockUser(tx, userOf(r).ID)
		if err != nil {
			return err
		}
		if owner.Disabled {
			return problem(403, "账户已停用")
		}
		if err = tx.First(&row, "id = ? AND user_id = ?", r.PathValue("id"), owner.ID).Error; err != nil {
			return err
		}
		applyPWAPushTemplate(&row, in)
		row.UpdatedAt = s.Now()
		if err = tx.Save(&row).Error; err != nil {
			return err
		}
		return recordAudit(tx, owner.ID, "pwa.push.template.update", row.ID, "")
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, pwaPushTemplateViewOf(row))
}

func (s *Server) deletePWAPushTemplate(w http.ResponseWriter, r *http.Request) {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		owner, err := lockUser(tx, userOf(r).ID)
		if err != nil {
			return err
		}
		if owner.Disabled {
			return problem(403, "账户已停用")
		}
		var row PWAPushTemplate
		if err = tx.First(&row, "id = ? AND user_id = ?", r.PathValue("id"), owner.ID).Error; err != nil {
			return err
		}
		if err = tx.Delete(&row).Error; err != nil {
			return err
		}
		return recordAudit(tx, owner.ID, "pwa.push.template.delete", row.ID, "")
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

const pwaMaxPushAssetBytes = 2 << 20

func (s *Server) uploadPWAPushAsset(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, pwaMaxPushAssetBytes+(64<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		fail(w, problem(400, "请使用 multipart 上传 PNG、JPEG 或 WebP 图片"))
		return
	}
	var raw []byte
	var kind string
	for n := 0; ; n++ {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil || n >= 2 {
			fail(w, problem(400, "图片上传格式无效"))
			return
		}
		switch part.FormName() {
		case "file":
			if raw != nil || part.FileName() == "" {
				part.Close()
				fail(w, problem(400, "每次仅上传一张图片"))
				return
			}
			raw, e = io.ReadAll(io.LimitReader(part, pwaMaxPushAssetBytes+1))
			if e != nil || len(raw) > pwaMaxPushAssetBytes {
				part.Close()
				fail(w, problem(413, "图片不能超过 2 MiB"))
				return
			}
		case "kind":
			if kind != "" || part.FileName() != "" {
				part.Close()
				fail(w, problem(400, "图片类型无效"))
				return
			}
			v, readErr := io.ReadAll(io.LimitReader(part, 16))
			if readErr != nil {
				part.Close()
				fail(w, problem(400, "图片类型无效"))
				return
			}
			kind = strings.TrimSpace(string(v))
		default:
			part.Close()
			fail(w, problem(400, "图片上传字段无效"))
			return
		}
		part.Close()
	}
	normalized, width, height, err := normalizePWAPushAsset(raw, kind)
	if err != nil {
		fail(w, err)
		return
	}
	var row PWAPushAsset
	objectKey := ""
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		owner, err := lockUser(tx, userOf(r).ID)
		if err != nil {
			return err
		}
		if owner.Disabled {
			return problem(403, "账户已停用")
		}
		digest := hashString(string(normalized))
		err = tx.Omit("png").First(&row, "user_id = ? AND sha256 = ?", owner.ID, digest).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var count int64
		if err = tx.Model(&PWAPushAsset{}).Where("user_id = ?", owner.ID).Count(&count).Error; err != nil {
			return err
		}
		if count >= 500 {
			return problem(409, "通知图片已达 500 张，请复用已有图片")
		}
		row = PWAPushAsset{ID: randomHex(16), UserID: owner.ID, SHA256: digest, PNG: normalized, Width: width, Height: height, CreatedAt: s.Now()}
		if s.Media != nil {
			objectKey, err = s.Media.putPNG(r.Context(), tx, "push-images", normalized)
			if err != nil {
				return err
			}
			row.ObjectKey, row.PNG = objectKey, []byte{}
		}
		if err = tx.Create(&row).Error; err != nil {
			return err
		}
		return recordAudit(tx, owner.ID, "pwa.push.asset.create", row.ID, "")
	})
	if err != nil {
		s.Media.discard(objectKey)
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]any{"url": strings.TrimRight(s.Config.PublicURL, "/") + "/pwa-push-assets/" + row.ID + ".png", "width": row.Width, "height": row.Height})
}

func normalizePWAPushAsset(raw []byte, kind string) ([]byte, int, int, error) {
	invalid := problem(400, "请选择有效的 PNG、JPEG 或 WebP 图片，尺寸不超过 4096 × 4096")
	if kind != "icon" && kind != "image" {
		return nil, 0, 0, problem(400, "图片类型须为 icon 或 image")
	}
	if len(raw) == 0 || len(raw) > pwaMaxPushAssetBytes {
		return nil, 0, 0, invalid
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "png" && format != "jpeg" && format != "webp") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 16_000_000 {
		return nil, 0, 0, invalid
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, invalid
	}
	var out image.Image
	width, height := cfg.Width, cfg.Height
	if kind == "icon" {
		out, width, height = resizePWAIcon(src, 512), 512, 512
	} else {
		if width > 1600 || height > 1600 {
			if width >= height {
				height, width = max(1, height*1600/width), 1600
			} else {
				width, height = max(1, width*1600/height), 1600
			}
		}
		dst := image.NewNRGBA(image.Rect(0, 0, width, height))
		bounds := src.Bounds()
		if bounds.Dx() == width && bounds.Dy() == height {
			draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
		} else {
			for y := range height {
				for x := range width {
					dst.Set(x, y, src.At(bounds.Min.X+x*bounds.Dx()/width, bounds.Min.Y+y*bounds.Dy()/height))
				}
			}
		}
		out = dst
	}
	var encoded bytes.Buffer
	if err = png.Encode(&encoded, out); err != nil {
		return nil, 0, 0, err
	}
	if encoded.Len() > 5<<20 {
		return nil, 0, 0, problem(400, "图片转换后过大，请压缩后重试")
	}
	return encoded.Bytes(), width, height, nil
}

func (s *Server) publicPWAPushAsset(w http.ResponseWriter, r *http.Request) {
	if requestHost(r) != s.Config.Host() {
		http.NotFound(w, r)
		return
	}
	file := r.PathValue("file")
	id := strings.TrimSuffix(file, ".png")
	if len(id) != 32 || file != id+".png" || strings.Trim(id, "0123456789abcdef") != "" {
		http.NotFound(w, r)
		return
	}
	var row PWAPushAsset
	if err := s.DB.Joins("JOIN users ON users.id = pwa_push_assets.user_id AND users.disabled = ?", false).First(&row, "pwa_push_assets.id = ?", id).Error; err != nil {
		http.NotFound(w, r)
		return
	}
	if row.ObjectKey != "" {
		data, err := s.Media.readPNG(r.Context(), row.ObjectKey)
		if err != nil {
			http.Error(w, "Image unavailable", 503)
			return
		}
		row.PNG = data
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("ETag", `"`+row.SHA256+`"`)
	http.ServeContent(w, r, "notification.png", row.CreatedAt, bytes.NewReader(row.PNG))
}
