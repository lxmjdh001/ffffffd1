package platform

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestLandingDecorationVariesOnlyInertSourceAndKeepsTenantClassStable(t *testing.T) {
	s := &Server{Config: Config{Secret: strings.Repeat("test-secret", 4)}}
	owner := strings.Repeat("a", 32)
	class := s.landingClass("apk", owner)
	if !regexp.MustCompile(`^d1-apk[a-f0-9]{16}-reference$`).MatchString(class) || strings.Contains(class, owner) {
		t.Fatal("invalid or identifying tenant class", class)
	}
	if class == s.landingClass("apk", "another-owner") || class == s.landingClass("pwa", owner) {
		t.Fatal("owner/page kind not isolated")
	}
	seen := map[string]bool{}
	corners := map[string]bool{}
	for range 64 {
		d := s.newLandingDecoration("apk", owner)
		raw := string(d.Noise)
		if d.Class != class || seen[raw] || len(raw) > 1024 {
			t.Fatal("unstable class, repeated fragment or oversized response")
		}
		seen[raw] = true
		assertLandingDecoration(t, "<html><body class=\""+class+"\">"+raw+"<main>Untouched</main></body></html>", class)
		if !regexp.MustCompile(`width:[1235]px`).MatchString(raw) || !regexp.MustCompile(`height:[1235]px`).MatchString(raw) || !regexp.MustCompile(`border:[1235]px (solid|dotted|dashed) #[a-f0-9]{6}`).MatchString(raw) || !strings.Contains(raw, "pointer-events:none") || !strings.Contains(raw, "opacity:0") {
			t.Fatal("unexpected decorative CSS", raw)
		}
		vertical, horizontal := "top", "left"
		if strings.Contains(raw, "bottom:0") {
			vertical = "bottom"
		}
		if strings.Contains(raw, "right:0") {
			horizontal = "right"
		}
		corners[vertical+horizontal] = true
	}
	if len(corners) != 4 {
		t.Fatal("corner choice did not vary", corners)
	}
}

func assertLandingDecoration(t *testing.T, raw, class string) string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var body *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "body" {
			body = n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	if body == nil || !strings.Contains(" "+decorationAttr(body, "class")+" ", " "+class+" ") {
		t.Fatal("tenant root class missing")
	}
	first := body.FirstChild
	for first != nil && first.Type == html.TextNode && strings.TrimSpace(first.Data) == "" {
		first = first.NextSibling
	}
	if first == nil || first.Type != html.CommentNode || !regexp.MustCompile(`^[a-z]{32}$`).MatchString(first.Data) {
		t.Fatal("BODY must start with a random letters-only comment")
	}
	fragment := first.NextSibling
	if fragment == nil || fragment.Type != html.ElementNode || fragment.Data != "template" || decorationAttr(fragment, "aria-hidden") != "true" {
		t.Fatal("random fragment must stay in an inaccessible, inert template")
	}
	var validate func(*html.Node)
	validate = func(n *html.Node) {
		if n.Type == html.CommentNode && !regexp.MustCompile(`^[a-z]+$`).MatchString(n.Data) {
			t.Fatal("nonrandom text in comment", n.Data)
		}
		if n.Type == html.ElementNode && n.Data != "template" && n.Data != "style" && n.Data != "span" {
			t.Fatal("active or unexpected element in fragment", n.Data)
		}
		for _, a := range n.Attr {
			if strings.HasPrefix(a.Key, "on") || a.Key == "src" || a.Key == "href" {
				t.Fatal("fragment introduced an event or resource", a.Key)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			validate(c)
		}
	}
	validate(fragment)
	return first.Data
}

func decorationAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func TestAPKLandingDecorationAcrossTemplatesPreviewAndCloak(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	app := seedApp(t, s, owner, 120)
	cookie, csrf := sessionFor(t, s, owner)
	class := s.landingClass("apk", owner.ID)
	d := defaultPageDesign(app)
	for _, layout := range []string{"google_play", "custom", "store", "minimal"} {
		d.Layout = layout
		requireCode(t, call(t, s, "PUT", "/api/apps/"+app.ID+"/page-design", d, cookie, csrf), 200)
		previous := ""
		for range 2 {
			page := call(t, s, "GET", "/a/"+app.Slug+"/info", nil, nil, "")
			requireCode(t, page, 200)
			comment := assertLandingDecoration(t, page.Body.String(), class)
			if previous == comment || !strings.Contains(page.Body.String(), `action="/a/`+app.Slug+`/download"`) {
				t.Fatal("fragment not fresh or native download URL changed", layout)
			}
			previous = comment
			if layout == "google_play" || layout == "custom" {
				if strings.Contains(page.Body.String(), ".d1-apk-reference") || !strings.Contains(page.Body.String(), "."+class) || !strings.Contains(page.Body.String(), "data-d1-apk-reference") {
					t.Fatal("CSS and runtime marker not synchronized with tenant class", layout)
				}
			}
		}
		preview := call(t, s, "POST", "/api/apps/"+app.ID+"/page-design/preview", d, cookie, csrf)
		requireCode(t, preview, 200)
		var result map[string]string
		if err := json.Unmarshal(preview.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		assertLandingDecoration(t, result["html"], class)
	}
	t.Run("cloak", func(t *testing.T) {
		body := cloakFixture()
		body["target_type"], body["target_id"], body["app_target_mode"], body["safe_url"] = "app", app.ID, "info", ""
		body["blocked_languages"] = []string{}
		cloak := createTestCloak(t, s, owner, body)
		addChosenDomain(t, s, owner, "chosen.example")
		page := chosenCall(s, "GET", "chosen.example", "/c/"+cloak.Slug)
		requireCode(t, page, 200)
		assertLandingDecoration(t, page.Body.String(), class)
		if page.Header().Get("Location") != "" {
			t.Fatal("cloak started redirecting")
		}
	})
	if countRows(t, s, &Ticket{}) != 0 || countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &Download{}) != 0 {
		t.Fatal("source variation triggered downloads/billing")
	}
}

func TestPWALandingDecorationAcrossTemplatesPreviewAndCloak(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	cookie, csrf := sessionFor(t, s, owner)
	app := createPWAFixture(t, s, owner, "PWA example")
	class := s.landingClass("pwa", owner.ID)
	base := "/p/" + app.Slug + "/"
	c := landingFixture()
	for _, layout := range []string{"google_play", "custom", "legacy"} {
		c.Template = layout
		requireCode(t, call(t, s, "PUT", "/api/pwas/"+app.ID, map[string]any{"landing_config": c}, cookie, csrf), 200)
		previous := ""
		for range 2 {
			page := pwaPublicCall(t, s, base, "", "en")
			requireCode(t, page, 200)
			comment := assertLandingDecoration(t, page.Body.String(), class)
			if previous == comment || !strings.Contains(page.Body.String(), base+"manifest.webmanifest") || !strings.Contains(page.Body.String(), base+"client.js") {
				t.Fatal("fragment repeated or same-domain PWA paths changed", layout)
			}
			previous = comment
		}
		if layout == "legacy" {
			css := pwaPublicCall(t, s, base+"style.css", "", "")
			requireCode(t, css, 200)
			if !strings.Contains(css.Body.String(), "."+class+"{") || strings.Contains(css.Body.String(), ".install-page{") {
				t.Fatal("legacy PWA CSS no longer matches body")
			}
		} else {
			for _, apk := range []bool{false, true} {
				path, wantClass := "/api/pwa-landing/preview/"+layout, class
				if apk {
					path, wantClass = path+"?apk=1", s.landingClass("apk", owner.ID)
				}
				preview := call(t, s, "GET", path, nil, cookie, "")
				requireCode(t, preview, 200)
				assertLandingDecoration(t, preview.Body.String(), wantClass)
				if apk && (!strings.Contains(preview.Body.String(), "."+wantClass) || strings.Contains(preview.Body.String(), ".d1-apk-reference")) {
					t.Fatal("APK preview CSS no longer matches body")
				}
			}
		}
	}
	launch := pwaPublicCall(t, s, base+"launch", "", "")
	requireCode(t, launch, 200)
	if strings.Contains(launch.Body.String(), "data-d1-decoration") || !strings.Contains(launch.Body.String(), `class="launch-page"`) {
		t.Fatal("runtime target page modified")
	}
	body := cloakFixture()
	body["target_type"], body["target_id"], body["safe_url"] = "pwa", app.ID, ""
	body["blocked_languages"] = []string{}
	cloak := createTestCloak(t, s, owner, body)
	addChosenDomain(t, s, owner, "chosen.example")
	page := chosenCall(s, "GET", "chosen.example", "/c/"+cloak.Slug)
	requireCode(t, page, 200)
	assertLandingDecoration(t, page.Body.String(), class)
	if page.Header().Get("Location") != "" || countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &PWAPushDelivery{}) != 0 {
		t.Fatal("source variation changed routing, billing or push")
	}
}
