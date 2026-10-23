package platform

import (
	"bytes"
	"encoding/binary"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"context"
)

func uploadFixture(t *testing.T, s *Server, cookie *http.Cookie, csrf, name string, content []byte, slug ...string) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	form := multipart.NewWriter(&b)
	_ = form.WriteField("name", "上传测试")
	_ = form.WriteField("version", "1.0.0")
	if len(slug) > 0 {
		_ = form.WriteField("slug", slug[0])
	}
	f, e := form.CreateFormFile("file", name)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = f.Write(content)
	_ = form.Close()
	r := httptest.NewRequest("POST", "http://localhost:8088/api/apps", &b)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestUploadContentQuotaAndSizeLimits(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	requireCode(t, uploadFixture(t, s, cookie, csrf, "fake.exe", []byte("not-an-exe")), 400)
	requireCode(t, uploadFixture(t, s, cookie, csrf, "page.html", []byte("<script>alert(1)</script>")), 400)
	s.DB.Model(&Settings{}).Where("id = ?", 1).Update("max_upload_bytes", 8)
	requireCode(t, uploadFixture(t, s, cookie, csrf, "too-big.exe", []byte("MZ123456789012345")), 413)
	s.DB.Model(&Settings{}).Where("id = ?", 1).Update("max_upload_bytes", 1024)
	s.DB.Model(&Plan{}).Where("id = ?", "free").Update("app_limit", 1)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- uploadFixture(t, s, cookie, csrf, "test.exe", []byte("MZ1234567890")).Code
		}()
	}
	wg.Wait()
	close(codes)
	accepted := 0
	denied := 0
	for code := range codes {
		if code == 201 {
			accepted++
		} else if code == 403 {
			denied++
		} else {
			t.Fatalf("unexpected upload status %d", code)
		}
	}
	if accepted != 1 || denied != 1 || countRows(t, s, &Application{}) != 1 {
		t.Fatal("concurrent upload exceeded app quota")
	}
	files, e := os.ReadDir(filepath.Join(s.Config.DataDir, "files"))
	if e != nil {
		t.Fatal(e)
	}
	if len(files) != 1 {
		t.Fatalf("rejected upload leaked files: %d", len(files))
	}
}
func TestClamAVProtocolFailClosed(t *testing.T) {
	for _, reply := range []string{"stream: OK\x00", "stream: Eicar-Test-Signature FOUND\x00", "INSTREAM size limit exceeded. ERROR\x00"} {
		t.Run(reply, func(t *testing.T) {
			s := testServer(t)
			listener, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer listener.Close()
			s.Config.ClamAVAddr = listener.Addr().String()
			got := make(chan []byte, 1)
			go func() {
				conn, e := listener.Accept()
				if e != nil {
					got <- nil
					return
				}
				defer conn.Close()
				command := make([]byte, 10)
				_, _ = io.ReadFull(conn, command)
				var received []byte
				for {
					var length uint32
					if e := binary.Read(conn, binary.BigEndian, &length); e != nil {
						break
					}
					if length == 0 {
						break
					}
					chunk := make([]byte, length)
					_, _ = io.ReadFull(conn, chunk)
					received = append(received, chunk...)
				}
				_, _ = io.WriteString(conn, reply)
				got <- received
			}()
			f, e := os.CreateTemp(s.Config.DataDir, "scan-")
			if e != nil {
				t.Fatal(e)
			}
			defer f.Close()
			_, _ = f.WriteString("MZ-clean-fixture")
			e = s.scan(context.Background(), f)
			if (e == nil) != (reply == "stream: OK\x00") {
				t.Fatalf("wrong scan result %v", e)
			}
			if string(<-got) != "MZ-clean-fixture" {
				t.Fatal("ClamAV did not receive original file")
			}
		})
	}
}
