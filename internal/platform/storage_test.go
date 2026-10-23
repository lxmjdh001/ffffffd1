package platform

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the real S3 SDK's lazy HEAD/GET and seek behavior against an HTTP
// contract fixture, not a fake BlobStore. Live vendor credentials are separate.
func TestS3PrivateRangeRead(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789"), 100)
	var reads atomic.Int32
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256") {
			t.Error("object access was not signed")
		}
		if r.URL.Path == "/d1apk/" || r.URL.Path == "/d1apk" {
			w.Header().Set("x-amz-bucket-region", "us-east-1")
			w.WriteHeader(200)
			return
		}
		if r.URL.Path != "/d1apk/apps/"+strings.Repeat("a", 32) {
			t.Errorf("unexpected S3 key: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", `"0123456789abcdef0123456789abcdef"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if r.Method == "GET" {
			reads.Add(1)
		}
		http.ServeContent(w, r, "object", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), bytes.NewReader(data))
	}))
	defer service.Close()
	store, e := NewBlobStore(Config{S3Endpoint: strings.TrimPrefix(service.URL, "http://"), S3AccessKey: "test-access", S3SecretKey: "test-secret", S3Bucket: "d1apk", S3Region: "us-east-1", S3Secure: false})
	if e != nil {
		t.Fatal(e)
	}
	f, size, e := store.Open(context.Background(), strings.Repeat("a", 32))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if size != int64(len(data)) {
		t.Fatal("incorrect object size")
	}
	if _, e = f.Seek(950, io.SeekStart); e != nil {
		t.Fatal(e)
	}
	tail, e := io.ReadAll(f)
	if e != nil || !bytes.Equal(tail, data[950:]) {
		t.Fatalf("range read failed len=%d err=%v", len(tail), e)
	}
	if _, e = f.Seek(20, io.SeekStart); e != nil {
		t.Fatal(e)
	}
	prefix := make([]byte, 30)
	if _, e = io.ReadFull(f, prefix); e != nil || !bytes.Equal(prefix, data[20:50]) {
		t.Fatal("resume seek failed", e)
	}
	if reads.Load() < 2 {
		t.Fatal("expected separate S3 range GET requests")
	}
}

func TestOrphanCleanupPreservesLiveAndRecentFiles(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	app := seedApp(t, s, u, 100)
	root := filepath.Join(s.Config.DataDir, "files")
	old := s.Now().Add(-48 * time.Hour)
	if e := os.Chtimes(filepath.Join(root, app.FileKey), old, old); e != nil {
		t.Fatal(e)
	}
	orphan := randomHex(16)
	recent := randomHex(16)
	for _, key := range []string{orphan, recent} {
		if e := os.WriteFile(filepath.Join(root, key), []byte("fixture"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	_ = os.Chtimes(filepath.Join(root, orphan), old, old)
	s.cleanupOrphans(context.Background())
	for _, key := range []string{app.FileKey, recent} {
		if _, e := os.Stat(filepath.Join(root, key)); e != nil {
			t.Fatal("cleanup deleted protected file", key, e)
		}
	}
	if _, e := os.Stat(filepath.Join(root, orphan)); !os.IsNotExist(e) {
		t.Fatal("old orphan was not removed")
	}
}
