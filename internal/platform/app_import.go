package platform

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"
)

// Clone app-scoped pictures as well as the design: deleting or changing the
// source must never invalidate a new application's landing page.
func (s *Server) importAppDesign(ctx context.Context, tx *gorm.DB, source, target Application, copied *[]string) error {
	var saved AppPageDesign
	err := tx.First(&saved, "app_id = ?", source.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	replacements := map[string]string{}
	var clone func(*PageDesign) error
	image := func(raw string) (string, error) {
		if s.PageImages == nil || !strings.HasPrefix(raw, s.PageImages.PublicURL+"/"+s.PageImages.Prefix) {
			return raw, nil
		}
		if value, ok := replacements[raw]; ok {
			return value, nil
		}
		var asset PageImageObject
		if err := tx.First(&asset, "app_id = ? AND user_id = ? AND url = ?", source.ID, source.UserID, raw).Error; err != nil {
			return "", err
		}
		if !s.PageImages.validKey(asset.Key) {
			return "", problem(400, "图片引用无效")
		}
		object, err := s.PageImages.Client.GetObject(ctx, s.PageImages.Bucket, asset.Key, minio.GetObjectOptions{})
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(io.LimitReader(object, maxPageImageBytes+1))
		_ = object.Close()
		if err != nil {
			return "", err
		}
		if len(data) > maxPageImageBytes {
			return "", problem(400, "图片引用无效")
		}
		key := s.PageImages.Prefix + target.ID + "/" + randomHex(16) + ".jpg"
		*copied = append(*copied, key)
		if err = s.PageImages.Put(ctx, key, data); err != nil {
			return "", err
		}
		next := PageImageObject{Key: key, UserID: target.UserID, AppID: target.ID, URL: s.PageImages.PublicURL + "/" + key, CreatedAt: s.Now()}
		if err = tx.Create(&next).Error; err != nil {
			return "", err
		}
		replacements[raw] = next.URL
		return next.URL, nil
	}
	clone = func(d *PageDesign) error {
		var err error
		if d.Icon, err = image(d.Icon); err != nil {
			return err
		}
		for i := range d.Screenshots {
			if d.Screenshots[i], err = image(d.Screenshots[i]); err != nil {
				return err
			}
		}
		for i := range d.Localizations {
			if err = clone(&d.Localizations[i]); err != nil {
				return err
			}
		}
		return nil
	}
	if err = clone(&saved.Design); err != nil {
		return err
	}
	saved.AppID = target.ID
	saved.UpdatedAt = s.Now()
	return tx.Create(&saved).Error
}

func (s *Server) discardImportedImages(keys []string) {
	if s.PageImages == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, key := range keys {
		_ = s.PageImages.Delete(ctx, key)
	}
}
