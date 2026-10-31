package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const visitBatchSize = 128

// Analytics only: financial events, download receipts and advertising outboxes
// never enter this queue. A bounded inbox avoids one goroutine per page view.
type visitWriter struct {
	db      *gorm.DB
	dir     string
	inbox   chan AccessVisit
	stop    chan struct{}
	done    chan struct{}
	gate    sync.RWMutex
	closing bool
	retryAt time.Time // worker-owned; a database outage must not stall every batch
}

// Start once before accepting HTTP traffic, then stop after HTTP has drained.
// Graceful shutdown flushes the inbox; failed writes and overflow are journaled
// for restart recovery. An abrupt kill can lose the unflushed in-memory tail.
func (s *Server) StartVisitWriter() func(context.Context) error {
	if s.visitWriter == nil {
		v := &visitWriter{db: s.DB, dir: filepath.Join(s.Config.DataDir, "visit-journal"), inbox: make(chan AccessVisit, 4096), stop: make(chan struct{}), done: make(chan struct{})}
		if err := os.MkdirAll(v.dir, 0700); err != nil {
			slog.Error("visit journal unavailable; retaining synchronous statistics", "error", err)
			return func(context.Context) error { return nil }
		}
		s.visitWriter = v
		go v.run()
	}
	v := s.visitWriter
	return func(ctx context.Context) error {
		v.gate.Lock()
		if !v.closing {
			v.closing = true
			close(v.stop)
		}
		v.gate.Unlock()
		select {
		case <-v.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (v *visitWriter) record(row AccessVisit) {
	v.gate.RLock()
	if !v.closing {
		select {
		case v.inbox <- row:
			v.gate.RUnlock()
			return
		default:
		}
	}
	v.gate.RUnlock()
	// A full inbox never waits for PostgreSQL. The exceptional overflow path
	// pays a local durable write instead of silently dropping the visit.
	v.journal([]AccessVisit{row})
}

func (v *visitWriter) journal(rows []AccessVisit) {
	if err := v.persist(rows); err != nil {
		slog.Error("visit statistics journal failed", "visits", len(rows), "error", err)
	}
}

func (v *visitWriter) persist(rows []AccessVisit) error {
	f, err := os.CreateTemp(v.dir, ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = json.NewEncoder(f).Encode(rows); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), filepath.Join(v.dir, randomHex(16)+".json")); err != nil {
		return err
	}
	if d, e := os.Open(v.dir); e == nil {
		defer d.Close()
		return d.Sync()
	}
	return nil
}

func (v *visitWriter) insert(ctx context.Context, rows []AccessVisit) error {
	if len(rows) == 0 {
		return nil
	}
	// One atomic INSERT is sufficient. Stable IDs make retries safe even if
	// PostgreSQL committed but the acknowledgement was lost.
	return v.db.WithContext(ctx).Session(&gorm.Session{SkipDefaultTransaction: true}).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func (v *visitWriter) flush(rows []AccessVisit) {
	if len(rows) == 0 {
		return
	}
	if time.Now().Before(v.retryAt) {
		v.journal(rows)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := v.insert(ctx, rows); err != nil {
		v.retryAt = time.Now().Add(time.Second)
		v.journal(rows)
		slog.Warn("visit statistics deferred to journal", "error", err)
	}
}

func (v *visitWriter) replay(ctx context.Context) {
	f, err := os.Open(v.dir)
	if err != nil {
		return
	}
	defer f.Close()
	// Read and replay bounded work so recovery cannot starve fresh visits.
	processed := 0
	for processed < 32 {
		entries, readErr := f.ReadDir(32)
		for _, entry := range entries {
			if processed >= 32 || ctx.Err() != nil {
				return
			}
			if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			processed++
			path := filepath.Join(v.dir, entry.Name())
			journal, err := os.Open(path)
			if err != nil {
				continue
			}
			var rows []AccessVisit
			err = json.NewDecoder(io.LimitReader(journal, 1<<20)).Decode(&rows)
			journal.Close()
			if err != nil || len(rows) == 0 || len(rows) > visitBatchSize {
				slog.Error("invalid visit journal", "file", entry.Name())
				// Retain malformed files for inspection without letting the first
				// directory page permanently block valid recovery records.
				quarantine := filepath.Join(v.dir, "invalid")
				if e := os.MkdirAll(quarantine, 0700); e == nil {
					if e = os.Rename(path, filepath.Join(quarantine, entry.Name())); e != nil {
						slog.Error("visit journal quarantine failed", "file", entry.Name(), "error", e)
					}
				}
				continue
			}
			if err = v.insert(ctx, rows); err != nil {
				v.retryAt = time.Now().Add(time.Second)
				if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
					slog.Warn("visit statistics recovery deferred", "error", err)
				}
				return
			}
			if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
				slog.Warn("visit journal cleanup deferred", "error", err)
			}
			if processed == 32 {
				return
			}
		}
		if readErr != nil || len(entries) == 0 {
			return
		}
	}
}

func (v *visitWriter) run() {
	defer close(v.done)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	retry := time.NewTicker(time.Second)
	defer retry.Stop()
	rows := make([]AccessVisit, 0, visitBatchSize)
	flush := func() { v.flush(rows); rows = rows[:0] }
	appendRow := func(row AccessVisit) {
		rows = append(rows, row)
		if len(rows) == visitBatchSize {
			flush()
		}
	}
	for {
		select {
		case row := <-v.inbox:
			appendRow(row)
		case <-ticker.C:
			flush()
		case <-retry.C:
			if !time.Now().Before(v.retryAt) {
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				v.replay(ctx)
				cancel()
			}
		case <-v.stop:
			// The gate prevents sends racing the last empty-queue check.
			for {
				select {
				case row := <-v.inbox:
					appendRow(row)
				default:
					flush()
					return
				}
			}
		}
	}
}
