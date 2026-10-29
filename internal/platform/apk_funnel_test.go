package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestAPKLandingPaidFunnelDeduplicatesHTTPAndRecovery(t *testing.T) {
	for _, layout := range []string{"google_play", "custom"} {
		t.Run(layout, func(t *testing.T) {
			s := testServer(t)
			u := seedUser(t, s, Dollar)
			a := seedApp(t, s, u, 128)
			if e := s.DB.Model(&a).Updates(map[string]any{"platform": "Android", "file_name": "fixture.apk", "download_mode": "info"}).Error; e != nil {
				t.Fatal(e)
			}
			if e := s.DB.Model(&Plan{}).Where("id = ?", "free").Update("download_limit", 0).Error; e != nil {
				t.Fatal(e)
			}
			cookie, csrf := sessionFor(t, s, u)
			d := defaultPageDesign(a)
			d.Layout = layout
			d.Language = "en"
			d.Title = "Saved APK title"
			d.ButtonText = "Download tested APK"
			d.DisplayRating = "4.7"
			d.DisplayDownloadCount = "20K+"
			d.Description = "Saved application description"
			d.Landing = &APKLandingOptions{AgeRating: "18", ButtonPosition: "center", ButtonBgColor: "#123456", ButtonTextColor: "#FFFFFF", Reviews: []PWALandingReview{{Nickname: "Tester", Comment: "Saved review", Rating: 4, CommentLanguage: "en-US"}}}
			rf := defaultReturnFlowConfig()
			rf.Enabled = true
			rf.TriggerMode = "both"
			rf.ShowOnce = false
			rf.ButtonAction = "download"
			rf.Title = "Saved return offer"
			rf.ButtonText = "Download from return page"
			d.ReturnFlowConfig = &rf
			api := "/api/apps/" + a.ID + "/page-design"
			base := "/a/" + a.Slug
			requireCode(t, call(t, s, "PUT", api, d, cookie, csrf), 200)
			read := call(t, s, "GET", api, nil, cookie, "")
			requireCode(t, read, 200)
			for _, text := range []string{d.Title, d.ButtonText, "Saved review", rf.Title, `"buttonAction":"download"`, `"button_position":"center"`} {
				if !strings.Contains(read.Body.String(), text) {
					t.Fatalf("save/reopen lost %s", text)
				}
			}
			page := directRequest(t, s, "GET", base+"/info?fbclid=apk-funnel", nil)
			requireCode(t, page, 200)
			body := html.UnescapeString(page.Body.String())
			if !strings.Contains(body, rf.Title) || !strings.Contains(body, "d1-native-download") {
				t.Fatal("public page did not apply return flow/native download")
			}
			fields := url.Values{}
			for _, name := range []string{"request_key", "attribution_token"} {
				m := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(body)
				if len(m) != 2 {
					t.Fatal("missing original download field", name)
				}
				fields.Set(name, m[1])
			}
			cookies := page.Result().Cookies()
			visitor := ""
			for _, c := range cookies {
				if c.Name == "d1_visitor" {
					visitor = c.Value
				}
			}
			if visitor == "" {
				t.Fatal("visitor missing")
			}
			config := directRequest(t, s, "GET", base+"/landing-config.js", cookies)
			requireCode(t, config, 200)
			for _, text := range []string{d.Title, d.ButtonText, "4.7", rf.Title, "Saved review"} {
				if !strings.Contains(config.Body.String(), text) {
					t.Fatal("public config lost", text)
				}
			}
			assertBilling := func(downloads, spent, held int64) {
				t.Helper()
				var owner User
				var app Application
				s.DB.First(&owner, "id = ?", u.ID)
				s.DB.First(&app, "id = ?", a.ID)
				if countRows(t, s, &Download{}) != downloads || countRows(t, s, &Ledger{}) != downloads || owner.BalanceMicros != Dollar-spent || owner.HeldMicros != held || app.Downloads != downloads {
					t.Fatalf("accounting mismatch: downloads=%d ledgers=%d balance=%d held=%d app_downloads=%d", countRows(t, s, &Download{}), countRows(t, s, &Ledger{}), owner.BalanceMicros, owner.HeldMicros, app.Downloads)
				}
			}
			assertBilling(0, 0, 0)
			proof := regexp.MustCompile(`data-return-flow-token="([^"]+)"`).FindStringSubmatch(body)
			if len(proof) != 2 {
				t.Fatal("return display proof missing")
			}
			for i := 0; i < 8; i++ {
				requireCode(t, returnBillingCall(s, base+"/return-flow/view", proof[1], "203.0.113.25", s.Config.PublicURL), 200)
			}
			assertBilling(0, 0, 0)
			handler := s.Handler()
			post := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "http://localhost:8088"+base+"/download", strings.NewReader(fields.Encode()))
				r.RemoteAddr = "203.0.113.25:1234"
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.Header.Set("Accept", "application/json")
				r.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 14; Mobile) Chrome/123")
				for _, c := range cookies {
					r.AddCookie(c)
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				return w
			}
			first := post()
			requireCode(t, first, 200)
			var answer map[string]string
			if e := json.Unmarshal(first.Body.Bytes(), &answer); e != nil {
				t.Fatal(e)
			}
			raw := strings.TrimPrefix(answer["download_url"], "/d/")
			if len(raw) != 64 {
				t.Fatal("invalid download URL")
			}
			var wg sync.WaitGroup
			for i := 0; i < 24; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					w := post()
					if w.Code != 200 || w.Body.String() != first.Body.String() {
						t.Errorf("repeated start diverged: %d %s", w.Code, w.Body.String())
					}
				}()
			}
			wg.Wait()
			if countRows(t, s, &Ticket{}) != 1 {
				t.Fatal("same landing request created duplicate tickets")
			}
			assertBilling(0, 0, 0)
			requireCode(t, download(t, s, raw, visitor, "HEAD", ""), 200)
			requireCode(t, download(t, s, raw, visitor, "GET", "bytes=900-999"), 416)
			assertBilling(0, 0, 0)
			for i := 0; i < 4; i++ {
				requireCode(t, download(t, s, raw, visitor, "GET", "bytes=0-15"), 206)
			}
			assertBilling(0, 0, 3000)
			for i := 0; i < 16; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					rangeHeader := ""
					want := 200
					if i%2 == 0 {
						rangeHeader = "bytes=16-127"
						want = 206
					}
					w := download(t, s, raw, visitor, "GET", rangeHeader)
					if w.Code != want {
						t.Errorf("concurrent transfer: %d", w.Code)
					}
				}(i)
			}
			wg.Wait()
			assertBilling(1, 3000, 0)
			for i := 0; i < 3; i++ {
				requireCode(t, download(t, s, raw, visitor, "GET", ""), 200)
				requireCode(t, post(), 200)
			}
			assertBilling(1, 3000, 0)
			var ticket Ticket
			if e := s.DB.First(&ticket, "hash = ?", hashString(raw)).Error; e != nil {
				t.Fatal(e)
			}
			if !ticket.Charged || ticket.Free || ticket.PriceMicros != 3000 {
				t.Fatal("paid download fixture was not charged")
			}
			if _, e := s.writeReceipt(ticket, 0, a.Size-1); e != nil {
				t.Fatal(e)
			}
			s.replayCompletions(context.Background())
			s.replayCompletions(context.Background())
			drainDownloadAds(s)
			assertBilling(1, 3000, 0)
			journal, e := os.ReadDir(filepath.Join(s.Config.DataDir, "completions"))
			if e != nil || len(journal) != 0 {
				t.Fatalf("replayed journal not cleared: %v", e)
			}
			if countRows(t, s, &AccessVisit{}) != 1 {
				t.Fatal("download retries or return-flow requests inflated page visits")
			}
			var record Download
			s.DB.First(&record)
			if record.AmountMicros != 3000 || record.Bytes != a.Size || record.Device != "手机" || record.OS != "Android" || record.CompletedAt.IsZero() {
				t.Fatalf("download metadata incorrect: %+v", record)
			}
			var usage Usage
			s.DB.First(&usage)
			if usage.PaidUsed != 1 || usage.SpentMicros != 3000 {
				t.Fatalf("usage duplicated: %+v", usage)
			}
			t.Log(fmt.Sprintf("%s: save/reopen/public configuration, 25 starts, repeated partial ranges, 16 concurrent completions, retries and journal replay settled once; one return display charge $0.01 + one download charge $0.003; balance $1 -> $0.987, held $0", layout))
		})
	}
}
