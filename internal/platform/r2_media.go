package platform

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/gorm"
)

// Packages and PWA media may share the existing public page-image R2 bucket.
// Writes remain signed on the backend; knowing an object's public URL permits
// direct reads outside D1's download controls, as intended for public storage.
type R2Config struct{ Endpoint, AccessKey, SecretKey, Bucket string }

func (c Config) r2Config() R2Config {
	if c.R2 != (R2Config{}) || c.S3Endpoint != "" || c.RemoteStorageURL != "" {
		return c.R2
	}
	return R2Config{Endpoint: c.PageImages.Endpoint, AccessKey: c.PageImages.AccessKey, SecretKey: c.PageImages.SecretKey, Bucket: c.PageImages.Bucket}
}

func (r R2Config) validate(c Config) error {
	if r == (R2Config{}) {
		return nil
	}
	u, err := url.Parse(r.Endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || r.AccessKey == "" || r.SecretKey == "" || r.Bucket == "" || strings.ContainsAny(r.Bucket, "/\\ \t\r\n") {
		return errors.New("complete R2_ENDPOINT (HTTPS origin), R2_ACCESS_KEY, R2_SECRET_KEY and R2_BUCKET are required")
	}
	if c.RemoteStorageURL != "" || c.S3Endpoint != "" {
		return errors.New("R2 storage cannot be combined with S3_ENDPOINT or REMOTE_STORAGE_URL")
	}
	return nil
}

type R2MediaStore struct {
	Client         *minio.Client
	Bucket, Prefix string
}
type R2MediaObject struct {
	Key       string    `gorm:"primaryKey;size:250"`
	CreatedAt time.Time `gorm:"index"`
}

func NewR2MediaStore(c Config) (*R2MediaStore, error) {
	c.R2 = c.r2Config()
	if err := c.R2.validate(c); err != nil {
		return nil, err
	}
	if c.R2 == (R2Config{}) {
		return nil, nil
	}
	u, _ := url.Parse(c.R2.Endpoint)
	client, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(c.R2.AccessKey, c.R2.SecretKey, ""), Secure: true, Region: "auto", BucketLookup: minio.BucketLookupPath})
	if err != nil {
		return nil, err
	}
	scope := hashString(u.Host + "\n" + c.R2.Bucket + "\n" + c.PublicURL)[:16]
	return &R2MediaStore{Client: client, Bucket: c.R2.Bucket, Prefix: "d1apk/files/" + scope + "/"}, nil
}
func (s *R2MediaStore) packages() S3Store {
	return S3Store{Client: s.Client, Bucket: s.Bucket, Prefix: s.Prefix + "apps/"}
}
func (s *R2MediaStore) checkBucket(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ok, err := s.Client.BucketExists(ctx, s.Bucket)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("R2 bucket missing; configure an existing R2 bucket")
	}
	return nil
}
func (s *R2MediaStore) validKey(key string) bool {
	if !strings.HasPrefix(key, s.Prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(key, s.Prefix), "/")
	if len(parts) != 2 {
		return false
	}
	name := parts[1]
	switch parts[0] {
	case "pwa-icons", "push-images":
		name = strings.TrimSuffix(name, ".png")
		if parts[1] == name {
			return false
		}
	case "return-flow":
		return returnFlowFilePattern.MatchString(name)
	default:
		return false
	}
	return hexKey.MatchString(name)
}
func (s *R2MediaStore) put(ctx context.Context, tx *gorm.DB, key string, source io.Reader, size int64, contentType, sha string) error {
	if !s.validKey(key) {
		return errors.New("invalid R2 media key")
	}
	_, err := s.Client.PutObject(ctx, s.Bucket, key, source, size, minio.PutObjectOptions{ContentType: contentType, UserMetadata: map[string]string{"sha256": sha}, PartSize: 16 << 20, NumThreads: 2, DisableMultipart: size <= 5<<20})
	if err != nil {
		slog.Warn("R2 media upload failed", "key", key, "error", err)
		return problem(502, "R2 文件上传失败，请稍后重试")
	}
	return tx.Create(&R2MediaObject{Key: key, CreatedAt: time.Now().UTC()}).Error
}
func (s *R2MediaStore) putPNG(ctx context.Context, tx *gorm.DB, kind string, data []byte) (string, error) {
	key := s.Prefix + kind + "/" + randomHex(16) + ".png"
	err := s.put(ctx, tx, key, bytes.NewReader(data), int64(len(data)), "image/png", hashString(string(data)))
	return key, err
}
func (s *R2MediaStore) open(ctx context.Context, key string) (io.ReadSeekCloser, int64, error) {
	if !s.validKey(key) {
		return nil, 0, errors.New("invalid R2 media key")
	}
	f, err := s.Client.GetObject(ctx, s.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size, nil
}
func (s *R2MediaStore) readPNG(ctx context.Context, key string) ([]byte, error) {
	if s == nil {
		return nil, errors.New("R2 media storage is not configured")
	}
	f, size, err := s.open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if size <= 0 || size > 5<<20 {
		return nil, errors.New("invalid R2 image size")
	}
	data, err := io.ReadAll(io.LimitReader(f, size+1))
	if err == nil && int64(len(data)) != size {
		err = io.ErrUnexpectedEOF
	}
	return data, err
}
func (s *R2MediaStore) remove(ctx context.Context, key string) error {
	if !s.validKey(key) {
		return errors.New("refusing to remove media outside this R2 namespace")
	}
	return s.Client.RemoveObject(ctx, s.Bucket, key, minio.RemoveObjectOptions{})
}

// Called after rollback only. Successful objects remain registered so obsolete
// icons and deleted applications can be cleaned without listing a shared bucket.
func (s *R2MediaStore) discard(key string) {
	if s == nil || key == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.remove(ctx, key); err != nil {
		slog.Error("R2 rollback cleanup failed", "key", key, "error", err)
	}
}
func (s *Server) cleanupR2Media(ctx context.Context) {
	if s.Media == nil || s.Config.Role == "download" {
		return
	}
	var objects []R2MediaObject
	if err := s.DB.WithContext(ctx).Where("created_at < ? AND key LIKE ?", s.Now().Add(-24*time.Hour), s.Media.Prefix+"%").Find(&objects).Error; err != nil {
		return
	}
	for _, object := range objects {
		if ctx.Err() != nil {
			return
		}
		var icons, images, media int64
		if s.DB.Model(&PWAApp{}).Where("icon_object_key = ?", object.Key).Count(&icons).Error != nil || s.DB.Model(&PWAPushAsset{}).Where("object_key = ?", object.Key).Count(&images).Error != nil || s.DB.Model(&ReturnFlowAsset{}).Where("object_key = ?", object.Key).Count(&media).Error != nil {
			continue
		}
		if icons+images+media != 0 {
			continue
		}
		if err := s.Media.remove(ctx, object.Key); err != nil {
			slog.Warn("R2 orphan cleanup failed", "error", err)
			continue
		}
		s.DB.Where("key = ?", object.Key).Delete(&R2MediaObject{})
	}
}
