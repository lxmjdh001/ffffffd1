package platform

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
)

func (s *Server) listApps(w http.ResponseWriter, r *http.Request) {
	listOut[Application](w, r, addQuery(s.DB.Model(&Application{}).Where("user_id = ?", userOf(r).ID), r, "name", "file_name"))
}
func (s *Server) ownedApp(r *http.Request) (Application, error) {
	var a Application
	e := s.DB.First(&a, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Error
	return a, e
}
func (s *Server) getApp(w http.ResponseWriter, r *http.Request) {
	a, e := s.ownedApp(r)
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, a)
}
func validAppFields(name, description, version string) error {
	if strings.TrimSpace(name) == "" || len([]rune(name)) > 100 || len([]rune(description)) > 4000 || len([]rune(version)) > 40 {
		return problem(400, "应用名称须为 1–100 字，版本最多 40 字，说明最多 4000 字")
	}
	return nil
}

var appSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

func normalizeAppSlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if !appSlugPattern.MatchString(slug) {
		return "", problem(400, "分享标识须为 1–32 位字母、数字、短横线或下划线，且以字母或数字开头")
	}
	return slug, nil
}

func appSaveError(err error) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return problem(409, "分享标识已被使用，请换一个")
	}
	return err
}
func (s *Server) uploadApp(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	if e := s.rate("upload:"+u.ID, 60, time.Hour); e != nil {
		fail(w, e)
		return
	}
	select {
	case s.uploadSlots <- struct{}{}:
		defer func() { <-s.uploadSlots }()
	default:
		fail(w, problem(429, "上传通道繁忙，请稍后再试"))
		return
	}
	settings, e := getSettings(s.DB)
	if e != nil {
		fail(w, e)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, settings.MaxUploadBytes+(1<<20))
	mr, e := r.MultipartReader()
	if e != nil {
		fail(w, problem(400, "请使用 multipart 上传文件"))
		return
	}
	if e = os.MkdirAll(filepath.Join(s.Config.DataDir, "files"), 0700); e != nil {
		fail(w, e)
		return
	}
	f, e := os.CreateTemp(filepath.Join(s.Config.DataDir, "files"), "upload-")
	if e != nil {
		fail(w, e)
		return
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	fields := map[string]string{}
	filename := ""
	h := sha256.New()
	var size int64
	for count := 0; ; count++ {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil || count > 10 {
			fail(w, problem(400, "上传表单无效或超出限制"))
			return
		}
		if part.FileName() != "" {
			if filename != "" || part.FormName() != "file" {
				fail(w, problem(400, "每次只能上传一个应用文件"))
				return
			}
			filename = filepath.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
			if len(filename) > 200 || strings.ContainsAny(filename, "\r\n\x00") {
				fail(w, problem(400, "文件名无效"))
				return
			}
			size, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(part, settings.MaxUploadBytes+1))
			if err != nil || size > settings.MaxUploadBytes {
				fail(w, problem(413, "文件超过上传大小限制"))
				return
			}
		} else {
			b, err := io.ReadAll(io.LimitReader(part, 16385))
			if err != nil || len(b) > 16384 {
				fail(w, problem(400, "表单字段过长"))
				return
			}
			fields[part.FormName()] = string(b)
		}
		_ = part.Close()
	}
	if filename == "" || size == 0 {
		fail(w, problem(400, "请选择非空的应用文件"))
		return
	}
	name := strings.TrimSpace(fields["name"])
	description := strings.TrimSpace(fields["description"])
	version := strings.TrimSpace(fields["version"])
	slug := ""
	if r.PathValue("id") == "" && strings.TrimSpace(fields["slug"]) != "" {
		var err error
		slug, err = normalizeAppSlug(fields["slug"])
		if err != nil {
			fail(w, err)
			return
		}
	}
	if e = validAppFields(name, description, version); e != nil {
		fail(w, e)
		return
	}
	if e = f.Sync(); e != nil {
		fail(w, e)
		return
	}
	// Let the bounded scanner reject archive bombs before Go enumerates ZIP
	// directories. The default development listener without a scanner is loopback-only.
	if e = s.scan(r.Context(), f); e != nil {
		fail(w, e)
		return
	}
	platform, e := inspectPackage(f, filename, size)
	if e != nil {
		fail(w, e)
		return
	}
	if e = f.Close(); e != nil {
		fail(w, e)
		return
	}
	key := randomHex(16)
	sha := hex.EncodeToString(h.Sum(nil))
	if e = s.Blobs.Put(r.Context(), key, temp, size, sha); e != nil {
		fail(w, e)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.Blobs.Delete(context.Background(), key)
		}
	}()
	var app Application
	oldKey := ""
	e = s.DB.Transaction(func(tx *gorm.DB) error {
		owner, err := lockUser(tx, u.ID)
		if err != nil {
			return err
		}
		if owner.Disabled {
			return problem(403, "账户已停用")
		}
		plan, err := getPlan(tx, owner, s.Now())
		if err != nil {
			return err
		}
		var count int64
		if err = tx.Model(&Application{}).Where("user_id = ?", u.ID).Count(&count).Error; err != nil {
			return err
		}
		var used int64
		if err = tx.Model(&Application{}).Where("user_id = ?", u.ID).Select("COALESCE(SUM(size),0)").Scan(&used).Error; err != nil {
			return err
		}
		if r.PathValue("id") == "" {
			if count >= plan.AppLimit {
				return problem(403, "应用数量已达套餐上限，请升级会员或删除应用")
			}
			app = Application{ID: randomHex(16), UserID: u.ID, Slug: randomHex(6), Enabled: true}
			if slug != "" {
				app.Slug = slug
			}
		} else {
			if err = tx.First(&app, "id = ? AND user_id = ?", r.PathValue("id"), u.ID).Error; err != nil {
				return err
			}
			if app.Blocked {
				return problem(403, "该应用已被平台停用")
			}
			oldKey = app.FileKey
			used -= app.Size
		}
		if used+size > plan.StorageBytes {
			return problem(403, "存储空间不足，请删除旧文件或升级套餐")
		}
		app.Name = name
		app.Description = description
		app.Version = version
		app.Platform = platform
		app.FileKey = key
		app.FileName = filename
		app.Size = size
		app.SHA256 = hex.EncodeToString(h.Sum(nil))
		return appSaveError(tx.Save(&app).Error)
	})
	if e != nil {
		fail(w, e)
		return
	}
	committed = true
	if oldKey != "" {
		_ = s.Blobs.Delete(context.Background(), oldKey)
	}
	jsonOut(w, 201, app)
}
func inspectPackage(f *os.File, name string, size int64) (string, error) {
	header := make([]byte, 8)
	_, _ = f.ReadAt(header, 0)
	ext := strings.ToLower(filepath.Ext(name))
	bad := problem(400, "文件内容与扩展名不匹配，支持 APK、IPA、EXE、MSI、DMG、ZIP、AAB")
	switch ext {
	case ".exe":
		if !bytes.HasPrefix(header, []byte("MZ")) {
			return "", bad
		}
		return "Windows", nil
	case ".msi":
		if !bytes.Equal(header, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
			return "", bad
		}
		return "Windows", nil
	case ".dmg":
		trailer := make([]byte, 4)
		if size < 512 {
			return "", bad
		}
		_, _ = f.ReadAt(trailer, size-512)
		if string(trailer) != "koly" {
			return "", bad
		}
		return "macOS", nil
	case ".apk", ".ipa", ".zip", ".aab":
		z, e := zip.NewReader(f, size)
		if e != nil || len(z.File) > 100000 {
			return "", bad
		}
		if ext == ".zip" {
			return "通用", nil
		}
		for _, entry := range z.File {
			if ext == ".apk" && entry.Name == "AndroidManifest.xml" {
				return "Android", nil
			}
			if ext == ".aab" && entry.Name == "base/manifest/AndroidManifest.xml" {
				return "Android", nil
			}
			if ext == ".ipa" && strings.HasPrefix(entry.Name, "Payload/") && strings.HasSuffix(entry.Name, ".app/Info.plist") {
				return "iOS", nil
			}
		}
		return "", bad
	default:
		return "", bad
	}
}
func (s *Server) scan(ctx context.Context, f *os.File) error {
	if s.Config.ClamAVAddr == "" {
		if s.Config.Production() {
			return problem(503, "文件扫描服务未配置")
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	conn, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", s.Config.ClamAVAddr)
	if e != nil {
		return problem(503, "文件安全扫描服务不可用，请稍后再试")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Minute))
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if _, e = io.WriteString(conn, "zINSTREAM\x00"); e != nil {
		return e
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return e
	}
	buf := make([]byte, 32768)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if e = binary.Write(conn, binary.BigEndian, uint32(n)); e != nil {
				return e
			}
			if _, e = io.Copy(conn, bytes.NewReader(buf[:n])); e != nil {
				return e
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if e = binary.Write(conn, binary.BigEndian, uint32(0)); e != nil {
		return e
	}
	reply, e := io.ReadAll(io.LimitReader(conn, 4096))
	if e != nil {
		return problem(503, "文件扫描未完成，请重试")
	}
	result := strings.TrimRight(string(reply), "\x00\r\n")
	if result != "stream: OK" {
		return problem(400, "文件未通过安全扫描，或文件超出扫描限制")
	}
	return nil
}
func (s *Server) updateApp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Slug        *string `json:"slug"`
		Name        string  `json:"name"`
		Description string  `json:"description"`
		Version     string  `json:"version"`
		Enabled     bool    `json:"enabled"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if e := validAppFields(in.Name, in.Description, in.Version); e != nil {
		fail(w, e)
		return
	}
	if in.Slug != nil {
		slug, err := normalizeAppSlug(*in.Slug)
		if err != nil {
			fail(w, err)
			return
		}
		in.Slug = &slug
	}
	var app Application
	e := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, e := lockUser(tx, userOf(r).ID); e != nil {
			return e
		}
		if e := tx.First(&app, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Error; e != nil {
			return e
		}
		app.Name = in.Name
		app.Description = in.Description
		app.Version = in.Version
		app.Enabled = in.Enabled
		if in.Slug != nil {
			app.Slug = *in.Slug
		}
		return appSaveError(tx.Save(&app).Error)
	})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, app)
}
func (s *Server) deleteApp(w http.ResponseWriter, r *http.Request) {
	var app Application
	e := s.DB.Transaction(func(tx *gorm.DB) error {
		if _, e := lockUser(tx, userOf(r).ID); e != nil {
			return e
		}
		if e := tx.First(&app, "id = ? AND user_id = ?", r.PathValue("id"), userOf(r).ID).Error; e != nil {
			return e
		}
		return tx.Delete(&app).Error
	})
	if e != nil {
		fail(w, e)
		return
	}
	if e = s.Blobs.Delete(context.Background(), app.FileKey); e != nil && !errors.Is(e, os.ErrNotExist) {
		fmt.Println("file cleanup deferred")
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
