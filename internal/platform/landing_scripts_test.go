package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type landingScriptFixture struct {
	mu      sync.Mutex
	objects map[string][]byte
	failPut bool
}

func attachLandingScripts(t *testing.T, s *Server) *landingScriptFixture {
	t.Helper()
	f := &landingScriptFixture{objects: map[string][]byte{}}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, isCORS := r.URL.Query()["cors"]; isCORS {
			t.Error("JS publication must not read or modify shared bucket CORS")
			w.WriteHeader(400)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/bucket/")
		isJS := strings.HasPrefix(key, "d1apk/landing-js/") && path.Ext(key) == ".js"
		isSelectedImage := strings.HasPrefix(key, "d1apk/landing-images/") && (strings.HasSuffix(key, "/GooglePlaybestapp-right.png") || strings.HasSuffix(key, "/age/age0.png"))
		if !isJS && !isSelectedImage {
			t.Errorf("unselected landing asset upload/access: %s", key)
		}
		if isJS {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", "image/png")
		}
		switch r.Method {
		case "PUT":
			if r.Header.Get("Authorization") == "" {
				t.Error("unsigned script upload")
			}
			if f.failPut {
				w.WriteHeader(403)
				return
			}
			if r.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
				t.Error("missing immutable JS cache header")
			}
			f.objects[key], _ = io.ReadAll(r.Body)
			w.Header().Set("ETag", `"0123456789abcdef0123456789abcdef"`)
		case "GET":
			if r.Header.Get("Authorization") != "" {
				t.Error("public script probe must be anonymous")
			}
			data, ok := f.objects[key]
			if !ok {
				w.WriteHeader(404)
				return
			}
			w.Write(data)
		default:
			t.Errorf("unexpected JS operation %s", r.Method)
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(server.Close)
	client, err := minio.New(strings.TrimPrefix(server.URL, "https://"), &minio.Options{Creds: credentials.NewStaticV4("access", "secret", ""), Secure: true, Region: "auto", BucketLookup: minio.BucketLookupPath, Transport: server.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	s.PageImages = &R2PageImageStore{Client: client, Bucket: "bucket", PublicURL: server.URL + "/bucket"}
	s.HTTP = server.Client()
	return f
}

func TestLandingScriptsPublishOnlyJSAndTwoSelectedImages(t *testing.T) {
	s := testServer(t)
	f := attachLandingScripts(t, s)
	if err := s.PublishLandingScripts(context.Background()); err != nil {
		t.Fatal(err)
	}
	files, err := landingJavaScript()
	if err != nil {
		t.Fatal(err)
	}
	if len(f.objects) != len(files)+2 || len(files) != 5 || len(s.LandingScripts.Images) != 2 {
		t.Fatalf("unexpected JS release size: %d", len(f.objects))
	}
	for key, data := range f.objects {
		if path.Ext(key) != ".js" {
			continue
		}
		name := path.Base(key)
		expected := string(files[name])
		for image, raw := range s.LandingScripts.Images {
			expected = strings.ReplaceAll(expected, "/pwa-template-assets/"+image, raw)
		}
		if !bytes.Equal(data, []byte(expected)) {
			t.Fatal("existing JS content changed during publication", name)
		}
		if strings.HasPrefix(name, "google_play-") {
			for image, raw := range s.LandingScripts.Images {
				if !bytes.Contains(data, []byte(raw)) || bytes.Contains(data, []byte("/pwa-template-assets/"+image)) {
					t.Fatal("selected image URL was not replaced", image)
				}
			}
			if !bytes.Contains(data, []byte("/pwa-template-assets/shield.png")) || !bytes.Contains(data, []byte("/pwa-template-assets/age/age3.webp")) {
				t.Fatal("unselected image references changed")
			}
		}
	}
	for _, name := range []string{"sw.js", "landing-config.js", "style.css", "shield.png", "manifest.webmanifest"} {
		if _, ok := files[name]; ok {
			t.Fatal("non-static-JS entry was published", name)
		}
	}
	first := landingScriptVersion(files)
	files["apk-client.js"] = append(files["apk-client.js"], []byte("\n// release change")...)
	if first == landingScriptVersion(files) {
		t.Fatal("changed JS reused immutable release URL")
	}
}

func TestLandingScriptsDoNotRequireCORSAndFallbackOnFailure(t *testing.T) {
	s := testServer(t)
	f := attachLandingScripts(t, s)
	if err := s.PublishLandingScripts(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.LandingScripts = nil
	f.failPut = true
	if err := s.PublishLandingScripts(context.Background()); err == nil {
		t.Fatal("failed upload activated release")
	}
	if s.LandingScripts != nil || s.landingModuleURL("x.js") != "/pwa-template-assets/x.js" {
		t.Fatal("publication failure did not retain same-origin fallback")
	}
}

func TestLandingScriptReferencesAndDynamicEndpointsStaySeparate(t *testing.T) {
	s := testServer(t)
	attachLandingScripts(t, s)
	if err := s.PublishLandingScripts(context.Background()); err != nil {
		t.Fatal(err)
	}
	u := seedUser(t, s, 0)
	a := seedApp(t, s, u, 32)
	cookie, csrf := sessionFor(t, s, u)
	for _, layout := range []string{"google_play", "custom", "store", "minimal"} {
		d := defaultPageDesign(a)
		d.Layout = layout
		requireCode(t, call(t, s, "PUT", "/api/apps/"+a.ID+"/page-design", d, cookie, csrf), 200)
		w := call(t, s, "GET", "/a/"+a.Slug+"/info", nil, nil, "")
		requireCode(t, w, 200)
		if !strings.Contains(w.Body.String(), s.LandingScripts.Files["apk-client.js"]) {
			t.Fatal("APK client did not use R2", layout)
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), s.LandingScripts.BaseURL) {
			t.Fatal("public APK CSP blocks R2 JS")
		}
		if layout == "google_play" || layout == "custom" {
			if !strings.Contains(w.Body.String(), `href="/pwa-template-assets/`) || !strings.Contains(w.Body.String(), "/a/"+a.Slug+"/landing-config.js") {
				t.Fatal("CSS or dynamic config was moved to R2")
			}
		}
	}
	p := createPWAFixture(t, s, u, "Scripts only")
	for _, layout := range []string{"google_play", "custom", "legacy"} {
		config := landingFixture()
		config.Template = layout
		raw, _ := json.Marshal(config)
		s.DB.Model(&p).Update("landing_json", string(raw))
		w := pwaPublicCall(t, s, "/p/"+p.Slug+"/", "", "")
		requireCode(t, w, 200)
		if !strings.Contains(w.Body.String(), s.LandingScripts.Files["pwa-client.js"]) || !strings.Contains(w.Body.String(), "/p/"+p.Slug+"/style.css") {
			t.Fatal("PWA script/CSS separation changed", layout)
		}
	}
	for _, base := range []string{"/a/" + a.Slug + "/", "/p/" + p.Slug + "/"} {
		w := call(t, s, "GET", base+"landing-config.js", nil, nil, "")
		requireCode(t, w, 200)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("dynamic config accidentally cached")
		}
	}
	w := pwaPublicCall(t, s, "/p/"+p.Slug+"/sw.js", "", "")
	requireCode(t, w, 200)
	if strings.Contains(w.Body.String(), s.LandingScripts.BaseURL) || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal("service worker entry moved from origin")
	}
	w = call(t, s, "GET", "/api/public", nil, nil, "")
	if strings.Contains(w.Header().Get("Content-Security-Policy"), s.LandingScripts.BaseURL) {
		t.Fatal("R2 script permission leaked into console/API CSP")
	}
}
