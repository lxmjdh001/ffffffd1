package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"math/big"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/dchest/captcha"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func normalizeEmail(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	a, e := mail.ParseAddress(s)
	if e != nil || a.Address != s || len(s) > 254 {
		return "", problem(400, "请输入有效的邮箱地址")
	}
	return s, nil
}

var adminAccountPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,31}$`)

func normalizeAdminAccount(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.Contains(s, "@") {
		return normalizeEmail(s)
	}
	if !adminAccountPattern.MatchString(s) {
		return "", problem(400, "管理员账号须为 3–32 位字母、数字、短横线或下划线")
	}
	return s, nil
}

type authInput struct {
	Email       string `json:"email"`
	Name        string `json:"name"`
	Password    string `json:"password"`
	CaptchaID   string `json:"captcha_id"`
	CaptchaCode string `json:"captcha_code"`
}

func (s *Server) captcha(w http.ResponseWriter, r *http.Request) {
	if e := s.rate("captcha:"+s.clientIP(r), 30, time.Minute); e != nil {
		fail(w, e)
		return
	}
	id := randomHex(16)
	digits := make([]byte, 5)
	code := make([]byte, 5)
	for i := range digits {
		v, e := rand.Int(rand.Reader, big.NewInt(10))
		if e != nil {
			fail(w, e)
			return
		}
		digits[i] = byte(v.Int64())
		code[i] = '0' + digits[i]
	}
	var b bytes.Buffer
	if _, e := captcha.NewImage(id, digits, 190, 64).WriteTo(&b); e != nil {
		fail(w, e)
		return
	}
	if e := s.DB.Create(&Challenge{ID: id, Hash: s.mac(id + ":" + string(code)), ExpiresAt: s.Now().Add(5 * time.Minute)}).Error; e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]string{"id": id, "image": "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())})
}
func (s *Server) verifyBot(r *http.Request, in authInput, action string) error {
	var ch Challenge
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if e := tx.First(&ch, "id = ?", in.CaptchaID).Error; e != nil {
			return e
		}
		res := tx.Delete(&Challenge{}, "id = ?", in.CaptchaID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil || !ch.ExpiresAt.After(s.Now()) || subtle.ConstantTimeCompare([]byte(ch.Hash), []byte(s.mac(in.CaptchaID+":"+in.CaptchaCode))) != 1 {
		return problem(400, "图形验证码错误或已过期")
	}
	return nil
}
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if e := s.rate("register:"+s.clientIP(r), 10, time.Hour); e != nil {
		fail(w, e)
		return
	}
	settings, e := getSettings(s.DB)
	if e != nil {
		fail(w, e)
		return
	}
	if !settings.RegistrationOpen {
		fail(w, problem(403, "当前已关闭注册"))
		return
	}
	var in authInput
	if e = decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	email, e := normalizeEmail(in.Email)
	if e != nil {
		fail(w, e)
		return
	}
	if len(in.Password) < 6 || len(in.Password) > 72 {
		fail(w, problem(400, "密码长度须为 6–72 字节"))
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len([]rune(name)) > 40 {
		fail(w, problem(400, "请输入 1–40 字的用户名称"))
		return
	}
	if e = s.verifyBot(r, in, "register"); e != nil {
		fail(w, e)
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(in.Password), 12)
	if e != nil {
		fail(w, e)
		return
	}
	u := User{ID: randomHex(16), Email: email, Name: name, PasswordHash: string(hash), Role: "member", CreatedAt: s.Now()}
	if e = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&u).Error; err != nil {
			return err
		}
		plan, err := getPlan(tx, u, s.Now())
		if err != nil {
			return err
		}
		_, err = activeDownloadGrant(tx, u, plan, s.Now())
		return err
	}); e != nil {
		if errors.Is(e, gorm.ErrDuplicatedKey) {
			e = problem(409, "该邮箱已注册，请直接登录")
		}
		fail(w, e)
		return
	}
	s.establishSession(w, r, u)
}

var dummyHash = []byte("$2a$12$R9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW")

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if e := s.rate("login-ip:"+s.clientIP(r), 30, 15*time.Minute); e != nil {
		fail(w, e)
		return
	}
	var in authInput
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	email, e := normalizeAdminAccount(in.Email)
	if e != nil {
		fail(w, problem(401, "账号或密码错误"))
		return
	}
	if e = s.rate("login-email:"+email, 12, 15*time.Minute); e != nil {
		fail(w, e)
		return
	}
	if e = s.verifyBot(r, in, "login"); e != nil {
		fail(w, e)
		return
	}
	var u User
	q := s.DB.Where("email = ?", email)
	if !strings.Contains(email, "@") {
		q = q.Where("role = ?", "admin")
	}
	e = q.First(&u).Error
	hash := []byte(u.PasswordHash)
	if e != nil {
		hash = dummyHash
	}
	pass := bcrypt.CompareHashAndPassword(hash, []byte(in.Password))
	if e != nil || pass != nil || u.Disabled {
		fail(w, problem(401, "账号或密码错误，或账户已停用"))
		return
	}
	s.establishSession(w, r, u)
}
func (s *Server) cookieName() string {
	if s.Config.Production() {
		return "__Host-d1_session"
	}
	return "d1_session"
}
func (s *Server) establishSession(w http.ResponseWriter, r *http.Request, u User) {
	token := randomHex(32)
	session := Session{Hash: hashString(token), UserID: u.ID, CSRF: randomHex(32), ExpiresAt: s.Now().Add(7 * 24 * time.Hour)}
	e := s.DB.Transaction(func(tx *gorm.DB) error {
		if old, e := r.Cookie(s.cookieName()); e == nil {
			if e = tx.Delete(&Session{}, "hash = ?", hashString(old.Value)).Error; e != nil {
				return e
			}
		}
		return tx.Create(&session).Error
	})
	if e != nil {
		fail(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: token, Path: "/", Secure: s.Config.Production(), HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 86400})
	jsonOut(w, 200, map[string]any{"user": u, "csrf_token": session.CSRF})
}
func (s *Server) auth(fn http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := r.Cookie(s.cookieName())
		if e != nil || len(c.Value) != 64 {
			fail(w, problem(401, "请先登录"))
			return
		}
		var session Session
		if e = s.DB.First(&session, "hash = ? AND expires_at > ?", hashString(c.Value), s.Now()).Error; e != nil {
			fail(w, problem(401, "登录已过期，请重新登录"))
			return
		}
		var u User
		if e = s.DB.First(&u, "id = ?", session.UserID).Error; e != nil || u.Disabled {
			fail(w, problem(401, "账户不可用"))
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && subtle.ConstantTimeCompare([]byte(session.CSRF), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
			fail(w, problem(403, "安全令牌失效，请刷新页面"))
			return
		}
		ctx := context.WithValue(r.Context(), userKey, u)
		ctx = context.WithValue(ctx, sessionKey, session)
		fn(w, r.WithContext(ctx))
	})
}
func (s *Server) admin(fn http.HandlerFunc) http.Handler {
	return s.auth(func(w http.ResponseWriter, r *http.Request) {
		if userOf(r).Role != "admin" {
			fail(w, problem(403, "需要管理员权限"))
			return
		}
		fn(w, r)
	})
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	p, e := getPlan(s.DB, u, s.Now())
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]any{"user": u, "plan": p, "csrf_token": r.Context().Value(sessionKey).(Session).CSRF})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(Session)
	if e := s.DB.Delete(&Session{}, "hash = ?", session.Hash).Error; e != nil {
		fail(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Path: "/", Value: "", MaxAge: -1, Secure: s.Config.Production(), HttpOnly: true, SameSite: http.SameSiteLaxMode})
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if len([]rune(in.Name)) < 1 || len([]rune(in.Name)) > 40 {
		fail(w, problem(400, "名称须为 1–40 字"))
		return
	}
	u := userOf(r)
	if e := s.DB.Model(&u).Update("name", in.Name).Error; e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, u)
}
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	if e := s.rate("password:"+userOf(r).ID, 5, time.Hour); e != nil {
		fail(w, e)
		return
	}
	var in struct {
		Current  string `json:"current_password"`
		Password string `json:"password"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if len(in.Password) < 6 || len(in.Password) > 72 {
		fail(w, problem(400, "密码长度须为 6–72 字节"))
		return
	}
	u := userOf(r)
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Current)) != nil {
		fail(w, problem(400, "当前密码错误"))
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(in.Password), 12)
	if e != nil {
		fail(w, e)
		return
	}
	e = s.DB.Transaction(func(tx *gorm.DB) error {
		current, e := lockUser(tx, u.ID)
		if e != nil {
			return e
		}
		if current.PasswordHash != u.PasswordHash {
			return problem(409, "密码已变更，请重新登录")
		}
		if e := tx.Model(&current).Update("password_hash", string(hash)).Error; e != nil {
			return e
		}
		return tx.Delete(&Session{}, "user_id = ?", u.ID).Error
	})
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
