package platform

import (
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store only a fingerprint of the recorded UA. The owner is part of the key:
// blocking a visitor for one customer must never affect another customer.
type UABlock struct {
	UserID    string `gorm:"primaryKey;size:32"`
	UAHash    string `gorm:"primaryKey;size:64"`
	CreatedAt time.Time
}

func uaFingerprint(ua string) string {
	if ua = cleanText(ua, 2048); ua != "" {
		return hashString(ua)
	}
	return ""
}

func (s *Server) setDownloadUABlock(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Blocked *bool `json:"blocked"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Blocked == nil {
		fail(w, problem(400, "请选择是否拉黑 UA"))
		return
	}
	ownerID := userOf(r).ID
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, ownerID); err != nil {
			return err
		}
		var record Download
		if err := tx.First(&record, "id = ? AND user_id = ?", r.PathValue("id"), ownerID).Error; err != nil {
			return err
		}
		if strings.TrimSpace(cleanText(record.UserAgent, 2048)) == "" {
			return problem(400, "此记录没有可拉黑的 UA")
		}
		hash := uaFingerprint(record.UserAgent)
		action := "download.ua.unblock"
		var result *gorm.DB
		if *in.Blocked {
			action = "download.ua.block"
			result = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&UABlock{UserID: ownerID, UAHash: hash, CreatedAt: s.Now()})
		} else {
			result = tx.Where("user_id = ? AND ua_hash = ?", ownerID, hash).Delete(&UABlock{})
		}
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		return recordAudit(tx, ownerID, action, hash, "更新此账户的 UA 访问限制")
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, http.StatusOK, map[string]bool{"blocked": *in.Blocked})
}

func (s *Server) blockedUAHashes(ownerID string, hashes []string) (map[string]bool, error) {
	blocked := map[string]bool{}
	if len(hashes) == 0 {
		return blocked, nil
	}
	var rows []UABlock
	if err := s.DB.Select("ua_hash").Where("user_id = ? AND ua_hash IN ?", ownerID, hashes).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		blocked[row.UAHash] = true
	}
	return blocked, nil
}

// Check before attribution, tickets, redirect selection, file headers or billing.
// A ticket's original UA is included as well as the current request so changing
// the header cannot revive a blocked ticket or resume its remaining bytes.
func (s *Server) rejectBlockedUA(w http.ResponseWriter, r *http.Request, ownerID string, uas ...string) bool {
	hashes := []string{}
	seen := map[string]bool{}
	for _, ua := range uas {
		if hash := uaFingerprint(ua); hash != "" && !seen[hash] {
			seen[hash] = true
			hashes = append(hashes, hash)
		}
	}
	blocked, err := s.blockedUAHashes(ownerID, hashes)
	if err != nil {
		fail(w, problem(http.StatusServiceUnavailable, "访问检查暂时不可用，请稍后重试"))
		return true
	}
	if len(blocked) != 0 {
		s.cloakNotFound(w, r)
		return true
	}
	return false
}

func (s *Server) markBlockedDownloadUAs(ownerID string, records []Download) error {
	hashes := []string{}
	seen := map[string]bool{}
	for _, record := range records {
		if hash := uaFingerprint(record.UserAgent); hash != "" && !seen[hash] {
			seen[hash] = true
			hashes = append(hashes, hash)
		}
	}
	blocked, err := s.blockedUAHashes(ownerID, hashes)
	if err != nil {
		return err
	}
	for i := range records {
		records[i].UABlocked = blocked[uaFingerprint(records[i].UserAgent)]
	}
	return nil
}
