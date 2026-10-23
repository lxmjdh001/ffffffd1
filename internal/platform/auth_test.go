package platform

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestRegistrationAndSessionLifecycle(t *testing.T) {
	s := testServer(t)
	input := authInput{Email: "Customer@Example.com", Name: "测试工作室", Password: "123456", CaptchaCode: "12345"}
	challenge := func() {
		input.CaptchaID = randomHex(16)
		if e := s.DB.Create(&Challenge{ID: input.CaptchaID, Hash: s.mac(input.CaptchaID + ":" + input.CaptchaCode), ExpiresAt: s.Now().Add(time.Minute)}).Error; e != nil {
			t.Fatal(e)
		}
	}
	for _, password := range []string{"12345", strings.Repeat("a", 73)} {
		invalid := input
		invalid.Password = password
		requireCode(t, call(t, s, "POST", "/api/auth/register", invalid, nil, ""), 400)
	}
	challenge()
	w := call(t, s, "POST", "/api/auth/register", input, nil, "")
	requireCode(t, w, 200)
	var response struct {
		User User   `json:"user"`
		CSRF string `json:"csrf_token"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &response); e != nil {
		t.Fatal(e)
	}
	if response.User.Role != "member" || response.User.Email != "customer@example.com" || response.CSRF == "" {
		t.Fatal("invalid registration result")
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == s.cookieName() {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("missing protected session cookie")
	}
	requireCode(t, call(t, s, "GET", "/api/me", nil, cookie, ""), 200)
	requireCode(t, call(t, s, "GET", "/api/admin/settings", nil, cookie, ""), 403)
	requireCode(t, call(t, s, "POST", "/api/auth/logout", nil, cookie, "wrong"), 403)
	requireCode(t, call(t, s, "POST", "/api/auth/logout", nil, cookie, response.CSRF), 200)
	requireCode(t, call(t, s, "GET", "/api/me", nil, cookie, ""), 401)
	challenge()
	input.Password = "wrong-password-12345"
	requireCode(t, call(t, s, "POST", "/api/auth/login", input, nil, ""), 401)
	challenge()
	input.Password = "123456"
	w = call(t, s, "POST", "/api/auth/login", input, nil, "")
	requireCode(t, w, 200)
	if countRows(t, s, &Session{}) != 1 {
		t.Fatal("unexpected active sessions")
	}
}

func TestSixDigitPasswordChanges(t *testing.T) {
	for _, action := range []string{"self", "admin"} {
		t.Run(action, func(t *testing.T) {
			s := testServer(t)
			u := seedUser(t, s, 0)
			userCookie, userCSRF := sessionFor(t, s, u)
			cookie, csrf := userCookie, userCSRF
			path := "/api/me/password"
			if action == "admin" {
				admin := seedUser(t, s, 0)
				if err := s.DB.Model(&admin).Update("role", "admin").Error; err != nil {
					t.Fatal(err)
				}
				cookie, csrf = sessionFor(t, s, admin)
				path = "/api/admin/users/" + u.ID + "/password"
			}
			for _, password := range []string{"12345", strings.Repeat("a", 73), "654321"} {
				body := map[string]string{"password": password}
				if action == "self" {
					body["current_password"] = "test-password-12345"
				}
				want := 400
				if password == "654321" {
					want = 200
				}
				requireCode(t, call(t, s, "POST", path, body, cookie, csrf), want)
			}
			if err := s.DB.First(&u, "id = ?", u.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("654321")); err != nil {
				t.Fatal("six-digit password was not saved")
			}
			requireCode(t, call(t, s, "GET", "/api/me", nil, userCookie, ""), 401)
		})
	}
}
