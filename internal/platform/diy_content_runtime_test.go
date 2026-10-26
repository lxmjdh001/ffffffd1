package platform

import (
	"encoding/json"
	"html"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func runtimeDIYContentPage(t *testing.T, s *Server, owner User) (diyLandingDetail, DIYLandingContent) {
	t.Helper()
	in := diyTestInput("Reusable editable page")
	in["html"] = `<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><title>Browser title</title></head><body><h1 data-d1-bind="app-name">Original headline</h1><p>Original paragraph <strong>with emphasis</strong></p><img alt="Hero picture" src="https://example.com/hero.png"><button data-d1-action="primary"><svg aria-hidden="true"><path d="M0 0h10"/></svg><span data-d1-bind="primary-label">Install</span></button><input type="button" value="Install"><a href="#details">Details</a></body></html>`
	w := pushTaskCall(t, s, "POST", "/api/diy-landings", in, owner, "", "correct")
	requireCode(t, w, 201)
	page := publishDIYFixture(t, s, owner, diyTestDetail(t, w))
	var revision DIYLandingRevision
	if err := s.DB.First(&revision, "id = ?", page.LatestPublishedRevisionID).Error; err != nil {
		t.Fatal(err)
	}
	fields, err := diyContentFields(revision.RenderHTML, revision.Config)
	if err != nil {
		t.Fatal(err)
	}
	content := DIYLandingContent{"primary-label": "开始体验"}
	for _, field := range fields {
		switch {
		case field.Kind == "title" && field.Value == "Original headline":
			content[field.ID] = "当前应用专属标题"
		case field.Kind == "text" && strings.Contains(field.Value, "Original paragraph"):
			content[field.ID] = "当前应用专属介绍"
		case field.Kind == "image":
			content[field.ID] = "https://example.com/custom-hero.jpg"
		}
	}
	if len(content) != 4 {
		t.Fatalf("expected title, text, image and primary label fields; got %d", len(content))
	}
	return page, content
}

func runtimeDIYFrameURL(t *testing.T, body string) string {
	t.Helper()
	match := regexp.MustCompile(`<iframe[^>]+src="([^"]+)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatal("public shell has no isolated content frame")
	}
	return html.UnescapeString(match[1])
}

func runtimeDIYContentContains(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	requireCode(t, w, 200)
	for _, value := range []string{"当前应用专属标题", "当前应用专属介绍", "custom-hero.jpg", "开始体验", "<svg", "data-d1-action=\"primary\""} {
		if !strings.Contains(w.Body.String(), value) {
			t.Fatalf("customized document is missing %q", value)
		}
	}
	if regexp.MustCompile(`<h1[^>]*data-d1-bind="app-name"`).MatchString(w.Body.String()) {
		t.Fatal("app-name binding would replace the explicitly edited headline")
	}
}

func TestDIYContentReferencePreviewIsPrivateAndDoesNotEnterBilling(t *testing.T) {
	s := meteredServer(t, 0)
	owner, other := seedUser(t, s, Dollar), seedUser(t, s, Dollar)
	page, content := runtimeDIYContentPage(t, s, owner)
	ref := &DIYLandingReference{PageID: page.ID, RevisionID: page.LatestPublishedRevisionID, Content: content}
	for _, mode := range []string{"apk", "pwa"} {
		w := pushTaskCall(t, s, "POST", "/api/diy-landings/preview", map[string]any{"reference": ref, "mode": mode}, owner, "", "correct")
		requireCode(t, w, 200)
		var out struct {
			PreviewURL string `json:"preview_url"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.PreviewURL == "" {
			t.Fatal("preview URL missing", err)
		}
		preview := pushTaskCall(t, s, "GET", out.PreviewURL, nil, owner, "", "correct")
		runtimeDIYContentContains(t, preview)
		if !strings.Contains(preview.Header().Get("Content-Security-Policy"), "sandbox allow-scripts") {
			t.Fatal("customized preview lost isolation")
		}
		requireCode(t, pushTaskCall(t, s, "GET", out.PreviewURL, nil, other, "", "correct"), 404)
		requireCode(t, pushTaskCall(t, s, "POST", "/api/diy-landings/preview", map[string]any{"reference": ref, "mode": mode}, other, "", "correct"), 404)
	}
	invalid := *ref
	invalid.Content = DIYLandingContent{"action": "/p/another-app/install"}
	requireCode(t, pushTaskCall(t, s, "POST", "/api/diy-landings/preview", map[string]any{"reference": invalid, "mode": "pwa"}, owner, "", "correct"), 400)
	for _, model := range []any{&AccessVisit{}, &MeteredEvent{}, &Ledger{}, &Ticket{}, &Download{}} {
		if countRows(t, s, model) != 0 {
			t.Fatal("editing preview caused business activity")
		}
	}
	if current := usageOwner(t, s, owner.ID); current.BalanceMicros != Dollar || current.HeldMicros != 0 {
		t.Fatal("editing preview changed billing")
	}
}

func TestDIYContentPublicAPKAndPWAUseOnlyTheirBoundContentAndActions(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	page, content := runtimeDIYContentPage(t, s, owner)
	fields := diyAPKFields(page)
	fields["diy_content"] = diyJSON(content)
	w := diyAPKUpload(t, s, owner, "/api/apps", fields, diyAPKBytes(t, "custom content fixture"))
	requireCode(t, w, 201)
	app := diyAPKFromResponse(t, w)
	base := "/a/" + app.Slug + "/"
	shell := pwaPublicCall(t, s, base+"info", "", "zh-CN")
	requireCode(t, shell, 200)
	if !strings.Contains(shell.Body.String(), `action="`+base+`download"`) || !strings.Contains(shell.Body.String(), ">开始体验</button>") {
		t.Fatal("custom APK button no longer uses its original download route")
	}
	frame := pwaPublicCall(t, s, runtimeDIYFrameURL(t, shell.Body.String()), "", "zh-CN")
	runtimeDIYContentContains(t, frame)
	// An independent binding of the same immutable revision keeps its source.
	second := createDIYAPKFixture(t, s, owner, page)
	secondShell := pwaPublicCall(t, s, "/a/"+second.Slug+"/info", "", "zh-CN")
	secondFrame := pwaPublicCall(t, s, runtimeDIYFrameURL(t, secondShell.Body.String()), "", "zh-CN")
	requireCode(t, secondFrame, 200)
	if !strings.Contains(secondFrame.Body.String(), "Original headline") || strings.Contains(secondFrame.Body.String(), "当前应用专属标题") {
		t.Fatal("editing one application changed another application's content")
	}
	config := diyPWAConfig(page)
	config.DIYLanding.Content = content
	w = pushTaskCall(t, s, "POST", "/api/pwas", map[string]any{"name": "Editable PWA", "target_url": "https://example.com/current-pwa", "landing_config": config}, owner, "", "correct")
	requireCode(t, w, 201)
	pwa := diyPWAView(t, w)
	pwaBase := "/p/" + pwa.Slug + "/"
	pwaShell := pwaPublicCall(t, s, pwaBase, "", "zh-CN")
	requireCode(t, pwaShell, 200)
	if !strings.Contains(pwaShell.Body.String(), `id="install-button" type="button">开始体验</button>`) || !strings.Contains(pwaShell.Body.String(), `data-base="`+pwaBase+`"`) {
		t.Fatal("custom PWA CTA lost its own installation runtime")
	}
	runtimeDIYContentContains(t, pwaPublicCall(t, s, runtimeDIYFrameURL(t, pwaShell.Body.String()), "", "zh-CN"))
	var stored DIYLandingRevision
	if err := s.DB.First(&stored, "id = ?", page.LatestPublishedRevisionID).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored.RawHTML, "当前应用专属") || !strings.Contains(stored.RenderHTML, "Original headline") {
		t.Fatal("application editing mutated the immutable shared revision")
	}
}

func TestDIYContentCorruptBindingKeepsTrustedDownloadFallback(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	in := diyTestInput("Hidden native button")
	in["config"] = DIYLandingOptions{Targets: []string{"apk", "pwa"}, PlatformButton: false}
	w := pushTaskCall(t, s, "POST", "/api/diy-landings", in, owner, "", "correct")
	requireCode(t, w, 201)
	page := publishDIYFixture(t, s, owner, diyTestDetail(t, w))
	app := createDIYAPKFixture(t, s, owner, page)
	if err := s.DB.Model(&DIYLandingBinding{}).Where("resource_type = ? AND resource_id = ?", "apk", app.ID).UpdateColumn("content", `{"unknown-content-field":"Corrupt stored value"}`).Error; err != nil {
		t.Fatal(err)
	}
	w = pwaPublicCall(t, s, "/a/"+app.Slug+"/info", "", "zh-CN")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), `class="diy-fallback"`) || strings.Contains(w.Body.String(), `id="d1-diy-actions" hidden`) || !strings.Contains(w.Body.String(), `action="/a/`+app.Slug+`/download"`) || strings.Contains(w.Body.String(), `id="d1-diy-content"`) {
		t.Fatal("corrupt editable content removed the trusted download fallback")
	}
}

func TestDIYCSSResourceSignaturesRemainValidCSSURLs(t *testing.T) {
	signed := `/a/app/diy-asset?expires=42&kind=image&signature=real-signature&url=https%3A%2F%2Fcdn.example%2Fhero.jpg`
	css := diyRewriteCSS(`.hero{background-image:url("https://cdn.example/hero.jpg")}`, "", func(string, string) string { return signed })
	if !strings.Contains(css, `url("`+signed+`")`) || strings.Contains(css, `\u0026`) {
		t.Fatal("CSS quoting corrupted the static proxy signature query")
	}
	if got := diyCSSQuotedString("a\"b\\c\n</style>"); strings.Contains(got, "</style>") || !strings.Contains(got, `\a `) || !strings.Contains(got, `\"`) {
		t.Fatal("CSS quoting failed to escape control/delimiter characters")
	}
}

func runtimeDIYDocumentTitle(t *testing.T, body string) string {
	t.Helper()
	match := regexp.MustCompile(`(?s)<title>(.*?)</title>`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatal("document has no title element")
	}
	return html.UnescapeString(match[1])
}

func TestDIYContentDocumentTitleIsIndependentAndAppliedToPublicShell(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	page, baseContent := runtimeDIYContentPage(t, s, owner)
	for _, tc := range []struct {
		name     string
		value    *string
		expected string
	}{
		{name: "original", expected: "Browser title"},
		{name: "custom", value: func() *string { v := "网页标签标题"; return &v }(), expected: "网页标签标题"},
		{name: "empty", value: func() *string { v := ""; return &v }(), expected: ""},
		{name: "plain-text", value: func() *string { v := `Title <script>unsafe()</script> & copy`; return &v }(), expected: `Title <script>unsafe()</script> & copy`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := DIYLandingContent{}
			for key, value := range baseContent {
				content[key] = value
			}
			if tc.value != nil {
				content["document-title"] = *tc.value
			}
			fields := diyAPKFields(page)
			fields["diy_content"] = diyJSON(content)
			w := diyAPKUpload(t, s, owner, "/api/apps", fields, diyAPKBytes(t, "document title "+tc.name))
			requireCode(t, w, 201)
			app := diyAPKFromResponse(t, w)
			check := func(path string) {
				shell := pwaPublicCall(t, s, path, "", "zh-CN")
				requireCode(t, shell, 200)
				if title := runtimeDIYDocumentTitle(t, shell.Body.String()); title != tc.expected {
					t.Fatalf("public browser title=%q want %q", title, tc.expected)
				}
				frame := pwaPublicCall(t, s, runtimeDIYFrameURL(t, shell.Body.String()), "", "zh-CN")
				runtimeDIYContentContains(t, frame)
				if title := runtimeDIYDocumentTitle(t, frame.Body.String()); title != tc.expected {
					t.Fatalf("frame title=%q want %q", title, tc.expected)
				}
				if strings.Contains(shell.Body.String(), "<script>unsafe()") || strings.Contains(frame.Body.String(), "<script>unsafe()") {
					t.Fatal("title was interpreted as executable HTML")
				}
			}
			check("/a/" + app.Slug + "/info")
			config := diyPWAConfig(page)
			config.DIYLanding.Content = content
			w = pushTaskCall(t, s, "POST", "/api/pwas", map[string]any{"name": "Application name unchanged", "target_url": "https://example.com/target", "landing_config": config}, owner, "", "correct")
			requireCode(t, w, 201)
			pwa := diyPWAView(t, w)
			check("/p/" + pwa.Slug + "/")
			if pwa.Name != "Application name unchanged" {
				t.Fatal("editing the browser title renamed the application")
			}
		})
	}
	var revision DIYLandingRevision
	if err := s.DB.First(&revision, "id = ?", page.LatestPublishedRevisionID).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(revision.RawHTML, "<title>Browser title</title>") {
		t.Fatal("per-app title editing changed the reusable source")
	}
}
