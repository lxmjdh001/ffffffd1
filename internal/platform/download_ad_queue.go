package platform

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
)

type downloadAdStart struct {
	TicketHash string    `json:"ticket_hash"`
	UserID     string    `json:"user_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

// The streaming path performs no database, filesystem, logging, or provider
// operations. A bounded inbox also prevents a slow consumer from creating an
// unbounded number of goroutines. Confirmed-range journals recover overflow.
func (s *Server) noticeDownloadAdStart(t Ticket) {
	if t.Hash == "" || t.AttributionID == "" {
		return
	}
	select {
	case s.downloadAdStarts <- downloadAdStart{t.Hash, t.UserID, s.Now()}:
	default:
		s.downloadAdOverflows.Add(1)
	}
}

func (s *Server) wakeDownloadAds() {
	select {
	case s.downloadAdWake <- struct{}{}:
	default:
	}
}
func (s *Server) wakeCompletions() {
	select {
	case s.completionWake <- struct{}{}:
	default:
	}
}

// This runs only in the spool worker. Persist before touching the ad database,
// so a database outage cannot stall the inbox or lose already journaled starts.
func (s *Server) writeDownloadAdStart(event downloadAdStart) (string, error) {
	dir := filepath.Join(s.Config.DataDir, "download-ad-starts")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, event.TicketHash+".json")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	f, err := os.CreateTemp(dir, ".pending-")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = json.NewEncoder(f).Encode(event); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return "", err
	}
	if d, e := os.Open(dir); e == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return path, nil
}

// Ad preparation never takes the balance/owner row lock and has its own short
// deadline. Completion accounting remains independent even if this fails.
func (s *Server) recordDownloadAd(ctx context.Context, event downloadAdStart) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ticket Ticket
		if err := tx.First(&ticket, "hash = ? AND user_id = ?", event.TicketHash, event.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if ticket.AttributionID == "" || ticket.StartedAt == nil || !ticket.Reserved && !ticket.Charged {
			return nil
		}
		var owner User
		if err := tx.Select("id", "disabled").First(&owner, "id = ?", ticket.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if owner.Disabled {
			return nil
		}
		var attr Attribution
		if err := tx.First(&attr, "id = ?", ticket.AttributionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		at := event.OccurredAt
		if at.IsZero() {
			at = *ticket.StartedAt
		}
		if attr.SourceAttributionID == "" {
			// Preserve ordinary downloads' completed-file trigger.
			if !ticket.Charged {
				return nil
			}
			var completed Download
			if err := tx.Select("completed_at").First(&completed, "ticket_hash = ?", ticket.Hash).Error; err != nil {
				return err
			}
			at = completed.CompletedAt
		}
		if at.Before(s.Now().Add(-7 * 24 * time.Hour)) {
			return nil
		}
		return s.queueDownloadConversionAt(tx, ticket, at)
	})
}

func (s *Server) spoolDownloadAdStart(ctx context.Context, event downloadAdStart) {
	if _, err := s.writeDownloadAdStart(event); err != nil {
		slog.Error("download advertising journal unavailable", "error", err)
		// A local disk failure must not suppress an otherwise usable database.
		if err = s.recordDownloadAd(ctx, event); err != nil {
			slog.Error("download advertising deferred to range recovery", "error", err)
		}
		return
	}
	s.wakeDownloadAds()
}

func (s *Server) replayDownloadAdStarts(ctx context.Context) {
	dir := filepath.Join(s.Config.DataDir, "download-ad-starts")
	f, err := os.Open(dir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		slog.Error("download advertising journal read failed", "error", err)
		return
	}
	defer f.Close()
	// Bound work per pass. The caller also caps its time budget, while one bad
	// receipt must not prevent other downloads from preparing their callbacks.
	entries, _ := f.ReadDir(256)
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		var event downloadAdStart
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || json.Unmarshal(raw, &event) != nil || !hexKey.MatchString(event.TicketHash) || !hexKey.MatchString(event.UserID) || event.OccurredAt.IsZero() || event.TicketHash+".json" != entry.Name() {
			slog.Error("invalid download advertising receipt", "file", entry.Name())
			continue
		}
		if err = s.recordDownloadAd(ctx, event); err != nil {
			if ctx.Err() == nil {
				slog.Warn("download advertising receipt deferred", "error", err)
			}
			continue
		}
		_ = os.Remove(path)
	}
}

// Range receipts are also an independent recovery path for a saturated inbox
// or a restart. Never delete their accounting proof before its range is saved.
func (s *Server) replayDownloadAdCompletions(ctx context.Context) {
	dir := filepath.Join(s.Config.DataDir, "completions")
	f, err := os.Open(dir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		slog.Error("download advertising recovery read failed", "error", err)
		return
	}
	defer f.Close()
	entries, _ := f.ReadDir(256)
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		var receipt completionReceipt
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || json.Unmarshal(raw, &receipt) != nil || !hexKey.MatchString(receipt.TicketHash) || !hexKey.MatchString(receipt.UserID) {
			continue
		}
		readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		var ticket Ticket
		err = s.DB.WithContext(readCtx).Select("attribution_id", "charged", "ranges").First(&ticket, "hash = ? AND user_id = ?", receipt.TicketHash, receipt.UserID).Error
		cancel()
		if err != nil {
			continue
		}
		accounted := ticket.Charged
		if !accounted {
			var ranges []byteRange
			if json.Unmarshal([]byte(ticket.Ranges), &ranges) == nil {
				for _, r := range ranges {
					if r.Start <= receipt.Start && r.End >= receipt.End {
						accounted = true
						break
					}
				}
			}
		}
		if !accounted {
			s.wakeCompletions()
			continue
		}
		if ticket.AttributionID != "" {
			if err = s.recordDownloadAd(ctx, downloadAdStart{TicketHash: receipt.TicketHash, UserID: receipt.UserID}); err != nil {
				if ctx.Err() == nil {
					slog.Warn("download advertising recovery deferred", "error", err)
				}
				continue
			}
		}
		_ = os.Remove(path)
	}
}

func (s *Server) replayDownloadAds(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	s.replayDownloadAdStarts(ctx)
	if ctx.Err() == nil {
		s.replayDownloadAdCompletions(ctx)
	}
}

// Every download node journals locally and prepares the shared DB outbox.
// Provider HTTP remains exclusively in RunAdWorker on the delivery nodes.
func (s *Server) RunDownloadAdWorker(ctx context.Context) {
	spooled := make(chan struct{})
	go func() {
		defer close(spooled)
		for {
			select {
			case event := <-s.downloadAdStarts:
				s.spoolDownloadAdStart(ctx, event)
			case <-ctx.Done():
				// HTTP shutdown happens before this cancellation. Flush the finite
				// remaining inbox to disk; the next process replays its receipts.
				for {
					select {
					case event := <-s.downloadAdStarts:
						s.spoolDownloadAdStart(ctx, event)
					default:
						return
					}
				}
			}
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	s.replayDownloadAds(ctx)
	for {
		select {
		case <-ctx.Done():
			<-spooled
			return
		case <-s.downloadAdWake:
			s.replayDownloadAds(ctx)
		case <-ticker.C:
			if count := s.downloadAdOverflows.Swap(0); count > 0 {
				slog.Warn("download advertising inbox busy; confirmed ranges recover callbacks", "count", count)
				s.wakeCompletions()
			}
			s.replayDownloadAds(ctx)
		}
	}
}
