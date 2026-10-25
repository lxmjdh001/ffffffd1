package platform

import (
	"crypto/hmac"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// One rolling window per owner, application kind, application and normalized IP.
// Raw visitor addresses are not retained in the billing guard.
type ReturnFlowViewCharge struct {
	Key             string `gorm:"primaryKey;size:64"`
	UserID          string `gorm:"size:32;index"`
	AppID           string `gorm:"size:32;index"`
	AppKind         string `gorm:"size:8"`
	ChargedAt       time.Time
	LedgerReference string `gorm:"size:128"`
}

func migrateReturnFlowBilling(tx *gorm.DB) error {
	var n int64
	if e := tx.Model(&Migration{}).Where("version = ?", 17).Count(&n).Error; e != nil {
		return e
	}
	if n != 0 {
		return nil
	}
	if e := tx.AutoMigrate(&ReturnFlowViewCharge{}); e != nil {
		return e
	}
	return tx.Create(&Migration{Version: 17, AppliedAt: time.Now().UTC()}).Error
}
func (s *Server) returnFlowIP(r *http.Request) string {
	ip := net.ParseIP(s.clientIP(r))
	if ip == nil {
		return ""
	}
	return ip.String()
}
func (s *Server) returnFlowViewToken(kind, id string, r *http.Request) string {
	expires := strconv.FormatInt(s.Now().Add(24*time.Hour).Unix(), 10)
	nonce := randomHex(16)
	payload := kind + ":" + id + ":" + requestHost(r) + ":" + s.returnFlowIP(r) + ":" + expires + ":" + nonce
	return expires + "." + nonce + "." + s.mac("return-flow-view:"+payload)
}
func (s *Server) validReturnFlowViewToken(kind, id, token string, r *http.Request) bool {
	p := strings.Split(token, ".")
	if len(p) != 3 || len(p[1]) != 32 || len(p[2]) != 64 {
		return false
	}
	expiry, e := strconv.ParseInt(p[0], 10, 64)
	if e != nil || expiry <= s.Now().Unix() || expiry > s.Now().Add(24*time.Hour).Unix() {
		return false
	}
	payload := kind + ":" + id + ":" + requestHost(r) + ":" + s.returnFlowIP(r) + ":" + p[0] + ":" + p[1]
	return hmac.Equal([]byte(p[2]), []byte(s.mac("return-flow-view:"+payload)))
}
func (s *Server) returnFlowView(w http.ResponseWriter, r *http.Request, kind string) {
	w.Header().Set("Cache-Control", "no-store")
	expected := "https://" + r.Host
	if !s.Config.Production() {
		expected = "http://" + r.Host
	}
	if r.Header.Get("Origin") != expected || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		fail(w, problem(403, "请求来源不受信任"))
		return
	}
	var id, owner string
	if kind == "apk" {
		a, e := s.publicApp(r)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		id, owner = a.ID, a.UserID
	} else {
		a, e := s.loadPublicPWA(r)
		if e != nil {
			pwaPublicFailure(w, r, e)
			return
		}
		id, owner = a.ID, a.UserID
	}
	if s.rejectBlockedUA(w, r, owner, r.UserAgent()) {
		return
	}
	var in struct {
		Token   string `json:"token"`
		EventID string `json:"event_id"`
		Action  string `json:"action"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	ip := s.returnFlowIP(r)
	if ip == "" || !s.validReturnFlowViewToken(kind, id, in.Token, r) {
		fail(w, problem(403, "展示凭证无效，请刷新页面"))
		return
	}
	if e := s.rate("return-flow-view:"+ip, 1200, time.Minute); e != nil {
		fail(w, e)
		return
	}
	confirmationRequired := false
	e := s.DB.Transaction(func(tx *gorm.DB) error {
		u, e := lockUser(tx, owner)
		if e != nil {
			return e
		}
		if u.Disabled {
			return problem(404, "应用不可用")
		}
		if kind == "apk" {
			var a Application
			if e := tx.First(&a, "id = ? AND user_id = ? AND enabled = ? AND blocked = ?", id, owner, true, false).Error; e != nil {
				return problem(404, "应用不可用")
			}
			var d AppPageDesign
			if e := tx.First(&d, "app_id = ?", id).Error; e != nil || d.Design.ReturnFlowConfig == nil || !d.Design.ReturnFlowConfig.Enabled {
				return problem(409, "回流页未开启")
			}
		} else {
			var a PWAApp
			if e := tx.First(&a, "id = ? AND user_id = ? AND enabled = ?", id, owner, true).Error; e != nil {
				return problem(404, "应用不可用")
			}
			if !storedReturnFlow(a.ReturnFlowJSON).Enabled {
				return problem(409, "回流页未开启")
			}
		}
		return nil // Free feature: authorization only, never reserve or charge.
	})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]bool{"allowed": true, "confirmation_required": confirmationRequired})
}
