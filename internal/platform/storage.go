package platform

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type BlobStore interface {
	Put(context.Context, string, string, int64, string) error
	Open(context.Context, string) (io.ReadSeekCloser, int64, error)
	Delete(context.Context, string) error
}
type LocalStore struct{ Root string }

func (l LocalStore) Put(_ context.Context, key, path string, size int64, sha string) error {
	if e := os.MkdirAll(l.Root, 0700); e != nil {
		return e
	}
	return os.Rename(path, filepath.Join(l.Root, key))
}
func (l LocalStore) Open(_ context.Context, key string) (io.ReadSeekCloser, int64, error) {
	if !hexKey.MatchString(key) {
		return nil, 0, errors.New("invalid blob key")
	}
	f, e := os.Open(filepath.Join(l.Root, key))
	if e != nil {
		return nil, 0, e
	}
	st, e := f.Stat()
	if e != nil {
		_ = f.Close()
		return nil, 0, e
	}
	return f, st.Size(), nil
}
func (l LocalStore) Delete(_ context.Context, key string) error {
	if !hexKey.MatchString(key) {
		return errors.New("invalid blob key")
	}
	e := os.Remove(filepath.Join(l.Root, key))
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	return e
}

type S3Store struct {
	Client *minio.Client
	Bucket string
	Prefix string
}

func (b S3Store) prefix() string {
	if b.Prefix != "" {
		return b.Prefix
	}
	return "apps/"
}

func (b S3Store) Put(ctx context.Context, key, path string, size int64, sha string) error {
	if !hexKey.MatchString(key) {
		return errors.New("invalid blob key")
	}
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	_, e = b.Client.PutObject(ctx, b.Bucket, b.prefix()+key, f, size, minio.PutObjectOptions{ContentType: "application/octet-stream", UserMetadata: map[string]string{"sha256": sha}, PartSize: 16 << 20, NumThreads: 2})
	return e
}
func (b S3Store) Open(ctx context.Context, key string) (io.ReadSeekCloser, int64, error) {
	if !hexKey.MatchString(key) {
		return nil, 0, errors.New("invalid blob key")
	}
	f, e := b.Client.GetObject(ctx, b.Bucket, b.prefix()+key, minio.GetObjectOptions{})
	if e != nil {
		return nil, 0, e
	}
	st, e := f.Stat()
	if e != nil {
		_ = f.Close()
		return nil, 0, e
	}
	return f, st.Size, nil
}
func (b S3Store) Delete(ctx context.Context, key string) error {
	if !hexKey.MatchString(key) {
		return errors.New("invalid blob key")
	}
	return b.Client.RemoveObject(ctx, b.Bucket, b.prefix()+key, minio.RemoveObjectOptions{})
}
func NewBlobStore(c Config) (BlobStore, error) {
	if c.r2Config() != (R2Config{}) {
		store, err := NewR2MediaStore(c)
		if err != nil {
			return nil, err
		}
		if err = store.checkBucket(context.Background()); err != nil {
			return nil, err
		}
		return store.packages(), nil
	}
	if c.RemoteStorageURL != "" {
		u, e := url.Parse(c.RemoteStorageURL)
		if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.RawQuery != "" {
			return nil, errors.New("REMOTE_STORAGE_URL must be an origin on your private network")
		}
		return RemoteStore{Base: strings.TrimRight(c.RemoteStorageURL, "/"), Secret: c.Secret, HTTP: &http.Client{Timeout: 20 * time.Minute, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
	}
	if c.S3Endpoint != "" {
		client, e := minio.New(c.S3Endpoint, &minio.Options{Creds: credentials.NewStaticV4(c.S3AccessKey, c.S3SecretKey, ""), Secure: c.S3Secure, Region: c.S3Region})
		if e != nil {
			return nil, e
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		ok, e := client.BucketExists(ctx, c.S3Bucket)
		if e != nil {
			return nil, e
		}
		if !ok {
			return nil, errors.New("S3 bucket must be created privately before startup")
		}
		return S3Store{Client: client, Bucket: c.S3Bucket}, nil
	}
	root := filepath.Join(c.DataDir, "files")
	if e := os.MkdirAll(root, 0700); e != nil {
		return nil, e
	}
	return LocalStore{Root: root}, nil
}

// Control-server uploads to a dedicated download server use an authenticated
// private endpoint. The body hash is checked before atomic file publication.
type RemoteStore struct {
	Base, Secret string
	HTTP         *http.Client
}

func storageSign(secret, method, path, timestamp, sha string) string {
	h := hmac.New(sha256.New, []byte(secret))
	_, _ = io.WriteString(h, "storage-v1\n"+method+"\n"+path+"\n"+timestamp+"\n"+sha)
	return hex.EncodeToString(h.Sum(nil))
}
func (b RemoteStore) request(ctx context.Context, method, key string, body io.Reader, size int64, sha string) (*http.Response, error) {
	path := "/internal/storage/" + key
	req, e := http.NewRequestWithContext(ctx, method, b.Base+path, body)
	if e != nil {
		return nil, e
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("X-D1-Time", ts)
	req.Header.Set("X-D1-Content-SHA256", sha)
	req.Header.Set("X-D1-Signature", storageSign(b.Secret, method, path, ts, sha))
	if body != nil {
		req.ContentLength = size
	}
	return b.HTTP.Do(req)
}
func (b RemoteStore) Put(ctx context.Context, key, path string, size int64, sha string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	resp, e := b.request(ctx, "PUT", key, f, size, sha)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != 204 {
		return fmt.Errorf("download server upload rejected: HTTP %d", resp.StatusCode)
	}
	return nil
}
func (b RemoteStore) Delete(ctx context.Context, key string) error {
	resp, e := b.request(ctx, "DELETE", key, nil, 0, "")
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 {
		return errors.New("download server deletion failed")
	}
	return nil
}
func (b RemoteStore) Open(context.Context, string) (io.ReadSeekCloser, int64, error) {
	return nil, 0, errors.New("control server does not serve download files; route /a and /d to the download server")
}
func (s *Server) internalStorage(w http.ResponseWriter, r *http.Request) {
	if s.Config.Role != "download" || s.Config.RemoteStorageURL != "" {
		http.NotFound(w, r)
		return
	}
	key := r.PathValue("key")
	ts := r.Header.Get("X-D1-Time")
	n, e := strconv.ParseInt(ts, 10, 64)
	sha := r.Header.Get("X-D1-Content-SHA256")
	expected := storageSign(s.Config.Secret, r.Method, r.URL.Path, ts, sha)
	if e != nil || n < s.Now().Add(-5*time.Minute).Unix() || n > s.Now().Add(time.Minute).Unix() || !hexKey.MatchString(key) || subtle.ConstantTimeCompare([]byte(expected), []byte(r.Header.Get("X-D1-Signature"))) != 1 {
		http.Error(w, "forbidden", 403)
		return
	}
	if r.Method == "DELETE" {
		if e = s.Blobs.Delete(r.Context(), key); e != nil {
			http.Error(w, "storage unavailable", 503)
			return
		}
		w.WriteHeader(204)
		return
	}
	if r.Method != "PUT" || len(sha) != 64 || !hexKey.MatchString(sha) || r.ContentLength <= 0 || r.ContentLength > 2<<30 {
		http.Error(w, "invalid request", 400)
		return
	}
	if e = os.MkdirAll(filepath.Join(s.Config.DataDir, "tmp"), 0700); e != nil {
		fail(w, e)
		return
	}
	f, e := os.CreateTemp(filepath.Join(s.Config.DataDir, "tmp"), "transfer-")
	if e != nil {
		fail(w, e)
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()
	h := sha256.New()
	size, e := io.Copy(io.MultiWriter(f, h), http.MaxBytesReader(w, r.Body, r.ContentLength))
	if e != nil || size != r.ContentLength || hex.EncodeToString(h.Sum(nil)) != sha {
		http.Error(w, "checksum mismatch", 400)
		return
	}
	if e = f.Sync(); e != nil {
		fail(w, e)
		return
	}
	if e = f.Close(); e != nil {
		fail(w, e)
		return
	}
	if e = s.Blobs.Put(r.Context(), key, f.Name(), size, sha); e != nil {
		http.Error(w, "storage unavailable", 503)
		return
	}
	w.WriteHeader(204)
}
