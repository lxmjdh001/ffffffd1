package platform

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/minio/minio-go/v7"
)

// A release contains static JS plus two explicitly selected template images.
// CSS, other images, HTML, dynamic config and the service worker stay local.
type LandingScriptRelease struct {
	BaseURL string
	Files   map[string]string
	Images  map[string]string
}

var landingTemplateImageNames = []string{"GooglePlaybestapp-right.png", "age/age0.png"}

//go:embed templates/landing-classic/*.js
var landingClassicFiles embed.FS

type landingAssetEntry struct {
	File    string   `json:"file"`
	CSS     []string `json:"css"`
	IsEntry bool     `json:"isEntry"`
}

// Embedded build assets are immutable until the next binary is deployed.
// Share their parsed manifest, never visitor HTML, credentials or attribution.
var landingManifest = sync.OnceValues(func() (map[string]landingAssetEntry, error) {
	raw, err := pwaLandingFiles.ReadFile("templates/pwa-landing/.vite/manifest.json")
	if err != nil {
		return nil, err
	}
	var entries map[string]landingAssetEntry
	err = json.Unmarshal(raw, &entries)
	return entries, err
})

func landingTemplateEntry(layout string) (landingAssetEntry, error) {
	entries, err := landingManifest()
	if err != nil {
		return landingAssetEntry{}, err
	}
	key := "google-play/src/main.ts"
	if layout == "custom" {
		key = "custom-app/src/main.ts"
	}
	entry := entries[key]
	if entry.File == "" {
		return entry, fmt.Errorf("landing asset missing")
	}
	return entry, nil
}

func landingJavaScript() (map[string][]byte, error) {
	files := map[string][]byte{
		"apk-client.js":      []byte(apkClientScript()),
		"pwa-client.js":      []byte(externalBrowserJS + "\n" + pwaClientJS + "\n" + returnFlowJS + "\n" + pwaBrowserGuideJS),
		"preview-adapter.js": []byte(apkLandingBridgeJS),
	}
	err := fs.WalkDir(landingClassicFiles, "templates/landing-classic", func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || path.Ext(file) != ".js" {
			return nil
		}
		name := strings.TrimPrefix(file, "templates/landing-classic/")
		data, err := landingClassicFiles.ReadFile(file)
		if err != nil {
			return err
		}
		files[name] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	entries, err := landingManifest()
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsEntry && files[entry.File] == nil {
			return nil, fmt.Errorf("classic landing JS missing for %s; rebuild pwa-templates", entry.File)
		}
	}
	return files, err
}

func landingScriptVersion(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var content strings.Builder
	for _, name := range names {
		content.WriteString(name + "\n" + hashString(string(files[name])) + "\n")
	}
	return hashString(content.String())[:24]
}

func (s *Server) landingScriptURL(name, fallback string) string {
	if s.LandingScripts != nil {
		if u := s.LandingScripts.Files[name]; u != "" {
			return u
		}
	}
	return fallback
}
func (s *Server) landingModuleURL(name string) string {
	return s.landingScriptURL(name, "/pwa-template-assets/"+name)
}
func (s *Server) landingScriptType() string {
	if s.LandingScripts != nil {
		return "text/javascript"
	}
	return "module"
}
func (s *Server) landingScriptPolicy(policy string) string {
	if s.LandingScripts == nil {
		return policy
	}
	// Restrict the new script source to this content-addressed release folder,
	// not arbitrary objects in a bucket shared with other projects/customers.
	return strings.Replace(policy, "script-src 'self'", "script-src 'self' "+s.LandingScripts.BaseURL, 1)
}

// Publication is explicit at startup/deploy time, never triggered by visitors.
// The release becomes active only after all JS uploads and a public read
// succeed. Failed publication leaves all page references on the local server.
func (s *Server) PublishLandingScripts(ctx context.Context) error {
	if s.PageImages == nil {
		return nil
	}
	files, err := landingJavaScript()
	if err != nil {
		return err
	}
	images, err := s.publishLandingTemplateImages(ctx)
	if err != nil {
		return err
	}
	for name, data := range files {
		text := string(data)
		for image, raw := range images {
			text = strings.ReplaceAll(text, "/pwa-template-assets/"+image, raw)
		}
		files[name] = []byte(text)
	}
	prefix := "d1apk/landing-js/" + landingScriptVersion(files) + "/"
	release := &LandingScriptRelease{BaseURL: s.PageImages.PublicURL + "/" + prefix, Files: map[string]string{}, Images: images}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !fs.ValidPath(name) || path.Ext(name) != ".js" {
			return errors.New("non-JS file in landing release")
		}
		data := files[name]
		_, err = s.PageImages.Client.PutObject(ctx, s.PageImages.Bucket, prefix+name, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: "application/javascript; charset=utf-8", CacheControl: "public, max-age=31536000, immutable", DisableMultipart: true, UserMetadata: map[string]string{"sha256": hashString(string(data))}})
		if err != nil {
			return fmt.Errorf("publish landing JS %s: %w", name, err)
		}
		release.Files[name] = release.BaseURL + name
	}
	// The template modules were bundled into classic scripts at build time, so
	// loading them does not require changing this shared bucket's CORS settings.
	for _, name := range names {
		if err = s.checkLandingScript(ctx, release.Files[name], files[name]); err != nil {
			return fmt.Errorf("verify public landing JS %s: %w", name, err)
		}
	}
	s.LandingScripts = release
	return nil
}

func (s *Server) checkLandingScript(ctx context.Context, raw string, want []byte) error {
	return s.checkPublicLandingAsset(ctx, raw, want, "javascript")
}

func (s *Server) checkPublicLandingAsset(ctx context.Context, raw string, want []byte, contentType string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("R2 public asset returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(len(want))+1))
	if err != nil {
		return err
	}
	if !bytes.Equal(data, want) {
		return errors.New("R2 landing asset content mismatch")
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), contentType) {
		return errors.New("R2 landing asset has incorrect MIME type")
	}
	return nil
}

func (s *Server) publishLandingTemplateImages(ctx context.Context) (map[string]string, error) {
	images := map[string]string{}
	for _, name := range landingTemplateImageNames {
		data, err := pwaLandingFiles.ReadFile("templates/pwa-landing/" + name)
		if err != nil {
			return nil, err
		}
		key := "d1apk/landing-images/" + hashString(string(data))[:24] + "/" + name
		_, err = s.PageImages.Client.PutObject(ctx, s.PageImages.Bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: "image/png", CacheControl: "public, max-age=31536000, immutable", DisableMultipart: true, UserMetadata: map[string]string{"sha256": hashString(string(data))}})
		if err != nil {
			return nil, fmt.Errorf("publish template image %s: %w", name, err)
		}
		raw := s.PageImages.PublicURL + "/" + key
		if err = s.checkPublicLandingAsset(ctx, raw, data, "image/png"); err != nil {
			return nil, fmt.Errorf("verify template image %s: %w", name, err)
		}
		images[name] = raw
	}
	return images, nil
}
