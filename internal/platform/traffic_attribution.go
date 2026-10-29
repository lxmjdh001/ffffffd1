package platform

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const routedAttributionParam = "d1_route"

func migrateTrafficAttribution(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&Migration{}).Where("version = ?", 30).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	if err := tx.AutoMigrate(&Attribution{}, &Conversion{}); err != nil {
		return err
	}
	return tx.Create(&Migration{Version: 30, AppliedAt: time.Now().UTC()}).Error
}

// Only hand off to an enabled, owned APK served by this D1 installation. Raw
// external URLs and other owners' apps never receive an attribution capability.
func (s *Server) routedAPKTarget(r *http.Request, source Application, raw string) (Application, *url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return Application{}, nil, nil
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] != "a" || len(parts) == 3 && parts[2] != "info" && parts[2] != "direct" {
		return Application{}, nil, nil
	}
	var target Application
	err = s.DB.First(&target, "slug = ? AND user_id = ? AND enabled = ? AND blocked = ?", parts[1], source.UserID, true, false).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Application{}, nil, nil
	}
	if err != nil {
		return Application{}, nil, err
	}
	host := strings.ToLower(u.Hostname())
	trusted := strings.EqualFold(u.Host, r.Host)
	if public, e := url.Parse(s.Config.PublicURL); e == nil && strings.EqualFold(public.Host, u.Host) {
		trusted = true
	}
	if !trusted && (u.Port() == "" || u.Scheme == "https" && u.Port() == "443" || u.Scheme == "http" && u.Port() == "80") {
		settings, e := getSettings(s.DB)
		if e != nil {
			return Application{}, nil, e
		}
		_, trusted = settings.managedDownloadHosts(true)[host]
		if !trusted {
			var count int64
			if e = s.DB.Model(&Domain{}).Where("hostname = ? AND user_id = ? AND verified_at > ?", host, source.UserID, s.Now().Add(-48*time.Hour)).Count(&count).Error; e != nil {
				return Application{}, nil, e
			}
			trusted = count > 0
		}
	}
	if !trusted {
		return Application{}, nil, nil
	}
	return target, u, nil
}

func (s *Server) routedAttributionURL(r *http.Request, source Application, attr Attribution, raw string) (string, error) {
	if attr.ID == "" {
		return raw, nil
	}
	if attr.SourceAttributionID != "" {
		var root Attribution
		err := s.DB.First(&root, "id = ? AND source_attribution_id = ? AND expires_at > ?", attr.SourceAttributionID, "", s.Now()).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return raw, nil
		}
		if err != nil {
			return "", err
		}
		attr = root
	}
	if !attr.Consent {
		return raw, nil
	}
	var config AdConfig
	if err := s.DB.First(&config, "app_id = ? AND user_id = ?", attr.AppID, source.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return raw, nil
		}
		return "", err
	}
	if !(config.FacebookEnabled && attr.FBC != "" || config.TikTokEnabled && attr.TTClid != "") {
		return raw, nil
	}
	var origin Application
	if err := s.DB.First(&origin, "id = ? AND user_id = ?", attr.AppID, source.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return raw, nil
		}
		return "", err
	}
	target, u, err := s.routedAPKTarget(r, source, raw)
	if err != nil {
		return "", err
	}
	if u == nil {
		return raw, nil
	}
	expires := min(s.Now().Add(15*time.Minute).Unix(), attr.ExpiresAt.Unix())
	if expires <= s.Now().Unix() {
		return raw, nil
	}
	stamp := strconv.FormatInt(expires, 10)
	mac := s.mac("routed-attribution:" + attr.ID + ":" + target.ID + ":" + strings.ToLower(u.Host) + ":" + stamp)
	q := u.Query()
	q.Set(routedAttributionParam, attr.ID+"."+stamp+"."+mac)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s *Server) prepareRoutedAttribution(w http.ResponseWriter, r *http.Request, target Application, visitor string) (Attribution, bool, error) {
	parts := strings.Split(r.URL.Query().Get(routedAttributionParam), ".")
	if len(parts) != 3 || len(parts[0]) != 32 || !hexKey.MatchString(parts[0]) {
		return Attribution{}, false, nil
	}
	stamp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || stamp <= s.Now().Unix() || stamp > s.Now().Add(15*time.Minute).Unix() {
		return Attribution{}, false, nil
	}
	expected := s.mac("routed-attribution:" + parts[0] + ":" + target.ID + ":" + strings.ToLower(r.Host) + ":" + parts[1])
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[2])) != 1 {
		return Attribution{}, false, nil
	}
	var root Attribution
	if err = s.DB.First(&root, "id = ? AND source_attribution_id = ? AND expires_at > ?", parts[0], "", s.Now()).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Attribution{}, false, nil
		}
		return Attribution{}, false, err
	}
	var source Application
	if err = s.DB.First(&source, "id = ? AND user_id = ?", root.AppID, target.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Attribution{}, false, nil
		}
		return Attribution{}, false, err
	}
	// Even a route back to the source APK gets a derived record: it marks this
	// as routed traffic, whose advertising trigger is download start. The root
	// conversion identity remains the same across every destination and domain.
	attr := root
	attr.ID = hashString("routed-attribution:" + root.ID + ":" + target.ID + ":" + s.mac(visitor))[:32]
	attr.AppID, attr.VisitorHash, attr.SourceAttributionID = target.ID, s.mac(visitor), root.ID
	if err = s.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&attr).Error; err != nil {
		return Attribution{}, false, err
	}
	if err = s.DB.First(&attr, "id = ?", attr.ID).Error; err != nil {
		return Attribution{}, false, err
	}
	token := s.attributionToken(attr)
	http.SetCookie(w, &http.Cookie{Name: "d1_attr_" + target.Slug, Value: token, Path: "/a/" + target.Slug, HttpOnly: true, Secure: s.Config.Production(), SameSite: http.SameSiteLaxMode, MaxAge: 7 * 86400})
	return attr, true, nil
}
