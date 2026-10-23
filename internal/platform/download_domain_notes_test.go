package platform

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAvailableDomainPermissionsAndPersonalNotes(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	other := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	x, err := getSettings(s.DB)
	if err != nil {
		t.Fatal(err)
	}
	x.DownloadDomainRules = []DownloadDomainRule{{URL: "https://all.example.com", Audience: "all", Remark: "默认备注"}, {URL: "https://paid.example.com", Audience: "paid"}}
	if err := s.DB.Save(&x).Error; err != nil {
		t.Fatal(err)
	}
	now := s.Now()
	old := now.Add(-49 * time.Hour)
	for _, d := range []Domain{{ID: "mine", UserID: u.ID, Hostname: "mine.example.com", VerifiedAt: &now}, {ID: "pending", UserID: u.ID, Hostname: "pending.example.com"}, {ID: "old", UserID: u.ID, Hostname: "old.example.com", VerifiedAt: &old}, {ID: "other", UserID: other.ID, Hostname: "other.example.com", VerifiedAt: &now}} {
		if err := s.DB.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	list := func() []availableDomain {
		t.Helper()
		w := call(t, s, "GET", "/api/download-domains", nil, cookie, "")
		requireCode(t, w, 200)
		var out struct {
			Data []availableDomain `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Data
	}
	ds := list()
	if len(ds) != 2 || ds[0].URL != "https://all.example.com" || ds[1].URL != "https://mine.example.com" {
		t.Fatalf("incorrect free domain list: %+v", ds)
	}
	requireCode(t, call(t, s, "PATCH", "/api/download-domains/note", map[string]string{"url": "https://all.example.com", "remark": "个人投放 A"}, cookie, csrf), 200)
	if list()[0].Remark != "个人投放 A" {
		t.Fatal("personal note not returned")
	}
	saved, _ := getSettings(s.DB)
	if saved.DownloadDomainRules[0].Remark != "默认备注" {
		t.Fatal("personal note overwrote platform configuration")
	}
	requireCode(t, call(t, s, "PATCH", "/api/download-domains/note", map[string]string{"url": "https://paid.example.com", "remark": "forbidden"}, cookie, csrf), 403)
	requireCode(t, call(t, s, "PATCH", "/api/download-domains/note", map[string]string{"url": "https://mine.example.com", "remark": "自有域名"}, cookie, csrf), 200)
	requireCode(t, call(t, s, "PATCH", "/api/domains/other", map[string]string{"remark": "forbidden"}, cookie, csrf), 404)
	if err := s.DB.Model(&u).Update("pro_until", now.Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if len(list()) != 3 {
		t.Fatal("paid member did not see all permitted domains")
	}
	// A paid-only public hostname must also enforce the same audience on downloads.
	if x.managedDownloadHosts(true)["paid.example.com"] != "pro" {
		t.Fatal("paid audience routing differs from domain picker")
	}
}
