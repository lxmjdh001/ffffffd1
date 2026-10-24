package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
)

type ReturnFlowAsset struct {
	ObjectKey   string `json:"-" gorm:"size:250;not null;default:''"`
	ID          string `gorm:"primaryKey;size:32"`
	UserID      string `gorm:"size:32;not null;uniqueIndex:idx_return_flow_asset_owner_hash,priority:1;index"`
	SHA256      string `gorm:"size:64;not null;uniqueIndex:idx_return_flow_asset_owner_hash,priority:2"`
	PublicPath  string `gorm:"size:100;not null;uniqueIndex"`
	ContentType string `gorm:"size:40;not null"`
	Size        int64  `gorm:"not null"`
	CreatedAt   time.Time
}

const maxReturnFlowAssetBytes int64 = 50 << 20

var returnFlowFilePattern = regexp.MustCompile(`^[a-f0-9]{32}\.(png|jpg|jpeg|webp|gif|mp4|webm|mov|m4v)$`)

func (s *Server) returnFlowAssetDirectory() string {
	return filepath.Join(s.Config.DataDir, "return-flow-assets")
}
func returnFlowVideoType(header []byte) (string, string) {
	if len(header) >= 12 && string(header[4:8]) == "ftyp" {
		brand := string(header[8:12])
		switch brand {
		case "qt  ":
			return "video/quicktime", "mov"
		case "M4V ", "M4VH", "M4VP":
			return "video/mp4", "m4v"
		case "isom", "iso2", "iso3", "iso4", "iso5", "iso6", "mp41", "mp42", "avc1", "dash", "MSNV":
			return "video/mp4", "mp4"
		}
	}
	// WebM files declare the container document type in their EBML header.
	if bytes.HasPrefix(header, []byte{0x1a, 0x45, 0xdf, 0xa3}) && bytes.Contains(header, []byte{0x42, 0x82, 0x84, 'w', 'e', 'b', 'm'}) {
		return "video/webm", "webm"
	}
	return "", ""
}
func (s *Server) uploadReturnFlowAsset(w http.ResponseWriter, r *http.Request) {
	if err := s.rate("return-flow-upload:"+userOf(r).ID, 30, 10*time.Minute); err != nil {
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
	r.Body = http.MaxBytesReader(w, r.Body, maxReturnFlowAssetBytes+(64<<10))
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		fail(w, problem(400, "请选择不超过 50 MB 的图片或视频"))
		return
	}
	defer r.MultipartForm.RemoveAll()
	files := r.MultipartForm.File["file"]
	if len(files) != 1 || len(r.MultipartForm.File) != 1 || files[0].Size <= 0 || files[0].Size > maxReturnFlowAssetBytes {
		fail(w, problem(400, "每次只能上传一个不超过 50 MB 的素材"))
		return
	}
	source, err := files[0].Open()
	if err != nil {
		fail(w, err)
		return
	}
	defer source.Close()
	directory := s.returnFlowAssetDirectory()
	if err = os.MkdirAll(directory, 0700); err != nil {
		fail(w, err)
		return
	}
	temporary, err := os.CreateTemp(directory, ".upload-")
	if err != nil {
		fail(w, err)
		return
	}
	defer func() { temporary.Close(); os.Remove(temporary.Name()) }()
	size, err := io.Copy(temporary, io.LimitReader(source, maxReturnFlowAssetBytes+1))
	if err != nil || size <= 0 || size > maxReturnFlowAssetBytes {
		fail(w, problem(400, "素材读取失败或文件过大"))
		return
	}
	_, _ = temporary.Seek(0, io.SeekStart)
	header := make([]byte, min(size, 512))
	_, _ = io.ReadFull(temporary, header)
	_, _ = temporary.Seek(0, io.SeekStart)
	contentType, ext := returnFlowVideoType(header)

	if contentType == "" {
		cfg, format, e := image.DecodeConfig(temporary)
		formats := map[string][2]string{"png": {"image/png", "png"}, "jpeg": {"image/jpeg", "jpg"}, "webp": {"image/webp", "webp"}, "gif": {"image/gif", "gif"}}
		selected, ok := formats[format]
		if e != nil || !ok || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 16_000_000 {
			fail(w, problem(400, "素材须为 PNG、JPG、WebP、GIF 图片或 MP4、WebM、MOV、M4V 视频"))
			return
		}
		// Preserve the source bytes, as PWA does: re-encoding would flatten animated
		// GIF/WebP/APNG files and change the return-page effect.
		contentType, ext = selected[0], selected[1]
	}

	_, _ = temporary.Seek(0, io.SeekStart)
	digest := sha256.New()
	if _, err = io.Copy(digest, temporary); err != nil {
		fail(w, err)
		return
	}
	asset := ReturnFlowAsset{ID: randomHex(16), UserID: userOf(r).ID, SHA256: hex.EncodeToString(digest.Sum(nil)), ContentType: contentType, Size: size, CreatedAt: s.Now()}
	asset.PublicPath = "/return-flow-assets/" + asset.ID + "." + ext
	final := filepath.Join(directory, filepath.Base(asset.PublicPath))
	stored := false
	objectKey := ""
	err = s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		owner, e := lockUser(tx, asset.UserID)
		if e != nil {
			return e
		}
		if owner.Disabled {
			return problem(403, "账户已停用")
		}
		var existing ReturnFlowAsset
		e = tx.First(&existing, "user_id = ? AND sha256 = ?", owner.ID, asset.SHA256).Error
		if e == nil {
			asset = existing
			return nil
		}
		if e != gorm.ErrRecordNotFound {
			return e
		}
		var quota struct {
			Count int64
			Bytes int64
		}
		if e = tx.Model(&ReturnFlowAsset{}).Where("user_id = ?", owner.ID).Select("count(*) AS count, coalesce(sum(size),0) AS bytes").Scan(&quota).Error; e != nil {
			return e
		}
		if quota.Count >= 100 || quota.Bytes+size > 500<<20 {
			return problem(400, "回流素材已达 100 个或 500 MB 的账户上限")
		}
		if e = temporary.Sync(); e != nil {
			return e
		}
		if e = temporary.Close(); e != nil {
			return e
		}
		if s.Media != nil {
			objectKey = s.Media.Prefix + "return-flow/" + filepath.Base(asset.PublicPath)
			file, e := os.Open(temporary.Name())
			if e != nil {
				return e
			}
			defer file.Close()
			if e = s.Media.put(r.Context(), tx, objectKey, file, size, contentType, asset.SHA256); e != nil {
				return e
			}
			asset.ObjectKey = objectKey
		} else {
			if e = os.Rename(temporary.Name(), final); e != nil {
				return e
			}
			stored = true
		}
		if e = tx.Create(&asset).Error; e != nil {
			return e
		}
		return recordAudit(tx, owner.ID, "return_flow.asset", asset.ID, "")
	})
	if err != nil {
		s.Media.discard(objectKey)
		if stored {
			_ = os.Remove(final)
		}
		fail(w, err)
		return
	}
	jsonOut(w, 201, map[string]any{"id": asset.ID, "url": strings.TrimRight(s.Config.PublicURL, "/") + asset.PublicPath, "content_type": asset.ContentType, "size": asset.Size})
}
func (s *Server) publicReturnFlowAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !returnFlowFilePattern.MatchString(name) || requestHost(r) != s.Config.Host() {
		http.NotFound(w, r)
		return
	}
	var asset ReturnFlowAsset
	if err := s.DB.Table("return_flow_assets AS a").Select("a.*").Joins("JOIN users u ON u.id = a.user_id AND u.disabled = ?", false).Where("a.public_path = ?", "/return-flow-assets/"+name).Take(&asset).Error; err != nil {
		http.NotFound(w, r)
		return
	}
	var file io.ReadSeekCloser
	var err error
	if asset.ObjectKey != "" {
		if s.Media == nil {
			http.Error(w, "Media unavailable", 503)
			return
		}
		var size int64
		file, size, err = s.Media.open(r.Context(), asset.ObjectKey)
		if err == nil && size != asset.Size {
			file.Close()
			err = io.ErrUnexpectedEOF
		}
		if err != nil {
			http.Error(w, "Media unavailable", 503)
			return
		}
	} else {
		local, e := os.Open(filepath.Join(s.returnFlowAssetDirectory(), name))
		if e != nil {
			http.NotFound(w, r)
			return
		}
		stat, e := local.Stat()
		if e != nil || !stat.Mode().IsRegular() || stat.Size() != asset.Size {
			local.Close()
			http.NotFound(w, r)
			return
		}
		file = local
	}
	defer file.Close()
	w.Header().Set("Content-Type", asset.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("ETag", `"`+asset.SHA256+`"`)
	http.ServeContent(w, r, name, asset.CreatedAt, file)
}
