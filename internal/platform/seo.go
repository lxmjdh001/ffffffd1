package platform

import (
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Only the public marketing homepage is advertised to search engines. Private
// console routes use hash fragments and are never separate sitemap URLs.
func (s *Server) seoHomeURL() (string, error) {
	origin := strings.TrimRight(s.Config.PublicURL, "/")
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.Opaque != "" || (u.Scheme != "https" && u.Scheme != "http") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid public SEO origin")
	}
	u.Path = "/"
	return u.String(), nil
}

func writeSEOFile(w http.ResponseWriter, r *http.Request, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	home, err := s.seoHomeURL()
	if err != nil {
		http.Error(w, "Search metadata is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	body := "User-agent: *\nAllow: /\nDisallow: /api/\nDisallow: /internal/\nDisallow: /healthz\nDisallow: /a/\nDisallow: /d/\nDisallow: /r/\nDisallow: /c/\nDisallow: /p/\n\nSitemap: " + home + "sitemap.xml\n"
	writeSEOFile(w, r, "text/plain; charset=utf-8", []byte(body))
}

func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
	home, err := s.seoHomeURL()
	if err != nil {
		http.Error(w, "Search metadata is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	// XML encoding keeps the configured URL safe without trusting request Host,
	// X-Forwarded-Host, query parameters, or a crawler-specific response path.
	document := struct {
		XMLName xml.Name `xml:"urlset"`
		XMLNS   string   `xml:"xmlns,attr"`
		URLs    []struct {
			Location string `xml:"loc"`
		} `xml:"url"`
	}{XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	document.URLs = append(document.URLs, struct {
		Location string `xml:"loc"`
	}{Location: home})
	body, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		http.Error(w, "Search metadata is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	writeSEOFile(w, r, "application/xml; charset=utf-8", append([]byte(xml.Header), append(body, '\n')...))
}
