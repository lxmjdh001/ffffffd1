package platform

import (
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Action destinations travel inside the encrypted notification data. Only the
// stable action ID and label are passed to the browser's notification buttons.
type PWAPushAction struct {
	Action string `json:"action"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

var pwaPushActionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,50}$`)

func validPWAPushContentURL(raw, publicURL string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || len(raw) > 2048 || strings.ContainsAny(raw, "\x00\r\n") {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	origin, err := url.Parse(publicURL)
	if err != nil || origin.Scheme != "http" || u.Scheme != "http" || u.Host != origin.Host {
		return false
	}
	ip := net.ParseIP(origin.Hostname())
	return origin.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
}

func normalizePWAPushContent(title, body, target, icon, image *string, actions *[]PWAPushAction, publicURL string, allowEmptyURL bool) error {
	*title, *body = strings.TrimSpace(*title), strings.TrimSpace(*body)
	*target, *icon, *image = strings.TrimSpace(*target), strings.TrimSpace(*icon), strings.TrimSpace(*image)
	if utf8.RuneCountInString(*title) < 1 || utf8.RuneCountInString(*title) > 100 || utf8.RuneCountInString(*body) < 1 || utf8.RuneCountInString(*body) > 500 {
		return problem(400, "通知标题须为 1–100 字，正文须为 1–500 字")
	}
	if !(*target == "" && allowEmptyURL) && !validPWAPushContentURL(*target, publicURL) {
		return problem(400, "点击链接须为有效的 HTTPS 地址")
	}
	if *icon != "" && !validPWAPushContentURL(*icon, publicURL) || *image != "" && !validPWAPushContentURL(*image, publicURL) {
		return problem(400, "通知图片须为有效的 HTTPS 地址")
	}
	if len(*actions) > 2 {
		return problem(400, "每条通知最多添加两个操作按钮")
	}
	normalized := make([]PWAPushAction, 0, len(*actions))
	seen := map[string]bool{}
	for i, action := range *actions {
		action.Action, action.Title, action.URL = strings.TrimSpace(action.Action), strings.TrimSpace(action.Title), strings.TrimSpace(action.URL)
		if action.Action == "" {
			action.Action = "action_" + strconv.Itoa(i+1)
		}
		if !pwaPushActionID.MatchString(action.Action) || seen[action.Action] || utf8.RuneCountInString(action.Title) < 1 || utf8.RuneCountInString(action.Title) > 40 {
			return problem(400, "操作按钮标识须唯一，名称须为 1–40 字")
		}
		if !validPWAPushContentURL(action.URL, publicURL) {
			return problem(400, "操作按钮须填写有效的 HTTPS 跳转地址")
		}
		seen[action.Action] = true
		normalized = append(normalized, action)
	}
	*actions = normalized
	// Leave space for delivery capabilities and RFC8291 encryption overhead.
	payload, _ := json.Marshal(pwaPushContentPayload(*title, *body, *target, *icon, *image, normalized))
	if len(payload) > 3400 {
		return problem(400, "通知内容过长，请缩短正文或链接")
	}
	return nil
}

func pwaPushContentPayload(title, body, target, icon, image string, actions []PWAPushAction) map[string]any {
	content := map[string]any{"title": title, "body": body, "url": target, "icon": icon}
	if image != "" {
		content["image"] = image
	}
	if len(actions) > 0 {
		content["actions"] = actions
	}
	return content
}

func pwaTaskActions(task PWAPushTask) []PWAPushAction {
	actions := []PWAPushAction{}
	_ = json.Unmarshal([]byte(task.ActionsJSON), &actions)
	return actions
}
