package platform

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAdTokensOwnerReadback(t *testing.T) {
	for _, kind := range []string{"apps", "pwas"} {
		for _, mode := range []string{"quick", "deep"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				s := testServer(t)
				u := seedUser(t, s, Dollar)
				a := seedApp(t, s, u, 100)
				publicPath := "/a/" + a.Slug + "/info"
				id := a.ID
				if kind == "pwas" {
					p := createPWAFixture(t, s, u, "Token readback")
					a, id, publicPath = pwaAdApplication(p), p.ID, "/p/"+p.Slug+"/"
				}
				cfg, _, _ := seedAds(t, s, a)
				cfg.Mode = mode
				if err := s.DB.Save(&cfg).Error; err != nil {
					t.Fatal(err)
				}
				cookie, csrf := sessionFor(t, s, u)
				outsider := seedUser(t, s, Dollar)
				otherCookie, _ := sessionFor(t, s, outsider)
				path := "/api/" + kind + "/" + id + "/ads"
				checkNoTokens := func(body string) {
					t.Helper()
					for _, token := range []string{"facebook-secret", "tiktok-secret", cfg.FacebookToken, cfg.TikTokToken} {
						if strings.Contains(body, token) {
							t.Fatal("advertising token exposed outside owner editor readback")
						}
					}
				}
				for range 2 {
					w := call(t, s, "GET", path+"?include_tokens=1", nil, cookie, "")
					requireCode(t, w, 200)
					var out map[string]json.RawMessage
					if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
						t.Fatal(err)
					}
					if string(out["facebook_token"]) != `"facebook-secret"` || string(out["tiktok_token"]) != `"tiktok-secret"` || w.Header().Get("Cache-Control") != "no-store" {
						t.Fatal("saved tokens were not restored in an uncached editor response")
					}
					checkNoTokens(string(out["config"]))
				}
				for _, request := range []struct {
					path   string
					cookie *http.Cookie
					code   int
				}{{path, cookie, 200}, {"/api/ad-configurations", cookie, 200}, {publicPath, nil, 200}, {path + "?include_tokens=1", nil, 401}, {path + "?include_tokens=1", otherCookie, 404}} {
					w := call(t, s, "GET", request.path, nil, request.cookie, "")
					requireCode(t, w, request.code)
					checkNoTokens(w.Body.String())
				}
				w := call(t, s, "PUT", path, map[string]any{"config": cfg, "facebook_token": "", "tiktok_token": ""}, cookie, csrf)
				requireCode(t, w, 200)
				checkNoTokens(w.Body.String())
				var stored AdConfig
				if err := s.DB.First(&stored, "app_id = ?", a.ID).Error; err != nil {
					t.Fatal(err)
				}
				if stored.FacebookToken != cfg.FacebookToken || stored.TikTokToken != cfg.TikTokToken {
					t.Fatal("readback or saving unchanged settings replaced encrypted credentials")
				}
			})
		}
	}
}
