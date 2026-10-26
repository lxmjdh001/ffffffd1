package platform

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image/png"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	landingCoverContractVersion = 1
	landingCoverWidth           = 390
	landingCoverHeight          = 844
	maxLandingCoverBytes        = 3 << 20
	landingCoverMaxAttempts     = 3
	landingCoverLease           = 90 * time.Second
)

// HTML is original page source, processed again by the isolated DIY renderer.
// Capturing a catalog cover never navigates to an application or billing route.
type LandingCoverRenderInput struct {
	HTML   string
	Config DIYLandingOptions
	Name   string
	Mode   string
}

// Immutable rendering inputs and the resulting PNG live in the shared database,
// so the worker and image request may run on different replicas. The content key
// also prevents a late screenshot from replacing a more recently edited page.
type LandingCover struct {
	Hash          string            `gorm:"primaryKey;size:64"`
	HTML          string            `gorm:"type:text;not null"`
	Config        DIYLandingOptions `gorm:"serializer:json;type:text;not null"`
	Name          string            `gorm:"size:100;not null"`
	Mode          string            `gorm:"size:8;not null"`
	State         string            `gorm:"index:idx_landing_cover_queue,priority:1;size:16;not null"`
	Attempts      int               `gorm:"not null"`
	NextAttemptAt time.Time         `gorm:"index:idx_landing_cover_queue,priority:2;not null"`
	LeaseID       string            `gorm:"size:32;not null"`
	LeaseUntil    *time.Time        `gorm:"index"`
	LastError     string            `gorm:"size:500;not null"`
	PNG           []byte
	PNGHash       string `gorm:"size:64;not null"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Keep a cover attached to the collected immutable revision even after a user
// unfavorites it. Applications already bound to that revision keep their image;
// deleting the owned revision removes this permission through its foreign key.
type LandingCoverSnapshot struct {
	RevisionID string `gorm:"primaryKey;size:32"`
	UserID     string `gorm:"index;size:32;not null"`
	CoverHash  string `gorm:"index;size:64;not null"`
	CreatedAt  time.Time
	Revision   DIYLandingRevision `gorm:"foreignKey:RevisionID;constraint:OnDelete:CASCADE"`
}

func landingCoverInput(html string, config DIYLandingOptions, name string) LandingCoverRenderInput {
	mode := "apk"
	if diySupports(config, "pwa") {
		mode = "pwa"
	}
	return LandingCoverRenderInput{HTML: html, Config: config, Name: name, Mode: mode}
}

func landingCoverHash(in LandingCoverRenderInput) string {
	data, _ := json.Marshal(struct {
		Contract, Width, Height int
		Input                   LandingCoverRenderInput
	}{landingCoverContractVersion, landingCoverWidth, landingCoverHeight, in})
	return hashString(string(data))
}

func ensureLandingCover(tx *gorm.DB, in LandingCoverRenderInput, now time.Time) (string, error) {
	hash := landingCoverHash(in)
	row := LandingCover{Hash: hash, HTML: in.HTML, Config: in.Config, Name: in.Name, Mode: in.Mode, State: "pending", NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}
	err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
	return hash, err
}

func (s *Server) landingCoverProjection(hash string) (url, status, message string) {
	if hash == "" {
		return "", "pending", ""
	}
	var row LandingCover
	if err := s.DB.Select("state", "last_error").First(&row, "hash = ?", hash).Error; err != nil {
		return "", "pending", ""
	}
	if row.State == "ready" {
		return "/api/landing-template-covers/" + hash + ".png", row.State, ""
	}
	return "", row.State, row.LastError
}

func (s *Server) projectLandingTemplateCover(row *LandingLibraryTemplate) {
	row.CoverURL, row.CoverStatus, row.CoverError = s.landingCoverProjection(row.CoverHash)
}

// RunLandingCoverWorker is separate from the minute-level payment worker and
// can be started by a control-plane fixture without running financial jobs.
func (s *Server) RunLandingCoverWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastCleanup := time.Time{}
	for ctx.Err() == nil {
		if s.Now().Sub(lastCleanup) > time.Hour {
			if err := s.cleanupLandingCovers(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("landing cover cleanup failed", "error", err)
			}
			lastCleanup = s.Now()
		}
		worked, err := s.processLandingCoverJob(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Warn("landing cover job failed", "error", err)
		}
		if worked && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) claimLandingCover(ctx context.Context) (*LandingCover, error) {
	now := s.Now()
	var row LandingCover
	claimed := false
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A worker that stopped during the final attempt cannot leave a job in
		// processing forever. Fencing below rejects any late former lease.
		if err := tx.Model(&LandingCover{}).Where("attempts >= ? AND ((state = ? AND lease_until <= ?) OR state = ?)", landingCoverMaxAttempts, "processing", now, "pending").Updates(map[string]any{"state": "failed", "lease_id": "", "lease_until": nil, "last_error": "封面生成失败，请管理员重试", "updated_at": now}).Error; err != nil {
			return err
		}
		eligible := "attempts < ? AND ((state = ? AND next_attempt_at <= ?) OR (state = ? AND lease_until <= ?))"
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Omit("png").Where(eligible, landingCoverMaxAttempts, "pending", now, "processing", now).Order("next_attempt_at ASC, created_at ASC, hash ASC").First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		lease := now.Add(landingCoverLease)
		leaseID := randomHex(16)
		result := tx.Model(&LandingCover{}).Where("hash = ? AND "+eligible, row.Hash, landingCoverMaxAttempts, "pending", now, "processing", now).Updates(map[string]any{"state": "processing", "attempts": gorm.Expr("attempts + 1"), "lease_id": leaseID, "lease_until": lease, "last_error": "", "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		row.State, row.LeaseID, row.LeaseUntil, row.Attempts = "processing", leaseID, &lease, row.Attempts+1
		claimed = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, nil
	}
	return &row, nil
}

func (s *Server) processLandingCoverJob(ctx context.Context) (bool, error) {
	row, err := s.claimLandingCover(ctx)
	if err != nil || row == nil {
		return false, err
	}
	timeout := s.Config.LandingScreenshotTimeout
	if timeout == 0 {
		timeout = 25 * time.Second
	}
	captureCtx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()
	in := LandingCoverRenderInput{HTML: row.HTML, Config: row.Config, Name: row.Name, Mode: row.Mode}
	capture := s.landingCoverCapture
	if capture == nil {
		capture = s.captureLandingScreenshot
	}
	data, renderErr := capture(captureCtx, in)
	if renderErr == nil {
		renderErr = validateLandingCoverPNG(data)
	}
	if ctx.Err() != nil {
		// Let the durable lease expire after shutdown; don't spend another
		// retry on a control-plane restart or write with a cancelled context.
		return true, ctx.Err()
	}
	now := s.Now()
	values := map[string]any{"lease_id": "", "lease_until": nil, "updated_at": now}
	if renderErr == nil {
		values["state"], values["png"], values["png_hash"], values["last_error"] = "ready", data, hashString(string(data)), ""
	} else {
		slog.Warn("landing cover capture failed", "hash", row.Hash, "attempt", row.Attempts, "error", renderErr)
		values["state"], values["last_error"] = "pending", "封面生成暂未完成，正在自动重试"
		values["next_attempt_at"] = now.Add(time.Duration(row.Attempts*row.Attempts) * 10 * time.Second)
		if row.Attempts >= landingCoverMaxAttempts {
			values["state"], values["last_error"] = "failed", "封面生成失败，请管理员重试"
		}
	}
	err = s.DB.WithContext(ctx).Model(&LandingCover{}).Where("hash = ? AND state = ? AND lease_id = ?", row.Hash, "processing", row.LeaseID).Updates(values).Error
	return true, err
}

func validateLandingCoverPNG(data []byte) error {
	if len(data) == 0 || len(data) > maxLandingCoverBytes {
		return errors.New("landing cover PNG has invalid size")
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width != landingCoverWidth || config.Height != landingCoverHeight {
		return errors.New("landing cover PNG has invalid dimensions or encoding")
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return errors.New("landing cover PNG is incomplete or corrupt")
	}
	return nil
}

func (s *Server) cleanupLandingCovers(ctx context.Context) error {
	// Keep in-flight and recent edits for at least a day. Collected revision
	// snapshots deliberately count even after a favorite has been removed.
	return s.DB.WithContext(ctx).Where("created_at < ? AND (state <> ? OR lease_until <= ?) AND hash NOT IN (?) AND hash NOT IN (?) AND hash NOT IN (?)", s.Now().Add(-24*time.Hour), "processing", s.Now(),
		s.DB.Model(&LandingLibraryTemplate{}).Select("cover_hash").Where("cover_hash <> ?", ""),
		s.DB.Model(&LandingLibraryFavorite{}).Select("cover_hash").Where("cover_hash <> ?", ""),
		s.DB.Model(&LandingCoverSnapshot{}).Select("cover_hash").Where("cover_hash <> ?", "")).Delete(&LandingCover{}).Error
}

func (s *Server) canReadLandingCover(owner User, hash string) (bool, error) {
	if owner.Role == "admin" {
		return true, nil
	}
	var count int64
	if err := s.DB.Model(&LandingLibraryTemplate{}).Where("enabled = ? AND cover_hash = ?", true, hash).Count(&count).Error; err != nil || count > 0 {
		return count > 0, err
	}
	if err := s.DB.Model(&LandingLibraryFavorite{}).Where("user_id = ? AND cover_hash = ?", owner.ID, hash).Count(&count).Error; err != nil || count > 0 {
		return count > 0, err
	}
	err := s.DB.Model(&LandingCoverSnapshot{}).Where("user_id = ? AND cover_hash = ?", owner.ID, hash).Count(&count).Error
	return count > 0, err
}

func (s *Server) serveLandingCover(w http.ResponseWriter, r *http.Request) {
	value := r.PathValue("cover")
	hash := strings.TrimSuffix(value, ".png")
	decoded, err := hex.DecodeString(hash)
	if !strings.HasSuffix(value, ".png") || len(decoded) != 32 || err != nil || hash != strings.ToLower(hash) {
		fail(w, gorm.ErrRecordNotFound)
		return
	}
	allowed, err := s.canReadLandingCover(userOf(r), hash)
	if err != nil {
		fail(w, err)
		return
	}
	if !allowed {
		fail(w, gorm.ErrRecordNotFound)
		return
	}
	var row LandingCover
	if err = s.DB.Select("state", "png", "png_hash").First(&row, "hash = ?", hash).Error; err != nil {
		fail(w, err)
		return
	}
	if row.State != "ready" || len(row.PNG) == 0 {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Retry-After", "3")
		fail(w, problem(503, "预览封面正在生成，请稍后刷新"))
		return
	}
	etag := `"` + row.PNGHash + `"`
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	for _, match := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		if strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(match), "W/")) == etag || strings.TrimSpace(match) == "*" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(row.PNG)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(row.PNG)
}

func (s *Server) retryLandingTemplateCover(w http.ResponseWriter, r *http.Request) {
	var row LandingLibraryTemplate
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", r.PathValue("id")).Error; err != nil {
			return err
		}
		var err error
		row.CoverHash, err = ensureLandingCover(tx, landingCoverInput(row.HTML, row.Config, row.Name), s.Now())
		if err != nil {
			return err
		}
		if err := tx.Model(&row).Update("cover_hash", row.CoverHash).Error; err != nil {
			return err
		}
		// Keep ready bytes immutable and don't steal a healthy worker's lease.
		if err := tx.Model(&LandingCover{}).Where("hash = ? AND (state IN ? OR (state = ? AND lease_until <= ?))", row.CoverHash, []string{"failed", "pending"}, "processing", s.Now()).Updates(map[string]any{"state": "pending", "attempts": 0, "last_error": "", "lease_id": "", "lease_until": nil, "next_attempt_at": s.Now(), "updated_at": s.Now()}).Error; err != nil {
			return err
		}
		return recordAudit(tx, userOf(r).ID, "landing_library.cover_retry", row.ID, "")
	})
	if err != nil {
		fail(w, err)
		return
	}
	s.projectLandingTemplateCover(&row)
	jsonOut(w, 200, row)
}
