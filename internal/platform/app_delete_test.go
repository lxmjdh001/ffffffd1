package platform

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppDeletePermanentlyRemovesAppAndFileButPreservesHistory(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, Dollar)
	app := seedApp(t, s, u, 100)
	other := seedApp(t, s, u, 50)
	for _, a := range []Application{app, other} {
		if err := s.DB.Create(&AppPageDesign{AppID: a.ID, Design: defaultPageDesign(a)}).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.DB.Create(&AdConfig{AppID: a.ID, UserID: u.ID, APISecret: "test-secret"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	download := Download{ID: randomHex(16), TicketHash: randomHex(32), UserID: u.ID, AppID: app.ID, AppName: app.Name, CreatedAt: s.Now()}
	ledger := Ledger{ID: randomHex(16), UserID: u.ID, Reference: randomHex(16), Kind: "download", AmountMicros: -1000, BalanceMicros: u.BalanceMicros}
	if err := s.DB.Create(&download).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	cookie, csrf := sessionFor(t, s, u)
	w := call(t, s, "DELETE", "/api/apps/"+app.ID, nil, cookie, csrf)
	requireCode(t, w, 200)
	var result struct {
		OK                 bool `json:"ok"`
		FileCleanupPending bool `json:"file_cleanup_pending"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || !result.OK || result.FileCleanupPending {
		t.Fatal("successful permanent deletion response invalid", err)
	}
	var count int64
	if err := s.DB.Unscoped().Model(&Application{}).Where("id = ?", app.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("deleted app still exists, including soft-deleted rows", err)
	}
	if _, err := os.Stat(filepath.Join(s.Config.DataDir, "files", app.FileKey)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("installation package was not physically removed", err)
	}
	for _, model := range []any{&AppPageDesign{}, &AdConfig{}} {
		if err := s.DB.Model(model).Where("app_id = ?", app.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("deleted app configuration was retained", err)
		}
		if err := s.DB.Model(model).Where("app_id = ?", other.ID).Count(&count).Error; err != nil || count != 1 {
			t.Fatal("another app's configuration was affected", err)
		}
	}
	for _, path := range []string{"/api/apps/" + app.ID, "/a/" + app.Slug + "/info", "/a/" + app.Slug} {
		requireCode(t, call(t, s, "GET", path, nil, cookie, ""), 404)
	}
	if countRows(t, s, &Download{}) != 1 || countRows(t, s, &Ledger{}) != 1 {
		t.Fatal("permanent app deletion removed historical download or billing data")
	}
	if err := s.DB.First(&u, "id = ?", u.ID).Error; err != nil || u.BalanceMicros != Dollar {
		t.Fatal("permanent app deletion changed the user's balance", err)
	}
	if err := s.DB.Model(&Application{}).Where("user_id = ?", u.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("app slot was not released", err)
	}
	var used int64
	if err := s.DB.Model(&Application{}).Where("user_id = ?", u.ID).Select("COALESCE(SUM(size),0)").Scan(&used).Error; err != nil || used != other.Size {
		t.Fatal("storage quota was not released", err)
	}
	if err := s.DB.Model(&other).Update("slug", app.Slug).Error; err != nil {
		t.Fatal("permanently deleted share slug is still reserved", err)
	}
	if _, err := os.Stat(filepath.Join(s.Config.DataDir, "files", other.FileKey)); err != nil {
		t.Fatal("another app's package was removed", err)
	}
}

func TestAppDeleteRequiresOwnerAndCSRF(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	other := seedUser(t, s, 0)
	app := seedApp(t, s, u, 10)
	path := "/api/apps/" + app.ID
	requireCode(t, call(t, s, "DELETE", path, nil, nil, ""), 401)
	cookie, csrf := sessionFor(t, s, other)
	requireCode(t, call(t, s, "DELETE", path, nil, cookie, csrf), 404)
	cookie, csrf = sessionFor(t, s, u)
	requireCode(t, call(t, s, "DELETE", path, nil, cookie, "wrong-csrf"), 403)
	if countRows(t, s, &Application{}) != 1 {
		t.Fatal("unauthorized deletion changed an app")
	}
	requireCode(t, call(t, s, "DELETE", path, nil, cookie, csrf), 200)
	requireCode(t, call(t, s, "DELETE", path, nil, cookie, csrf), 404)
}

type failingAppDeleteStore struct{ BlobStore }

func (failingAppDeleteStore) Delete(context.Context, string) error {
	return errors.New("temporary storage outage")
}

func TestAppDeleteReportsFileCleanupFailureWithoutRetainingSoftDeletedRow(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	app := seedApp(t, s, u, 10)
	store := s.Blobs
	s.Blobs = failingAppDeleteStore{store}
	cookie, csrf := sessionFor(t, s, u)
	w := call(t, s, "DELETE", "/api/apps/"+app.ID, nil, cookie, csrf)
	requireCode(t, w, 200)
	var result map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || !result["ok"] || !result["file_cleanup_pending"] {
		t.Fatal("storage cleanup failure was hidden", err)
	}
	var count int64
	if err := s.DB.Unscoped().Model(&Application{}).Where("id = ?", app.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("storage failure left a soft-deleted application", err)
	}
	s.Blobs = store
	file := filepath.Join(s.Config.DataDir, "files", app.FileKey)
	old := s.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	s.cleanupOrphans(context.Background())
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("background orphan cleanup did not remove the package", err)
	}
}
