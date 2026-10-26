package platform

import (
	"html"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestDIYEmergencyFallbackPreservesBindingsAndBilling(t *testing.T) {
	s := meteredServer(t, 0)
	owner := seedUser(t, s, Dollar)
	a := seedApp(t, s, owner, 128)
	page := publishDIYFixture(t, s, owner, createDIYFixture(t, s, owner, "Emergency fallback"))
	ref := &DIYLandingReference{PageID: page.ID, RevisionID: page.LatestPublishedRevisionID}
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := s.saveAPKLandingSource(tx, &a, "diy", ref); err != nil {
			return err
		}
		return tx.Save(&a).Error
	}); err != nil {
		t.Fatal(err)
	}
	base := "/a/" + a.Slug + "/"
	pageResponse := call(t, s, "GET", base+"info", nil, nil, "")
	requireCode(t, pageResponse, 200)
	before := countRows(t, s, &MeteredEvent{})
	s.Config.DisableDIYLanding = true
	w := call(t, s, "GET", base+"info", nil, nil, "")
	requireCode(t, w, 200)
	if strings.Contains(w.Body.String(), "id=\"d1-diy-content\"") || !strings.Contains(w.Body.String(), "id=\"d1-native-download\"") || !strings.Contains(html.UnescapeString(w.Body.String()), a.Name) {
		t.Fatal("fallback lost the trusted original application action")
	}
	if countRows(t, s, &MeteredEvent{}) != before {
		t.Fatal("fallback charged the same visitor again")
	}
	stored, err := diyBindingReference(s.DB, "apk", a.ID)
	if err != nil || stored == nil || !diySameReference(stored, ref) {
		t.Fatal("fallback destroyed the version binding")
	}
	requireCode(t, pushTaskCall(t, s, "GET", "/api/diy-landings", nil, owner, "", "correct"), 200)
	requireCode(t, pushTaskCall(t, s, "POST", "/api/diy-landings", diyTestInput("Disabled"), owner, "", "correct"), 503)
	other := seedApp(t, s, owner, 128)
	err = s.DB.Transaction(func(tx *gorm.DB) error { return s.saveAPKLandingSource(tx, &other, "diy", ref) })
	if err == nil {
		t.Fatal("disabled DIY allowed a new application binding")
	}
	stored, err = diyBindingReference(s.DB, "apk", other.ID)
	if err != nil || stored != nil {
		t.Fatal("failed binding left a partial update")
	}
	s.Config.DisableDIYLanding = false
	w = call(t, s, "GET", base+"info", nil, nil, "")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), "id=\"d1-diy-content\"") {
		t.Fatal("re-enabling did not restore the pinned page")
	}
}

func TestDIYMissingBindingUsesTrustedFallback(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, Dollar)
	a := seedApp(t, s, owner, 128)
	d := defaultPageDesign(a)
	d.Layout = "diy"
	d.ReturnFlowConfig = &ReturnFlowConfig{Enabled: true, TriggerMode: "back", ButtonAction: "download", Title: "Recovery", MediaAreaPercent: 50}
	if err := s.DB.Create(&AppPageDesign{AppID: a.ID, Design: d}).Error; err != nil {
		t.Fatal(err)
	}
	w := call(t, s, "GET", "/a/"+a.Slug+"/info", nil, nil, "")
	requireCode(t, w, 200)
	if !strings.Contains(w.Body.String(), "Recovery") || !strings.Contains(w.Body.String(), "d1-native-download") || strings.Contains(w.Body.String(), "id=\"d1-diy-content\"") {
		t.Fatal("missing version failed to preserve a functional download/return page")
	}
}
