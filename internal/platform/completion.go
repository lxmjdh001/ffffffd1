package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Each download node owns its durable journal volume. A completion is journaled
// before the accounting transaction; replay is safe because ranges and billing
// are idempotent under the owner lock. Keep this volume across node restarts.
type completionReceipt struct {
	TicketHash string `json:"ticket_hash"`
	UserID     string `json:"user_id"`
	Start      int64  `json:"start"`
	End        int64  `json:"end"`
}

func (s *Server) writeReceipt(t Ticket, start, end int64) (string, error) {
	dir := filepath.Join(s.Config.DataDir, "completions")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	f, e := os.CreateTemp(dir, ".pending-")
	if e != nil {
		return "", e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = json.NewEncoder(f).Encode(completionReceipt{t.Hash, t.UserID, start, end}); e != nil {
		return "", e
	}
	if e = f.Sync(); e != nil {
		return "", e
	}
	if e = f.Close(); e != nil {
		return "", e
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%d-%d.json", t.Hash, start, end))
	if e = os.Rename(f.Name(), path); e != nil {
		return "", e
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return path, nil
}

func (s *Server) persistCompletion(t Ticket, start, end int64) {
	path, journalErr := s.writeReceipt(t, start, end)
	if journalErr != nil {
		slog.Error("completion journal unavailable", "error", journalErr, "ticket", t.Hash)
	}
	if e := s.finishRange(t, start, end); e != nil {
		slog.Error("download completion queued for recovery", "ticket", t.Hash, "journaled", path != "", "error", e)
		return
	}
	if t.AttributionID != "" {
		// Keep the durable range receipt until background ad preparation succeeds.
		// Advertising errors must never roll back this accounting transaction.
		s.wakeDownloadAds()
		return
	}
	if path != "" {
		_ = os.Remove(path)
	}
}

func (s *Server) replayCompletions(ctx context.Context) {
	dir := filepath.Join(s.Config.DataDir, "completions")
	entries, e := os.ReadDir(dir)
	if os.IsNotExist(e) {
		return
	}
	if e != nil {
		slog.Error("completion journal read failed", "error", e)
		return
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, e := os.ReadFile(path)
		var receipt completionReceipt
		if e != nil || json.Unmarshal(raw, &receipt) != nil || !hexKey.MatchString(receipt.TicketHash) || !hexKey.MatchString(receipt.UserID) {
			slog.Error("invalid completion receipt", "file", entry.Name())
			continue
		}
		if e = s.finishRange(Ticket{Hash: receipt.TicketHash, UserID: receipt.UserID}, receipt.Start, receipt.End); e != nil {
			slog.Error("completion recovery deferred", "file", entry.Name(), "error", e)
			continue
		}
		var ticket Ticket
		if e = s.DB.WithContext(ctx).Select("attribution_id").First(&ticket, "hash = ? AND user_id = ?", receipt.TicketHash, receipt.UserID).Error; e != nil {
			continue
		}
		if ticket.AttributionID != "" {
			s.wakeDownloadAds()
			continue
		}
		_ = os.Remove(path)
	}
}

func (s *Server) RunCompletionWorker(ctx context.Context) {
	s.replayCompletions(ctx)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.replayCompletions(ctx)
		case <-s.completionWake:
			s.replayCompletions(ctx)
		}
	}
}
