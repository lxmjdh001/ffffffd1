package platform

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

type supportReply struct {
	User  User               `json:"user"`
	CSRF  string             `json:"csrf_token"`
	Scope *impersonationInfo `json:"impersonation"`
}

func readSupportReply(t *testing.T, raw []byte) supportReply {
	t.Helper()
	var reply supportReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}
func supportFixture(t *testing.T) (*Server, User, User, *http.Cookie, string) {
	t.Helper()
	s := testServer(t)
	administrator := seedUser(t, s, 0)
	administrator.Role = "admin"
	if err := s.DB.Save(&administrator).Error; err != nil {
		t.Fatal(err)
	}
	member := seedUser(t, s, Dollar)
	cookie, csrf := sessionFor(t, s, administrator)
	return s, administrator, member, cookie, csrf
}
func TestAdminImpersonationIdentityIsolationAndReturn(t *testing.T) {
	s, administrator, member, cookie, csrf := supportFixture(t)
	adminApp, memberApp := seedApp(t, s, administrator, 10), seedApp(t, s, member, 10)
	otherCookie, _ := sessionFor(t, s, administrator)
	beforeBalance := member.BalanceMicros
	response := call(t, s, "POST", "/api/admin/users/"+member.ID+"/impersonation", map[string]any{}, cookie, csrf)
	requireCode(t, response, 200)
	entered := readSupportReply(t, response.Body.Bytes())
	if entered.User.ID != member.ID || entered.CSRF == "" || entered.CSRF == csrf {
		t.Fatal("identity or CSRF not switched")
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("support login replaced the administrator cookie")
	}
	me := call(t, s, "GET", "/api/me", nil, cookie, "")
	requireCode(t, me, 200)
	identity := readSupportReply(t, me.Body.Bytes())
	if identity.User.ID != member.ID || identity.User.Role != "member" || identity.Scope == nil || !identity.Scope.Available || identity.Scope.AdministratorID != administrator.ID {
		t.Fatalf("wrong support identity: %s", me.Body.String())
	}
	if identity.Scope.ExpiresAt == nil || identity.Scope.ExpiresAt.After(s.Now().Add(time.Hour)) {
		t.Fatal("scope is not time bounded")
	}
	if (member.PasswordHash != "" && strings.Contains(me.Body.String(), member.PasswordHash)) || (administrator.PasswordHash != "" && strings.Contains(me.Body.String(), administrator.PasswordHash)) {
		t.Fatal("identity response exposed a password hash")
	}
	requireCode(t, call(t, s, "GET", "/api/apps/"+memberApp.ID, nil, cookie, ""), 200)
	requireCode(t, call(t, s, "GET", "/api/apps/"+adminApp.ID, nil, cookie, ""), 404)
	requireCode(t, call(t, s, "GET", "/api/admin/users", nil, cookie, ""), 403)
	requireCode(t, call(t, s, "POST", "/api/admin/users/"+member.ID+"/impersonation", map[string]any{}, cookie, entered.CSRF), 403)
	requireCode(t, call(t, s, "PATCH", "/api/me", map[string]any{"name": "wrong CSRF"}, cookie, csrf), 403)
	requireCode(t, call(t, s, "PATCH", "/api/me", map[string]any{"name": "Support edited member"}, cookie, entered.CSRF), 200)
	if err := s.DB.First(&member, "id = ?", member.ID).Error; err != nil {
		t.Fatal(err)
	}
	if member.Name != "Support edited member" || member.BalanceMicros != beforeBalance {
		t.Fatal("member identity or balance changed unexpectedly")
	}
	otherMe := call(t, s, "GET", "/api/me", nil, otherCookie, "")
	requireCode(t, otherMe, 200)
	if readSupportReply(t, otherMe.Body.Bytes()).User.ID != administrator.ID {
		t.Fatal("another administrator session was switched")
	}
	stop := call(t, s, "POST", "/api/auth/impersonation/stop", map[string]any{}, cookie, entered.CSRF)
	requireCode(t, stop, 200)
	restored := readSupportReply(t, stop.Body.Bytes())
	if restored.User.ID != administrator.ID || restored.CSRF == entered.CSRF {
		t.Fatal("administrator identity or CSRF not restored")
	}
	requireCode(t, call(t, s, "GET", "/api/admin/users", nil, cookie, ""), 200)
	requireCode(t, call(t, s, "GET", "/api/apps/"+memberApp.ID, nil, cookie, ""), 404)
	requireCode(t, call(t, s, "PATCH", "/api/me", map[string]any{"name": "stale member token"}, cookie, entered.CSRF), 403)
	var audits []Audit
	if err := s.DB.Where("action LIKE ?", "support.%").Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	actions := map[string]bool{}
	for _, audit := range audits {
		if audit.ActorID != administrator.ID || audit.Target != member.ID {
			t.Fatalf("wrong support operator: %+v", audit)
		}
		actions[audit.Action] = true
		if strings.Contains(audit.Detail, entered.CSRF) || strings.Contains(audit.Detail, "Support edited member") {
			t.Fatal("support audit exposed a token or request body")
		}
	}
	for _, action := range []string{"support.start", "support.request", "support.stop"} {
		if !actions[action] {
			t.Fatal("missing support audit", action)
		}
	}
}
func TestAdminImpersonationAuthorizationAndRecovery(t *testing.T) {
	for _, scenario := range []string{"expired", "disabled", "deleted", "promoted"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, member, cookie, csrf := supportFixture(t)
			entered := call(t, s, "POST", "/api/admin/users/"+member.ID+"/impersonation", map[string]any{}, cookie, csrf)
			requireCode(t, entered, 200)
			csrf = readSupportReply(t, entered.Body.Bytes()).CSRF
			switch scenario {
			case "expired":
				s.DB.Model(&Session{}).Where("hash = ?", hashString(cookie.Value)).Update("impersonation_expires_at", s.Now().Add(-time.Second))
			case "disabled":
				s.DB.Model(&member).Update("disabled", true)
			case "deleted":
				s.DB.Delete(&member)
			case "promoted":
				s.DB.Model(&member).Update("role", "admin")
			}
			requireCode(t, call(t, s, "GET", "/api/apps", nil, cookie, ""), 403)
			requireCode(t, call(t, s, "GET", "/api/admin/users", nil, cookie, ""), 403)
			me := call(t, s, "GET", "/api/me", nil, cookie, "")
			requireCode(t, me, 200)
			reply := readSupportReply(t, me.Body.Bytes())
			if reply.Scope == nil || reply.Scope.Available || reply.User.Role != "member" {
				t.Fatal("unavailable support scope disappeared or gained admin rights")
			}
			requireCode(t, call(t, s, "POST", "/api/auth/impersonation/stop", map[string]any{}, cookie, csrf), 200)
			requireCode(t, call(t, s, "GET", "/api/admin/users", nil, cookie, ""), 200)
		})
	}
	s, admin, member, cookie, csrf := supportFixture(t)
	path := "/api/admin/users/" + member.ID + "/impersonation"
	requireCode(t, call(t, s, "POST", path, map[string]any{}, nil, ""), 401)
	memberCookie, memberCSRF := sessionFor(t, s, member)
	requireCode(t, call(t, s, "POST", path, map[string]any{}, memberCookie, memberCSRF), 403)
	requireCode(t, call(t, s, "POST", "/api/auth/impersonation/stop", map[string]any{}, memberCookie, memberCSRF), 403)
	requireCode(t, call(t, s, "POST", path, map[string]any{}, cookie, "wrong"), 403)
	requireCode(t, call(t, s, "POST", "/api/admin/users/"+admin.ID+"/impersonation", map[string]any{}, cookie, csrf), 400)
	requireCode(t, call(t, s, "POST", "/api/admin/users/does-not-exist/impersonation", map[string]any{}, cookie, csrf), 404)
	s.DB.Model(&member).Update("disabled", true)
	requireCode(t, call(t, s, "POST", path, map[string]any{}, cookie, csrf), 400)
	s.DB.Model(&member).Update("disabled", false)
	requireCode(t, call(t, s, "POST", path, map[string]any{}, cookie, csrf), 200)
	s.DB.Model(&admin).Update("disabled", true)
	requireCode(t, call(t, s, "GET", "/api/me", nil, cookie, ""), 401)
	requireCode(t, call(t, s, "POST", "/api/auth/impersonation/stop", map[string]any{}, cookie, csrf), 401)
}
func TestAdminImpersonationAuditFailureRollsBack(t *testing.T) {
	s, admin, member, cookie, csrf := supportFixture(t)
	name := "support-audit-failure"
	s.DB.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "audits" {
			tx.AddError(problem(500, "audit unavailable"))
		}
	})
	defer s.DB.Callback().Create().Remove(name)
	requireCode(t, call(t, s, "POST", "/api/admin/users/"+member.ID+"/impersonation", map[string]any{}, cookie, csrf), 500)
	me := call(t, s, "GET", "/api/me", nil, cookie, "")
	requireCode(t, me, 200)
	reply := readSupportReply(t, me.Body.Bytes())
	if reply.User.ID != admin.ID || reply.Scope != nil || reply.CSRF != csrf {
		t.Fatal("failed audit switched identity")
	}
}
func TestAdminImpersonationSessionMigrationPreservesLogin(t *testing.T) {
	s, admin, _, cookie, csrf := supportFixture(t)
	for _, column := range []string{"ImpersonatedUserID", "ImpersonationExpiresAt"} {
		if err := s.DB.Migrator().DropColumn(&Session{}, column); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(s.DB); err != nil {
		t.Fatal(err)
	}
	me := call(t, s, "GET", "/api/me", nil, cookie, "")
	requireCode(t, me, 200)
	reply := readSupportReply(t, me.Body.Bytes())
	if reply.User.ID != admin.ID || reply.CSRF != csrf || reply.Scope != nil {
		t.Fatal("migration changed the existing session")
	}
}

func TestAdminImpersonationConcurrentEntry(t *testing.T) {
	s, administrator, first, cookie, csrf := supportFixture(t)
	second := seedUser(t, s, 0)
	start := make(chan struct{})
	results := make(chan int, 2)
	for _, member := range []User{first, second} {
		go func(member User) {
			<-start
			results <- call(t, s, "POST", "/api/admin/users/"+member.ID+"/impersonation", map[string]any{}, cookie, csrf).Code
		}(member)
	}
	close(start)
	successes := 0
	for range 2 {
		code := <-results
		if code == 200 {
			successes++
		} else if code != 403 && code != 409 {
			t.Fatalf("unexpected concurrent entry status %d", code)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent entry switched identities %d times", successes)
	}
	var starts int64
	if err := s.DB.Model(&Audit{}).Where("actor_id = ? AND action = ?", administrator.ID, "support.start").Count(&starts).Error; err != nil {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatal("concurrent entry recorded multiple successful switches")
	}
	me := call(t, s, "GET", "/api/me", nil, cookie, "")
	requireCode(t, me, 200)
	reply := readSupportReply(t, me.Body.Bytes())
	if reply.Scope == nil || !reply.Scope.Available || (reply.User.ID != first.ID && reply.User.ID != second.ID) {
		t.Fatal("concurrent entry left an inconsistent support identity")
	}
}
