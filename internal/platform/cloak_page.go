package platform

import (
	"bytes"
	_ "embed"
	"html/template"
	"net/http"
	"net/url"
)

//go:embed templates/cloak-not-found.html
var cloakNotFoundHTML string

var cloakNotFoundTemplate = template.Must(template.New("cloak-not-found").Parse(cloakNotFoundHTML))

// The fallback never includes the requested URL, rule, owner, or destination.
func (s *Server) cloakNotFound(w http.ResponseWriter, r *http.Request) {
	var data struct{ SupportURL string }
	if settings, err := getSettings(s.DB); err == nil {
		if email, err := normalizeEmail(settings.SupportEmail); err == nil {
			// Escape the address so a valid but unusual local part cannot add
			// mailto query parameters or fragments.
			data.SupportURL = "mailto:" + url.PathEscape(email)
		}
	}
	var body bytes.Buffer
	if err := cloakNotFoundTemplate.Execute(&body, data); err != nil {
		http.Error(w, "Page unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}
