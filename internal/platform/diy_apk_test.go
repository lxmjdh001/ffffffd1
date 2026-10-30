package platform

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func diyAPKBytes(t *testing.T, content string) []byte {
	t.Helper()
	var out bytes.Buffer
	archive := zip.NewWriter(&out)
	manifest, err := archive.Create("AndroidManifest.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manifest.Write([]byte(`<manifest package="example.diy"/>`)); err != nil {
		t.Fatal(err)
	}
	data, err := archive.Create("classes.dex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = data.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func diyAPKUpload(t *testing.T, s *Server, owner User, path string, fields map[string]string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	cookie, csrf := sessionFor(t, s, owner)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := form.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	file, err := form.CreateFormFile("file", "fixture.apk")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(content); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, s.Config.PublicURL+path, &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func diyAPKFields(page diyLandingDetail) map[string]string {
	return map[string]string{"name": "DIY APK", "version": "1.0", "description": "Application description", "landing_source": "diy", "diy_page_id": page.ID, "diy_revision_id": page.LatestPublishedRevisionID}
}

func diyAPKFromResponse(t *testing.T, w *httptest.ResponseRecorder) Application {
	t.Helper()
	var app Application
	if err := json.Unmarshal(w.Body.Bytes(), &app); err != nil {
		t.Fatal(err)
	}
	return app
}

func createDIYAPKFixture(t *testing.T, s *Server, owner User, page diyLandingDetail) Application {
	t.Helper()
	w := diyAPKUpload(t, s, owner, "/api/apps", diyAPKFields(page), diyAPKBytes(t, "independent APK fixture"))
	requireCode(t, w, 201)
	return diyAPKFromResponse(t, w)
}

func diyAPKForm(t *testing.T, body string) url.Values {
	t.Helper()
	body = html.UnescapeString(body)
	fields := url.Values{}
	for _, name := range []string{"request_key", "attribution_token"} {
		m := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(body)
		if len(m) != 2 {
			t.Fatalf("missing native form field %s", name)
		}
		fields.Set(name, m[1])
	}
	return fields
}

func diyAPKPost(t *testing.T, s *Server, path, host string, fields url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	if host == "" {
		host = "localhost:8088"
	}
	r := httptest.NewRequest(http.MethodPost, "http://"+host+path, strings.NewReader(fields.Encode()))
	r.RemoteAddr = "203.0.113.25:1234"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Origin", "http://"+host)
	r.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 14; Mobile) Chrome/123")
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestDIYAPKCreateEditReplaceAndImportKeepVersionBinding(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, 0), seedUser(t, s, 0)
	page := publishDIYFixture(t, s, owner, createDIYFixture(t, s, owner, "Reusable APK"))
	foreign := publishDIYFixture(t, s, other, createDIYFixture(t, s, other, "Other APK"))
	fields := diyAPKFields(page)
	// Eighteen textual fields plus the file exercise the expanded multipart
	// limit; the old ten-part ceiling rejected valid DIY creation payloads.
	for i := len(fields); i < 18; i++ {
		fields[fmt.Sprintf("extra_%d", i)] = "bounded"
	}
	w := diyAPKUpload(t, s, owner, "/api/apps", fields, diyAPKBytes(t, "first version"))
	requireCode(t, w, 201)
	app := diyAPKFromResponse(t, w)
	if app.LandingSource != "diy" || app.DIYLanding == nil || app.DIYLanding.PageID != page.ID || app.DownloadMode != "info" || app.Platform != "Android" {
		t.Fatalf("DIY upload not applied: %+v", app)
	}
	var saved Application
	if err := s.DB.First(&saved, "id = ?", app.ID).Error; err != nil {
		t.Fatal(err)
	}
	originalKey := saved.FileKey
	fields["nineteenth_field"] = "excess"
	requireCode(t, diyAPKUpload(t, s, owner, "/api/apps", fields, diyAPKBytes(t, "excess")), 400)
	beforeApps := countRows(t, s, &Application{})
	beforeFiles, err := os.ReadDir(filepath.Join(s.Config.DataDir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	requireCode(t, diyAPKUpload(t, s, owner, "/api/apps", diyAPKFields(foreign), diyAPKBytes(t, "invalid binding")), 404)
	afterFiles, err := os.ReadDir(filepath.Join(s.Config.DataDir, "files"))
	if err != nil {
		t.Fatal(err)
	}
	if countRows(t, s, &Application{}) != beforeApps || len(beforeFiles) != len(afterFiles) {
		t.Fatal("invalid binding left partial application or file")
	}
	cookie, csrf := sessionFor(t, s, owner)
	edit := map[string]any{"name": "Edited APK", "version": "2.0", "description": "Edited description", "enabled": true}
	w = call(t, s, "PATCH", "/api/apps/"+app.ID, edit, cookie, csrf)
	requireCode(t, w, 200)
	if got := diyAPKFromResponse(t, w); got.DIYLanding == nil || !diySameReference(got.DIYLanding, app.DIYLanding) {
		t.Fatal("basic edit cleared existing binding")
	}
	edit["landing_source"] = "diy"
	edit["diy_landing"] = &DIYLandingReference{PageID: foreign.ID, RevisionID: foreign.LatestPublishedRevisionID}
	edit["name"] = "Should roll back"
	requireCode(t, call(t, s, "PATCH", "/api/apps/"+app.ID, edit, cookie, csrf), 404)
	w = call(t, s, "GET", "/api/apps/"+app.ID, nil, cookie, csrf)
	requireCode(t, w, 200)
	if diyAPKFromResponse(t, w).Name != "Edited APK" {
		t.Fatal("invalid edit partially changed application")
	}
	w = diyAPKUpload(t, s, owner, "/api/apps/"+app.ID+"/file", map[string]string{"name": "Updated package", "version": "3.0", "description": "Replacement"}, diyAPKBytes(t, "replacement package content"))
	requireCode(t, w, 201)
	replaced := diyAPKFromResponse(t, w)
	if replaced.ID != app.ID || replaced.DIYLanding == nil || !diySameReference(replaced.DIYLanding, app.DIYLanding) {
		t.Fatal("replacing APK changed DIY reference")
	}
	if err := s.DB.First(&saved, "id = ?", app.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.FileKey == originalKey {
		t.Fatal("APK replacement did not replace package")
	}
	w = diyAPKUpload(t, s, owner, "/api/apps", map[string]string{"name": "Imported APK", "version": "1.0", "import_from": app.ID}, diyAPKBytes(t, "imported independent package"))
	requireCode(t, w, 201)
	imported := diyAPKFromResponse(t, w)
	if imported.ID == app.ID || imported.DIYLanding == nil || !diySameReference(imported.DIYLanding, app.DIYLanding) || imported.DownloadMode != "info" {
		t.Fatal("same-account import lost fixed DIY version")
	}
	w = diyAPKUpload(t, s, owner, "/api/apps", map[string]string{"name": "Override import", "version": "1.0", "import_from": app.ID, "landing_source": "system"}, diyAPKBytes(t, "explicit system override"))
	requireCode(t, w, 201)
	if got := diyAPKFromResponse(t, w); got.LandingSource != "system" || got.DIYLanding != nil {
		t.Fatal("explicit source did not override import")
	}
	edit["name"] = "Restored system"
	edit["landing_source"] = "system"
	delete(edit, "diy_landing")
	w = call(t, s, "PATCH", "/api/apps/"+app.ID, edit, cookie, csrf)
	requireCode(t, w, 200)
	if got := diyAPKFromResponse(t, w); got.LandingSource != "system" || got.DIYLanding != nil {
		t.Fatal("system switch retained binding")
	}
	if design, err := s.pageDesign(saved); err != nil || design.Layout == "diy" {
		t.Fatal("system switch did not restore system mode", err)
	}
	requireCode(t, call(t, s, "DELETE", "/api/apps/"+imported.ID, nil, cookie, csrf), 200)
	if ref, err := diyBindingReference(s.DB, "apk", imported.ID); err != nil || ref != nil {
		t.Fatal("APK deletion left binding", err)
	}
}

func TestDIYAPKOriginalTicketRangeAndUVChain(t *testing.T) {
	for _, metered := range []bool{true, false} {
		t.Run(fmt.Sprint(metered), func(t *testing.T) {
			s := testServer(t)
			if metered {
				s = meteredServer(t, 0)
			}
			owner := seedUser(t, s, Dollar)
			page := publishDIYFixture(t, s, owner, createDIYFixture(t, s, owner, "Download"))
			app := createDIYAPKFixture(t, s, owner, page)
			if !metered {
				if err := s.DB.Model(&Plan{}).Where("id = ?", "free").Update("download_limit", 0).Error; err != nil {
					t.Fatal(err)
				}
			}
			base := "/a/" + app.Slug + "/"
			pageResponse := directRequest(t, s, "GET", base+"info?fbclid=diy-click", nil)
			requireCode(t, pageResponse, 200)
			body := html.UnescapeString(pageResponse.Body.String())
			frame := regexp.MustCompile(`<iframe[^>]*id="d1-diy-content"[^>]*src="([^"]+)"`).FindStringSubmatch(body)
			if len(frame) != 2 {
				t.Fatal("DIY iframe missing")
			}
			beforeVisits := countRows(t, s, &AccessVisit{})
			beforeEvents := countRows(t, s, &MeteredEvent{})
			beforeLedger := countRows(t, s, &Ledger{})
			child := directRequest(t, s, "GET", frame[1], pageResponse.Result().Cookies())
			requireCode(t, child, 200)
			if countRows(t, s, &AccessVisit{}) != beforeVisits || countRows(t, s, &MeteredEvent{}) != beforeEvents || countRows(t, s, &Ledger{}) != beforeLedger {
				t.Fatal("iframe content charged or recorded a second access")
			}
			if !strings.Contains(child.Body.String(), "Original source") || strings.Contains(child.Body.String(), "attribution_token") {
				t.Fatal("isolated content did not separate source and capabilities")
			}
			if metered && (beforeEvents != 0 || usageOwner(t, s, owner.ID).BalanceMicros != Dollar) {
				t.Fatal("APK page view charged before download")
			}
			fields := diyAPKForm(t, body)
			fields.Set("ad_consent", "yes")
			first := diyAPKPost(t, s, base+"download", "", fields, pageResponse.Result().Cookies())
			requireCode(t, first, 200)
			repeat := diyAPKPost(t, s, base+"download", "", fields, pageResponse.Result().Cookies())
			requireCode(t, repeat, 200)
			if first.Body.String() != repeat.Body.String() || countRows(t, s, &Ticket{}) != 1 {
				t.Fatal("duplicate click created another ticket")
			}
			var answer map[string]string
			if err := json.Unmarshal(first.Body.Bytes(), &answer); err != nil {
				t.Fatal(err)
			}
			raw := strings.TrimPrefix(answer["download_url"], "/d/")
			if len(raw) != 64 {
				t.Fatal("download bypassed original ticket URL")
			}
			var ticket Ticket
			if err := s.DB.First(&ticket, "hash = ?", hashString(raw)).Error; err != nil {
				t.Fatal(err)
			}
			var attribution Attribution
			if err := s.DB.First(&attribution, "id = ?", ticket.AttributionID).Error; err != nil {
				t.Fatal(err)
			}
			if attribution.FBClid != "diy-click" || !attribution.Consent {
				t.Fatal("DIY form lost attribution or consent")
			}
			visitor := ""
			for _, cookie := range pageResponse.Result().Cookies() {
				if cookie.Name == "d1_visitor" {
					visitor = cookie.Value
				}
			}
			if visitor == "" {
				t.Fatal("download visitor cookie missing")
			}
			requireCode(t, download(t, s, raw, visitor, "HEAD", ""), 200)
			requireCode(t, download(t, s, raw, visitor, "GET", "bytes=0-15"), 206)
			requireCode(t, download(t, s, raw, visitor, "GET", fmt.Sprintf("bytes=16-%d", app.Size-1)), 206)
			requireCode(t, download(t, s, raw, visitor, "GET", ""), 200)
			if countRows(t, s, &Download{}) != 1 || countRows(t, s, &Ledger{}) != 1 {
				t.Fatal("range/retry settlement was duplicated")
			}
			price := int64(3000)
			if metered {
				price = 50000
				if countRows(t, s, &MeteredEvent{}) != 1 {
					t.Fatal("download charged APK UV again")
				}
			}
			if got := usageOwner(t, s, owner.ID); got.BalanceMicros != Dollar-price || got.HeldMicros != 0 {
				t.Fatal("download settlement changed")
			}
		})
	}
}

func TestDIYAPKExternalClickRelayAndPaidReturnAvoidDuplicateCharge(t *testing.T) {
	s := meteredServer(t, 0)
	owner := seedUser(t, s, Dollar)
	page := publishDIYFixture(t, s, owner, createDIYFixture(t, s, owner, "External"))
	app := createDIYAPKFixture(t, s, owner, page)
	base := "/a/" + app.Slug + "/"
	target := s.Config.PublicURL + base + "info?campaign=paid-return"
	design, err := s.pageDesign(app)
	if err != nil {
		t.Fatal(err)
	}
	design.ExternalBrowser = &ExternalBrowserConfig{Enabled: true, URL: target}
	cookie, csrf := sessionFor(t, s, owner)
	requireCode(t, call(t, s, "PUT", "/api/apps/"+app.ID+"/page-design", design, cookie, csrf), 200)
	initial := directRequest(t, s, "GET", base+"info", nil)
	requireCode(t, initial, 200)
	if countRows(t, s, &MeteredEvent{}) != 0 {
		t.Fatal("external-mode landing charged before a click")
	}
	request := usageRequest(s, base, "203.0.113.25")
	click := map[string]string{"key": randomHex(16), "token": s.externalClickToken("apk", app.ID, target)}
	requireCode(t, usagePost(s, base+"external/click", click, request), 200)
	relay := directRequest(t, s, "GET", base+"external/open?key="+click["key"]+"&token="+url.QueryEscape(click["token"]), nil)
	requireCode(t, relay, 302)
	for range 2 {
		requireCode(t, usagePost(s, base+"external/click", click, request), 200)
		requireCode(t, directRequest(t, s, "GET", base+"external/open?key="+click["key"]+"&token="+url.QueryEscape(click["token"]), nil), 302)
	}
	returned, err := url.Parse(relay.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	proof := returned.Query().Get(externalReturnParam)
	if proof == "" || !s.paidExternalReturn("apk", app.ID, target, proof) {
		t.Fatal("paid return proof missing")
	}
	back := directRequest(t, s, "GET", returned.RequestURI(), initial.Result().Cookies())
	requireCode(t, back, 200)
	fields := diyAPKForm(t, back.Body.String())
	fields.Set("external_return", proof)
	w := diyAPKPost(t, s, base+"download", "", fields, initial.Result().Cookies())
	requireCode(t, w, 200)
	if countRows(t, s, &MeteredEvent{}) != 1 || countRows(t, s, &Ledger{}) != 1 || usageOwner(t, s, owner.ID).BalanceMicros != Dollar-50000 {
		t.Fatal("paid return download charged a second event")
	}
	if countRows(t, s, &Ticket{}) != 1 {
		t.Fatal("returned download did not use original ticket chain")
	}
}

func TestDIYAPKReturnFlowAuthorizationRemainsFree(t *testing.T) {
	s := meteredServer(t, 0)
	owner := seedUser(t, s, Dollar)
	page := publishDIYFixture(t, s, owner, createDIYFixture(t, s, owner, "Return flow"))
	app := createDIYAPKFixture(t, s, owner, page)
	design, err := s.pageDesign(app)
	if err != nil {
		t.Fatal(err)
	}
	ret := defaultReturnFlowConfig()
	ret.Enabled = true
	ret.TriggerMode = "both"
	ret.ButtonAction = "download"
	design.ReturnFlowConfig = &ret
	cookie, csrf := sessionFor(t, s, owner)
	requireCode(t, call(t, s, "PUT", "/api/apps/"+app.ID+"/page-design", design, cookie, csrf), 200)
	base := "/a/" + app.Slug + "/"
	token := returnBillingPage(t, s, base+"info", "203.0.113.90")
	before := usageOwner(t, s, owner.ID)
	ledger, events := countRows(t, s, &Ledger{}), countRows(t, s, &MeteredEvent{})
	for range 3 {
		requireCode(t, returnBillingCall(s, base+"return-flow/view", token, "203.0.113.90", s.Config.PublicURL), 200)
	}
	requireCode(t, returnBillingCall(s, base+"return-flow/view", token+"x", "203.0.113.90", s.Config.PublicURL), 403)
	requireCode(t, returnBillingCall(s, base+"return-flow/view", token, "203.0.113.91", s.Config.PublicURL), 403)
	requireCode(t, returnBillingCall(s, base+"return-flow/view", token, "203.0.113.90", "https://foreign.example"), 403)
	after := usageOwner(t, s, owner.ID)
	if after.BalanceMicros != before.BalanceMicros || after.HeldMicros != 0 || countRows(t, s, &Ledger{}) != ledger || countRows(t, s, &MeteredEvent{}) != events || countRows(t, s, &ReturnFlowViewCharge{}) != 0 {
		t.Fatal("free return flow changed usage accounting")
	}
}

func TestDIYAPKCloakInfoDirectAndCustomerDomain(t *testing.T) {
	s := testServer(t)
	owner, other := seedUser(t, s, Dollar), seedUser(t, s, Dollar)
	page := publishDIYFixture(t, s, owner, createDIYFixture(t, s, owner, "Cloaked APK"))
	app := createDIYAPKFixture(t, s, owner, page)
	addChosenDomain(t, s, owner, "chosen.example")
	addChosenDomain(t, s, other, "foreign.example")
	for _, mode := range []string{"info", "direct"} {
		body := cloakFixture()
		body["target_type"], body["target_id"], body["app_target_mode"], body["safe_url"] = "app", app.ID, mode, ""
		cloak := createTestCloak(t, s, owner, body)
		path := "/c/" + cloak.Slug
		response := chosenCall(s, "GET", "chosen.example", path+"?fbclid=cloak-diy")
		if mode == "info" {
			requireCode(t, response, 200)
			content := html.UnescapeString(response.Body.String())
			if !strings.Contains(content, "data-d1-diy") || !strings.Contains(content, `action="/a/`+app.Slug+`/download"`) || !strings.Contains(content, `data-base="/a/`+app.Slug+`/"`) {
				t.Fatal("cloak DIY used /c-relative business paths")
			}
			frame := regexp.MustCompile(`<iframe[^>]*id="d1-diy-content"[^>]*src="([^"]+)"`).FindStringSubmatch(content)
			if len(frame) != 2 {
				t.Fatal("cloaked content missing")
			}
			requireCode(t, chosenCall(s, "GET", "chosen.example", frame[1]), 200)
			requireCode(t, chosenCall(s, "GET", "foreign.example", frame[1]), 404)
		} else {
			requireCode(t, response, 307)
			if !strings.HasPrefix(response.Header().Get("Location"), "/d/") || strings.Contains(response.Body.String(), "d1-diy-content") {
				t.Fatal("direct cloak was changed into a page")
			}
			var ticket Ticket
			if err := s.DB.First(&ticket, "hash = ?", hashString(strings.TrimPrefix(response.Header().Get("Location"), "/d/"))).Error; err != nil {
				t.Fatal(err)
			}
			if ticket.Host != "chosen.example" {
				t.Fatal("direct cloak changed download domain")
			}
		}
		requireCode(t, chosenCall(s, "GET", "foreign.example", path), 404)
	}
}
