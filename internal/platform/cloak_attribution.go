package platform

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const routedCloakNamespace = "routed-cloak-attribution"
const routedCloakCookieNamespace = "routed-cloak-cookie"

// A cloak capability only conveys campaign attribution. It never grants entry
// or bypasses the cloak's own rules, IP lists, or destination ownership checks.
func (s *Server) routedCloakTarget(r *http.Request, owner, raw string) (Cloak, *url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Hostname() == "" || u.Scheme != "https" && u.Scheme != "http" {
		return Cloak{}, nil, nil
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "c" {
		return Cloak{}, nil, nil
	}
	var cloak Cloak
	if err = s.DB.First(&cloak, "slug = ? AND user_id = ?", parts[1], owner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Cloak{}, nil, nil
		}
		return Cloak{}, nil, err
	}
	if cloak.TargetType != "app" && cloak.TargetType != "traffic_pool" {
		return Cloak{}, nil, nil
	}
	trusted, err := s.trustedRoutedHost(r, owner, u)
	if err != nil {
		return Cloak{}, nil, err
	}
	if !trusted {
		return Cloak{}, nil, nil
	}
	return cloak, u, nil
}

func (s *Server) routedCapability(root Attribution, namespace, target, host string, ttl time.Duration) string {
	expires := min(s.Now().Add(ttl).Unix(), root.ExpiresAt.Unix())
	if root.ID == "" || expires <= s.Now().Unix() {
		return ""
	}
	stamp := strconv.FormatInt(expires, 10)
	return root.ID + "." + stamp + "." + s.mac(namespace+":"+root.ID+":"+target+":"+strings.ToLower(host)+":"+stamp)
}

// Forward only recognized click identifiers; arbitrary source-page parameters
// or credentials must not be copied into the selected destination URL.
func routedClickParams(root Attribution) map[string]string {
	values := map[string]string{"fbclid": root.FBClid, "ttclid": root.TTClid}
	if u, err := url.Parse(root.PageURL); err == nil {
		for _, key := range []string{"gclid", "bbg"} {
			values[key] = cleanText(u.Query().Get(key), 1024)
		}
	}
	return values
}

func cloakAttributionCookie(c Cloak) string { return "d1_cloak_attr_" + c.Slug }

func (s *Server) cloakCapabilityParts(raw, namespace string, r *http.Request, c Cloak) ([]string, int64, bool) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || len(parts[0]) != 32 || !hexKey.MatchString(parts[0]) {
		return nil, 0, false
	}
	expires, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, 0, false
	}
	expected := s.mac(namespace + ":" + parts[0] + ":" + c.ID + ":" + strings.ToLower(r.Host) + ":" + parts[1])
	return parts, expires, subtle.ConstantTimeCompare([]byte(expected), []byte(parts[2])) == 1
}

func (s *Server) readCloakRoutedAttribution(w http.ResponseWriter, r *http.Request, c Cloak) (Attribution, error) {
	raw := r.URL.Query().Get(routedAttributionParam)
	fromQuery := r.URL.Query().Has(routedAttributionParam)
	expectedRoot := ""
	var parts []string
	var expires int64
	if fromQuery {
		var valid bool
		parts, expires, valid = s.cloakCapabilityParts(raw, routedCloakNamespace, r, c)
		if !valid || expires > s.Now().Add(15*time.Minute).Unix() {
			return Attribution{}, nil
		}
		if expires <= s.Now().Unix() {
			// A reload may keep the expired short-lived URL. Only a previously
			// established cookie for that very same campaign can continue it.
			expectedRoot, fromQuery = parts[0], false
		}
	}
	if !fromQuery {
		cookie, err := r.Cookie(cloakAttributionCookie(c))
		if err != nil {
			return Attribution{}, nil
		}
		var valid bool
		parts, expires, valid = s.cloakCapabilityParts(cookie.Value, routedCloakCookieNamespace, r, c)
		if !valid || expires <= s.Now().Unix() || expires > s.Now().Add(7*24*time.Hour).Unix() || expectedRoot != "" && parts[0] != expectedRoot {
			return Attribution{}, nil
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	var root Attribution
	if err := s.DB.WithContext(ctx).First(&root, "id = ? AND source_attribution_id = ? AND consent = ? AND expires_at > ?", parts[0], "", true, s.Now()).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Attribution{}, nil
		}
		return Attribution{}, err
	}
	var source Application
	if err := s.DB.WithContext(ctx).Select("id").First(&source, "id = ? AND user_id = ?", root.AppID, c.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Attribution{}, nil
		}
		return Attribution{}, err
	}
	if !fromQuery {
		for key, value := range routedClickParams(root) {
			if incoming := r.URL.Query().Get(key); incoming != "" && incoming != value {
				http.SetCookie(w, &http.Cookie{Name: cloakAttributionCookie(c), Path: "/c/" + c.Slug, MaxAge: -1, HttpOnly: true, Secure: s.Config.Production(), SameSite: http.SameSiteLaxMode})
				return Attribution{}, nil
			}
		}
	}
	return root, nil
}

func (s *Server) rememberCloakAttribution(w http.ResponseWriter, r *http.Request, c Cloak, root Attribution) {
	token := s.routedCapability(root, routedCloakCookieNamespace, c.ID, r.Host, 7*24*time.Hour)
	if token == "" {
		return
	}
	expires := min(s.Now().Add(7*24*time.Hour).Unix(), root.ExpiresAt.Unix())
	http.SetCookie(w, &http.Cookie{Name: cloakAttributionCookie(c), Value: token, Path: "/c/" + c.Slug, MaxAge: int(expires - s.Now().Unix()), Expires: time.Unix(expires, 0), HttpOnly: true, Secure: s.Config.Production(), SameSite: http.SameSiteLaxMode})
}

func withoutRoutedAttribution(r *http.Request) *http.Request {
	if !r.URL.Query().Has(routedAttributionParam) {
		return r
	}
	copy := r.Clone(r.Context())
	query := copy.URL.Query()
	query.Del(routedAttributionParam)
	copy.URL.RawQuery = query.Encode()
	return copy
}

func (s *Server) cloakAppAttributionRequest(r *http.Request, root Attribution, app Application) *http.Request {
	if root.ID == "" {
		return r
	}
	token := s.routedCapability(root, "routed-attribution", app.ID, r.Host, 15*time.Minute)
	if token == "" {
		return r
	}
	copy := r.Clone(r.Context())
	query := copy.URL.Query()
	query.Set(routedAttributionParam, token)
	copy.URL.RawQuery = query.Encode()
	return copy
}
