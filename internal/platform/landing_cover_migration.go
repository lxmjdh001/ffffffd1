package platform

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const landingCoverMigrationVersion = 26

func migrateLandingCovers(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = ?", landingCoverMigrationVersion).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if err := tx.AutoMigrate(&LandingLibraryTemplate{}, &LandingLibraryFavorite{}, &LandingCover{}, &LandingCoverSnapshot{}); err != nil {
		return err
	}
	now := time.Now().UTC()
	// Old remote covers are deliberately retired. Every existing source and
	// collected revision gets a real screenshot task, including disabled sources
	// and favorites whose source has already been deleted.
	// Source HTML may be 5 MiB per row. Read only the rendering inputs and
	// bound each batch instead of retaining the entire catalog in memory.
	lastSourceID := ""
	for {
		var sources []LandingLibraryTemplate
		q := tx.Select("id", "html", "config", "name").Order("id ASC").Limit(10)
		if lastSourceID != "" {
			q = q.Where("id > ?", lastSourceID)
		}
		if err := q.Find(&sources).Error; err != nil {
			return err
		}
		if len(sources) == 0 {
			break
		}
		for _, source := range sources {
			hash, err := ensureLandingCover(tx, landingCoverInput(source.HTML, source.Config, source.Name), now)
			if err != nil {
				return err
			}
			if err := tx.Model(&source).UpdateColumns(map[string]any{"cover_hash": hash, "cover_url": ""}).Error; err != nil {
				return err
			}
		}
		lastSourceID = sources[len(sources)-1].ID
		if len(sources) < 10 {
			break
		}
	}
	lastUserID, lastTemplateID := "", ""
	for {
		var favorites []LandingLibraryFavorite
		q := tx.Select("user_id", "template_id", "page_id", "revision_id").Order("user_id ASC, template_id ASC").Limit(100)
		if lastUserID != "" || lastTemplateID != "" {
			q = q.Where("user_id > ? OR (user_id = ? AND template_id > ?)", lastUserID, lastUserID, lastTemplateID)
		}
		if err := q.Find(&favorites).Error; err != nil {
			return err
		}
		if len(favorites) == 0 {
			break
		}
		for _, favorite := range favorites {
			var revision DIYLandingRevision
			if err := tx.Select("id", "raw_html", "config").First(&revision, "id = ? AND page_id = ? AND user_id = ?", favorite.RevisionID, favorite.PageID, favorite.UserID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			} else if err != nil {
				return err
			}
			var page DIYLandingPage
			if err := tx.Select("name").First(&page, "id = ? AND user_id = ?", favorite.PageID, favorite.UserID).Error; err != nil {
				return err
			}
			hash, err := ensureLandingCover(tx, landingCoverInput(revision.RawHTML, revision.Config, page.Name), now)
			if err != nil {
				return err
			}
			if err := tx.Model(&favorite).UpdateColumn("cover_hash", hash).Error; err != nil {
				return err
			}
			if err := tx.Omit(clause.Associations).Clauses(clause.OnConflict{DoNothing: true}).Create(&LandingCoverSnapshot{RevisionID: revision.ID, UserID: favorite.UserID, CoverHash: hash, CreatedAt: now}).Error; err != nil {
				return err
			}
		}
		last := favorites[len(favorites)-1]
		lastUserID, lastTemplateID = last.UserID, last.TemplateID
		if len(favorites) < 100 {
			break
		}
	}
	return tx.Create(&Migration{Version: landingCoverMigrationVersion, AppliedAt: now}).Error
}
