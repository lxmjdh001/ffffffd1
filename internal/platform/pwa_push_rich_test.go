package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func richTemplateFixture(t *testing.T, s *Server, owner User) pwaPushTemplateView {
	t.Helper()
	input := pwaPushTemplateInput{Name: "Welcome", Title: "Welcome back", Body: "Your update is ready", Image: "https://images.example/banner.png", Actions: []PWAPushAction{{Title: "Open offer", URL: "https://shop.example/offer"}, {Title: "Contact us", URL: "https://shop.example/contact"}}}
	w := pushTaskCall(t, s, "POST", "/api/pwa-push/templates", input, owner, randomHex(16), "correct")
	requireCode(t, w, 201)
	var row pwaPushTemplateView
	if err := json.Unmarshal(w.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestPWAPushRichTemplateSnapshotsAndReplay(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	other := seedUser(t, s, 0)
	app := pushTestApp(t, s, owner)
	foreignApp := pushTestApp(t, s, other)
	template := richTemplateFixture(t, s, owner)
	request := pwaPushTaskInput{PWAID: app.ID, TemplateID: template.ID, ScheduleType: "now"}
	key := randomHex(16)
	foreign := request
	foreign.PWAID = foreignApp.ID
	requireCode(t, pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", foreign, other, randomHex(16), "correct"), 404)
	w := pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", request, owner, key, "correct")
	requireCode(t, w, 201)
	var created pwaPushTaskView
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Title != template.Title || created.Image != template.Image || len(created.Actions) != 2 || created.TemplateID == nil || *created.TemplateID != template.ID || created.URL != s.Config.PublicURL+"/p/"+app.Slug+"/launch" || !strings.HasSuffix(created.Icon, "/icon/192.png") {
		t.Fatalf("template snapshot mismatch: %+v", created)
	}
	requireCode(t, pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", request, owner, key, "correct"), 200)
	cleared := map[string]any{"pwa_id": app.ID, "template_id": template.ID, "schedule_type": "now", "actions": []PWAPushAction{}}
	requireCode(t, pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", cleared, owner, key, "correct"), 409)
	update := pwaPushTemplateInput{Name: "Changed", Title: "Changed title", Body: "Changed body", URL: "https://shop.example/new", Icon: "https://images.example/new-icon.png", Actions: []PWAPushAction{}}
	requireCode(t, pushTaskCall(t, s, "PUT", "/api/pwa-push/templates/"+template.ID, update, owner, "", "correct"), 200)
	replay := pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", request, owner, key, "correct")
	requireCode(t, replay, 200)
	var original pwaPushTaskView
	_ = json.Unmarshal(replay.Body.Bytes(), &original)
	if original.ID != created.ID || original.Title != template.Title || original.Image != template.Image || len(original.Actions) != 2 {
		t.Fatal("editing a template changed a queued task")
	}
	// Explicit composer fields override all template content, including cleared images and buttons.
	explicit := request
	explicit.Title = "Custom snapshot"
	explicit.Body = "Custom body"
	explicit.Actions = []PWAPushAction{}
	explicitKey := randomHex(16)
	edited := pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", explicit, owner, explicitKey, "correct")
	requireCode(t, edited, 201)
	var explicitTask pwaPushTaskView
	_ = json.Unmarshal(edited.Body.Bytes(), &explicitTask)
	if explicitTask.Image != "" || len(explicitTask.Actions) != 0 || !strings.HasSuffix(explicitTask.Icon, "/icon/192.png") || explicitTask.Title != explicit.Title {
		t.Fatal("explicit draft did not override source template")
	}
	explicit.Image = "https://images.example/different.png"
	requireCode(t, pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", explicit, owner, explicitKey, "correct"), 409)
	requireCode(t, pushTaskCall(t, s, "DELETE", "/api/pwa-push/templates/"+template.ID, nil, owner, "", "correct"), 200)
	requireCode(t, pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", request, owner, key, "correct"), 200)
	requireCode(t, pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", request, owner, randomHex(16), "correct"), 404)
	if countRows(t, s, &PWAPushTask{}) != 2 {
		t.Fatal("retry created duplicate tasks")
	}
}

func TestPWAPushRichEncryptedPayloadAndContentValidation(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	app := pushTestApp(t, s, owner)
	browser, private, auth := pushTestInput(t, "https://web.push.apple.com/rich-receiver")
	pushTestSubscribe(t, s, app, browser)
	task := pushTestTask(t, s, owner, app, "now", func(in *pwaPushTaskInput) {
		in.Icon = "https://images.example/icon.png"
		in.Image = "https://images.example/wide.png"
		in.Actions = []PWAPushAction{{Title: "Shop", URL: "https://shop.example/"}, {Action: "support", Title: "Help", URL: "https://help.example/"}}
	})
	pushExpand(t, s)
	sends := 0
	s.pwaPushHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		sends++
		payload := decryptPushTestPayload(t, r, private, auth)
		if payload["image"] != task.Image || payload["icon"] != task.Icon || payload["delivery_id"] == "" {
			t.Fatal("encrypted rich payload lost its snapshot")
		}
		actions, ok := payload["actions"].([]any)
		if !ok || len(actions) != 2 || actions[0].(map[string]any)["action"] != "action_1" || actions[1].(map[string]any)["url"] != "https://help.example/" {
			t.Fatal("encrypted button destinations missing")
		}
		return pushFakeResponse(201), nil
	})}
	if !s.deliverOnePWAPush(context.Background()) || sends != 1 || s.deliverOnePWAPush(context.Background()) {
		t.Fatal("rich delivery was missing or repeated")
	}
	for _, change := range []func(*pwaPushTaskInput){
		func(in *pwaPushTaskInput) { in.Image = "javascript:alert(1)" },
		func(in *pwaPushTaskInput) { in.Icon = "https://user:pass@images.example/icon.png" },
		func(in *pwaPushTaskInput) { in.Actions = []PWAPushAction{{Title: "A", URL: "http://shop.example"}} },
		func(in *pwaPushTaskInput) {
			in.Actions = []PWAPushAction{{Action: "same", Title: "A", URL: "https://a.example"}, {Action: "same", Title: "B", URL: "https://b.example"}}
		},
		func(in *pwaPushTaskInput) {
			in.Actions = []PWAPushAction{{Title: strings.Repeat("中", 41), URL: "https://a.example"}}
		},
		func(in *pwaPushTaskInput) {
			in.URL = "https://a.example/" + strings.Repeat("x", 1900)
			in.Image = "https://b.example/" + strings.Repeat("x", 1900)
		},
	} {
		in := pwaPushTaskInput{PWAID: app.ID, Title: "Test", Body: "Body", ScheduleType: "now"}
		change(&in)
		requireCode(t, pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", in, owner, randomHex(16), "correct"), 400)
	}
}

func TestPWAPushLegacyRequestFingerprintStillReplays(t *testing.T) {
	s := testServer(t)
	owner := seedUser(t, s, 0)
	app := pushTestApp(t, s, owner)
	key := randomHex(16)
	in := pwaPushTaskInput{PWAID: app.ID, Title: "Legacy", Body: "Body", ScheduleType: "now"}
	w := pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", in, owner, key, "correct")
	requireCode(t, w, 201)
	type legacyInput struct {
		PWAID           string     `json:"pwa_id"`
		Title           string     `json:"title"`
		Body            string     `json:"body"`
		URL             string     `json:"url"`
		ScheduleType    string     `json:"schedule_type"`
		ScheduledAt     *time.Time `json:"scheduled_at"`
		DelayMinutes    int        `json:"delay_minutes"`
		TargetCountries []string   `json:"target_countries"`
	}
	raw, _ := json.Marshal(legacyInput{PWAID: app.ID, Title: in.Title, Body: in.Body, URL: s.Config.PublicURL + "/p/" + app.Slug + "/launch", ScheduleType: "now", TargetCountries: []string{}})
	var task PWAPushTask
	if err := s.DB.Where("user_id = ? AND request_key_hash = ?", owner.ID, hashString(key)).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.RequestFingerprint != hashString(string(raw)) {
		t.Fatal("new optional fields changed existing request fingerprints")
	}
	requireCode(t, pushTaskCall(t, s, "POST", "/api/pwa-push/tasks", in, owner, key, "correct"), 200)
}
