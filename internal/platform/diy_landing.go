package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxDIYHTMLBytes    = 5 << 20
	maxDIYPageCount    = 200
	maxDIYStorageBytes = 100 << 20
)

// References are projected into application DTOs. The binding table is the
// authority, so replacing an application file cannot overwrite its page.
type DIYLandingReference struct {
	PageID     string            `json:"page_id"`
	RevisionID string            `json:"revision_id"`
	Content    DIYLandingContent `json:"content,omitempty"`
}

type DIYLandingOptions struct {
	Targets         []string `json:"targets"`
	PrimarySelector string   `json:"primary_selector"`
	PlatformButton  bool     `json:"platform_button"`
	ResourceBase    string   `json:"resource_base"`
}

type DIYLandingPage struct {
	ID                        string            `json:"id" gorm:"primaryKey;size:32"`
	UserID                    string            `json:"-" gorm:"index;size:32;not null"`
	Name                      string            `json:"name" gorm:"size:100;not null"`
	DraftHTML                 string            `json:"draft_html" gorm:"type:text;not null"`
	DraftConfig               DIYLandingOptions `json:"draft_config" gorm:"serializer:json;type:text;not null"`
	DraftVersion              int64             `json:"draft_version" gorm:"not null"`
	DraftBytes                int64             `json:"-" gorm:"not null"`
	LatestPublishedRevisionID string            `json:"latest_published_revision_id" gorm:"size:32;not null;default:''"`
	ArchivedAt                *time.Time        `json:"archived_at"`
	CreatedAt                 time.Time         `json:"created_at"`
	UpdatedAt                 time.Time         `json:"updated_at"`
}

// Published HTML and its processing contract are immutable. Updating a draft
// or publishing another revision never changes an application's bound version.
type DIYLandingRevision struct {
	ID                     string            `json:"id" gorm:"primaryKey;size:32"`
	PageID                 string            `json:"page_id" gorm:"uniqueIndex:idx_diy_revision_number,priority:1;uniqueIndex:idx_diy_publish_key,priority:1;index;size:32;not null"`
	UserID                 string            `json:"-" gorm:"index;size:32;not null"`
	RevisionNo             int64             `json:"revision_no" gorm:"uniqueIndex:idx_diy_revision_number,priority:2;not null"`
	RawHTML                string            `json:"raw_html" gorm:"type:text;not null"`
	RenderHTML             string            `json:"render_html" gorm:"type:text;not null"`
	Config                 DIYLandingOptions `json:"config" gorm:"serializer:json;type:text;not null"`
	ContentHash            string            `json:"content_hash" gorm:"size:64;not null"`
	RuntimeContractVersion int               `json:"runtime_contract_version" gorm:"not null"`
	SourceDraftVersion     int64             `json:"source_draft_version" gorm:"not null"`
	PublishKeyHash         string            `json:"-" gorm:"uniqueIndex:idx_diy_publish_key,priority:2;size:64;not null"`
	StorageBytes           int64             `json:"-" gorm:"not null"`
	PublishedAt            time.Time         `json:"published_at"`
	Page                   DIYLandingPage    `json:"-" gorm:"foreignKey:PageID;constraint:OnDelete:RESTRICT"`
}

type DIYLandingBinding struct {
	ResourceType string             `json:"resource_type" gorm:"primaryKey;size:8"`
	ResourceID   string             `json:"resource_id" gorm:"primaryKey;size:32"`
	UserID       string             `json:"-" gorm:"index;size:32;not null"`
	PageID       string             `json:"page_id" gorm:"index;size:32;not null"`
	RevisionID   string             `json:"revision_id" gorm:"index;size:32;not null"`
	Content      DIYLandingContent  `json:"content,omitempty" gorm:"serializer:json;type:text;not null;default:'{}'"`
	UpdatedAt    time.Time          `json:"updated_at"`
	Page         DIYLandingPage     `json:"-" gorm:"foreignKey:PageID;constraint:OnDelete:RESTRICT"`
	Revision     DIYLandingRevision `json:"-" gorm:"foreignKey:RevisionID;constraint:OnDelete:RESTRICT"`
}

type diyLandingInput struct {
	Name         string             `json:"name"`
	HTML         *string            `json:"html,omitempty"`
	Config       *DIYLandingOptions `json:"config,omitempty"`
	DraftHTML    string             `json:"draft_html"`
	DraftConfig  DIYLandingOptions  `json:"draft_config"`
	DraftVersion int64              `json:"draft_version"`
}

type diyLandingSummary struct {
	ID                        string            `json:"id"`
	Name                      string            `json:"name"`
	DraftConfig               DIYLandingOptions `json:"draft_config"`
	DraftVersion              int64             `json:"draft_version"`
	LatestPublishedRevisionID string            `json:"latest_published_revision_id"`
	LatestRevisionNo          int64             `json:"latest_revision_no"`
	BindingCount              int64             `json:"binding_count"`
	ArchivedAt                *time.Time        `json:"archived_at"`
	CreatedAt                 time.Time         `json:"created_at"`
	UpdatedAt                 time.Time         `json:"updated_at"`
}

type diyLandingDetail struct {
	DIYLandingPage
	Revisions    []DIYLandingRevision `json:"revisions"`
	BindingCount int64                `json:"binding_count"`
}

func (s *Server) registerDIYLandingRoutes(m *http.ServeMux) {
	m.Handle("GET /api/diy-landings", s.diyAuth(s.listDIYLandings))
	m.Handle("POST /api/diy-landings", s.diyAuth(s.createDIYLanding))
	m.Handle("POST /api/diy-landings/preview", s.diyAuth(s.previewDIYLanding))
	m.Handle("POST /api/diy-landings/images", s.diyAuth(s.uploadDIYLandingImage))
	m.Handle("GET /api/diy-landing-preview/{preview}", s.diyAuth(s.serveDIYPreview))
	m.Handle("GET /api/diy-landings/{id}", s.diyAuth(s.getDIYLanding))
	m.Handle("PUT /api/diy-landings/{id}", s.diyAuth(s.updateDIYLanding))
	m.Handle("POST /api/diy-landings/{id}/publish", s.diyAuth(s.publishDIYLanding))
	m.Handle("POST /api/diy-landings/{id}/copy", s.diyAuth(s.copyDIYLanding))
	m.Handle("PATCH /api/diy-landings/{id}/archive", s.diyAuth(s.archiveDIYLanding))
	m.Handle("GET /api/diy-landings/{id}/bindings", s.diyAuth(s.listDIYBindings))
	m.Handle("GET /api/diy-landings/{id}/revisions", s.diyAuth(s.listDIYRevisions))
	m.Handle("GET /api/diy-landings/{id}/revisions/{revision}", s.diyAuth(s.getDIYRevision))
	m.Handle("GET /api/diy-landings/{id}/revisions/{revision}/content-fields", s.diyAuth(s.getDIYContentFields))
	m.Handle("DELETE /api/diy-landings/{id}/revisions/{revision}", s.diyAuth(s.deleteDIYRevision))
	m.Handle("DELETE /api/diy-landings/{id}", s.diyAuth(s.deleteDIYLanding))
}

func (s *Server) diyAuth(fn http.HandlerFunc) http.Handler {
	return s.auth(func(w http.ResponseWriter, r *http.Request) {
		if s.Config.DisableDIYLanding && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodDelete {
			fail(w, problem(503, "DIY 落地页暂时停用，请稍后重试"))
			return
		}
		fn(w, r)
	})
}

func decodeDIY(w http.ResponseWriter, r *http.Request, out any) error {
	return decodeDIYLimit(w, r, out, 32<<20)
}

// Application forms contain up to 8 MiB of content overrides plus their
// original 1 MiB configuration allowance, without accepting source documents.
func decodeDIYApplication(w http.ResponseWriter, r *http.Request, out any) error {
	return decodeDIYLimit(w, r, out, 9<<20)
}

func decodeDIYLimit(w http.ResponseWriter, r *http.Request, out any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return problem(400, "落地页请求格式不正确或内容过大")
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return problem(400, "落地页请求格式不正确")
	}
	return nil
}

func normalizeDIYOptions(c *DIYLandingOptions) error {
	if len(c.Targets) == 0 {
		c.Targets = []string{"apk", "pwa"}
	}
	if len(c.Targets) > 2 {
		return problem(400, "请选择 APK 或 PWA 适用类型")
	}
	seen := map[string]bool{}
	for _, v := range c.Targets {
		if (v != "apk" && v != "pwa") || seen[v] {
			return problem(400, "请选择 APK 或 PWA 适用类型")
		}
		seen[v] = true
	}
	c.PrimarySelector = strings.TrimSpace(c.PrimarySelector)
	if len(c.PrimarySelector) > 512 || strings.ContainsAny(c.PrimarySelector, "\x00\r\n") {
		return problem(400, "主按钮选择器无效或超过 512 字节")
	}
	c.ResourceBase = strings.TrimSpace(c.ResourceBase)
	if c.ResourceBase != "" {
		u, err := url.Parse(c.ResourceBase)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(c.ResourceBase) > 2048 {
			return problem(400, "资源基址须为有效 HTTPS 地址")
		}
	}
	return nil
}

func validateDIYDraft(in *diyLandingInput) error {
	if in.HTML != nil {
		in.DraftHTML = *in.HTML
	}
	if in.Config != nil {
		in.DraftConfig = *in.Config
	}
	in.Name = strings.TrimSpace(in.Name)
	if n := len([]rune(in.Name)); n < 1 || n > 100 || strings.ContainsAny(in.Name, "\x00\r\n") {
		return problem(400, "落地页名称须为 1–100 个字符")
	}
	if len(in.DraftHTML) > maxDIYHTMLBytes {
		return problem(400, "落地页 HTML 最多 5 MiB")
	}
	if strings.ContainsRune(in.DraftHTML, '\x00') {
		return problem(400, "落地页代码不能包含空字符")
	}
	return normalizeDIYOptions(&in.DraftConfig)
}

func diyDraftBytes(raw string, options DIYLandingOptions) int64 {
	b, _ := json.Marshal(options)
	return int64(len(raw) + len(b))
}

func checkDIYQuota(tx *gorm.DB, owner string, additional int64, creating bool) error {
	var count int64
	if err := tx.Model(&DIYLandingPage{}).Where("user_id = ?", owner).Count(&count).Error; err != nil {
		return err
	}
	if creating && count >= maxDIYPageCount {
		return problem(409, "每个账号最多保存 200 个 DIY 落地页")
	}
	var drafts, revisions int64
	if err := tx.Model(&DIYLandingPage{}).Where("user_id = ?", owner).Select("COALESCE(SUM(draft_bytes), 0)").Scan(&drafts).Error; err != nil {
		return err
	}
	if err := tx.Model(&DIYLandingRevision{}).Where("user_id = ?", owner).Select("COALESCE(SUM(storage_bytes), 0)").Scan(&revisions).Error; err != nil {
		return err
	}
	if drafts+revisions+additional > maxDIYStorageBytes {
		return problem(409, "DIY 落地页源码总容量已达 100 MiB，请清理未引用历史版本")
	}
	return nil
}

func (s *Server) diyDetail(owner, id string) (diyLandingDetail, error) {
	var out diyLandingDetail
	if err := s.DB.First(&out.DIYLandingPage, "id = ? AND user_id = ?", id, owner).Error; err != nil {
		return out, err
	}
	out.Revisions = []DIYLandingRevision{}
	if err := s.DB.Omit("raw_html", "render_html").Where("page_id = ? AND user_id = ?", id, owner).Order("revision_no DESC").Find(&out.Revisions).Error; err != nil {
		return out, err
	}
	if err := s.DB.Model(&DIYLandingBinding{}).Where("page_id = ? AND user_id = ?", id, owner).Count(&out.BindingCount).Error; err != nil {
		return out, err
	}
	return out, nil
}

func (s *Server) writeDIYDetail(w http.ResponseWriter, status int, owner, id string) {
	out, err := s.diyDetail(owner, id)
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, status, out)
}

func (s *Server) listDIYLandings(w http.ResponseWriter, r *http.Request) {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if p < 1 {
		p = 1
	}
	if p > 100000 {
		p = 100000
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if n < 1 {
		n = 20
	}
	if n > maxDIYPageCount {
		n = maxDIYPageCount
	}
	q := s.DB.Model(&DIYLandingPage{}).Where("user_id = ?", userOf(r).ID)
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(search)) > 100 {
		fail(w, problem(400, "搜索内容最多 100 字"))
		return
	}
	if search != "" {
		search = strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.ToLower(search))
		q = q.Where("LOWER(name) LIKE ? ESCAPE '\\'", "%"+search+"%")
	}
	if r.URL.Query().Get("published") == "1" {
		q = q.Where("latest_published_revision_id <> '' AND archived_at IS NULL")
	}
	switch r.URL.Query().Get("status") {
	case "archived":
		q = q.Where("archived_at IS NOT NULL")
	case "draft":
		q = q.Where("latest_published_revision_id = '' AND archived_at IS NULL")
	case "published":
		q = q.Where("latest_published_revision_id <> '' AND archived_at IS NULL")
	}
	target := r.URL.Query().Get("target")
	if target != "" && target != "apk" && target != "pwa" {
		fail(w, problem(400, "适用类型无效"))
		return
	}
	// Decode only small config values to remain portable between SQLite and
	// PostgreSQL, without depending on database-specific JSON operators.
	rows := []DIYLandingPage{}
	if err := q.Select("id", "name", "draft_config", "draft_version", "latest_published_revision_id", "archived_at", "created_at", "updated_at").Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		fail(w, err)
		return
	}
	data := []diyLandingSummary{}
	for _, row := range rows {
		var rev DIYLandingRevision
		config := row.DraftConfig
		if row.LatestPublishedRevisionID != "" {
			if err := s.DB.Select("id", "revision_no", "config").First(&rev, "id = ? AND page_id = ?", row.LatestPublishedRevisionID, row.ID).Error; err != nil {
				fail(w, err)
				return
			}
			if r.URL.Query().Get("published") == "1" {
				config = rev.Config
			}
		}
		if target != "" && !diySupports(config, target) {
			continue
		}
		var bindings int64
		if err := s.DB.Model(&DIYLandingBinding{}).Where("page_id = ? AND user_id = ?", row.ID, userOf(r).ID).Count(&bindings).Error; err != nil {
			fail(w, err)
			return
		}
		data = append(data, diyLandingSummary{ID: row.ID, Name: row.Name, DraftConfig: config, DraftVersion: row.DraftVersion, LatestPublishedRevisionID: row.LatestPublishedRevisionID, LatestRevisionNo: rev.RevisionNo, BindingCount: bindings, ArchivedAt: row.ArchivedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt})
	}
	total := len(data)
	start := min(total, (p-1)*n)
	jsonOut(w, 200, map[string]any{"data": data[start:min(total, start+n)], "total": total, "page": p, "per_page": n})
}

func (s *Server) getDIYLanding(w http.ResponseWriter, r *http.Request) {
	s.writeDIYDetail(w, 200, userOf(r).ID, r.PathValue("id"))
}

func (s *Server) createDIYLanding(w http.ResponseWriter, r *http.Request) {
	var in diyLandingInput
	if err := decodeDIY(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if err := validateDIYDraft(&in); err != nil {
		fail(w, err)
		return
	}
	owner := userOf(r).ID
	row := DIYLandingPage{ID: randomHex(16), UserID: owner, Name: in.Name, DraftHTML: in.DraftHTML, DraftConfig: in.DraftConfig, DraftVersion: 1, DraftBytes: diyDraftBytes(in.DraftHTML, in.DraftConfig), CreatedAt: s.Now(), UpdatedAt: s.Now()}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, owner); err != nil {
			return err
		}
		if err := checkDIYQuota(tx, owner, row.DraftBytes, true); err != nil {
			return err
		}
		return tx.Create(&row).Error
	})
	if err != nil {
		fail(w, err)
		return
	}
	s.writeDIYDetail(w, 201, owner, row.ID)
}

func (s *Server) updateDIYLanding(w http.ResponseWriter, r *http.Request) {
	var in diyLandingInput
	if err := decodeDIY(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if err := validateDIYDraft(&in); err != nil {
		fail(w, err)
		return
	}
	owner, id := userOf(r).ID, r.PathValue("id")
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, owner); err != nil {
			return err
		}
		var row DIYLandingPage
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ? AND user_id = ?", id, owner).Error; err != nil {
			return err
		}
		if in.DraftVersion != row.DraftVersion {
			return problem(409, "落地页已在其他窗口更新，请刷新后重试")
		}
		if row.ArchivedAt != nil {
			return problem(409, "请先恢复已归档的落地页")
		}
		newBytes := diyDraftBytes(in.DraftHTML, in.DraftConfig)
		if err := checkDIYQuota(tx, owner, newBytes-row.DraftBytes, false); err != nil {
			return err
		}
		row.Name, row.DraftHTML, row.DraftConfig, row.DraftBytes = in.Name, in.DraftHTML, in.DraftConfig, newBytes
		row.DraftVersion++
		row.UpdatedAt = s.Now()
		return tx.Omit("CreatedAt").Save(&row).Error
	})
	if err != nil {
		fail(w, err)
		return
	}
	s.writeDIYDetail(w, 200, owner, id)
}

func (s *Server) publishDIYLanding(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DraftVersion int64 `json:"draft_version"`
	}
	if err := decodeDIY(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	owner, id := userOf(r).ID, r.PathValue("id")
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		key = "draft:" + strconv.FormatInt(in.DraftVersion, 10)
	}
	if len(key) > 128 || strings.ContainsAny(key, "\x00\r\n\t ") {
		fail(w, problem(400, "发布幂等键无效"))
		return
	}
	status := 201
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, owner); err != nil {
			return err
		}
		var row DIYLandingPage
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ? AND user_id = ?", id, owner).Error; err != nil {
			return err
		}
		var prior DIYLandingRevision
		err := tx.First(&prior, "page_id = ? AND publish_key_hash = ?", id, hashString(key)).Error
		if err == nil {
			if prior.SourceDraftVersion != in.DraftVersion {
				return problem(409, "发布幂等键已用于其他草稿版本")
			}
			status = 200
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if row.ArchivedAt != nil {
			return problem(409, "请先恢复已归档的落地页")
		}
		if in.DraftVersion != row.DraftVersion {
			return problem(409, "草稿版本已变化，请保存或刷新后发布")
		}
		if strings.TrimSpace(row.DraftHTML) == "" {
			return problem(400, "请先粘贴落地页 HTML 代码")
		}
		render, _, err := prepareDIYHTML(row.DraftHTML, row.DraftConfig)
		if err != nil {
			return err
		}
		config, _ := json.Marshal(row.DraftConfig)
		hash := hashString(row.DraftHTML + "\n" + string(config))
		// A second click using a different HTTP key still publishes this exact
		// saved draft once, rather than allocating another immutable revision.
		err = tx.First(&prior, "page_id = ? AND source_draft_version = ? AND content_hash = ?", id, row.DraftVersion, hash).Error
		if err == nil {
			status = 200
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		storage := int64(len(row.DraftHTML) + len(render) + len(config))
		if err := checkDIYQuota(tx, owner, storage, false); err != nil {
			return err
		}
		var maxNo int64
		if err := tx.Model(&DIYLandingRevision{}).Where("page_id = ?", id).Select("COALESCE(MAX(revision_no),0)").Scan(&maxNo).Error; err != nil {
			return err
		}
		revision := DIYLandingRevision{ID: randomHex(16), PageID: id, UserID: owner, RevisionNo: maxNo + 1, RawHTML: row.DraftHTML, RenderHTML: render, Config: row.DraftConfig, ContentHash: hash, RuntimeContractVersion: 1, SourceDraftVersion: row.DraftVersion, PublishKeyHash: hashString(key), StorageBytes: storage, PublishedAt: s.Now()}
		if err := tx.Omit(clause.Associations).Create(&revision).Error; err != nil {
			return err
		}
		return tx.Model(&row).Updates(map[string]any{"latest_published_revision_id": revision.ID, "updated_at": s.Now()}).Error
	})
	if err != nil {
		fail(w, err)
		return
	}
	s.writeDIYDetail(w, status, owner, id)
}

func (s *Server) copyDIYLanding(w http.ResponseWriter, r *http.Request) {
	owner, id := userOf(r).ID, r.PathValue("id")
	var copy DIYLandingPage
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, owner); err != nil {
			return err
		}
		var row DIYLandingPage
		if err := tx.First(&row, "id = ? AND user_id = ?", id, owner).Error; err != nil {
			return err
		}
		if err := checkDIYQuota(tx, owner, row.DraftBytes, true); err != nil {
			return err
		}
		name := []rune(row.Name)
		if len(name) > 95 {
			name = name[:95]
		}
		copy = DIYLandingPage{ID: randomHex(16), UserID: owner, Name: string(name) + " 副本", DraftHTML: row.DraftHTML, DraftConfig: row.DraftConfig, DraftVersion: 1, DraftBytes: row.DraftBytes, CreatedAt: s.Now(), UpdatedAt: s.Now()}
		return tx.Create(&copy).Error
	})
	if err != nil {
		fail(w, err)
		return
	}
	s.writeDIYDetail(w, 201, owner, copy.ID)
}

func (s *Server) archiveDIYLanding(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Archived bool `json:"archived"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	owner, id := userOf(r).ID, r.PathValue("id")
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, owner); err != nil {
			return err
		}
		var row DIYLandingPage
		if err := tx.First(&row, "id = ? AND user_id = ?", id, owner).Error; err != nil {
			return err
		}
		var archived *time.Time
		if in.Archived {
			now := s.Now()
			archived = &now
		}
		return tx.Model(&row).Updates(map[string]any{"archived_at": archived, "updated_at": s.Now()}).Error
	})
	if err != nil {
		fail(w, err)
		return
	}
	s.writeDIYDetail(w, 200, owner, id)
}

func (s *Server) listDIYRevisions(w http.ResponseWriter, r *http.Request) {
	var row DIYLandingPage
	if err := s.DB.First(&row, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Error; err != nil {
		fail(w, err)
		return
	}
	rows := []DIYLandingRevision{}
	if err := s.DB.Omit("raw_html", "render_html").Where("page_id = ? AND user_id = ?", row.ID, userOf(r).ID).Order("revision_no DESC").Find(&rows).Error; err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]any{"data": rows, "total": len(rows)})
}

func (s *Server) getDIYRevision(w http.ResponseWriter, r *http.Request) {
	var row DIYLandingRevision
	if err := s.DB.First(&row, "id = ? AND page_id = ? AND user_id = ?", r.PathValue("revision"), r.PathValue("id"), userOf(r).ID).Error; err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, row)
}

func (s *Server) deleteDIYRevision(w http.ResponseWriter, r *http.Request) {
	owner, id, revision := userOf(r).ID, r.PathValue("id"), r.PathValue("revision")
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, owner); err != nil {
			return err
		}
		var page DIYLandingPage
		if err := tx.First(&page, "id = ? AND user_id = ?", id, owner).Error; err != nil {
			return err
		}
		var row DIYLandingRevision
		if err := tx.First(&row, "id = ? AND page_id = ? AND user_id = ?", revision, id, owner).Error; err != nil {
			return err
		}
		if page.LatestPublishedRevisionID == revision {
			return problem(409, "不能删除最新发布版本")
		}
		var n int64
		if err := tx.Model(&DIYLandingBinding{}).Where("revision_id = ?", revision).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return problem(409, "该版本正在被应用使用，请先更换应用版本")
		}
		return tx.Delete(&row).Error
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func (s *Server) deleteDIYLanding(w http.ResponseWriter, r *http.Request) {
	owner, id := userOf(r).ID, r.PathValue("id")
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, owner); err != nil {
			return err
		}
		var row DIYLandingPage
		if err := tx.First(&row, "id = ? AND user_id = ?", id, owner).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&DIYLandingBinding{}).Where("page_id = ?", id).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return problem(409, "该落地页正在被应用使用，请先解除引用")
		}
		if err := tx.Where("page_id = ?", id).Delete(&DIYLandingRevision{}).Error; err != nil {
			return err
		}
		return tx.Delete(&row).Error
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func (s *Server) listDIYBindings(w http.ResponseWriter, r *http.Request) {
	owner, id := userOf(r).ID, r.PathValue("id")
	var page DIYLandingPage
	if err := s.DB.First(&page, "id = ? AND user_id = ?", id, owner).Error; err != nil {
		fail(w, err)
		return
	}
	rows := []DIYLandingBinding{}
	if err := s.DB.Where("page_id = ? AND user_id = ?", id, owner).Order("updated_at DESC").Find(&rows).Error; err != nil {
		fail(w, err)
		return
	}
	type bindingView struct {
		DIYLandingBinding
		Name       string `json:"name"`
		Slug       string `json:"slug"`
		RevisionNo int64  `json:"revision_no"`
	}
	out := make([]bindingView, 0, len(rows))
	for _, row := range rows {
		v := bindingView{DIYLandingBinding: row}
		var revision DIYLandingRevision
		if err := s.DB.Select("revision_no").First(&revision, "id = ?", row.RevisionID).Error; err != nil {
			fail(w, err)
			return
		}
		v.RevisionNo = revision.RevisionNo
		if row.ResourceType == "apk" {
			var app Application
			if err := s.DB.Select("name", "slug").First(&app, "id = ? AND user_id = ?", row.ResourceID, owner).Error; err != nil {
				fail(w, err)
				return
			}
			v.Name, v.Slug = app.Name, app.Slug
		} else {
			var app PWAApp
			if err := s.DB.Select("name", "slug").First(&app, "id = ? AND user_id = ?", row.ResourceID, owner).Error; err != nil {
				fail(w, err)
				return
			}
			v.Name, v.Slug = app.Name, app.Slug
		}
		out = append(out, v)
	}
	jsonOut(w, 200, map[string]any{"data": out, "total": len(out)})
}

func diySupports(options DIYLandingOptions, kind string) bool {
	for _, target := range options.Targets {
		if target == kind {
			return true
		}
	}
	return false
}

func diyBindingReference(db *gorm.DB, kind, id string) (*DIYLandingReference, error) {
	return loadDIYBindingReference(db, kind, id, true)
}

// List views need only source identity. Do not load potentially large inline
// images for every application when its detail editor is not being opened.
func diyBindingRevisionReference(db *gorm.DB, kind, id string) (*DIYLandingReference, error) {
	return loadDIYBindingReference(db, kind, id, false)
}

func loadDIYBindingReference(db *gorm.DB, kind, id string, includeContent bool) (*DIYLandingReference, error) {
	var binding DIYLandingBinding
	query := db
	if !includeContent {
		query = query.Select("page_id", "revision_id")
	}
	err := query.First(&binding, "resource_type = ? AND resource_id = ?", kind, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &DIYLandingReference{PageID: binding.PageID, RevisionID: binding.RevisionID, Content: binding.Content}, nil
}

func diySameRevision(a, b *DIYLandingReference) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.PageID == b.PageID && a.RevisionID == b.RevisionID
}

func diySameReference(a, b *DIYLandingReference) bool {
	return diySameRevision(a, b) && (a == nil || maps.Equal(a.Content, b.Content))
}

func loadDIYRevision(db *gorm.DB, owner, kind string, ref *DIYLandingReference) (*DIYLandingRevision, error) {
	if ref == nil || ref.PageID == "" || ref.RevisionID == "" {
		return nil, problem(400, "请选择已发布的 DIY 落地页版本")
	}
	if kind != "apk" && kind != "pwa" {
		return nil, problem(400, "DIY 落地页应用类型无效")
	}
	var page DIYLandingPage
	if err := db.First(&page, "id = ? AND user_id = ?", ref.PageID, owner).Error; err != nil {
		return nil, err
	}
	var revision DIYLandingRevision
	if err := db.First(&revision, "id = ? AND page_id = ? AND user_id = ?", ref.RevisionID, ref.PageID, owner).Error; err != nil {
		return nil, err
	}
	if !diySupports(revision.Config, kind) {
		return nil, problem(400, "所选落地页版本不支持此应用类型")
	}
	return &revision, nil
}

func setDIYBinding(tx *gorm.DB, owner, kind, id string, ref *DIYLandingReference) error {
	if kind != "apk" && kind != "pwa" {
		return problem(400, "DIY 落地页应用类型无效")
	}
	var count int64
	if kind == "apk" {
		if err := tx.Model(&Application{}).Where("id = ? AND user_id = ?", id, owner).Count(&count).Error; err != nil {
			return err
		}
	} else {
		if err := tx.Model(&PWAApp{}).Where("id = ? AND user_id = ?", id, owner).Count(&count).Error; err != nil {
			return err
		}
	}
	if count != 1 {
		return gorm.ErrRecordNotFound
	}
	if ref == nil {
		return tx.Where("resource_type = ? AND resource_id = ? AND user_id = ?", kind, id, owner).Delete(&DIYLandingBinding{}).Error
	}
	revision, err := loadDIYRevision(tx, owner, kind, ref)
	if err != nil {
		return err
	}
	if err := validateDIYContent(revision.RenderHTML, revision.Config, ref.Content); err != nil {
		return err
	}
	if err := validateDIYManagedImageReferences(tx, owner, ref.Content); err != nil {
		return err
	}
	var page DIYLandingPage
	if err := tx.First(&page, "id = ? AND user_id = ?", ref.PageID, owner).Error; err != nil {
		return err
	}
	if page.ArchivedAt != nil {
		prior, err := diyBindingReference(tx, kind, id)
		if err != nil {
			return err
		}
		if !diySameRevision(prior, ref) {
			return problem(409, "该落地页已归档，不能新增引用")
		}
	}
	content := ref.Content
	if len(content) == 0 {
		content = DIYLandingContent{}
	}
	row := DIYLandingBinding{ResourceType: kind, ResourceID: id, UserID: owner, PageID: ref.PageID, RevisionID: ref.RevisionID, Content: content, UpdatedAt: time.Now().UTC()}
	return tx.Omit(clause.Associations).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "resource_type"}, {Name: "resource_id"}}, DoUpdates: clause.AssignmentColumns([]string{"user_id", "page_id", "revision_id", "content", "updated_at"})}).Create(&row).Error
}

// String avoids accidental source-code logging when references are diagnosed.
func (ref DIYLandingReference) String() string {
	return fmt.Sprintf("%s/%s", ref.PageID, ref.RevisionID)
}
