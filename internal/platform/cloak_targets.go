package platform

import (
	"net/http"
	"strings"
)

type cloakAppChoice struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	DirectURL string `json:"direct_url,omitempty"`
	DetailURL string `json:"detail_url,omitempty"`
	EntryURL  string `json:"entry_url,omitempty"`
}

// Selectors expose only the owner's currently usable apps, without file or
// push credentials, and are not limited to the first page of the app list.
func (s *Server) cloakTargetChoices(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	var apps []Application
	var pwas []PWAApp
	if err := s.DB.Select("id", "name", "slug").Where("user_id = ? AND enabled = ? AND blocked = ?", u.ID, true, false).Order("created_at DESC, id").Find(&apps).Error; err != nil {
		fail(w, err)
		return
	}
	if err := s.DB.Select("id", "name", "slug").Where("user_id = ? AND enabled = ?", u.ID, true).Order("created_at DESC, id").Find(&pwas).Error; err != nil {
		fail(w, err)
		return
	}
	domains, err := s.downloadDomainsFor(u)
	if err != nil {
		fail(w, err)
		return
	}
	base := ""
	if len(domains) > 0 {
		base = domains[0].URL
	}
	appChoices, pwaChoices := []cloakAppChoice{}, []cloakAppChoice{}
	for _, app := range apps {
		choice := cloakAppChoice{ID: app.ID, Name: app.Name}
		if base != "" {
			choice.DirectURL, choice.DetailURL = base+"/a/"+app.Slug+"/direct", base+"/a/"+app.Slug+"/info"
		}
		appChoices = append(appChoices, choice)
	}
	for _, app := range pwas {
		entryBase := base
		if entryBase == "" {
			entryBase = strings.TrimRight(s.Config.PublicURL, "/")
		}
		pwaChoices = append(pwaChoices, cloakAppChoice{ID: app.ID, Name: app.Name, EntryURL: entryBase + "/p/" + app.Slug + "/"})
	}
	jsonOut(w, 200, map[string]any{"apps": appChoices, "pwas": pwaChoices})
}

// Resolve by owned entity on every visit. Deleted, paused or moderated targets
// fall back to the cloak's safe page instead of a stale saved destination.
func (s *Server) renderCloakApp(w http.ResponseWriter, r *http.Request, c Cloak, owner User, root Attribution) bool {
	if c.TargetType == "pwa" {
		var app PWAApp
		if err := s.DB.Select("id", "slug").First(&app, "id = ? AND user_id = ? AND enabled = ?", c.TargetID, owner.ID, true).Error; err != nil {
			return false
		}
		r.SetPathValue("slug", app.Slug)
		s.pwaInstallPage(w, r)
		return true
	}
	var app Application
	if err := s.DB.Select("id", "slug").First(&app, "id = ? AND user_id = ? AND enabled = ? AND blocked = ?", c.TargetID, owner.ID, true, false).Error; err != nil {
		return false
	}
	r = s.cloakAppAttributionRequest(r, root, app)
	r.SetPathValue("slug", app.Slug)
	if c.AppTargetMode == "info" {
		s.downloadPage(w, r)
	} else {
		s.directAppDownload(w, r)
	}
	return true
}
