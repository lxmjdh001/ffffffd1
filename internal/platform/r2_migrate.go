package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gorm.io/gorm"
)

func verifyStoredFile(ctx context.Context, store BlobStore, key string, size int64, sha string) error {
	f, n, err := store.Open(ctx, key)
	if err != nil {
		return err
	}
	defer f.Close()
	if n != size {
		return errors.New("R2 migration size mismatch")
	}
	h := sha256.New()
	n, err = io.Copy(h, io.LimitReader(f, size+1))
	if err != nil {
		return err
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != sha {
		return errors.New("R2 migration SHA256 mismatch")
	}
	return nil
}

// Run with writes stopped and a DB backup. Safe to repeat after interruption:
// packages retain their keys/URLs; media is switched only after a verified GET.
// No local files are deleted. External HTTPS URLs are never fetched or copied.
func (s *Server) MigrateFilesToR2(ctx context.Context) error {
	if s.Media == nil {
		return errors.New("R2 storage is not configured")
	}
	if err := s.Media.checkBucket(ctx); err != nil {
		return err
	}
	packages := s.Media.packages()
	var apps []Application
	if err := s.DB.WithContext(ctx).Select("id", "file_key", "size", "sha256").Find(&apps).Error; err != nil {
		return err
	}
	for _, a := range apps {
		if !hexKey.MatchString(a.FileKey) {
			return fmt.Errorf("invalid package key for app %s", a.ID)
		}
		if err := verifyStoredFile(ctx, packages, a.FileKey, a.Size, a.SHA256); err == nil {
			continue
		}
		path := filepath.Join(s.Config.DataDir, "files", a.FileKey)
		local := LocalStore{Root: filepath.Join(s.Config.DataDir, "files")}
		if err := verifyStoredFile(ctx, local, a.FileKey, a.Size, a.SHA256); err != nil {
			return fmt.Errorf("source package %s: %w", a.ID, err)
		}
		if err := packages.Put(ctx, a.FileKey, path, a.Size, a.SHA256); err != nil {
			return err
		}
		if err := verifyStoredFile(ctx, packages, a.FileKey, a.Size, a.SHA256); err != nil {
			return err
		}
	}
	// Binary columns are read one row at a time, never all users' images at once.
	var icons []PWAApp
	if err := s.DB.WithContext(ctx).Select("id").Where("length(icon_png) > 0 AND icon_object_key = ''").Find(&icons).Error; err != nil {
		return err
	}
	for _, a := range icons {
		objectKey := ""
		if err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var row PWAApp
			if err := tx.Unscoped().First(&row, "id = ?", a.ID).Error; err != nil {
				return err
			}
			if row.IconObjectKey != "" || len(row.IconPNG) == 0 {
				return nil
			}
			key, err := s.migrateR2PNG(ctx, tx, "pwa-icons", row.IconPNG)
			objectKey = key
			if err != nil {
				return err
			}
			return tx.Unscoped().Model(&PWAApp{}).Where("id = ?", row.ID).Updates(map[string]any{"icon_object_key": key, "icon_png": []byte{}}).Error
		}); err != nil {
			s.Media.discard(objectKey)
			return err
		}
	}
	var images []PWAPushAsset
	if err := s.DB.WithContext(ctx).Select("id").Where("length(png) > 0 AND object_key = ''").Find(&images).Error; err != nil {
		return err
	}
	for _, a := range images {
		objectKey := ""
		if err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var row PWAPushAsset
			if err := tx.First(&row, "id = ?", a.ID).Error; err != nil {
				return err
			}
			if row.ObjectKey != "" || len(row.PNG) == 0 {
				return nil
			}
			key, err := s.migrateR2PNG(ctx, tx, "push-images", row.PNG)
			objectKey = key
			if err != nil {
				return err
			}
			return tx.Model(&PWAPushAsset{}).Where("id = ?", row.ID).Updates(map[string]any{"object_key": key, "png": []byte{}}).Error
		}); err != nil {
			s.Media.discard(objectKey)
			return err
		}
	}
	var assets []ReturnFlowAsset
	if err := s.DB.WithContext(ctx).Where("object_key = ''").Find(&assets).Error; err != nil {
		return err
	}
	for _, a := range assets {
		name := filepath.Base(a.PublicPath)
		if a.PublicPath != "/return-flow-assets/"+name || !returnFlowFilePattern.MatchString(name) {
			return errors.New("invalid legacy media path")
		}
		file, err := os.Open(filepath.Join(s.returnFlowAssetDirectory(), name))
		if err != nil {
			return err
		}
		key := s.Media.Prefix + "return-flow/" + name
		err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := s.Media.put(ctx, tx, key, file, a.Size, a.ContentType, a.SHA256); err != nil {
				return err
			}
			if err := s.verifyMedia(ctx, key, a.Size, a.SHA256); err != nil {
				return err
			}
			return tx.Model(&ReturnFlowAsset{}).Where("id = ? AND object_key = ''", a.ID).Update("object_key", key).Error
		})
		file.Close()
		if err != nil {
			s.Media.discard(key)
			return err
		}
	}
	return nil
}

func (s *Server) verifyMedia(ctx context.Context, key string, size int64, sha string) error {
	f, n, err := s.Media.open(ctx, key)
	if err != nil {
		return err
	}
	defer f.Close()
	if n != size {
		return errors.New("R2 media size mismatch")
	}
	h := sha256.New()
	n, err = io.Copy(h, io.LimitReader(f, size+1))
	if err != nil {
		return err
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != sha {
		return errors.New("R2 media SHA256 mismatch")
	}
	return nil
}
func (s *Server) migrateR2PNG(ctx context.Context, tx *gorm.DB, kind string, data []byte) (key string, err error) {
	key, err = s.Media.putPNG(ctx, tx, kind, data)
	if err == nil {
		err = s.verifyMedia(ctx, key, int64(len(data)), hashString(string(data)))
	}
	return
}
