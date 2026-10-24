package platform

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/gorm"
)

// Page images are public assets, independent of private APK storage. Never
// repurpose S3_*: doing so would change where existing packages are read.
type PageImageConfig struct {
	Endpoint, AccessKey, SecretKey, Bucket, PublicURL string
}

func (c PageImageConfig) validate() error {
	if c == (PageImageConfig{}) {
		return nil
	}
	if c.Endpoint == "" || c.AccessKey == "" || c.SecretKey == "" || c.Bucket == "" || c.PublicURL == "" {
		return errors.New("all PAGE_IMAGE_R2 settings are required together")
	}
	for _, raw := range []string{c.Endpoint, c.PublicURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return errors.New("PAGE_IMAGE_R2 endpoint and public URL must be HTTPS origins")
		}
	}
	if strings.ContainsAny(c.Bucket, "/\\ \t\r\n") || len(c.PublicURL) > 512 {
		return errors.New("invalid PAGE_IMAGE_R2 bucket or public URL")
	}
	return nil
}

type R2PageImageStore struct {
	Client                    *minio.Client
	Bucket, PublicURL, Prefix string
}

func NewPageImageStore(c Config) (*R2PageImageStore, error) {
	if err := c.PageImages.validate(); err != nil {
		return nil, err
	}
	if c.PageImages.Endpoint == "" {
		return nil, nil
	}
	u, _ := url.Parse(c.PageImages.Endpoint)
	client, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(c.PageImages.AccessKey, c.PageImages.SecretKey, ""), Secure: true, Region: "auto", BucketLookup: minio.BucketLookupPath})
	if err != nil {
		return nil, err
	}
	// Different origins/environments never share a cleanup namespace, even
	// when development and production use PWA's same bucket and credentials.
	scope := hashString(u.Host + "\n" + c.PageImages.Bucket + "\n" + c.PublicURL)[:16]
	return &R2PageImageStore{Client: client, Bucket: c.PageImages.Bucket, PublicURL: strings.TrimRight(c.PageImages.PublicURL, "/"), Prefix: "d1apk/page-images/" + scope + "/"}, nil
}

func (s *R2PageImageStore) validKey(key string) bool {
	if !strings.HasPrefix(key, s.Prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(key, s.Prefix), "/")
	return len(parts) == 2 && hexKey.MatchString(parts[0]) && strings.HasSuffix(parts[1], ".jpg") && hexKey.MatchString(strings.TrimSuffix(parts[1], ".jpg"))
}

func (s *R2PageImageStore) Put(ctx context.Context, key string, data []byte) error {
	if !s.validKey(key) {
		return errors.New("invalid D1 page image key")
	}
	_, err := s.Client.PutObject(ctx, s.Bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: "image/jpeg", CacheControl: "public, max-age=31536000, immutable", DisableMultipart: true})
	return err
}

func (s *R2PageImageStore) Delete(ctx context.Context, key string) error {
	if !s.validKey(key) {
		return errors.New("refusing to delete an image outside this D1 namespace")
	}
	return s.Client.RemoveObject(ctx, s.Bucket, key, minio.RemoveObjectOptions{})
}

// Records contain only D1-created objects. Cleanup never lists the shared R2
// bucket and never derives deletion targets from user-pasted image URLs.
type PageImageObject struct {
	Key       string    `gorm:"primaryKey;size:200"`
	UserID    string    `gorm:"size:32;index"`
	AppID     string    `gorm:"size:32;index"`
	URL       string    `gorm:"size:2048"`
	CreatedAt time.Time `gorm:"index"`
}

const maxPageImageBytes = 512 << 10

func (s *Server) uploadPageImage(w http.ResponseWriter, r *http.Request) {
	a, err := s.ownedApp(r)
	if err != nil {
		fail(w, err)
		return
	}
	if s.PageImages == nil {
		fail(w, problem(503, "图片存储尚未配置，请联系管理员配置 R2"))
		return
	}
	if err = s.rate("page-image:"+a.UserID, 60, 10*time.Minute); err != nil {
		fail(w, err)
		return
	}
	select {
	case s.uploadSlots <- struct{}{}:
		defer func() { <-s.uploadSlots }()
	default:
		fail(w, problem(429, "上传繁忙，请稍后重试"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPageImageBytes+8192)
	if err = r.ParseMultipartForm(maxPageImageBytes + 8192); err != nil {
		fail(w, problem(400, "图片上传格式无效或超过 512 KB，请重新选择图片"))
		return
	}
	defer r.MultipartForm.RemoveAll()
	files := r.MultipartForm.File["file"]
	if len(files) != 1 || len(r.MultipartForm.File) != 1 || files[0].Size > maxPageImageBytes {
		fail(w, problem(400, "每次只能上传一张不超过 512 KB 的图片"))
		return
	}
	f, err := files[0].Open()
	if err != nil {
		fail(w, err)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPageImageBytes+1))
	if err != nil || len(data) > maxPageImageBytes {
		fail(w, problem(400, "图片读取失败或文件过大"))
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 1400 || cfg.Height > 1400 {
		fail(w, problem(400, "图片无法读取，请重新选择 PNG、JPEG 或 WebP 图片上传"))
		return
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		fail(w, problem(400, "图片文件已损坏，请重新选择"))
		return
	}
	// Re-encode to strip metadata and appended payloads; never trust MIME or
	// filenames from the browser. WebP is converted to JPEG before upload.
	clean := image.NewRGBA(decoded.Bounds())
	draw.Draw(clean, clean.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(clean, clean.Bounds(), decoded, decoded.Bounds().Min, draw.Over)
	var out bytes.Buffer
	if err = jpeg.Encode(&out, clean, &jpeg.Options{Quality: 85}); err != nil || out.Len() > maxPageImageBytes {
		fail(w, problem(400, "图片压缩后仍过大，请选择更小的图片"))
		return
	}
	key := s.PageImages.Prefix + a.ID + "/" + randomHex(16) + ".jpg"
	asset := PageImageObject{Key: key, AppID: a.ID, UserID: a.UserID, URL: s.PageImages.PublicURL + "/" + key, CreatedAt: s.Now()}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	stored := false
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockUser(tx, a.UserID); err != nil {
			return err
		}
		// Serialize with app deletion and saving so a concurrent delete cannot
		// leave a late upload attached to an already-deleted application.
		if err := tx.First(&Application{}, "id = ? AND user_id = ?", a.ID, a.UserID).Error; err != nil {
			return err
		}
		if err := s.PageImages.Put(ctx, key, out.Bytes()); err != nil {
			slog.Warn("page image upload failed", "app", a.ID, "error", err)
			return problem(502, "R2 图片上传失败，请稍后重试；持续失败请联系管理员检查配置")
		}
		stored = true
		return tx.Create(&asset).Error
	})
	if err != nil {
		if stored {
			cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			if e := s.PageImages.Delete(cleanup, key); e != nil {
				slog.Warn("uncommitted page image cleanup failed", "key", key, "error", e)
			}
		}
		fail(w, err)
		return
	}
	jsonOut(w, 201, map[string]string{"url": asset.URL})
}

func (s *Server) validatePageImageReferences(tx *gorm.DB, appID string, d PageDesign) error {
	if s.PageImages == nil {
		return nil
	}
	for _, raw := range append([]string{d.Icon}, d.Screenshots...) {
		if !strings.HasPrefix(raw, s.PageImages.PublicURL+"/"+s.PageImages.Prefix) {
			continue // Existing inline images and external HTTPS images still work.
		}
		var asset PageImageObject
		if err := tx.First(&asset, "app_id = ? AND url = ?", appID, raw).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return problem(400, "图片已过期或不属于当前应用，请重新上传")
			}
			return err
		}
	}
	return nil
}

func (s *Server) cleanupPageImages(ctx context.Context, appID string) error {
	if s.PageImages == nil {
		return nil
	}
	query := s.DB.WithContext(ctx).Where("key LIKE ?", s.PageImages.Prefix+"%")
	if appID != "" {
		query = query.Where("app_id = ?", appID)
	}
	var assets []PageImageObject
	if err := query.Find(&assets).Error; err != nil {
		return err
	}
	var firstError error
	for _, asset := range assets {
		err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if _, err := lockUser(tx, asset.UserID); err != nil {
				return err
			}
			var a Application
			err := tx.First(&a, "id = ?", asset.AppID).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil {
				// Keep pending drafts for at least a day. Published images never
				// expire, and save uses this same user lock and checks ownership.
				if asset.CreatedAt.After(s.Now().Add(-24 * time.Hour)) {
					return nil
				}
				var design AppPageDesign
				if err := tx.First(&design, "app_id = ?", asset.AppID).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				for _, raw := range append([]string{design.Design.Icon}, design.Design.Screenshots...) {
					if raw == asset.URL {
						return nil
					}
				}
			}
			remove, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			if err := s.PageImages.Delete(remove, asset.Key); err != nil {
				return err // Keep the record for a later retry; no soft-deleted app.
			}
			return tx.Delete(&asset).Error
		})
		if err != nil {
			if firstError == nil {
				firstError = err
			}
			slog.Warn("page image cleanup deferred", "app", asset.AppID, "key", asset.Key, "error", err)
		}
		if ctx.Err() != nil {
			break
		}
	}
	return firstError
}
