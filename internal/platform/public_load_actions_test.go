package platform

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
)

type loadActions struct {
	apk, installed, cancelled, failed atomic.Int64
}

var loadFormFields = regexp.MustCompile(`name="(request_key|attribution_token)" value="([^"]+)"`)
var loadInstallProof = regexp.MustCompile(`data-return-flow-token="([^"]+)"`)

func (a *loadActions) run(client *http.Client, base string, page *http.Request, cookies []*http.Cookie, body, kind string, apk Application, pwa PWAApp, index int) error {
	request := func(method, path, payload, contentType, span string, expected int) ([]byte, error) {
		// Refuse external URLs: this fixture can only call the local test server.
		if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
			return nil, fmt.Errorf("non-local action path")
		}
		r, err := http.NewRequest(method, base+path, strings.NewReader(payload))
		if err != nil {
			return nil, err
		}
		r.Host = page.Host
		r.Header = page.Header.Clone()
		r.Header.Set("Origin", "http://"+page.Host)
		r.Header.Set("Accept", "application/json")
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		if span != "" {
			r.Header.Set("Range", span)
		}
		for _, c := range cookies {
			r.AddCookie(c)
		}
		response, err := client.Do(r)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != expected {
			return nil, fmt.Errorf("%s %s status=%d want=%d: %.200s", method, path, response.StatusCode, expected, raw)
		}
		return raw, nil
	}
	if kind == "apk" {
		form := url.Values{}
		for _, m := range loadFormFields.FindAllStringSubmatch(body, -1) {
			form.Set(m[1], html.UnescapeString(m[2]))
		}
		if len(form) != 2 {
			return fmt.Errorf("missing APK form capability")
		}
		raw, err := request("POST", "/a/"+apk.Slug+"/download", form.Encode(), "application/x-www-form-urlencoded", "", 200)
		if err != nil {
			return err
		}
		var ticket struct {
			URL string `json:"download_url"`
		}
		if err = json.Unmarshal(raw, &ticket); err != nil {
			return err
		}
		// Repeat the same first-byte download. One UV debit and one callback.
		for range 3 {
			if _, err = request("GET", ticket.URL, "", "", "bytes=0-0", 206); err != nil {
				return err
			}
		}
		a.apk.Add(1)
		return nil
	}
	proof := loadInstallProof.FindStringSubmatch(body)
	if len(proof) != 2 {
		return fmt.Errorf("missing PWA install capability")
	}
	raw, _ := json.Marshal(map[string]string{"key": randomHex(16), "token": html.UnescapeString(proof[1])})
	path := "/p/" + pwa.Slug + "/install/"
	if _, err := request("POST", path+"start", string(raw), "application/json", "", 200); err != nil {
		return err
	}
	action := "complete"
	if index/12%2 == 0 {
		action = "cancel"
	}
	for range 2 {
		if _, err := request("POST", path+action, string(raw), "application/json", "", 200); err != nil {
			return err
		}
	}
	if action == "cancel" {
		a.cancelled.Add(1)
	} else {
		a.installed.Add(1)
	}
	return nil
}
