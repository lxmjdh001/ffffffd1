package platform

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestSearchMetadataUsesConfiguredOriginAndRootOnly(t *testing.T) {
	s := &Server{Config: Config{Env: "production", PublicURL: "https://d1.example:8443"}}
	handler := s.Handler()
	for _, path := range []string{"/robots.txt", "/sitemap.xml"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, s.Config.PublicURL+path+"?url=https://untrusted.example/", nil)
			request.Header.Set("X-Forwarded-Host", "untrusted.example")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("GET status = %d: %s", response.Code, response.Body.String())
			}
			body := response.Body.String()
			if strings.Contains(body, "untrusted.example") || strings.Contains(body, "#") {
				t.Fatalf("search metadata contains a request URL or hash route: %s", body)
			}
			if response.Header().Get("Cache-Control") != "public, max-age=3600" {
				t.Fatalf("unexpected metadata cache policy: %v", response.Header())
			}
			if response.Header().Get("Content-Length") != strconv.Itoa(len(body)) {
				t.Fatalf("incorrect metadata byte length: %v", response.Header())
			}
			if path == "/robots.txt" {
				if response.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
					t.Fatalf("incorrect robots content type: %v", response.Header())
				}
				if !strings.Contains(body, "User-agent: *\nAllow: /\n") || !strings.Contains(body, "Sitemap: "+s.Config.PublicURL+"/sitemap.xml\n") {
					t.Fatalf("robots does not advertise the configured sitemap: %s", body)
				}
				for _, privatePath := range []string{"/api/", "/internal/", "/healthz", "/a/", "/d/", "/r/", "/c/", "/p/"} {
					if !strings.Contains(body, "Disallow: "+privatePath+"\n") {
						t.Errorf("robots does not exclude %s", privatePath)
					}
				}
			} else {
				if response.Header().Get("Content-Type") != "application/xml; charset=utf-8" {
					t.Fatalf("incorrect sitemap content type: %v", response.Header())
				}
				var document struct {
					XMLName xml.Name
					URLs    []struct {
						Location string `xml:"loc"`
					} `xml:"url"`
				}
				if err := xml.Unmarshal(response.Body.Bytes(), &document); err != nil {
					t.Fatal(err)
				}
				if document.XMLName.Local != "urlset" || document.XMLName.Space != "http://www.sitemaps.org/schemas/sitemap/0.9" || len(document.URLs) != 1 || document.URLs[0].Location != s.Config.PublicURL+"/" {
					t.Fatalf("sitemap must contain only the canonical root URL: %#v", document)
				}
			}

			head := httptest.NewRecorder()
			handler.ServeHTTP(head, httptest.NewRequest(http.MethodHead, s.Config.PublicURL+path, nil))
			if head.Code != response.Code || head.Body.Len() != 0 || head.Header().Get("Content-Type") != response.Header().Get("Content-Type") || head.Header().Get("Content-Length") != response.Header().Get("Content-Length") {
				t.Fatalf("HEAD must preserve metadata headers without a response body: status=%d body=%q headers=%v", head.Code, head.Body.String(), head.Header())
			}
		})
	}
}

func TestSearchMetadataHonorsHostRoleAndMethodRestrictions(t *testing.T) {
	for _, test := range []struct {
		name   string
		host   string
		role   string
		method string
		status int
	}{
		{name: "foreign host", host: "https://untrusted.example", role: "all", method: http.MethodGet, status: http.StatusNotFound},
		{name: "download service", host: "https://d1.example", role: "download", method: http.MethodGet, status: http.StatusNotFound},
		{name: "control service", host: "https://d1.example", role: "control", method: http.MethodGet, status: http.StatusOK},
		{name: "unsupported method", host: "https://d1.example", role: "all", method: http.MethodPost, status: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &Server{Config: Config{Env: "production", Role: test.role, PublicURL: "https://d1.example"}}
			for _, path := range []string{"/robots.txt", "/sitemap.xml"} {
				response := httptest.NewRecorder()
				s.Handler().ServeHTTP(response, httptest.NewRequest(test.method, test.host+path, nil))
				if response.Code != test.status {
					t.Errorf("%s status = %d, want %d", path, response.Code, test.status)
				}
			}
		})
	}
}

func TestSearchMetadataRejectsInvalidCanonicalOrigin(t *testing.T) {
	for _, origin := range []string{"", "ftp://d1.example", "https://user:secret@d1.example", "https://d1.example/path", "https://d1.example?path=/private", "https://d1.example#private", "https://d1.example\nSitemap: https://untrusted.example"} {
		s := &Server{Config: Config{PublicURL: origin}}
		if home, err := s.seoHomeURL(); err == nil {
			t.Errorf("invalid canonical origin %q produced %q", origin, home)
		}
	}
	s := &Server{Config: Config{PublicURL: "https://d1.example/path"}}
	for _, handler := range []http.HandlerFunc{s.robots, s.sitemap} {
		response := httptest.NewRecorder()
		handler(response, httptest.NewRequest(http.MethodGet, "https://d1.example/sitemap.xml", nil))
		if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "/path") {
			t.Fatalf("invalid origin should fail without advertising a URL: status=%d body=%s", response.Code, response.Body.String())
		}
	}
}
