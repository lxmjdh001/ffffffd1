package platform

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Library content is global and administrator-managed. Favorites are owned,
// immutable DIY snapshots, so normal app ownership and billing rules apply.
type LandingLibraryTemplate struct {
	ID           string            `json:"id" gorm:"primaryKey;size:32"`
	AdminID      string            `json:"-" gorm:"index;size:32;not null"`
	Name         string            `json:"name" gorm:"size:100;not null"`
	Description  string            `json:"description" gorm:"size:500"`
	CoverURL     string            `json:"cover_url" gorm:"size:2048"`
	CoverHash    string            `json:"cover_hash" gorm:"index;size:64;not null;default:''"`
	CoverStatus  string            `json:"cover_status" gorm:"-"`
	CoverError   string            `json:"cover_error" gorm:"-"`
	HTML         string            `json:"html" gorm:"type:text;not null"`
	RenderHTML   string            `json:"-" gorm:"type:text;not null"`
	Config       DIYLandingOptions `json:"config" gorm:"serializer:json;type:text;not null"`
	Enabled      bool              `json:"enabled" gorm:"index;not null"`
	Version      int64             `json:"version" gorm:"not null"`
	StorageBytes int64             `json:"-" gorm:"not null"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	DeletedAt    gorm.DeletedAt    `json:"-" gorm:"index"`
}

type LandingLibraryFavorite struct {
	UserID          string             `json:"-" gorm:"primaryKey;size:32"`
	TemplateID      string             `json:"template_id" gorm:"primaryKey;size:32"`
	PageID          string             `json:"page_id" gorm:"index;size:32;not null"`
	RevisionID      string             `json:"revision_id" gorm:"index;size:32;not null"`
	TemplateVersion int64              `json:"template_version" gorm:"not null"`
	CoverHash       string             `json:"cover_hash" gorm:"index;size:64;not null;default:''"`
	CreatedAt       time.Time          `json:"created_at"`
	Page            DIYLandingPage     `json:"-" gorm:"foreignKey:PageID;constraint:OnDelete:CASCADE"`
	Revision        DIYLandingRevision `json:"-" gorm:"foreignKey:RevisionID;constraint:OnDelete:CASCADE"`
}

type landingLibraryInput struct {
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	HTML            string            `json:"html"`
	Config          DIYLandingOptions `json:"config"`
	CoverURL        string            `json:"cover_url"`
	Enabled         *bool             `json:"enabled"`
	ExpectedVersion *int64            `json:"expected_version,omitempty"`
}

type landingLibrarySummary struct {
	ID                string               `json:"id"`
	Name              string               `json:"name"`
	Description       string               `json:"description"`
	CoverURL          string               `json:"cover_url"`
	CoverStatus       string               `json:"cover_status"`
	CoverError        string               `json:"cover_error"`
	Config            DIYLandingOptions    `json:"config"`
	Enabled           bool                 `json:"enabled"`
	Version           int64                `json:"version"`
	Favorite          bool                 `json:"favorite"`
	FavoriteReference *DIYLandingReference `json:"favorite_reference,omitempty"`
	FavoriteCount     int64                `json:"favorite_count"`
	FavoriteVersion   int64                `json:"favorite_version,omitempty"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
}

type landingFavoriteView struct {
	ID                string              `json:"id"`
	TemplateID        string              `json:"template_id"`
	Name              string              `json:"name"`
	Description       string              `json:"description"`
	CoverURL          string              `json:"cover_url"`
	CoverStatus       string              `json:"cover_status"`
	CoverError        string              `json:"cover_error"`
	Reference         DIYLandingReference `json:"reference"`
	FavoriteReference DIYLandingReference `json:"favorite_reference"`
	PageID            string              `json:"page_id"`
	RevisionID        string              `json:"revision_id"`
	Config            DIYLandingOptions   `json:"config"`
	Version           int64               `json:"version"`
	TemplateVersion   int64               `json:"template_version"`
	LatestVersion     int64               `json:"latest_version"`
	Favorite          bool                `json:"favorite"`
	Enabled           bool                `json:"enabled"`
	SourceAvailable   bool                `json:"source_available"`
	CreatedAt         time.Time           `json:"created_at"`
}

func (s *Server) registerLandingLibraryRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/landing-template-assets/{id}", s.serveDIYAsset)
	m.Handle("GET /api/landing-template-covers/{cover}", s.auth(s.serveLandingCover))
	m.Handle("GET /api/landing-templates", s.auth(s.listLandingTemplates))
	m.Handle("GET /api/landing-templates/{id}", s.auth(s.getLandingTemplate))
	m.Handle("POST /api/landing-templates/{id}/favorite", s.diyAuth(s.favoriteLandingTemplate))
	m.Handle("DELETE /api/landing-templates/{id}/favorite", s.auth(s.unfavoriteLandingTemplate))
	m.Handle("GET /api/landing-favorites", s.auth(s.listLandingFavorites))
	m.Handle("GET /api/admin/landing-templates", s.admin(s.listAdminLandingTemplates))
	m.Handle("POST /api/admin/landing-templates", s.admin(s.createLandingTemplate))
	m.Handle("GET /api/admin/landing-templates/{id}", s.admin(s.getLandingTemplate))
	m.Handle("PUT /api/admin/landing-templates/{id}", s.admin(s.updateLandingTemplate))
	m.Handle("DELETE /api/admin/landing-templates/{id}", s.admin(s.deleteLandingTemplate))
	m.Handle("POST /api/admin/landing-templates/{id}/cover/retry", s.admin(s.retryLandingTemplateCover))
}

func validateLandingLibraryInput(in *landingLibraryInput) (string, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	// Kept in the input DTO for older clients. Covers always come from the
	// actual HTML renderer, never from a caller-supplied remote image.
	in.CoverURL = ""
	if len([]rune(in.Name)) < 1 || len([]rune(in.Name)) > 100 || strings.ContainsAny(in.Name, "\x00\r\n") {
		return "", problem(400, "模版名称须为 1–100 个字符")
	}
	if len([]rune(in.Description)) > 500 || strings.ContainsRune(in.Description, '\x00') {
		return "", problem(400, "模版介绍最多 500 个字符")
	}
	if strings.ContainsRune(in.HTML, '\x00') {
		return "", problem(400, "落地页代码不能包含空字符")
	}
	if err := normalizeDIYOptions(&in.Config); err != nil {
		return "", err
	}
	render, _, err := prepareDIYHTML(in.HTML, in.Config)
	return render, err
}

func (s *Server) createLandingTemplate(w http.ResponseWriter, r *http.Request) {
	s.saveLandingTemplate(w, r, true)
}
func (s *Server) updateLandingTemplate(w http.ResponseWriter, r *http.Request) {
	s.saveLandingTemplate(w, r, false)
}
func (s *Server) saveLandingTemplate(w http.ResponseWriter, r *http.Request, creating bool) {
	if s.Config.DisableDIYLanding {
		fail(w, problem(503, "DIY 落地页暂时停用，请稍后重试"))
		return
	}
	var in landingLibraryInput
	if err := decodeDIY(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	render, err := validateLandingLibraryInput(&in)
	if err != nil {
		fail(w, err)
		return
	}
	if err = s.rate("landing-library-write:"+userOf(r).ID, 120, time.Hour); err != nil {
		fail(w, err)
		return
	}
	var row LandingLibraryTemplate
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, userOf(r).ID); err != nil {
			return err
		}
		// Serialize the bounded global catalog across administrators as well.
		var settings Settings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&settings, "id = 1").Error; err != nil {
			return err
		}
		oldBytes := int64(0)
		if creating {
			row = LandingLibraryTemplate{ID: randomHex(16), AdminID: userOf(r).ID, Enabled: true, Version: 1, CreatedAt: s.Now()}
		} else {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", r.PathValue("id")).Error; err != nil {
				return err
			}
			if in.ExpectedVersion != nil && *in.ExpectedVersion != row.Version {
				return problem(409, "模版已更新，请刷新后再保存")
			}
			oldBytes = row.StorageBytes
			row.Version++
		}
		var count, total int64
		if err := tx.Model(&LandingLibraryTemplate{}).Count(&count).Error; err != nil {
			return err
		}
		if creating && count >= 500 {
			return problem(409, "模版库最多保存 500 个模版")
		}
		if err := tx.Model(&LandingLibraryTemplate{}).Select("COALESCE(SUM(storage_bytes), 0)").Scan(&total).Error; err != nil {
			return err
		}
		config, _ := json.Marshal(in.Config)
		size := int64(len(in.HTML) + len(render) + len(config))
		if total-oldBytes+size > 500<<20 {
			return problem(409, "模版库源码总容量已达 500 MiB")
		}
		row.Name = in.Name
		row.Description = in.Description
		row.CoverURL = in.CoverURL
		row.HTML = in.HTML
		row.RenderHTML = render
		row.Config = in.Config
		row.StorageBytes = size
		row.UpdatedAt = s.Now()
		if in.Enabled != nil {
			row.Enabled = *in.Enabled
		}
		row.CoverHash, err = ensureLandingCover(tx, landingCoverInput(row.HTML, row.Config, row.Name), s.Now())
		if err != nil {
			return err
		}
		// An explicit administrator save retries this content after a repaired
		// browser/runtime failure. Preserve ready bytes and an active worker's
		// lease; merely collecting a template must not reset its retry budget.
		if err := tx.Model(&LandingCover{}).Where("hash = ? AND state = ?", row.CoverHash, "failed").Updates(map[string]any{"state": "pending", "attempts": 0, "next_attempt_at": s.Now(), "last_error": "", "lease_id": "", "lease_until": nil, "updated_at": s.Now()}).Error; err != nil {
			return err
		}
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return recordAudit(tx, userOf(r).ID, "landing_library.save", row.ID, row.Name)
	})
	if err != nil {
		fail(w, err)
		return
	}
	status := 200
	if creating {
		status = 201
	}
	s.projectLandingTemplateCover(&row)
	jsonOut(w, status, row)
}

func (s *Server) librarySummary(row LandingLibraryTemplate) landingLibrarySummary {
	s.projectLandingTemplateCover(&row)
	return landingLibrarySummary{ID: row.ID, Name: row.Name, Description: row.Description, CoverURL: row.CoverURL, CoverStatus: row.CoverStatus, CoverError: row.CoverError, Config: row.Config, Enabled: row.Enabled, Version: row.Version, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}
func (s *Server) listAdminLandingTemplates(w http.ResponseWriter, r *http.Request) {
	s.listLandingLibrary(w, r, true)
}
func (s *Server) listLandingTemplates(w http.ResponseWriter, r *http.Request) {
	s.listLandingLibrary(w, r, false)
}
func (s *Server) listLandingLibrary(w http.ResponseWriter, r *http.Request, admin bool) {
	if !admin && r.URL.Query().Get("favorites") == "1" {
		s.listLandingFavorites(w, r)
		return
	}
	target := r.URL.Query().Get("target")
	if target != "" && target != "apk" && target != "pwa" {
		fail(w, problem(400, "适用类型无效"))
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(query)) > 100 {
		fail(w, problem(400, "搜索内容最多 100 字"))
		return
	}
	q := s.DB.Omit("html", "render_html").Model(&LandingLibraryTemplate{})
	if !admin {
		q = q.Where("enabled = ?", true)
	}
	if query != "" {
		query = strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.ToLower(query))
		q = q.Where("LOWER(name) LIKE ? ESCAPE '\\'", "%"+query+"%")
	}
	var rows []LandingLibraryTemplate
	if err := q.Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		fail(w, err)
		return
	}
	var favorites []LandingLibraryFavorite
	if err := s.DB.Where("user_id = ?", userOf(r).ID).Find(&favorites).Error; err != nil {
		fail(w, err)
		return
	}
	favoriteMap := map[string]LandingLibraryFavorite{}
	for _, f := range favorites {
		favoriteMap[f.TemplateID] = f
	}
	data := []landingLibrarySummary{}
	for _, row := range rows {
		if target != "" && !diySupports(row.Config, target) {
			continue
		}
		out := s.librarySummary(row)
		if f, ok := favoriteMap[row.ID]; ok {
			var page DIYLandingPage
			if err := s.DB.Select("id", "archived_at").First(&page, "id = ? AND user_id = ?", f.PageID, userOf(r).ID).Error; err == nil && page.ArchivedAt == nil {
				out.Favorite = true
				out.FavoriteVersion = f.TemplateVersion
				out.FavoriteReference = &DIYLandingReference{PageID: f.PageID, RevisionID: f.RevisionID}
			}
		}
		if r.URL.Query().Get("favorites") == "1" && !out.Favorite {
			continue
		}
		if admin {
			if err := s.DB.Model(&LandingLibraryFavorite{}).Where("template_id = ?", row.ID).Count(&out.FavoriteCount).Error; err != nil {
				fail(w, err)
				return
			}
		}
		data = append(data, out)
	}
	page, perPage := libraryPage(r)
	total := len(data)
	start := min(total, (page-1)*perPage)
	jsonOut(w, 200, map[string]any{"data": data[start:min(total, start+perPage)], "total": total, "page": page, "per_page": perPage})
}
func libraryPage(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		page = 100000
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if n < 1 {
		n = 20
	}
	if n > 200 {
		n = 200
	}
	return page, n
}
func (s *Server) getLandingTemplate(w http.ResponseWriter, r *http.Request) {
	q := s.DB
	if userOf(r).Role != "admin" {
		q = q.Where("enabled = ?", true)
	}
	var row LandingLibraryTemplate
	if err := q.First(&row, "id = ?", r.PathValue("id")).Error; err != nil {
		fail(w, err)
		return
	}
	s.projectLandingTemplateCover(&row)
	jsonOut(w, 200, row)
}
func (s *Server) deleteLandingTemplate(w http.ResponseWriter, r *http.Request) {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, userOf(r).ID); err != nil {
			return err
		}
		var row LandingLibraryTemplate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", r.PathValue("id")).Error; err != nil {
			return err
		}
		if err := tx.Model(&row).Updates(map[string]any{"html": "", "render_html": "", "storage_bytes": 0}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&row).Error; err != nil {
			return err
		}
		return recordAudit(tx, userOf(r).ID, "landing_library.delete", row.ID, row.Name)
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func (s *Server) favoriteLandingTemplate(w http.ResponseWriter, r *http.Request) {
	owner, id := userOf(r).ID, r.PathValue("id")
	var favorite LandingLibraryFavorite
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, owner); err != nil {
			return err
		}
		err := tx.First(&favorite, "user_id = ? AND template_id = ?", owner, id).Error
		if err == nil {
			var page DIYLandingPage
			if err = tx.First(&page, "id = ? AND user_id = ?", favorite.PageID, owner).Error; err == nil {
				var revision DIYLandingRevision
				if err = tx.First(&revision, "id = ? AND page_id = ? AND user_id = ?", favorite.RevisionID, page.ID, owner).Error; err != nil {
					return err
				}
				if page.ArchivedAt != nil {
					return tx.Model(&page).Update("archived_at", nil).Error
				}
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err = tx.Delete(&favorite).Error; err != nil {
				return err
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var source LandingLibraryTemplate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&source, "id = ? AND enabled = ?", id, true).Error; err != nil {
			return err
		}
		config, _ := json.Marshal(source.Config)
		draftBytes := diyDraftBytes(source.HTML, source.Config)
		revisionBytes := int64(len(source.HTML) + len(source.RenderHTML) + len(config))
		if err := checkDIYQuota(tx, owner, draftBytes+revisionBytes, true); err != nil {
			return err
		}
		pageID, revisionID := randomHex(16), randomHex(16)
		page := DIYLandingPage{ID: pageID, UserID: owner, Name: source.Name, DraftHTML: source.HTML, DraftConfig: source.Config, DraftVersion: 1, DraftBytes: draftBytes, LatestPublishedRevisionID: revisionID, CreatedAt: s.Now(), UpdatedAt: s.Now()}
		if err := tx.Create(&page).Error; err != nil {
			return err
		}
		revision := DIYLandingRevision{ID: revisionID, PageID: pageID, UserID: owner, RevisionNo: 1, RawHTML: source.HTML, RenderHTML: source.RenderHTML, Config: source.Config, ContentHash: hashString(source.HTML + "\n" + string(config)), RuntimeContractVersion: 1, SourceDraftVersion: 1, PublishKeyHash: hashString("library:" + source.ID + ":" + strconv.FormatInt(source.Version, 10)), StorageBytes: revisionBytes, PublishedAt: s.Now()}
		if err := tx.Omit(clause.Associations).Create(&revision).Error; err != nil {
			return err
		}
		coverHash, err := ensureLandingCover(tx, landingCoverInput(source.HTML, source.Config, source.Name), s.Now())
		if err != nil {
			return err
		}
		favorite = LandingLibraryFavorite{UserID: owner, TemplateID: id, PageID: pageID, RevisionID: revisionID, TemplateVersion: source.Version, CoverHash: coverHash, CreatedAt: s.Now()}
		if err := tx.Omit(clause.Associations).Create(&favorite).Error; err != nil {
			return err
		}
		if err := tx.Omit(clause.Associations).Create(&LandingCoverSnapshot{RevisionID: revisionID, UserID: owner, CoverHash: coverHash, CreatedAt: s.Now()}).Error; err != nil {
			return err
		}
		return recordAudit(tx, owner, "landing_library.favorite", id, source.Name)
	})
	if err != nil {
		fail(w, err)
		return
	}
	ref := DIYLandingReference{PageID: favorite.PageID, RevisionID: favorite.RevisionID}
	coverURL, coverStatus, coverError := s.landingCoverProjection(favorite.CoverHash)
	jsonOut(w, 200, map[string]any{"reference": ref, "favorite_reference": ref, "page_id": ref.PageID, "revision_id": ref.RevisionID, "favorite": true, "template_version": favorite.TemplateVersion, "cover_url": coverURL, "cover_status": coverStatus, "cover_error": coverError})
}
func (s *Server) unfavoriteLandingTemplate(w http.ResponseWriter, r *http.Request) {
	owner, id := userOf(r).ID, r.PathValue("id")
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, owner); err != nil {
			return err
		}
		var favorite LandingLibraryFavorite
		if err := tx.First(&favorite, "user_id = ? AND template_id = ?", owner, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		if err := tx.Model(&DIYLandingPage{}).Where("id = ? AND user_id = ? AND draft_version = ? AND latest_published_revision_id = ?", favorite.PageID, owner, 1, favorite.RevisionID).Update("archived_at", s.Now()).Error; err != nil {
			return err
		}
		if err := tx.Delete(&favorite).Error; err != nil {
			return err
		}
		return recordAudit(tx, owner, "landing_library.unfavorite", id, "")
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (s *Server) listLandingFavorites(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target != "" && target != "apk" && target != "pwa" {
		fail(w, problem(400, "适用类型无效"))
		return
	}
	var rows []LandingLibraryFavorite
	if err := s.DB.Where("user_id = ?", userOf(r).ID).Order("created_at DESC, template_id DESC").Find(&rows).Error; err != nil {
		fail(w, err)
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if len([]rune(query)) > 100 {
		fail(w, problem(400, "搜索内容最多 100 字"))
		return
	}
	data := []landingFavoriteView{}
	for _, row := range rows {
		var page DIYLandingPage
		if err := s.DB.Omit("draft_html").First(&page, "id = ? AND user_id = ? AND archived_at IS NULL", row.PageID, userOf(r).ID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		} else if err != nil {
			fail(w, err)
			return
		}
		var revision DIYLandingRevision
		if err := s.DB.Omit("raw_html", "render_html").First(&revision, "id = ? AND page_id = ? AND user_id = ?", row.RevisionID, row.PageID, userOf(r).ID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		} else if err != nil {
			fail(w, err)
			return
		}
		if target != "" && !diySupports(revision.Config, target) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(page.Name), query) {
			continue
		}
		var source LandingLibraryTemplate
		err := s.DB.Unscoped().Omit("html", "render_html").First(&source, "id = ?", row.TemplateID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			fail(w, err)
			return
		}
		ref := DIYLandingReference{PageID: row.PageID, RevisionID: row.RevisionID}
		coverURL, coverStatus, coverError := s.landingCoverProjection(row.CoverHash)
		data = append(data, landingFavoriteView{ID: row.TemplateID, TemplateID: row.TemplateID, Name: page.Name, Description: source.Description, CoverURL: coverURL, CoverStatus: coverStatus, CoverError: coverError, Reference: ref, FavoriteReference: ref, PageID: row.PageID, RevisionID: row.RevisionID, Config: revision.Config, Version: row.TemplateVersion, TemplateVersion: row.TemplateVersion, LatestVersion: source.Version, Favorite: true, Enabled: true, SourceAvailable: source.Enabled && !source.DeletedAt.Valid, CreatedAt: row.CreatedAt})
	}
	page, n := libraryPage(r)
	total := len(data)
	start := min(total, (page-1)*n)
	jsonOut(w, 200, map[string]any{"data": data[start:min(total, start+n)], "total": total, "page": page, "per_page": n})
}
