package platform

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type impersonationInfo struct {
	AdministratorID   string     `json:"administrator_id"`
	AdministratorName string     `json:"administrator_name"`
	MemberID          string     `json:"member_id"`
	MemberName        string     `json:"member_name"`
	ExpiresAt         *time.Time `json:"expires_at"`
	Available         bool       `json:"available"`
}

func (s *Server) impersonatedIdentity(session Session, administrator User) (User, impersonationInfo, error) {
	if administrator.Role != "admin" {
		return User{}, impersonationInfo{}, problem(401, "管理员会话已失效，请重新登录")
	}
	var member User
	err := s.DB.First(&member, "id = ?", session.ImpersonatedUserID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return User{}, impersonationInfo{}, err
	}
	available := err == nil && !member.Disabled && member.Role == "member" && session.ImpersonationExpiresAt != nil && session.ImpersonationExpiresAt.After(s.Now())
	if err != nil {
		member = User{ID: session.ImpersonatedUserID, Name: session.ImpersonatedUserID}
	}
	// A changed target role must never turn a support scope into administrator access.
	member.Role = "member"
	return member, impersonationInfo{AdministratorID: administrator.ID, AdministratorName: administrator.Name, MemberID: member.ID, MemberName: member.Name, ExpiresAt: session.ImpersonationExpiresAt, Available: available}, nil
}

func (s *Server) auditImpersonationRequest(r *http.Request, administratorID, memberID string) error {
	detail, _ := json.Marshal(map[string]any{"method": r.Method, "path": r.URL.Path, "ip": s.clientIP(r), "attempted": true})
	// Record the operator before executing a mutation. Never log request bodies,
	// passwords, CSRF values, access tokens or download credentials.
	return recordAudit(s.DB, administratorID, "support.request", memberID, string(detail))
}

func (s *Server) startImpersonation(w http.ResponseWriter, r *http.Request) {
	original := r.Context().Value(sessionKey).(Session)
	if original.ImpersonatedUserID != "" {
		fail(w, problem(403, "请先返回管理员再进入其他会员后台"))
		return
	}
	if err := s.rate("impersonation:"+userOf(r).ID, 30, time.Minute); err != nil {
		fail(w, err)
		return
	}
	var session Session
	var member User
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, "hash = ? AND expires_at > ?", original.Hash, s.Now()).Error; err != nil {
			return problem(401, "登录已过期，请重新登录")
		}
		if session.CSRF != original.CSRF || session.ImpersonatedUserID != "" {
			return problem(409, "登录身份已变化，请刷新页面")
		}
		var administrator User
		if err := tx.First(&administrator, "id = ? AND role = ? AND disabled = ?", session.UserID, "admin", false).Error; err != nil {
			return problem(403, "需要管理员权限")
		}
		if err := tx.First(&member, "id = ?", r.PathValue("id")).Error; err != nil {
			return err
		}
		if member.Role != "member" || member.Disabled {
			return problem(400, "只能进入正常会员的后台")
		}
		until := s.Now().Add(time.Hour)
		if until.After(session.ExpiresAt) {
			until = session.ExpiresAt
		}
		session.ImpersonatedUserID, session.ImpersonationExpiresAt, session.CSRF = member.ID, &until, randomHex(32)
		if err := tx.Model(&Session{}).Where("hash = ?", session.Hash).Updates(map[string]any{"impersonated_user_id": member.ID, "impersonation_expires_at": until, "csrf": session.CSRF}).Error; err != nil {
			return err
		}
		detail, _ := json.Marshal(map[string]any{"ip": s.clientIP(r), "expires_at": until})
		return recordAudit(tx, administrator.ID, "support.start", member.ID, string(detail))
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]any{"user": member, "csrf_token": session.CSRF})
}

func (s *Server) stopImpersonation(w http.ResponseWriter, r *http.Request) {
	original := r.Context().Value(sessionKey).(Session)
	var session Session
	var administrator User
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, "hash = ? AND expires_at > ?", original.Hash, s.Now()).Error; err != nil {
			return problem(401, "登录已过期，请重新登录")
		}
		if session.CSRF != original.CSRF {
			return problem(409, "登录身份已变化，请刷新页面")
		}
		if err := tx.First(&administrator, "id = ? AND role = ? AND disabled = ?", session.UserID, "admin", false).Error; err != nil {
			return problem(403, "需要管理员权限")
		}
		if session.ImpersonatedUserID == "" {
			return nil
		}
		target := session.ImpersonatedUserID
		session.CSRF = randomHex(32)
		if err := tx.Model(&Session{}).Where("hash = ?", session.Hash).Updates(map[string]any{"impersonated_user_id": "", "impersonation_expires_at": nil, "csrf": session.CSRF}).Error; err != nil {
			return err
		}
		return recordAudit(tx, administrator.ID, "support.stop", target, "管理员返回原账号")
	})
	if err != nil {
		fail(w, err)
		return
	}
	jsonOut(w, 200, map[string]any{"user": administrator, "csrf_token": session.CSRF})
}
