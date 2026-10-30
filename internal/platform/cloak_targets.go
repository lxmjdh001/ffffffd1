package platform

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"gorm.io/gorm"
)

// Keep a cloak entry in place when its pool selects an owned native landing.
// Selection, risk filtering and later routing buttons retain the pool's rules.
// External or unavailable targets keep their existing redirect behavior.
func (s *Server) renderCloakPoolTarget(w http.ResponseWriter, r *http.Request, owner User, root Attribution, raw string) (bool, error) {
	app, target, err := s.routedAPKTarget(r, Application{UserID: owner.ID}, raw)
	if err != nil {
		return false, err
	}
	c := Cloak{TargetType: "app", TargetID: app.ID, AppTargetMode: "direct"}
	if app.ID != "" {
		parts := strings.Split(strings.Trim(target.Path, "/"), "/")
		if len(parts) == 3 {
			c.AppTargetMode = parts[2]
		} else if app.DownloadMode == "info" {
			c.AppTargetMode = "info"
		}
	} else {
		target, err = url.Parse(raw)
		if err != nil || target.User != nil || target.Hostname() == "" || target.Scheme != "https" && target.Scheme != "http" {
			return false, nil
		}
		parts := strings.Split(strings.Trim(target.Path, "/"), "/")
		if len(parts) != 2 || parts[0] != "p" {
			return false, nil
		}
		trusted, err := s.trustedRoutedHost(r, owner.ID, target)
		if err != nil || !trusted {
			return false, err
		}
		var pwa PWAApp
		if err := s.DB.Select("id").First(&pwa, "slug = ? AND user_id = ? AND enabled = ?", parts[1], owner.ID, true).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil
			}
			return false, err
		}
		c.TargetType, c.TargetID = "pwa", pwa.ID
	}
	request := r.Clone(r.Context())
	u := *r.URL
	request.URL = &u
	// Use the merged target parameters, while retaining the incoming /c/ path
	// and host for document URLs, forms, manifests and download tickets.
	request.URL.RawQuery = target.RawQuery
	return s.renderCloakApp(w, request, c, owner, root), nil
}

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
