package platform

import (
	"testing"
	"time"
)

func TestAdminAccountRenameRevokesSessionsAndOldLogin(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	old := u.Email
	if err := s.DB.Model(&u).Update("role", "admin").Error; err != nil {
		t.Fatal(err)
	}
	cookie, _ := sessionFor(t, s, u)
	t.Setenv("ADMIN_PREVIOUS_ACCOUNT", old)
	c := s.Config
	c.AdminEmail = "test_admin"
	c.AdminPassword = "changed-test-password"
	if err := ResetAdmin(s.DB, c); err != nil {
		t.Fatal(err)
	}
	requireCode(t, call(t, s, "GET", "/api/me", nil, cookie, ""), 401)
	if err := s.DB.First(&u, "id = ?", u.ID).Error; err != nil {
		t.Fatal(err)
	}
	if u.Email != "test_admin" || u.Role != "admin" {
		t.Fatalf("admin identity not renamed: %+v", u)
	}
	for _, tc := range []struct {
		account, password string
		want              int
	}{
		{old, c.AdminPassword, 401}, {"test_admin", "test-password-12345", 401}, {"test_admin", c.AdminPassword, 200},
	} {
		id := randomHex(16)
		if err := s.DB.Create(&Challenge{ID: id, Hash: s.mac(id + ":12345"), ExpiresAt: s.Now().Add(time.Minute)}).Error; err != nil {
			t.Fatal(err)
		}
		requireCode(t, call(t, s, "POST", "/api/auth/login", authInput{Email: tc.account, Password: tc.password, CaptchaID: id, CaptchaCode: "12345"}, nil, ""), tc.want)
	}
}

func TestUsernameLoginDoesNotAuthenticateMember(t *testing.T) {
	s := testServer(t)
	u := seedUser(t, s, 0)
	if err := s.DB.Model(&u).Update("email", "member_name").Error; err != nil {
		t.Fatal(err)
	}
	id := randomHex(16)
	if err := s.DB.Create(&Challenge{ID: id, Hash: s.mac(id + ":12345"), ExpiresAt: s.Now().Add(time.Minute)}).Error; err != nil {
		t.Fatal(err)
	}
	requireCode(t, call(t, s, "POST", "/api/auth/login", authInput{Email: "member_name", Password: "test-password-12345", CaptchaID: id, CaptchaCode: "12345"}, nil, ""), 401)
}
