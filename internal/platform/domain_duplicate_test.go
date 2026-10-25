package platform

import (
	"strings"
	"testing"
)

func TestDuplicateDomainReturnsFriendlyConflictBeforeQuota(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, owner)
	body := map[string]any{"hostname": "download.example.com", "remark": "test"}
	requireCode(t, call(t, s, "POST", "/api/domains", body, cookie, csrf), 201)
	s.DB.Model(&Settings{}).Where("id = ?", 1).Update("domain_limit", 1)
	body["hostname"] = "DOWNLOAD.EXAMPLE.COM."
	w := call(t, s, "POST", "/api/domains", body, cookie, csrf)
	requireCode(t, w, 409)
	if !strings.Contains(w.Body.String(), "该域名已被添加过,请更换") {
		t.Fatal(w.Body.String())
	}
	if countRows(t, s, &Domain{}) != 1 {
		t.Fatal("duplicate domain created")
	}
}
