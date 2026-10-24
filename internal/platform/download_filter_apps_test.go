package platform

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestDownloadFilterAppsExcludeDeletedAppsWithoutChangingHistory(t *testing.T) {
	s := testServer(t)
	u, other := seedUser(t, s, 0), seedUser(t, s, 0)
	cookie, _ := sessionFor(t, s, u)
	requireCode(t, call(t, s, "GET", "/api/downloads/apps", nil, nil, ""), 401)
	var current []Application
	for i := 0; i < 105; i++ {
		current = append(current, Application{ID: fmt.Sprintf("picker-%03d", i), UserID: u.ID, Name: fmt.Sprintf("应用 %03d", i), Slug: fmt.Sprintf("picker-%03d", i)})
	}
	if err := s.DB.Create(&current).Error; err != nil {
		t.Fatal(err)
	}
	foreign := Application{ID: "foreign", UserID: other.ID, Name: "其他客户的应用", Slug: "foreign-app"}
	if err := s.DB.Create(&foreign).Error; err != nil {
		t.Fatal(err)
	}
	// Neither legacy soft-deleted rows nor hard-deleted historical apps belong in the picker.
	legacy := Application{ID: "legacy", UserID: u.ID, Name: "旧字段名称", Slug: "legacy-app"}
	if err := s.DB.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Delete(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	old := s.Now().Add(-90 * 24 * time.Hour)
	for i, row := range []struct {
		owner, app, name string
		at               time.Time
	}{
		{u.ID, current[0].ID, "改名前的应用名称", old},
		{u.ID, "hard-deleted", "最早的名称", old},
		{u.ID, "hard-deleted", "删除前的名称", old.Add(time.Hour)},
		{u.ID, "legacy", "历史快照名称", old},
		{other.ID, "other-deleted", "不应看到的历史应用", old},
		{other.ID, "hard-deleted", "其他客户同 ID 的记录", old.Add(2 * time.Hour)},
	} {
		d := Download{ID: fmt.Sprintf("picker-record-%d", i), TicketHash: randomHex(32), UserID: row.owner, AppID: row.app, AppName: row.name, CreatedAt: row.at}
		if err := s.DB.Create(&d).Error; err != nil {
			t.Fatal(err)
		}
	}
	w := call(t, s, "GET", "/api/downloads/apps", nil, cookie, "")
	requireCode(t, w, 200)
	var out struct {
		Data []downloadFilterApp `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 105 {
		t.Fatalf("application picker truncated or leaked rows: %+v", out.Data)
	}
	byID := map[string]downloadFilterApp{}
	for _, app := range out.Data {
		byID[app.ID] = app
	}
	if len(byID) != len(out.Data) {
		t.Fatal("duplicate application options")
	}
	if byID[current[0].ID].Name != current[0].Name {
		t.Fatal("current name was replaced by an old download snapshot")
	}
	if _, ok := byID[current[104].ID]; !ok {
		t.Fatal("apps beyond the first 100 are missing")
	}
	for _, id := range []string{"foreign", "other-deleted", "hard-deleted", "legacy"} {
		if _, exists := byID[id]; exists {
			t.Fatal("deleted or cross-account option exposed")
		}
	}
	var historyCount int64
	if err := s.DB.Model(&Download{}).Where("user_id = ?", u.ID).Count(&historyCount).Error; err != nil || historyCount != 4 {
		t.Fatal("picker changed historical download records")
	}
	empty := seedUser(t, s, 0)
	emptyCookie, _ := sessionFor(t, s, empty)
	w = call(t, s, "GET", "/api/downloads/apps", nil, emptyCookie, "")
	requireCode(t, w, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Data == nil || len(out.Data) != 0 {
		t.Fatal("empty account should get an empty list")
	}
}

func TestDeletedApplicationCannotBeFilteredButAllHistoryRemains(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	cookie, csrf := sessionFor(t, s, u)
	a := seedApp(t, s, u, 10)
	now := s.Now()
	d := Download{ID: randomHex(16), TicketHash: randomHex(32), UserID: u.ID, AppID: a.ID, AppName: a.Name, CreatedAt: now, StartedAt: now, CompletedAt: now}
	if err := s.DB.Create(&d).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, call(t, s, "DELETE", "/api/apps/"+a.ID, nil, cookie, csrf), 200)
	for _, tc := range []struct {
		path  string
		total int
	}{{"/api/downloads", 1}, {"/api/downloads?app_id=" + a.ID, 0}} {
		w := call(t, s, "GET", tc.path, nil, cookie, "")
		requireCode(t, w, 200)
		var out struct {
			Total int `json:"total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Total != tc.total {
			t.Fatalf("incorrect history/filter result: %s", w.Body.String())
		}
	}
}
