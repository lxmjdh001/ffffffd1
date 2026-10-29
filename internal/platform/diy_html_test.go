package platform

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func diyHTMLTestOptions() DIYLandingOptions {
	return DIYLandingOptions{Targets: []string{"apk", "pwa"}, PlatformButton: true, ResourceBase: "https://assets.example/campaign/"}
}

func TestDIYPrepareRemovesDocumentAuthorityAndPreservesContent(t *testing.T) {
	raw := `<!doctype html><html><head><base href="https://console.example/"><meta http-equiv="refresh" content="0;url=https://console.example/api/me"><link rel="manifest" href="https://console.example/manifest"><style>.hero{color:#167d9a;background:url(./hero.png)}</style><script>window.customAnimation=true;</script></head><body><h1>Customer page</h1><iframe src="https://console.example/api/me"></iframe><object data="https://console.example/"></object><form action="https://console.example/api/apps" method="post"><input name="name"></form><a href="https://console.example/a/private/info" target="_top" ping="https://console.example/a/private/info">Link</a><img src="./hero.png" srcset="https://console.example/a/private/info 1x"><button data-d1-action="primary" formaction="https://console.example/api/apps" download>Install</button></body></html>`
	prepared, issues, err := prepareDIYHTML(raw, diyHTMLTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prepared, "Customer page") || !strings.Contains(prepared, "window.customAnimation=true;") || !strings.Contains(prepared, "https://assets.example/campaign/hero.png") {
		t.Fatal("customer content or static resource resolution was lost")
	}
	doc, err := html.Parse(strings.NewReader(prepared))
	if err != nil {
		t.Fatal(err)
	}
	var inspect func(*html.Node)
	inspect = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "base" || n.Data == "iframe" || n.Data == "object" || n.Data == "embed" || n.Data == "portal" {
				t.Errorf("restricted document node retained: %s", n.Data)
			}
			for _, attr := range n.Attr {
				if attr.Key == "http-equiv" || attr.Key == "srcset" || attr.Key == "target" || attr.Key == "formaction" || attr.Key == "download" || attr.Key == "ping" || (n.Data == "form" && (attr.Key == "action" || attr.Key == "method")) || (n.Data == "a" && attr.Key == "href") {
					t.Errorf("restricted attribute retained on %s: %s", n.Data, attr.Key)
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			inspect(child)
		}
	}
	inspect(doc)
	if len(issues) == 0 {
		t.Fatal("restricted features were removed without diagnostics")
	}
}

func TestDIYPrepareRequiresRealAvailablePrimaryAction(t *testing.T) {
	options := diyHTMLTestOptions()
	options.PlatformButton = false
	for _, raw := range []string{
		`<!-- data-d1-action="primary" --><h1>No button</h1>`,
		`<script>const label="data-d1-action";</script><h1>No button</h1>`,
		`<button data-d1-action="">Empty action</button>`,
		`<button data-d1-action="complete">Unsupported action</button>`,
	} {
		if _, _, err := prepareDIYHTML(raw, options); err == nil {
			t.Errorf("accepted a page with no available primary action: %s", raw)
		}
	}
	options.PrimarySelector = "#missing"
	if _, _, err := prepareDIYHTML(`<button id="present">Install</button>`, options); err == nil {
		t.Fatal("accepted an unmatched primary selector with no platform button")
	}
	for _, selector := range []string{"button#install.primary[data-role=download]", "#missing, .primary", "[data-role]", `[data-role="download"]`} {
		options.PrimarySelector = selector
		if _, _, err := prepareDIYHTML(`<button id="install" class="primary" data-role="download">Install</button>`, options); err != nil {
			t.Errorf("valid selector %q was rejected: %v", selector, err)
		}
	}
	options.PrimarySelector = ""
	if _, _, err := prepareDIYHTML(`<button data-d1-action="primary">Install</button>`, options); err != nil {
		t.Fatal("real declarative primary button was rejected", err)
	}
}

func TestDIYContentRewritesAssetsByAttributeTypeAndInjectsBridgeFirst(t *testing.T) {
	prepared, _, err := prepareDIYHTML(`<html><head><link rel="StyleSheet" href="https://assets.example/site.css"><script src="https://assets.example/site.js"></script><style>@import "https://assets.example/unsafe.css";.hero{background:url(https://assets.example/bg.png)}</style></head><body><video src="https://assets.example/movie.mp4" poster="https://assets.example/poster.png"></video><img src="https://assets.example/hero.png"><script>window.customerCode=true</script></body></html>`, diyHTMLTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	body, err := diyContentDocument(prepared, diyHTMLTestOptions(), "channel", "pwa", "Install", "Example", "/assets", false, func(raw, kind string) string {
		kinds[raw] = kind
		return "/asset?kind=" + kind
	})
	if err != nil {
		t.Fatal(err)
	}
	for raw, kind := range map[string]string{"https://assets.example/site.css": "style", "https://assets.example/site.js": "script", "https://assets.example/movie.mp4": "media", "https://assets.example/poster.png": "image", "https://assets.example/hero.png": "image", "https://assets.example/bg.png": "image"} {
		if kinds[raw] != kind {
			t.Errorf("%s signed as %q; want %q", raw, kinds[raw], kind)
		}
	}
	if strings.Contains(body, "unsafe.css") || strings.Contains(body, "@import") {
		t.Fatal("uncontrolled CSS import retained")
	}
	if strings.Index(body, "D1Landing") > strings.Index(body, "window.customerCode=true") {
		t.Fatal("customer code precedes the platform bridge")
	}
	if strings.Contains(body, "attribution_token") || strings.Contains(body, "install_usage") || strings.Contains(body, "external_return") {
		t.Fatal("a platform business capability leaked to the child document")
	}
}

func TestDIYResourcePublicIPRejectsPrivateAndReservedDestinations(t *testing.T) {
	for _, raw := range []string{"0.0.0.0", "127.0.0.1", "10.2.3.4", "172.16.5.6", "192.168.4.5", "169.254.169.254", "100.64.0.1", "100.100.100.200", "198.18.0.1", "192.0.0.5", "224.0.0.1", "240.0.0.1", "::", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1"} {
		if diyPublicIP(net.ParseIP(raw)) {
			t.Errorf("non-public destination was accepted: %s", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !diyPublicIP(net.ParseIP(raw)) {
			t.Errorf("public destination was rejected: %s", raw)
		}
	}
}

func TestDIYAssetProxyRejectsBusinessHostsAndUntrustedCapabilities(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	addChosenDomain(t, s, owner, "customer-download.example")
	base := "/api/diy-landing-preview/" + randomHex(16) + "/asset"
	expires := s.Now().Add(10 * time.Minute)
	for _, raw := range []string{s.Config.PublicURL + "/api/me", "https://customer-download.example/a/app/info", "https://user:pass@assets.example/image.png", "http://assets.example/image.png", "https://assets.example:8443/image.png"} {
		path := s.diyAssetURL(base, raw, "image", expires)
		r := httptest.NewRequest(http.MethodGet, s.Config.PublicURL+path, nil)
		w := httptest.NewRecorder()
		s.serveDIYAsset(w, r)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s returned %d; want 404 without contacting upstream", raw, w.Code)
		}
	}
	for _, change := range []func(url.Values){
		func(q url.Values) { q.Set("signature", strings.Repeat("0", 64)) },
		func(q url.Values) { q.Set("kind", "script") },
		func(q url.Values) { q.Set("url", "https://other.example/image.png") },
		func(q url.Values) { q.Set("expires", "0") },
	} {
		path, err := url.Parse(s.diyAssetURL(base, "https://assets.example/image.png", "image", expires))
		if err != nil {
			t.Fatal(err)
		}
		q := path.Query()
		change(q)
		path.RawQuery = q.Encode()
		w := httptest.NewRecorder()
		s.serveDIYAsset(w, httptest.NewRequest(http.MethodGet, s.Config.PublicURL+path.String(), nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("tampered capability returned %d; want 404", w.Code)
		}
	}
}

func TestDIYPreviewUsesOwnedOpaqueContentWithoutBusinessEvents(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, 0), seedUser(t, s, 0)
	models := []any{&Ledger{}, &MeteredEvent{}, &Ticket{}, &Download{}, &AccessVisit{}}
	before := make([]int64, len(models))
	for i, model := range models {
		before[i] = countRows(t, s, model)
	}
	w := pushTaskCall(t, s, http.MethodPost, "/api/diy-landings/preview", map[string]any{"html": `<button data-d1-action="primary">Install</button><script>window.previewOnly=true</script>`, "config": diyHTMLTestOptions(), "mode": "pwa"}, owner, "", "correct")
	requireCode(t, w, http.StatusOK)
	var result struct {
		PreviewURL string `json:"preview_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.PreviewURL, "/api/diy-landing-preview/") {
		t.Fatal("preview did not return an isolated managed content path")
	}
	requireCode(t, call(t, s, http.MethodGet, result.PreviewURL, nil, nil, ""), http.StatusUnauthorized)
	requireCode(t, pushTaskCall(t, s, http.MethodGet, result.PreviewURL, nil, other, "", "correct"), http.StatusNotFound)
	w = pushTaskCall(t, s, http.MethodGet, result.PreviewURL, nil, owner, "", "correct")
	requireCode(t, w, http.StatusOK)
	policy := w.Header().Get("Content-Security-Policy")
	for _, expected := range []string{"sandbox allow-scripts", "connect-src 'none'", "worker-src 'none'", "frame-src 'none'", "form-action 'none'", "base-uri 'none'", "frame-ancestors 'self'"} {
		if !strings.Contains(policy, expected) {
			t.Errorf("missing content isolation policy: %s", expected)
		}
	}
	if strings.Contains(policy, "allow-same-origin") || strings.Contains(policy, "allow-top-navigation") || strings.Contains(policy, "script-src 'self'") || w.Header().Get("Cache-Control") != "no-store, no-transform" {
		t.Fatal("preview granted ambient platform authority or cache persistence")
	}
	if !strings.Contains(w.Body.String(), `"preview":true`) || !strings.Contains(w.Body.String(), "window.previewOnly=true") {
		t.Fatal("preview mode or isolated customer content missing")
	}
	for i, model := range models {
		if got := countRows(t, s, model); got != before[i] {
			t.Errorf("preview created business events in %T: before=%d after=%d", model, before[i], got)
		}
	}
	if err := s.DB.Model(&DIYLandingPreview{}).Where("id = ?", strings.TrimPrefix(result.PreviewURL, "/api/diy-landing-preview/")).Update("expires_at", s.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, pushTaskCall(t, s, http.MethodGet, result.PreviewURL, nil, owner, "", "correct"), http.StatusNotFound)
}

func TestDIYPreviewConcurrentSnapshotsStayBounded(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := pushTaskCall(t, s, "POST", "/api/diy-landings/preview", map[string]any{"html": "<h1>Preview</h1>", "config": diyHTMLTestOptions(), "mode": "apk"}, owner, "", "correct")
			if w.Code != 200 {
				t.Errorf("preview failed: %d %s", w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	if count := countRows(t, s, &DIYLandingPreview{}); count != 12 {
		t.Fatalf("concurrent preview snapshot quota: got %d, want 12", count)
	}
	if countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &AccessVisit{}) != 0 {
		t.Fatal("preview quota created business events")
	}
}
