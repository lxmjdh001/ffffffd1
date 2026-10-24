package platform

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func seedUADownload(t *testing.T, s *Server, u User, a Application, ua string) Download {
	t.Helper()
	d := Download{ID: randomHex(16), TicketHash: randomHex(32), UserID: u.ID, AppID: a.ID, AppName: a.Name, UserAgent: ua, CreatedAt: s.Now(), CompletedAt: s.Now()}
	if err := s.DB.Create(&d).Error; err != nil {
		t.Fatal(err)
	}
	return d
}

func setTestUABlock(t *testing.T, s *Server, u User, d Download, blocked bool) {
	t.Helper()
	cookie, csrf := sessionFor(t, s, u)
	w := call(t, s, "PUT", "/api/downloads/"+d.ID+"/ua-block", map[string]bool{"blocked": blocked}, cookie, csrf)
	requireCode(t, w, http.StatusOK)
	var response struct {
		Blocked bool `json:"blocked"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Blocked != blocked {
		t.Fatalf("unexpected UA block result: %s", w.Body.String())
	}
}

func uaRequest(s *Server, method, path, ua string, visitor, byteRange string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://localhost:8088"+path, nil)
	r.Header.Set("User-Agent", ua)
	r.Header.Set("Range", byteRange)
	if visitor != "" {
		r.AddCookie(&http.Cookie{Name: "d1_visitor", Value: visitor})
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func requireUA404(t *testing.T, w *httptest.ResponseRecorder, head bool) {
	t.Helper()
	requireCode(t, w, http.StatusNotFound)
	if w.Header().Get("Content-Type") != "text/html; charset=utf-8" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Robots-Tag") != "noindex, nofollow" {
		t.Fatalf("blocked response is not the private cloak fallback: %v", w.Header())
	}
	if w.Header().Get("Location") != "" || w.Header().Get("Content-Disposition") != "" || w.Header().Get("Content-Range") != "" || w.Header().Get("Accept-Ranges") != "" || w.Header().Get("ETag") != "" || len(w.Result().Cookies()) != 0 {
		t.Fatal("blocked response leaked a destination, attachment metadata or visitor cookies")
	}
	if head {
		if w.Body.Len() != 0 {
			t.Fatal("HEAD 404 must not contain a body")
		}
	} else if !strings.Contains(w.Body.String(), "Account not found") || !strings.Contains(w.Body.String(), "Please contact customer support") {
		t.Fatal("blocked request did not receive the English account-not-found page")
	}
}

func TestDownloadUABlockAPIIsolationIdempotenceAndMigration(t *testing.T) {
	s := testServer(t)
	u, other := seedUser(t, s, Dollar), seedUser(t, s, Dollar)
	a := seedApp(t, s, u, 10)
	d := seedUADownload(t, s, u, a, "private-full-UA/123 (test)")
	d2 := seedUADownload(t, s, u, a, d.UserAgent)
	empty := seedUADownload(t, s, u, a, "\r\n\x00")
	blank := seedUADownload(t, s, u, a, "   ")
	foreign := seedUADownload(t, s, other, a, d.UserAgent)
	cookie, csrf := sessionFor(t, s, u)
	path := "/api/downloads/" + d.ID + "/ua-block"
	requireCode(t, call(t, s, "PUT", path, map[string]bool{"blocked": true}, nil, ""), 401)
	requireCode(t, call(t, s, "PUT", path, map[string]bool{"blocked": true}, cookie, "wrong"), 403)
	for _, body := range []any{map[string]any{}, map[string]any{"blocked": nil}, map[string]any{"blocked": "true"}, map[string]any{"blocked": true, "user_id": other.ID}} {
		requireCode(t, call(t, s, "PUT", path, body, cookie, csrf), 400)
	}
	for _, id := range []string{foreign.ID, "missing-id"} {
		requireCode(t, call(t, s, "PUT", "/api/downloads/"+id+"/ua-block", map[string]bool{"blocked": true}, cookie, csrf), 404)
	}
	requireCode(t, call(t, s, "PUT", "/api/downloads/"+empty.ID+"/ua-block", map[string]bool{"blocked": true}, cookie, csrf), 400)
	requireCode(t, call(t, s, "PUT", "/api/downloads/"+blank.ID+"/ua-block", map[string]bool{"blocked": true}, cookie, csrf), 400)
	if countRows(t, s, &UABlock{}) != 0 || countRows(t, s, &Audit{}) != 0 {
		t.Fatal("invalid requests changed the blacklist")
	}
	setTestUABlock(t, s, u, d, true)
	setTestUABlock(t, s, u, d, true)
	setTestUABlock(t, s, u, d2, true)
	if countRows(t, s, &UABlock{}) != 1 || countRows(t, s, &Audit{}) != 1 {
		t.Fatal("repeated blocking must be idempotent, including audit events")
	}
	var block UABlock
	if err := s.DB.First(&block).Error; err != nil {
		t.Fatal(err)
	}
	if block.UserID != u.ID || block.UAHash != hashString(d.UserAgent) {
		t.Fatal("blacklist lacks its tenant-scoped normalized fingerprint")
	}
	var audit Audit
	if err := s.DB.First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(audit)
	if strings.Contains(string(encoded), d.UserAgent) || audit.Action != "download.ua.block" || audit.Target != block.UAHash {
		t.Fatal("audit should record the action without raw UA")
	}
	// The same UA can be blocked independently by another owner.
	setTestUABlock(t, s, other, foreign, true)
	if countRows(t, s, &UABlock{}) != 2 {
		t.Fatal("blacklist key must include the owner")
	}
	for range 2 {
		if err := Migrate(s.DB); err != nil {
			t.Fatal(err)
		}
	}
	if countRows(t, s, &UABlock{}) != 2 || countRows(t, s, &Download{}) != 5 {
		t.Fatal("migration rewrote existing records")
	}
	setTestUABlock(t, s, u, d2, false)
	setTestUABlock(t, s, u, d, false)
	if countRows(t, s, &UABlock{}) != 1 || countRows(t, s, &Audit{}) != 3 {
		t.Fatal("unblocking must be idempotent and tenant scoped")
	}
	var remaining UABlock
	if err := s.DB.First(&remaining, "user_id = ?", other.ID).Error; err != nil {
		t.Fatal("unblocking changed another owner")
	}
}

func TestDownloadUABlockListMarksAllMatchingRecordsWithinOwner(t *testing.T) {
	s := testServer(t)
	u, other := seedUser(t, s, 0), seedUser(t, s, 0)
	a := seedApp(t, s, u, 10)
	blocked := seedUADownload(t, s, u, a, "Browser/1")
	matching := seedUADownload(t, s, u, a, "Brow\nser/1")
	otherUA := seedUADownload(t, s, u, a, "browser/1")
	empty := seedUADownload(t, s, u, a, "")
	foreign := seedUADownload(t, s, other, a, blocked.UserAgent)
	setTestUABlock(t, s, u, blocked, true)
	check := func(owner User, want map[string]bool) {
		t.Helper()
		cookie, _ := sessionFor(t, s, owner)
		w := call(t, s, "GET", "/api/downloads?per_page=50&app_id="+a.ID, nil, cookie, "")
		requireCode(t, w, 200)
		var response struct {
			Data  []Download `json:"data"`
			Total int        `json:"total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Total != len(want) || len(response.Data) != len(want) {
			t.Fatalf("wrong total: %s", w.Body.String())
		}
		for _, d := range response.Data {
			value, ok := want[d.ID]
			if !ok || value != d.UABlocked {
				t.Fatalf("incorrect UA mark for %s: %s", d.ID, w.Body.String())
			}
		}
		if !strings.Contains(w.Body.String(), `"ua_blocked":false`) {
			t.Fatal("unblocked status must be explicit")
		}
	}
	check(u, map[string]bool{blocked.ID: true, matching.ID: true, otherUA.ID: false, empty.ID: false})
	check(other, map[string]bool{foreign.ID: false})
	setTestUABlock(t, s, u, matching, false)
	check(u, map[string]bool{blocked.ID: false, matching.ID: false, otherUA.ID: false, empty.ID: false})
}

func seedUATraffic(t *testing.T, s *Server, u User) TrafficPool {
	t.Helper()
	p := TrafficPool{Slug: randomHex(6), UserID: u.ID, Name: "test", Enabled: true, Mode: "sequential", UpdatedAt: s.Now()}
	if err := s.DB.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&TrafficLink{ID: randomHex(16), PoolSlug: p.Slug, UserID: u.ID, URL: "https://target.example/file", Position: 0}).Error; err != nil {
		t.Fatal(err)
	}
	return p
}

func seedUACloak(t *testing.T, s *Server, u User) Cloak {
	t.Helper()
	c := Cloak{ID: randomHex(16), Slug: randomHex(4), UserID: u.ID, Enabled: true, Name: "test", TargetType: "url", TargetURL: "https://target.example/file", SafeURL: "https://safe.example/private"}
	if err := s.DB.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDownloadUABlockProtectsOwnedPublicEntriesBeforeSideEffects(t *testing.T) {
	s := testServer(t)
	u, other := seedUser(t, s, Dollar), seedUser(t, s, Dollar)
	a, a2, b := seedApp(t, s, u, 10), seedApp(t, s, u, 10), seedApp(t, s, other, 10)
	ua := "UA-exact/123"
	d := seedUADownload(t, s, u, a, ua)
	pool, otherPool := seedUATraffic(t, s, u), seedUATraffic(t, s, other)
	cloak, otherCloak := seedUACloak(t, s, u), seedUACloak(t, s, other)
	setTestUABlock(t, s, u, d, true)
	for _, path := range []string{"/a/" + a.Slug, "/a/" + a2.Slug, "/a/" + a.Slug + "/info", "/r/" + pool.Slug, "/c/" + cloak.Slug} {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(method+path, func(t *testing.T) {
				requireUA404(t, uaRequest(s, method, path+"?fbclid=secret", ua, "", ""), method == "HEAD")
			})
		}
	}
	requireUA404(t, uaRequest(s, "POST", "/a/"+a.Slug+"/download", ua, "", ""), false)
	// Even disabling cloak filtering cannot bypass an account's explicit UA block.
	if err := s.DB.Model(&cloak).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	requireUA404(t, uaRequest(s, "GET", "/c/"+cloak.Slug, ua, "", ""), false)
	for _, model := range []any{&Ticket{}, &Attribution{}, &Usage{}, &Ledger{}, &Conversion{}, &Delivery{}} {
		if countRows(t, s, model) != 0 {
			t.Fatalf("blocked request changed %T", model)
		}
	}
	if countRows(t, s, &Download{}) != 1 {
		t.Fatal("blocked request created a download record")
	}
	var freshPool TrafficPool
	if err := s.DB.First(&freshPool, "slug = ?", pool.Slug).Error; err != nil || freshPool.Cursor != 0 {
		t.Fatal("blocked request advanced the traffic cursor")
	}
	var freshUser User
	if err := s.DB.First(&freshUser, "id = ?", u.ID).Error; err != nil || freshUser.BalanceMicros != Dollar || freshUser.HeldMicros != 0 {
		t.Fatal("blocked request changed the account balance")
	}
	for _, path := range []string{"/r/" + otherPool.Slug, "/c/" + otherCloak.Slug} {
		requireCode(t, uaRequest(s, "GET", path, ua, "", ""), 302)
	}
	requireCode(t, uaRequest(s, "HEAD", "/a/"+b.Slug, ua, "", ""), 200)
	for _, allowedUA := range []string{"ua-exact/123", ua + " extra", "prefix " + ua, ""} {
		requireCode(t, uaRequest(s, "HEAD", "/a/"+a.Slug, allowedUA, "", ""), 200)
	}
	// A blocked visitor can still use their own authenticated console.
	cookie, _ := sessionFor(t, s, u)
	r := httptest.NewRequest("GET", "http://localhost:8088/api/me", nil)
	r.Header.Set("User-Agent", ua)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	requireCode(t, w, 200)
}

func TestDownloadUABlockRevokesOldTicketsAndRangeResume(t *testing.T) {
	s := testServer(t)
	u, other := seedUser(t, s, Dollar), seedUser(t, s, Dollar)
	a, b := seedApp(t, s, u, 10), seedApp(t, s, other, 10)
	if err := s.DB.Model(&Plan{}).Where("id = ?", "free").Update("download_limit", 0).Error; err != nil {
		t.Fatal(err)
	}
	ticket, raw, visitor := seedTicket(t, s, a)
	_, otherRaw, otherVisitor := seedTicket(t, s, b)
	// Reserve a partial download before the owner blocks its original UA.
	requireCode(t, uaRequest(s, "GET", "/d/"+raw, ticket.UserAgent, visitor, "bytes=0-4"), 206)
	var before Ticket
	if err := s.DB.First(&before, "hash = ?", ticket.Hash).Error; err != nil {
		t.Fatal(err)
	}
	if !before.Reserved || before.Charged || before.Ranges == "" {
		t.Fatal("test must start with a reserved, incomplete transfer")
	}
	d := seedUADownload(t, s, u, a, ticket.UserAgent)
	setTestUABlock(t, s, u, d, true)
	for _, currentUA := range []string{ticket.UserAgent, "New browser/1", ""} {
		for _, method := range []string{"GET", "HEAD"} {
			for _, byteRange := range []string{"", "bytes=5-9"} {
				requireUA404(t, uaRequest(s, method, "/d/"+raw, currentUA, visitor, byteRange), method == "HEAD")
			}
		}
	}
	var after Ticket
	if err := s.DB.First(&after, "hash = ?", ticket.Hash).Error; err != nil {
		t.Fatal(err)
	}
	if after.Ranges != before.Ranges || after.Charged || after.Reserved != before.Reserved {
		t.Fatal("blocked resume changed the ticket reservation or completion")
	}
	for _, model := range []any{&Ledger{}, &Attribution{}, &Conversion{}} {
		if countRows(t, s, model) != 0 {
			t.Fatalf("blocked old ticket changed %T", model)
		}
	}
	if countRows(t, s, &Ticket{}) != 2 || countRows(t, s, &Download{}) != 1 {
		t.Fatal("blocked old ticket generated extra records")
	}
	// The identical original UA on another account's ticket remains valid.
	requireCode(t, uaRequest(s, "GET", "/d/"+otherRaw, ticket.UserAgent, otherVisitor, ""), 200)
	// A clean ticket also rejects a currently blocked request UA.
	cleanTicket, cleanRaw, cleanVisitor := seedTicket(t, s, a)
	if err := s.DB.Model(&cleanTicket).Update("user_agent", "clean/1").Error; err != nil {
		t.Fatal(err)
	}
	requireUA404(t, uaRequest(s, "GET", "/d/"+cleanRaw, ticket.UserAgent, cleanVisitor, "bytes=0-4"), false)
	setTestUABlock(t, s, u, d, false)
	requireCode(t, uaRequest(s, "GET", "/d/"+raw, "New browser/1", visitor, "bytes=5-9"), 206)
	if countRows(t, s, &Download{}) != 3 || countRows(t, s, &Ledger{}) != 2 {
		t.Fatal("unblocked resume did not complete and settle exactly once")
	}
}

func TestDownloadUABlockFailsClosedWhenLookupUnavailable(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	a := seedApp(t, s, u, 10)
	pool := seedUATraffic(t, s, u)
	cloak := seedUACloak(t, s, u)
	_, raw, visitor := seedTicket(t, s, a)
	d := seedUADownload(t, s, u, a, "some-UA")
	cookie, _ := sessionFor(t, s, u)
	if err := s.DB.Migrator().DropTable(&UABlock{}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/a/" + a.Slug, "/a/" + a.Slug + "/info", "/r/" + pool.Slug, "/c/" + cloak.Slug, "/d/" + raw} {
		for _, method := range []string{"GET", "HEAD"} {
			requireCode(t, uaRequest(s, method, path, "some-UA", visitor, ""), 503)
		}
	}
	requireCode(t, uaRequest(s, "POST", "/a/"+a.Slug+"/download", "some-UA", "", ""), 503)
	requireCode(t, call(t, s, "GET", "/api/downloads", nil, cookie, ""), 503)
	if countRows(t, s, &Ticket{}) != 1 || countRows(t, s, &Download{}) != 1 || countRows(t, s, &Ledger{}) != 0 || countRows(t, s, &Attribution{}) != 0 {
		t.Fatal("failed lookup allowed side effects")
	}
	var actual Download
	if err := s.DB.First(&actual, "id = ?", d.ID).Error; err != nil {
		t.Fatal(err)
	}
}

func TestUAFingerprintMatchesRecordedNormalization(t *testing.T) {
	if uaFingerprint("") != "" || uaFingerprint("\x00\n\r\t") != "" {
		t.Fatal("empty UA must not receive a fingerprint")
	}
	if uaFingerprint("Brow\nser/1") != hashString("Browser/1") || uaFingerprint("Browser/1") == uaFingerprint("browser/1") {
		t.Fatal("UA normalization must remove controls while preserving exact case")
	}
	ua := strings.Repeat("a", 2047) + "界" + "tail"
	if uaFingerprint(ua) != hashString(strings.Repeat("a", 2047)) || !validUTF8(cleanText(ua, 2048)) {
		t.Fatal("fingerprint must use the same UTF-8-safe 2048-byte cap as recorded downloads")
	}
}

func TestDownloadUABlockPreservesCustomDomainAndManagedAudienceIsolation(t *testing.T) {
	s := testServer(t)
	u, other := seedUser(t, s, Dollar), seedUser(t, s, Dollar)
	a, b := seedApp(t, s, u, 10), seedApp(t, s, other, 10)
	pool, cloak := seedUATraffic(t, s, u), seedUACloak(t, s, u)
	now := s.Now()
	for _, domain := range []Domain{
		{ID: randomHex(16), UserID: u.ID, Hostname: "own.example.com", Token: randomHex(24), VerifiedAt: &now},
		{ID: randomHex(16), UserID: other.ID, Hostname: "other.example.com", Token: randomHex(24), VerifiedAt: &now},
	} {
		if err := s.DB.Create(&domain).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DB.Model(&Settings{}).Where("id = 1").Updates(map[string]any{"download_domain_mode": "separate", "free_download_base_url": "https://free.example.com", "paid_download_base_url": "https://paid.example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	ua := "test browser/1"
	d := seedUADownload(t, s, u, a, ua)
	setTestUABlock(t, s, u, d, true)
	request := func(host, path string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("HEAD", "https://"+host+path, nil)
		r.Header.Set("User-Agent", ua)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/a/" + a.Slug, "/a/" + a.Slug + "/info", "/r/" + pool.Slug, "/c/" + cloak.Slug} {
		for _, host := range []string{"own.example.com", "free.example.com"} {
			requireUA404(t, request(host, path), true)
		}
		for _, host := range []string{"other.example.com", "paid.example.com"} {
			w := request(host, path)
			requireCode(t, w, 404)
			if strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
				t.Fatal("blacklist was checked before domain or audience ownership")
			}
		}
	}
	requireCode(t, request("other.example.com", "/a/"+b.Slug), 200)
}

func TestDownloadTicketStillRequiresEnabledOwnerAndMatchingManagedAudience(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	a := seedApp(t, s, u, 10)
	if err := s.DB.Model(&Settings{}).Where("id = 1").Updates(map[string]any{"download_domain_mode": "separate", "free_download_base_url": "https://free.example.com", "paid_download_base_url": "https://paid.example.com"}).Error; err != nil {
		t.Fatal(err)
	}
	ticket, raw, visitor := seedTicket(t, s, a)
	if err := s.DB.Model(&ticket).Update("host", "free.example.com").Error; err != nil {
		t.Fatal(err)
	}
	request := func(method string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "https://free.example.com/d/"+raw, nil)
		r.AddCookie(&http.Cookie{Name: "d1_visitor", Value: visitor})
		r.Header.Set("User-Agent", ticket.UserAgent)
		r.Header.Set("Range", "bytes=0-4")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	requireCode(t, request("HEAD"), 206)
	for _, scenario := range []string{"disabled owner", "changed audience"} {
		if scenario == "disabled owner" {
			if err := s.DB.Model(&u).Update("disabled", true).Error; err != nil {
				t.Fatal(err)
			}
		} else {
			if err := s.DB.Model(&u).Updates(map[string]any{"disabled": false, "pro_until": s.Now().Add(time.Hour)}).Error; err != nil {
				t.Fatal(err)
			}
		}
		for _, method := range []string{"GET", "HEAD"} {
			w := request(method)
			requireCode(t, w, 404)
			if w.Header().Get("Content-Disposition") != "" || w.Header().Get("Content-Range") != "" {
				t.Fatal("rejected old ticket exposed file metadata")
			}
		}
	}
	var after Ticket
	if err := s.DB.First(&after, "hash = ?", ticket.Hash).Error; err != nil {
		t.Fatal(err)
	}
	if after.Reserved || after.Charged || after.Ranges != "" {
		t.Fatal("rejected old ticket changed reservation or ranges")
	}
	for _, model := range []any{&Download{}, &Ledger{}, &Usage{}, &Attribution{}} {
		if countRows(t, s, model) != 0 {
			t.Fatalf("rejected old ticket changed %T", model)
		}
	}
}
