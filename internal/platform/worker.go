package platform

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
)

// Durable orders survive restarts. The rate counter is a shared lease to keep
// multiple replicas from hammering the external payment and DNS services.
func (s *Server) RunWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	s.maintenance(ctx)
	go s.runTrafficRiskChecks(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.maintenance(ctx)
		}
	}
}
func (s *Server) maintenance(ctx context.Context) {
	if e := s.rate("worker-lease", 1, time.Minute); e != nil {
		return
	}
	s.cleanupVisitStatistics(ctx)
	if e := s.cleanupMeteredUsage(ctx); e != nil {
		slog.Error("usage reservation cleanup failed", "error", e)
	}
	now := s.Now()
	var expired []Ticket
	if e := s.DB.WithContext(ctx).Where("reserved = ? AND charged = ? AND released = ? AND expires_at < ?", true, false, false, now).Limit(1000).Find(&expired).Error; e == nil {
		for _, ticket := range expired {
			if e = s.releaseTicket(ticket); e != nil {
				slog.Error("reservation release failed", "ticket", ticket.Hash, "error", e)
			}
		}
	}
	s.DB.WithContext(ctx).Where("expires_at < ? AND (reserved = ? OR charged = ? OR released = ?)", now.Add(-24*time.Hour), false, true, true).Delete(&Ticket{})
	for _, m := range []any{&Session{}, &Challenge{}, &RateBucket{}} {
		if e := s.DB.WithContext(ctx).Where("expires_at < ?", now).Delete(m).Error; e != nil {
			slog.Error("expiry cleanup failed", "error", e)
		}
	}
	if e := s.DB.WithContext(ctx).Model(&Order{}).Where("status = ? AND expires_at < ?", "pending", now).Update("status", "expired").Error; e != nil {
		slog.Error("order expiry failed", "error", e)
	}
	var orders []Order
	if e := s.DB.WithContext(ctx).Where("method = ? AND status IN ? AND created_at > ?", "usdt", []string{"pending", "expired"}, now.Add(-48*time.Hour)).Order("created_at DESC").Find(&orders).Error; e != nil {
		slog.Error("payment queue read failed", "error", e)
		return
	}
	// At most 15 calls per minute; rotate over older pending orders as well.
	if len(orders) > 0 {
		start := int(now.Unix()/60*15) % len(orders)
		count := len(orders)
		if count > 15 {
			count = 15
		}
		for i := 0; i < count; i++ {
			if ctx.Err() != nil {
				return
			}
			o := orders[(start+i)%len(orders)]
			if e := s.reconcileUSDT(ctx, o); e != nil {
				slog.Warn("payment reconciliation deferred", "order", o.ID)
			}
		}
	}
	if e := s.rate("domain-worker-lease", 1, 6*time.Hour); e == nil {
		var ds []Domain
		if e = s.DB.WithContext(ctx).Where("verified_at IS NOT NULL").Find(&ds).Error; e != nil {
			slog.Error("domain queue failed", "error", e)
		} else {
			for _, d := range ds {
				// The native domain worker revalidates proxy-backed domains with
				// an HTTP challenge and refreshes verified_at after success.
				if d.VerificationMethod == "http" {
					continue
				}
				if ctx.Err() != nil {
					return
				}
				if e = s.checkDNS(ctx, d); e == nil {
					s.DB.WithContext(ctx).Model(&Domain{}).Where("id = ? AND token = ?", d.ID, d.Token).Updates(map[string]any{"verified_at": now, "last_error": ""})
				} else {
					s.DB.WithContext(ctx).Model(&Domain{}).Where("id = ? AND token = ?", d.ID, d.Token).Update("last_error", e.Error())
				}
			}
		}
	}
	if e := s.rate("file-worker-lease", 1, 24*time.Hour); e == nil {
		s.cleanupOrphans(ctx)
	}
}
func (s *Server) cleanupOrphans(ctx context.Context) {
	if s.Config.Role != "download" {
		_ = s.cleanupPageImages(ctx, "")
	}
	var apps []Application
	if e := s.DB.WithContext(ctx).Select("file_key").Find(&apps).Error; e != nil {
		return
	}
	live := map[string]bool{}
	for _, a := range apps {
		live[a.FileKey] = true
	}
	if storage, ok := s.Blobs.(S3Store); ok {
		for object := range storage.Client.ListObjects(ctx, storage.Bucket, minio.ListObjectsOptions{Prefix: "apps/", Recursive: true}) {
			if object.Err != nil {
				slog.Error("object cleanup listing failed", "error", object.Err)
				return
			}
			key := strings.TrimPrefix(object.Key, "apps/")
			if !hexKey.MatchString(key) || live[key] || object.LastModified.After(s.Now().Add(-24*time.Hour)) {
				continue
			}
			if e := storage.Delete(ctx, key); e != nil {
				slog.Warn("orphan object cleanup failed", "key", key, "error", e)
			}
		}
	}
	entries, e := os.ReadDir(filepath.Join(s.Config.DataDir, "files"))
	if e != nil {
		return
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if entry.IsDir() || live[entry.Name()] {
			continue
		}
		info, e := entry.Info()
		if e != nil || info.ModTime().After(s.Now().Add(-24*time.Hour)) {
			continue
		}
		if e = os.Remove(filepath.Join(s.Config.DataDir, "files", entry.Name())); e != nil {
			slog.Warn("orphan cleanup failed", "file", entry.Name())
		}
	}
}

// Local B nodes have their own disks; A's shared maintenance lease cannot clean
// those disks. S3 cleanup is already owned by the control-server maintenance.
func (s *Server) RunFileWorker(ctx context.Context) {
	if _, local := s.Blobs.(LocalStore); !local {
		return
	}
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		s.cleanupOrphans(ctx)
		entries, _ := os.ReadDir(filepath.Join(s.Config.DataDir, "tmp"))
		for _, entry := range entries {
			if info, e := entry.Info(); e == nil && !entry.IsDir() && info.ModTime().Before(s.Now().Add(-24*time.Hour)) {
				_ = os.Remove(filepath.Join(s.Config.DataDir, "tmp", entry.Name()))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
