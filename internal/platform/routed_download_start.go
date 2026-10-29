package platform

import (
	"errors"

	"gorm.io/gorm"
)

func (s *Server) queueRoutedDownloadStart(tx *gorm.DB, t Ticket) error {
	if t.AttributionID == "" {
		return nil
	}
	var attr Attribution
	err := tx.Select("id", "source_attribution_id").First(&attr, "id = ?", t.AttributionID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if attr.SourceAttributionID == "" {
		return nil
	}
	return s.queueDownloadConversion(tx, t)
}

// Called after the first file bytes are written, never on landing-page clicks,
// ticket redirects, HEAD, error pages, or zero-byte responses. No provider HTTP
// request runs in the streaming path; the existing durable ad worker sends it.
func (s *Server) recordRoutedDownloadStart(t Ticket) error {
	if t.Hash == "" || t.AttributionID == "" {
		return nil
	}
	var attr Attribution
	err := s.DB.Select("id", "source_attribution_id").First(&attr, "id = ?", t.AttributionID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if attr.SourceAttributionID == "" {
		return nil
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		u, e := lockUser(tx, t.UserID)
		if e != nil {
			return e
		}
		if u.Disabled {
			return nil
		}
		var ticket Ticket
		if e = tx.First(&ticket, "hash = ? AND user_id = ?", t.Hash, t.UserID).Error; e != nil {
			return e
		}
		if ticket.StartedAt == nil || ticket.Released || !ticket.Reserved && !ticket.Charged {
			return nil
		}
		return s.queueRoutedDownloadStart(tx, ticket)
	})
}
