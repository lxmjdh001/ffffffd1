package platform

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func diyContentTestPrepared(t *testing.T, source string) string {
	t.Helper()
	prepared, _, err := prepareDIYHTML(source, diyHTMLTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func diyContentTestField(t *testing.T, fields []DIYLandingContentField, kind, value string) DIYLandingContentField {
	t.Helper()
	for _, field := range fields {
		if field.Kind == kind && field.Value == value {
			return field
		}
	}
	t.Fatalf("missing %s field %q in %+v", kind, value, fields)
	return DIYLandingContentField{}
}

func diyContentTestDocument() string {
	return `<!doctype html><html><head><title>Document title</title><style>.hero{background:url(https://assets.example/first.png),url(https://assets.example/second.png)}@font-face{font-family:Test;src:url(https://assets.example/font.woff2)}</style><script>window.originalScript="keep"</script></head><body><h1 data-d1-bind="app-name" data-d1-label="Campaign title">Fresh <em>ideas</em></h1><p>Good <strong>details</strong> remain.</p><div hidden>Hidden text<img src="https://assets.example/hidden.png"></div><div aria-hidden="true">Also hidden</div><span class="material-icons">download</span><img alt="Hero image" src="https://assets.example/photo.png"><div style="background-image:url(https://assets.example/inline.png)"></div><button id="install" onclick="window.originalClick=true"><svg viewBox="0 0 12 12"><path d="M0 0h12v12z"></path><text>SVG icon text</text></svg><span data-d1-bind="primary-label">Get it</span></button><input type="submit" value="Continue"><a href="#privacy">Privacy policy</a></body></html>`
}

func TestDIYContentFieldsExtractVisibleContentWithStableIDs(t *testing.T) {
	prepared := diyContentTestPrepared(t, diyContentTestDocument())
	fields, err := diyContentFields(prepared, diyHTMLTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	again, err := diyContentFields(prepared, diyHTMLTestOptions())
	if err != nil || !reflect.DeepEqual(fields, again) {
		t.Fatal("field IDs changed for one immutable revision")
	}
	if len(fields) != 9 {
		t.Fatalf("unexpected field count %d: %+v", len(fields), fields)
	}
	title := diyContentTestField(t, fields, "title", "Fresh ideas")
	if title.ID != "title-1" || title.Label != "Campaign title" || title.MaxLength != 300 {
		t.Fatalf("title schema: %+v", title)
	}
	if documentTitle := fields[len(fields)-2]; documentTitle.ID != "document-title" || documentTitle.Kind != "title" || documentTitle.Value != "Document title" || documentTitle.MaxLength != 300 || documentTitle.Label != "" {
		t.Fatalf("browser title must use a fixed field after the visible content: %+v", documentTitle)
	}
	diyContentTestField(t, fields, "text", "Good details remain.")
	diyContentTestField(t, fields, "text", "Privacy policy")
	if photo := diyContentTestField(t, fields, "image", "https://assets.example/photo.png"); photo.Label != "Hero image" {
		t.Fatal("image alt label was lost")
	}
	for _, field := range fields {
		if strings.Contains(field.Value, "hidden") || strings.Contains(field.Value, "originalScript") || field.Value == "download" || strings.Contains(field.Value, "SVG icon") || strings.Contains(field.Value, "font.woff2") {
			t.Fatalf("nonvisual code became editable: %+v", field)
		}
	}
	button := fields[len(fields)-1]
	if button.ID != "primary-label" || button.Kind != "button" || button.Value != "Get it" || button.MaxLength != 80 {
		t.Fatalf("fixed button schema: %+v", button)
	}
}

func TestDIYContentApplyPreservesMarkupAndLocksButtons(t *testing.T) {
	prepared := diyContentTestPrepared(t, diyContentTestDocument())
	fields, err := diyContentFields(prepared, diyHTMLTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	title := diyContentTestField(t, fields, "title", "Fresh ideas")
	paragraph := diyContentTestField(t, fields, "text", "Good details remain.")
	background := diyContentTestField(t, fields, "image", "https://assets.example/second.png")
	photo := diyContentTestField(t, fields, "image", "https://assets.example/photo.png")
	content := DIYLandingContent{title.ID: `<img src=x onerror=alert(1)> New title`, paragraph.ID: "A replacement paragraph", background.ID: "https://images.example/new.webp?a=1&b=2", photo.ID: "", "primary-label": "Start now"}
	updated, err := applyDIYContent(prepared, diyHTMLTestOptions(), content)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated, "&lt;img src=x onerror=alert(1)&gt; New title") || !strings.Contains(updated, "<em></em>") || !strings.Contains(updated, "<strong></strong>") || !strings.Contains(updated, `window.originalScript="keep"`) {
		t.Fatal("text was not safely replaced with structural tags preserved")
	}
	if strings.Contains(updated, `data-d1-bind="app-name"`) {
		t.Fatal("app-name runtime binding would overwrite explicit title")
	}
	if !strings.Contains(updated, "https://assets.example/first.png") || !strings.Contains(updated, "https://images.example/new.webp?a=1&b=2") || strings.Contains(updated, "https://assets.example/second.png") || !strings.Contains(updated, "https://assets.example/font.woff2") {
		t.Fatal("background replacement changed unrelated CSS resources")
	}
	if strings.Contains(updated, `src="https://assets.example/photo.png"`) {
		t.Fatal("image clearing retained its source")
	}
	doc, err := html.Parse(strings.NewReader(updated))
	if err != nil {
		t.Fatal(err)
	}
	var button, input, privacy *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "button":
				button = n
			case "input":
				input = n
			case "a":
				privacy = n
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if button == nil || input == nil || privacy == nil {
		t.Fatal("original controls lost")
	}
	if diyAttr(button, "data-d1-action") != "primary" || diyAttr(button, "data-d1-custom-label") != "Start now" || diyAttr(input, "data-d1-action") != "primary" || diyAttr(input, "value") != "Start now" || diyAttr(input, "data-d1-bind") != "primary-label" {
		t.Fatal("button override changed its trusted action contract")
	}
	if diyAttr(button, "data-d1-bind") == "primary-label" || !strings.Contains(updated, `<svg viewBox="0 0 12 12">`) || !strings.Contains(updated, `<path d="M0 0h12v12z">`) || !strings.Contains(updated, `<span data-d1-bind="primary-label">Start now</span>`) {
		t.Fatal("button label replacement removed or wrapped the icon")
	}
	if diyAttr(privacy, "href") != "#privacy" || diyAttr(privacy, "data-d1-action") != "" {
		t.Fatal("ordinary privacy link became a download action")
	}
	if updated == prepared || content[title.ID] != `<img src=x onerror=alert(1)> New title` {
		t.Fatal("application content was not applied or its input map was mutated")
	}
	if unchanged, err := applyDIYContent(prepared, diyHTMLTestOptions(), nil); err != nil || unchanged != prepared {
		t.Fatal("empty application overrides changed the original template")
	}
}

func TestDIYContentRejectsUnknownUnsafeAndOversizedOverrides(t *testing.T) {
	prepared := diyContentTestPrepared(t, `<h1>Title</h1><p>Words</p><img src="https://assets.example/image.png"><button data-d1-action="primary">Install</button>`)
	for _, content := range []DIYLandingContent{
		{"action": "https://evil.example"}, {"page_id": "another-page"}, {"title-1": strings.Repeat("a", 301)}, {"text-1": strings.Repeat("a", 16001)}, {"primary-label": strings.Repeat("a", 81)}, {"primary-label": " \t\n"}, {"text-1": "bad\x00text"}, {"image-1": "javascript:alert(1)"}, {"image-1": "https://127.0.0.1/logo.png"}, {"image-1": "https://[::1]/logo.png"}, {"image-1": "https://10.0.0.1/logo.png"}, {"image-1": "https://169.254.169.254/latest"}, {"image-1": "https://metadata.google.internal/image"}, {"image-1": "https://assets.example:8443/image.png"}, {"image-1": "https://user:secret@assets.example/image.png"}, {"image-1": `https://assets.example/x</style><script>alert(1)</script>`}, {"image-1": "data:image/svg+xml;base64,PHN2Zz48L3N2Zz4="}, {"image-1": "data:image/png;base64,bm90LWFuLWltYWdl"},
	} {
		if err := validateDIYContent(prepared, diyHTMLTestOptions(), content); err == nil {
			t.Fatalf("unsafe overrides accepted: keys=%v", reflect.ValueOf(content).MapKeys())
		}
	}
	if err := validateDIYContent(prepared, diyHTMLTestOptions(), DIYLandingContent{"text-1": strings.Repeat("a", maxDIYContentBytes+1)}); err == nil {
		t.Fatal("oversized content accepted")
	}
	if err := validateDIYContent(prepared, diyHTMLTestOptions(), DIYLandingContent{"title-1": "中文标题", "text-1": "", "image-1": "https://images.example/art.webp?version=1&size=large", "primary-label": "打开应用"}); err != nil {
		t.Fatal(err)
	}
	if got := diyContentPrimaryLabel(DIYLandingContent{"primary-label": "  Install now  "}); got != "Install now" {
		t.Fatal("button label was not normalized")
	}
}

func diyContentTestPNG(t *testing.T, width, height int) string {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, width, height))
	picture.SetNRGBA(0, 0, color.NRGBA{R: 230, G: 100, B: 40, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, picture); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes())
}

func TestDIYContentImageValidationChecksBytesTypeDimensionsAndCompleteness(t *testing.T) {
	pngURL := diyContentTestPNG(t, 2, 2)
	if !validDIYContentImage(pngURL) || !validDIYContentImage("") {
		t.Fatal("valid inline/cleared PNG rejected")
	}
	if validDIYContentImage(strings.Replace(pngURL, "image/png", "image/jpeg", 1)) {
		t.Fatal("data MIME and actual image mismatch accepted")
	}
	data, err := base64.StdEncoding.DecodeString(strings.SplitN(pngURL, ",", 2)[1])
	if err != nil {
		t.Fatal(err)
	}
	if validDIYContentImage("data:image/png;base64," + base64.StdEncoding.EncodeToString(data[:len(data)-9])) {
		t.Fatal("truncated image accepted")
	}
	if validDIYContentImage(diyContentTestPNG(t, 4097, 1)) {
		t.Fatal("oversized decoded dimensions accepted")
	}
	if validDIYContentImage("data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, maxDIYContentImageBytes+1))) {
		t.Fatal("oversized image bytes accepted")
	}
	var jpegBytes bytes.Buffer
	if err := jpeg.Encode(&jpegBytes, image.NewRGBA(image.Rect(0, 0, 3, 3)), nil); err != nil {
		t.Fatal(err)
	}
	if !validDIYContentImage("data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpegBytes.Bytes())) {
		t.Fatal("real JPEG rejected")
	}
	if !validDIYContentImage("data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7") {
		t.Fatal("real GIF rejected")
	}
}

func TestDIYContentFieldsHonorsOwnerRevisionAndSupportedTarget(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, 0), seedUser(t, s, 0)
	page := publishDIYFixture(t, s, owner, createDIYFixture(t, s, owner, "Editable source"))
	path := "/api/diy-landings/" + page.ID + "/revisions/" + page.LatestPublishedRevisionID + "/content-fields?target=apk"
	requireCode(t, call(t, s, "GET", path, nil, nil, ""), 401)
	requireCode(t, pushTaskCall(t, s, "GET", path, nil, other, "", "correct"), 404)
	w := pushTaskCall(t, s, "GET", path, nil, owner, "", "correct")
	requireCode(t, w, 200)
	var response struct {
		Fields          []DIYLandingContentField
		MaxImageBytes   int `json:"max_image_bytes"`
		MaxContentBytes int `json:"max_content_bytes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.MaxImageBytes != maxDIYContentImageBytes || response.MaxContentBytes != maxDIYContentBytes {
		t.Fatalf("invalid editing schema: %s", w.Body.String())
	}
	diyContentTestField(t, response.Fields, "title", "Original source")
	diyContentTestField(t, response.Fields, "button", "Install")
	requireCode(t, pushTaskCall(t, s, "GET", strings.Replace(path, "target=apk", "target=invalid", 1), nil, owner, "", "correct"), 400)
	var revision DIYLandingRevision
	if err := s.DB.First(&revision, "id = ?", page.LatestPublishedRevisionID).Error; err != nil {
		t.Fatal(err)
	}
	config := revision.Config
	config.Targets = []string{"pwa"}
	configJSON, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&revision).Update("config", string(configJSON)).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, pushTaskCall(t, s, "GET", path, nil, owner, "", "correct"), 400)
}

func TestDIYContentFieldsForFavoriteUseOwnedSnapshotAndPlatformButtonDefaults(t *testing.T) {
	s := testServer(t)
	admin, owner := landingLibraryAdmin(t, s), seedUser(t, s, 0)
	source := createLandingLibraryFixture(t, s, admin, "Collected editable source")
	ref := favoriteLandingLibraryFixture(t, s, owner, source.ID)
	path := "/api/diy-landings/" + ref.PageID + "/revisions/" + ref.RevisionID + "/content-fields?target=pwa"
	w := pushTaskCall(t, s, "GET", path, nil, owner, "", "correct")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), "Library original") || strings.Contains(w.Body.String(), "user_id") {
		t.Fatal("favorite schema did not use owned immutable source")
	}
	page := createDIYFixture(t, s, owner, "Platform-only source")
	in := diyTestInput(page.Name)
	in["html"] = "<h1>Only heading</h1>"
	in["draft_version"] = page.DraftVersion
	w = pushTaskCall(t, s, "PUT", "/api/diy-landings/"+page.ID, in, owner, "", "correct")
	requireCode(t, w, 200)
	page = publishDIYFixture(t, s, owner, diyTestDetail(t, w))
	base := "/api/diy-landings/" + page.ID + "/revisions/" + page.LatestPublishedRevisionID + "/content-fields?target="
	for target, label := range map[string]string{"apk": "下载应用", "pwa": "安装应用"} {
		w = pushTaskCall(t, s, "GET", base+target, nil, owner, "", "correct")
		requireCode(t, w, 200)
		var out struct{ Fields []DIYLandingContentField }
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		diyContentTestField(t, out.Fields, "button", label)
	}
}

func TestDIYContentFieldsBoundFieldAndDepthWork(t *testing.T) {
	tooMany := strings.Repeat("<p>Separate paragraph</p>", maxDIYContentFields)
	if _, err := diyContentFields(tooMany, diyHTMLTestOptions()); err == nil {
		t.Fatal("unbounded field list accepted")
	}
	tooDeep := strings.Repeat("<div>", 140) + "Deep text" + strings.Repeat("</div>", 140)
	if _, err := diyContentFields(tooDeep, diyHTMLTestOptions()); err == nil {
		t.Fatal("unbounded structure traversal accepted")
	}
}

func TestDIYContentBackgroundExtractionPreservesCommentsAndIconImages(t *testing.T) {
	prepared := diyContentTestPrepared(t, `<style>.hero{/* hero ; : decoration */background:url("https://images.example/one.png?variant=a;b"),url("https://images.example/two.png")}@font-face{src:url("https://images.example/font.woff2")}</style><div class="icon"><img src="https://images.example/app-icon.png"><span>do-not-edit-icon-code</span></div><p>Copy <a role="button">Download</a></p>`)
	fields, err := diyContentFields(prepared, diyHTMLTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	first := diyContentTestField(t, fields, "image", "https://images.example/one.png?variant=a;b")
	second := diyContentTestField(t, fields, "image", "https://images.example/two.png")
	diyContentTestField(t, fields, "image", "https://images.example/app-icon.png")
	diyContentTestField(t, fields, "text", "Copy")
	for _, field := range fields {
		if strings.Contains(field.Value, "do-not-edit") || field.Kind == "text" && strings.Contains(field.Value, "Download") {
			t.Fatalf("icon/button copy became generic text: %+v", field)
		}
	}
	updated, err := applyDIYContent(prepared, diyHTMLTestOptions(), DIYLandingContent{first.ID: "", second.ID: "https://images.example/replaced.jpg"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated, `url("")`) || !strings.Contains(updated, `url("https://images.example/replaced.jpg")`) || !strings.Contains(updated, "hero ; : decoration") || !strings.Contains(updated, "font.woff2") {
		t.Fatal("CSS URLs were not safely replaced in place")
	}
}

func TestDIYContentImageThumbnailsUseSignedStaticProxy(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	in := diyTestInput("Safe thumbnails")
	in["html"] = `<h1>Thumbnails</h1><img src="https://images.example/hero.png?variant=1&amp;size=small"><img src="https://localhost/api/me">`
	w := pushTaskCall(t, s, "POST", "/api/diy-landings", in, owner, "", "correct")
	requireCode(t, w, 201)
	page := publishDIYFixture(t, s, owner, diyTestDetail(t, w))
	path := "/api/diy-landings/" + page.ID + "/revisions/" + page.LatestPublishedRevisionID + "/content-fields?target=apk"
	w = pushTaskCall(t, s, "GET", path, nil, owner, "", "correct")
	requireCode(t, w, 200)
	var out struct{ Fields []DIYLandingContentField }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, field := range out.Fields {
		if field.Kind != "image" {
			continue
		}
		count++
		proxy, err := url.Parse(field.PreviewURL)
		if err != nil || proxy.IsAbs() || proxy.Path != "/api/landing-template-assets/"+page.LatestPublishedRevisionID || proxy.Query().Get("url") != field.Value || proxy.Query().Get("kind") != "image" || proxy.Query().Get("signature") == "" {
			t.Fatalf("arbitrary remote image was exposed as a thumbnail request: %+v", field)
		}
		if strings.Contains(field.Value, "localhost") {
			requireCode(t, pushTaskCall(t, s, "GET", field.PreviewURL, nil, owner, "", "correct"), 404)
		}
	}
	if count != 2 {
		t.Fatal("missing image thumbnails")
	}
	if countRows(t, s, &AccessVisit{}) != 0 || countRows(t, s, &MeteredEvent{}) != 0 || countRows(t, s, &Ticket{}) != 0 {
		t.Fatal("thumbnail preparation reached application business routes")
	}
}

func TestDIYContentRecognizesExplicitCTAAnchorClassesAndKeepsNavigation(t *testing.T) {
	for _, class := range []string{"download-btn", "download-button", "btn-download", "install-btn", "install-button", "btn-install", "cta", "cta-button", "btn primary", "button primary", "btn btn-primary", "button button-primary", "btn primary-button", "button primary-btn", "btn download", "button install", "DOWNLOAD-BTN"} {
		t.Run(class, func(t *testing.T) {
			anchor := &html.Node{Type: html.ElementNode, Data: "a", Attr: []html.Attribute{{Key: "class", Val: class}, {Key: "href", Val: "https://assets.example/download"}}}
			if !diyContentButton(anchor, nil) {
				t.Fatal("clear CTA anchor was not recognized")
			}
			diyRemoveAttr(anchor, "href")
			if !diyContentButton(anchor, nil) {
				t.Fatal("legacy prepared CTA without href was not recognized")
			}
			diySetAttr(anchor, "href", "#details")
			if diyContentButton(anchor, nil) {
				t.Fatal("fragment-only navigation was promoted to installation")
			}
		})
	}
	for _, class := range []string{"", "privacy", "btn", "button", "primary", "footer-link", "download-info"} {
		anchor := &html.Node{Type: html.ElementNode, Data: "a", Attr: []html.Attribute{{Key: "class", Val: class}, {Key: "href", Val: "https://example.com/privacy"}}}
		if diyContentButton(anchor, nil) {
			t.Fatalf("ordinary link class %q became a CTA", class)
		}
	}
	fragment := &html.Node{Type: html.ElementNode, Data: "a", Attr: []html.Attribute{{Key: "id", Val: "explicit"}, {Key: "href", Val: "#details"}, {Key: "role", Val: "button"}}}
	if diyContentButton(fragment, nil) {
		t.Fatal("role alone overrode fragment navigation")
	}
	selectors, err := diySelectors("#explicit")
	if err != nil {
		t.Fatal(err)
	}
	if !diyContentButton(fragment, selectors) {
		t.Fatal("explicit primary selector was ignored")
	}
	diySetAttr(fragment, "data-d1-action", "primary")
	if !diyContentButton(fragment, nil) {
		t.Fatal("explicit primary data action was ignored")
	}
}

func TestDIYContentCTAAnchorLabelEditingPreservesIconsAndOrdinaryLinks(t *testing.T) {
	// Older published HTML has already had its external href removed. The
	// original class still identifies its CTA without broadening plain links.
	prepared := `<h1>Application</h1><p>Get the app: <a id="download" class="download-btn"><svg aria-hidden="true"><path d="M0 0h12v12z"></path></svg><span>Download now</span></a></p><a id="details" href="#details" class="cta">Details</a><a id="privacy" href="#privacy" class="btn primary">Privacy</a><a id="terms" class="footer-link">Terms</a>`
	fields, err := diyContentFields(prepared, diyHTMLTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	diyContentTestField(t, fields, "button", "Download now")
	diyContentTestField(t, fields, "text", "Get the app:")
	diyContentTestField(t, fields, "text", "Details")
	diyContentTestField(t, fields, "text", "Privacy")
	for _, field := range fields {
		if field.Kind == "text" && strings.Contains(field.Value, "Download now") {
			t.Fatal("CTA label leaked into the ordinary paragraph editor")
		}
	}
	updated, err := applyDIYContent(prepared, diyHTMLTestOptions(), DIYLandingContent{"primary-label": "Install this app"})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(strings.NewReader(updated))
	if err != nil {
		t.Fatal(err)
	}
	nodes := map[string]*html.Node{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if id := diyAttr(n, "id"); id != "" {
				nodes[id] = n
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if diyAttr(nodes["download"], "data-d1-action") != "primary" || diyAttr(nodes["download"], "data-d1-custom-label") != "Install this app" {
		t.Fatal("CTA anchor was not locked to the trusted application action")
	}
	if !strings.Contains(updated, `<span data-d1-bind="primary-label">Install this app</span>`) || !strings.Contains(updated, `<path d="M0 0h12v12z">`) {
		t.Fatal("CTA anchor lost its icon or independent editable label")
	}
	for _, id := range []string{"details", "privacy", "terms"} {
		if diyAttr(nodes[id], "data-d1-action") != "" {
			t.Fatalf("navigation link %s was promoted to installation", id)
		}
	}
	if diyAttr(nodes["details"], "href") != "#details" || diyAttr(nodes["privacy"], "href") != "#privacy" {
		t.Fatal("fragment navigation was modified")
	}
}

func TestDIYContentDocumentTitleUsesFixedFieldWithoutRenumberingBody(t *testing.T) {
	withTitle := diyContentTestPrepared(t, diyContentTestDocument())
	withoutTitle := strings.Replace(withTitle, "<title>Document title</title>", "", 1)
	first, err := diyContentFields(withTitle, diyHTMLTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	second, err := diyContentFields(withoutTitle, diyHTMLTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatal("adding a browser title changed the available field count")
	}
	for index, field := range first {
		if field.ID == "document-title" {
			if second[index].ID != "document-title" || second[index].Value != "" || field.Value != "Document title" || field.MaxLength != 300 || field.Label != "" {
				t.Fatal("document title fixed field was not stable")
			}
			continue
		}
		if !reflect.DeepEqual(field, second[index]) {
			t.Fatalf("body field changed when only head title changed: %+v / %+v", field, second[index])
		}
	}
	if first[len(first)-2].ID != "document-title" || first[len(first)-1].ID != "primary-label" {
		t.Fatal("fixed fields were inserted before old body field IDs")
	}
}

func TestDIYContentDocumentTitleCanBeAddedClearedAndEscapesPlainText(t *testing.T) {
	for _, source := range []string{
		`<h1>Visible hero</h1><p>Visible copy</p>`,
		`<!doctype html><html><head><title data-d1-bind="app-name">Original document title</title><meta name="description" content="Keep metadata"><script>window.originalTitleScript=true;</script></head><body><h1>Visible hero</h1><p>Visible copy</p></body></html>`,
	} {
		prepared := diyContentTestPrepared(t, source)
		for _, value := range []string{"A new page title", "", `App </title><script>window.injectedTitle=true</script> & Co`} {
			updated, err := applyDIYContent(prepared, diyHTMLTestOptions(), DIYLandingContent{"document-title": value})
			if err != nil {
				t.Fatal(err)
			}
			document, err := html.Parse(strings.NewReader(updated))
			if err != nil {
				t.Fatal(err)
			}
			head, title := diyContentHeadTitle(document)
			if head == nil || title == nil || title.Parent != head || diyContentTitleText(title) != value {
				t.Fatalf("title was not a plain text head child for value %q", value)
			}
			if diyAttr(title, "data-d1-bind") != "" {
				t.Fatal("automatic app-name binding would overwrite the custom browser title")
			}
			if !strings.Contains(updated, "<h1>Visible hero</h1>") || !strings.Contains(updated, "<p>Visible copy</p>") || strings.Contains(updated, "<script>window.injectedTitle=true</script>") {
				t.Fatal("editing the browser title changed visible content or inserted markup")
			}
			if strings.Contains(prepared, "Keep metadata") && (!strings.Contains(updated, "Keep metadata") || !strings.Contains(updated, "window.originalTitleScript=true;")) {
				t.Fatal("title editing discarded other head elements")
			}
			if got, exists := diyContentDocumentTitle(updated, diyHTMLTestOptions(), nil); !exists || got != value {
				t.Fatalf("rendered title semantics lost: %q exists=%v", got, exists)
			}
		}
	}
	if err := validateDIYContent(`<h1>Hero</h1>`, diyHTMLTestOptions(), DIYLandingContent{"document-title": strings.Repeat("字", 301)}); err == nil {
		t.Fatal("browser title length limit was not checked")
	}
}

func TestDIYContentDocumentTitleHelperDistinguishesAbsentAndExplicitEmpty(t *testing.T) {
	for _, test := range []struct {
		source  string
		content DIYLandingContent
		value   string
		exists  bool
	}{
		{source: `<h1>Hero</h1>`, exists: false},
		{source: `<title></title><h1>Hero</h1>`, exists: true},
		{source: `<title>Source browser title</title>`, value: "Source browser title", exists: true},
		{source: `<title>Source browser title</title>`, content: DIYLandingContent{"document-title": "Customer title"}, value: "Customer title", exists: true},
		{source: `<title>Source browser title</title>`, content: DIYLandingContent{"document-title": ""}, exists: true},
		{source: `<h1>Hero</h1>`, content: DIYLandingContent{"document-title": ""}, exists: true},
		{source: `<title>Still readable</title>` + strings.Repeat("<p>Many unrelated editable fields</p>", maxDIYContentFields), value: "Still readable", exists: true},
	} {
		value, exists := diyContentDocumentTitle(test.source, diyHTMLTestOptions(), test.content)
		if value != test.value || exists != test.exists {
			t.Fatalf("value=%q exists=%v; want value=%q exists=%v", value, exists, test.value, test.exists)
		}
	}
}

func TestDIYContentDocumentTitlePreservesOldFieldCapacityAndAppIsolation(t *testing.T) {
	prepared := diyContentTestPrepared(t, `<title>Source browser title</title>`+strings.Repeat("<p>Editable paragraph</p>", 199))
	fields, err := diyContentFields(prepared, diyHTMLTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 201 || fields[198].ID != "text-199" || fields[199].ID != "document-title" || fields[200].ID != "primary-label" {
		t.Fatal("old automatic field slots were consumed by the new fixed title")
	}
	all := DIYLandingContent{}
	for _, field := range fields {
		value := field.Value
		if field.ID == "primary-label" {
			value = "Install"
		}
		all[field.ID] = value
	}
	if err := validateDIYContent(prepared, diyHTMLTestOptions(), all); err != nil {
		t.Fatalf("all 201 legal fields could not be saved: %v", err)
	}
	first, err := applyDIYContent(prepared, diyHTMLTestOptions(), DIYLandingContent{"document-title": "Application A title"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := applyDIYContent(prepared, diyHTMLTestOptions(), DIYLandingContent{"document-title": "Application B title"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ source, want string }{{prepared, "Source browser title"}, {first, "Application A title"}, {second, "Application B title"}} {
		if title, exists := diyContentDocumentTitle(test.source, diyHTMLTestOptions(), nil); !exists || title != test.want {
			t.Fatalf("one application's title changed another source: %q", title)
		}
	}
}
