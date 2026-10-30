package platform

import (
	"crypto/hmac"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// The page issues a capability for one app's saved destination. It can cross
// browsers, so it deliberately does not depend on cookies, UA, or IP.
func (s *Server) externalClickToken(kind, id, target string) string {
	expires, nonce := strconv.FormatInt(s.Now().Add(24*time.Hour).Unix(), 10), randomHex(16)
	return expires + "." + nonce + "." + s.mac("external-click:"+kind+":"+id+":"+hashString(target)+":"+expires+":"+nonce)
}

func (s *Server) validExternalClickToken(kind, id, target, token string) bool {
	p := strings.Split(token, ".")
	if len(p) != 3 || len(p[1]) != 32 || len(p[2]) != 64 {
		return false
	}
	expires, e := strconv.ParseInt(p[0], 10, 64)
	if e != nil || expires <= s.Now().Unix() || expires > s.Now().Add(24*time.Hour).Unix() {
		return false
	}
	return hmac.Equal([]byte(p[2]), []byte(s.mac("external-click:"+kind+":"+id+":"+hashString(target)+":"+p[0]+":"+p[1])))
}

// The original page reports immediately even if the host refuses the intent.
// The relay retries the SAME event if navigation cancelled that report.
func (s *Server) chargeExternalClick(kind, id, owner, target, key, token string) (string, error) {
	if !hexKey.MatchString(key) || !s.validExternalClickToken(kind, id, target, token) {
		return "", problem(403, "操作凭证无效，请刷新页面")
	}
	eventID := s.mac("external-click-event:" + kind + ":" + id + ":" + key)
	e := s.DB.Transaction(func(tx *gorm.DB) error {
		u, e := lockUser(tx, owner)
		if e != nil {
			return e
		}
		x, e := getSettings(tx)
		if e != nil {
			return e
		}
		if !x.MeteredBilling {
			return nil
		}
		now := s.Now()
		event, e := reserveMetered(tx, &u, x, now, eventID, kind+"_external", id, hashString(target), now)
		if e != nil {
			return e
		}
		if event.SourceID != hashString(target) {
			return problem(409, "用量记录不匹配")
		}
		return settleMetered(tx, &u, &event, now, true)
	})
	return eventID, e
}

// A paid handoff back to this application's landing page continues its native
// install/download without charging the same action a second time.
func (s *Server) paidExternalReturn(kind, id, target, token string) bool {
	if !s.validExternalReturnToken(kind, id, target, token) {
		return false
	}
	parts := strings.Split(token, ".")
	var n int64
	return s.DB.Model(&MeteredEvent{}).Where("id = ? AND kind = ? AND resource_id = ? AND state = ?", parts[1], kind+"_external", id, "charged").Count(&n).Error == nil && n == 1
}
