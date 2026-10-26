package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image"
	"image/gif"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
)

var diyManagedImagePattern = regexp.MustCompile(`^/return-flow-assets/[a-f0-9]{32}\.(png|jpg|jpeg|gif|webp)$`)

func diyManagedImagePath(raw string) string {
	if !diyManagedImagePattern.MatchString(raw) {
		return ""
	}
	return raw
}

func diyLandingImageType(contentType string) bool {
	switch contentType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

// Page-image R2 is independent of APK storage. The adapter intentionally owns
// only a new media subnamespace; regular page-image objects cannot be read here.
func (s *Server) diyLandingPageImageStore() *R2MediaStore {
	if s.PageImages == nil || s.PageImages.Client == nil {
		return nil
	}
	return &R2MediaStore{Client: s.PageImages.Client, Bucket: s.PageImages.Bucket, Prefix: s.PageImages.Prefix + "diy/"}
}

func (s *Server) diyLandingImageUploadStore() *R2MediaStore {
	if store := s.diyLandingPageImageStore(); store != nil {
		return store
	}
	if s.Media != nil && s.Media.Client != nil {
		return s.Media
	}
	return nil
}

func (s *Server) diyLandingImageReadStore(key string) *R2MediaStore {
	for _, store := range []*R2MediaStore{s.diyLandingPageImageStore(), s.Media} {
		if store != nil && store.Client != nil && strings.HasPrefix(key, store.Prefix+"return-flow/") && store.validKey(key) {
			return store
		}
	}
	return nil
}

func (s *Server) uploadDIYLandingImage(w http.ResponseWriter, r *http.Request) {
	store := s.diyLandingImageUploadStore()
	if store == nil {
		fail(w, problem(503, "图片存储尚未配置，请联系管理员配置 R2"))
		return
	}
	if err := s.rate("diy-image-upload:"+userOf(r).ID, 30, 10*time.Minute); err != nil {
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
	r.Body = http.MaxBytesReader(w, r.Body, maxDIYContentImageBytes+(64<<10))
	if err := r.ParseMultipartForm(maxDIYContentImageBytes); err != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		fail(w, problem(400, "请选择不超过 2 MiB 的 PNG、JPG、GIF、WebP 图片"))
		return
	}
	defer r.MultipartForm.RemoveAll()
	files := r.MultipartForm.File["file"]
	if len(files) != 1 || len(r.MultipartForm.File) != 1 || files[0].Size <= 0 || files[0].Size > maxDIYContentImageBytes {
		fail(w, problem(400, "每次只能上传一张不超过 2 MiB 的图片"))
		return
	}
	source, err := files[0].Open()
	if err != nil {
		fail(w, err)
		return
	}
	defer source.Close()
	data, err := io.ReadAll(io.LimitReader(source, maxDIYContentImageBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxDIYContentImageBytes || int64(len(data)) != files[0].Size {
		fail(w, problem(400, "图片读取失败或文件过大"))
		return
	}
	contentType, ext, valid := diyLandingUploadedImage(data)
	if !valid {
		fail(w, problem(400, "图片须为完整的 PNG、JPG、GIF、WebP 图片，尺寸最多 4096 像素且不超过 1600 万像素"))
		return
	}
	digest := sha256.Sum256(data)
	asset := ReturnFlowAsset{ID: randomHex(16), UserID: userOf(r).ID, SHA256: hex.EncodeToString(digest[:]), ContentType: contentType, Size: int64(len(data)), CreatedAt: s.Now()}
	asset.PublicPath = "/return-flow-assets/" + asset.ID + "." + ext
	objectKey := ""
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		owner, err := lockUser(tx, asset.UserID)
		if err != nil {
			return err
		}
		if owner.Disabled {
			return problem(403, "账户已停用")
		}
		var existing ReturnFlowAsset
		err = tx.First(&existing, "user_id = ? AND sha256 = ?", owner.ID, asset.SHA256).Error
		upgrade := err == nil
		if upgrade {
			if diyManagedImagePath(existing.PublicPath) == "" || existing.Size != asset.Size || existing.ContentType != asset.ContentType {
				return problem(400, "已有图片素材信息无效，请联系管理员")
			}
			asset = existing
			if existing.ObjectKey != "" {
				if s.diyLandingImageReadStore(existing.ObjectKey) == nil {
					return problem(503, "已有图片的 R2 配置不可用，请联系管理员")
				}
				return nil
			}
		} else if err != gorm.ErrRecordNotFound {
			return err
		} else {
			var quota struct{ Count, Bytes int64 }
			if err = tx.Model(&ReturnFlowAsset{}).Where("user_id = ?", owner.ID).Select("count(*) AS count, coalesce(sum(size),0) AS bytes").Scan(&quota).Error; err != nil {
				return err
			}
			if quota.Count >= 100 || quota.Bytes+asset.Size > 500<<20 {
				return problem(400, "图片素材已达 100 个或 500 MB 的账户上限")
			}
		}
		objectKey = store.Prefix + "return-flow/" + filepath.Base(asset.PublicPath)
		if err = store.put(ctx, tx, objectKey, bytes.NewReader(data), asset.Size, contentType, asset.SHA256); err != nil {
			return err
		}
		asset.ObjectKey = objectKey
		if upgrade {
			if err = tx.Model(&ReturnFlowAsset{}).Where("id = ? AND object_key = ''", asset.ID).Update("object_key", objectKey).Error; err != nil {
				return err
			}
		} else if err = tx.Create(&asset).Error; err != nil {
			return err
		}
		return recordAudit(tx, owner.ID, "diy_landing.image", asset.ID, "")
	})
	if err != nil {
		store.discard(objectKey)
		fail(w, err)
		return
	}
	jsonOut(w, 201, map[string]any{"id": asset.ID, "url": strings.TrimRight(s.Config.PublicURL, "/") + asset.PublicPath, "reference": asset.PublicPath, "preview_url": asset.PublicPath, "content_type": asset.ContentType, "size": asset.Size})
}

func validateDIYManagedImageReferences(tx *gorm.DB, owner string, content DIYLandingContent) error {
	paths := map[string]bool{}
	for _, value := range content {
		if path := diyManagedImagePath(value); path != "" {
			paths[path] = true
		}
	}
	if len(paths) == 0 {
		return nil
	}
	values := make([]string, 0, len(paths))
	for path := range paths {
		values = append(values, path)
	}
	var rows []ReturnFlowAsset
	err := tx.Table("return_flow_assets AS a").Select("a.public_path", "a.content_type", "a.size").Joins("JOIN users u ON u.id = a.user_id AND u.disabled = ?", false).Where("a.user_id = ? AND a.public_path IN ?", owner, values).Find(&rows).Error
	if err != nil {
		return err
	}
	if len(rows) != len(paths) {
		return problem(400, "请选择当前账号上传的落地页图片")
	}
	for _, asset := range rows {
		if !diyLandingImageType(asset.ContentType) || asset.Size <= 0 || asset.Size > maxDIYContentImageBytes {
			return problem(400, "所选落地页素材不是有效的图片")
		}
	}
	return nil
}

func (s *Server) diyOwnedImageAssetURL(base, raw, kind string, expires time.Time, owner string) string {
	path := diyManagedImagePath(raw)
	if path == "" {
		return s.diyAssetURL(base, raw, kind, expires)
	}
	if kind != "image" || validateDIYManagedImageReferences(s.DB, owner, DIYLandingContent{"image": path}) != nil {
		return ""
	}
	return s.diyAssetURL(base, path, "managed-image", expires)
}

// This path is reached only after the caller verifies the short-lived asset
// signature. Reads use registered object metadata and never fetch a business URL.
func (s *Server) serveDIYManagedImage(w http.ResponseWriter, r *http.Request, path string) {
	if diyManagedImagePath(path) == "" || r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	var asset ReturnFlowAsset
	if err := s.DB.WithContext(r.Context()).Table("return_flow_assets AS a").Select("a.*").Joins("JOIN users u ON u.id = a.user_id AND u.disabled = ?", false).Where("a.public_path = ?", path).Take(&asset).Error; err != nil || !diyLandingImageType(asset.ContentType) || asset.Size <= 0 || asset.Size > maxDIYContentImageBytes {
		http.NotFound(w, r)
		return
	}
	var file io.ReadSeekCloser
	if asset.ObjectKey != "" {
		store := s.diyLandingImageReadStore(asset.ObjectKey)
		if store == nil || filepath.Base(asset.ObjectKey) != filepath.Base(path) {
			http.Error(w, "Image unavailable", 503)
			return
		}
		var size int64
		var err error
		file, size, err = store.open(r.Context(), asset.ObjectKey)
		if err != nil || size != asset.Size {
			if file != nil {
				file.Close()
			}
			http.Error(w, "Image unavailable", 503)
			return
		}
	} else {
		name := filepath.Join(s.returnFlowAssetDirectory(), filepath.Base(path))
		stat, err := os.Lstat(name)
		if err != nil || !stat.Mode().IsRegular() || stat.Size() != asset.Size {
			http.NotFound(w, r)
			return
		}
		local, err := os.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		file = local
	}
	defer file.Close()
	w.Header().Set("Content-Type", asset.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("ETag", `"`+asset.SHA256+`"`)
	http.ServeContent(w, r, filepath.Base(path), asset.CreatedAt, file)
}

func diyLandingUploadedImage(data []byte) (contentType, ext string, valid bool) {
	if len(data) == 0 || len(data) > maxDIYContentImageBytes {
		return "", "", false
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	formats := map[string][2]string{"png": {"image/png", "png"}, "jpeg": {"image/jpeg", "jpg"}, "gif": {"image/gif", "gif"}, "webp": {"image/webp", "webp"}}
	selected, supported := formats[format]
	if err != nil || !supported || config.Width <= 0 || config.Height <= 0 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 16_000_000 {
		return "", "", false
	}
	switch format {
	case "gif":
		if !diyLandingGIFBounds(data) {
			return "", "", false
		}
		_, err = gif.DecodeAll(bytes.NewReader(data))
	case "webp":
		_, _, err = image.Decode(bytes.NewReader(data))
		if err != nil && diyLandingAnimatedWebP(data, config.Width, config.Height) {
			err = nil
		}
	default:
		_, _, err = image.Decode(bytes.NewReader(data))
	}
	return selected[0], selected[1], err == nil
}

// Inspect animation bounds before a full GIF decode so small compressed uploads
// cannot force an unbounded allocation for thousands of canvas-sized frames.
func diyLandingGIFBounds(data []byte) bool {
	if len(data) < 13 {
		return false
	}
	position := 13
	if data[10]&0x80 != 0 {
		position += 3 << ((data[10] & 7) + 1)
	}
	pixels, frames := int64(0), 0
	blocks := func() bool {
		for position < len(data) {
			n := int(data[position])
			position++
			if n == 0 {
				return true
			}
			if n > len(data)-position {
				return false
			}
			position += n
		}
		return false
	}
	for position < len(data) {
		kind := data[position]
		position++
		switch kind {
		case 0x3b:
			return frames > 0 && position == len(data)
		case 0x21:
			if position >= len(data) {
				return false
			}
			position++ // Extension label precedes its size-delimited data blocks.
			if !blocks() {
				return false
			}
		case 0x2c:
			if len(data)-position < 9 {
				return false
			}
			width := int64(binary.LittleEndian.Uint16(data[position+4:]))
			height := int64(binary.LittleEndian.Uint16(data[position+6:]))
			pixels += width * height
			frames++
			if width <= 0 || height <= 0 || width > 4096 || height > 4096 || pixels > 16_000_000 || frames > 512 {
				return false
			}
			packed := data[position+8]
			position += 9
			if packed&0x80 != 0 {
				position += 3 << ((packed & 7) + 1)
			}
			if position >= len(data) {
				return false
			}
			position++ // LZW minimum code size.
			if !blocks() {
				return false
			}
		default:
			return false
		}
	}
	return false
}

func diyLandingAnimatedWebP(data []byte, width, height int) bool {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" || uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return false
	}
	frames, pixels := 0, int64(0)
	animation := false
	for position := 12; position < len(data); {
		if len(data)-position < 8 {
			return false
		}
		kind := string(data[position : position+4])
		size := uint64(binary.LittleEndian.Uint32(data[position+4 : position+8]))
		start := position + 8
		if size > uint64(len(data)-start) {
			return false
		}
		chunk := data[start : start+int(size)]
		position = start + int(size) + int(size&1)
		if position > len(data) {
			return false
		}
		if kind == "ANIM" {
			if len(chunk) != 6 {
				return false
			}
			animation = true
		}
		if kind != "ANMF" {
			continue
		}
		if !animation || len(chunk) < 16 {
			return false
		}
		u24 := func(raw []byte) int { return int(raw[0]) | int(raw[1])<<8 | int(raw[2])<<16 }
		x, y := u24(chunk[:3])*2, u24(chunk[3:6])*2
		w, h := u24(chunk[6:9])+1, u24(chunk[9:12])+1
		pixels += int64(w) * int64(h)
		frames++
		if x+w > width || y+h > height || w > 4096 || h > 4096 || pixels > 16_000_000 || frames > 512 {
			return false
		}
		payload := chunk[16:]
		alpha, images := false, 0
		for offset := 0; offset < len(payload); {
			if len(payload)-offset < 8 {
				return false
			}
			name := string(payload[offset : offset+4])
			n := uint64(binary.LittleEndian.Uint32(payload[offset+4 : offset+8]))
			if n > uint64(len(payload)-offset-8) {
				return false
			}
			if name == "ALPH" {
				alpha = true
			}
			if name == "VP8 " || name == "VP8L" {
				images++
			}
			offset += 8 + int(n) + int(n&1)
			if offset > len(payload) {
				return false
			}
		}
		if images != 1 {
			return false
		}
		// The standard decoder does not handle ANMF containers. Decode each
		// actual frame as a bounded static WebP while retaining the original file.
		frame := make([]byte, 30+len(payload))
		copy(frame, "RIFF")
		binary.LittleEndian.PutUint32(frame[4:8], uint32(len(frame)-8))
		copy(frame[8:12], "WEBP")
		copy(frame[12:16], "VP8X")
		binary.LittleEndian.PutUint32(frame[16:20], 10)
		if alpha {
			frame[20] = 0x10
		}
		for index := 0; index < 3; index++ {
			frame[24+index] = byte((w - 1) >> (8 * index))
			frame[27+index] = byte((h - 1) >> (8 * index))
		}
		copy(frame[30:], payload)
		decoded, format, err := image.Decode(bytes.NewReader(frame))
		if err != nil || format != "webp" || decoded.Bounds().Dx() != w || decoded.Bounds().Dy() != h {
			return false
		}
	}
	return animation && frames > 0
}
