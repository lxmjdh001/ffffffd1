package platform

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	_ "golang.org/x/image/webp"
	"golang.org/x/net/html"
)

const (
	maxDIYContentBytes      = 8 << 20
	maxDIYContentImageBytes = 2 << 20
	maxDIYContentFields     = 201
)

// Content belongs to one application binding. It never changes a published
// template, a collected snapshot or another application using the same source.
type DIYLandingContent map[string]string

type DIYLandingContentField struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Label      string `json:"label"`
	Value      string `json:"value"`
	MaxLength  int    `json:"max_length"`
	PreviewURL string `json:"preview_url,omitempty"`
}

type diyContentTarget struct {
	field     DIYLandingContentField
	text      []*html.Node
	element   *html.Node
	attribute string
	css       *diyContentCSS
	cssStart  int
	cssEnd    int
}

type diyContentCSS struct {
	node      *html.Node
	attribute string
	value     string
	targets   []*diyContentTarget
}

type diyContentDocumentFields struct {
	document *html.Node
	targets  []*diyContentTarget
	buttons  []*html.Node
	css      []*diyContentCSS
	head     *html.Node
	title    *html.Node
}

func (s *Server) getDIYContentFields(w http.ResponseWriter, r *http.Request) {
	ref := &DIYLandingReference{PageID: r.PathValue("id"), RevisionID: r.PathValue("revision")}
	revision, err := loadDIYRevision(s.DB, userOf(r).ID, r.URL.Query().Get("target"), ref)
	if err != nil {
		fail(w, err)
		return
	}
	fields, err := diyContentFields(revision.RenderHTML, revision.Config)
	if err != nil {
		fail(w, err)
		return
	}
	for index := range fields {
		if fields[index].Kind == "image" && strings.HasPrefix(fields[index].Value, "https://") {
			// Reuse the signed, static-only resource proxy. The browser never
			// loads arbitrary source URLs with the customer's console session.
			fields[index].PreviewURL = s.diyAssetURL("/api/landing-template-assets/"+revision.ID, fields[index].Value, "image", s.Now().Add(15*time.Minute))
		}
		if fields[index].ID == "primary-label" && fields[index].Value == "" {
			fields[index].Value = "安装应用"
			if r.URL.Query().Get("target") == "apk" {
				fields[index].Value = "下载应用"
			}
		}
	}
	jsonOut(w, 200, map[string]any{"fields": fields, "max_image_bytes": maxDIYContentImageBytes, "max_content_bytes": maxDIYContentBytes})
}

func diyContentFields(prepared string, options DIYLandingOptions) ([]DIYLandingContentField, error) {
	doc, err := parseDIYContentFields(prepared, options)
	if err != nil {
		return nil, err
	}
	fields := make([]DIYLandingContentField, 0, len(doc.targets))
	for _, target := range doc.targets {
		fields = append(fields, target.field)
	}
	return fields, nil
}

func validateDIYContent(prepared string, options DIYLandingOptions, content DIYLandingContent) error {
	if len(content) == 0 {
		return nil
	}
	doc, err := parseDIYContentFields(prepared, options)
	if err != nil {
		return err
	}
	return validateDIYContentTargets(doc, content)
}

func validateDIYContentTargets(doc *diyContentDocumentFields, content DIYLandingContent) error {
	if len(content) > maxDIYContentFields {
		return problem(400, "落地页可编辑内容最多 201 项")
	}
	// Avoid allocating an unbounded JSON copy merely to check its encoded size.
	total := 2
	for key, value := range content {
		if len(key)+len(value) > maxDIYContentBytes-total {
			return problem(413, "落地页编辑内容最多 8 MiB")
		}
		total += len(key) + len(value)
	}
	raw, err := json.Marshal(content)
	if err != nil || len(raw) > maxDIYContentBytes {
		return problem(413, "落地页编辑内容最多 8 MiB")
	}
	known := make(map[string]*diyContentTarget, len(doc.targets))
	for _, target := range doc.targets {
		known[target.field.ID] = target
	}
	for key, value := range content {
		target := known[key]
		if target == nil {
			return problem(400, "落地页包含未知的可编辑字段，请重新选择页面版本")
		}
		if !utf8.ValidString(value) || strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) >= 0 {
			return problem(400, "落地页编辑内容包含无效字符")
		}
		if target.field.Kind == "image" {
			if !validDIYContentImage(value) {
				return problem(400, "图片须为 HTTPS 地址或不超过 2 MiB 的 PNG、JPG、GIF、WebP 图片，尺寸最多 4096 像素")
			}
			continue
		}
		if utf8.RuneCountInString(value) > target.field.MaxLength {
			return problem(400, "落地页文案超过当前字段的长度限制")
		}
		if target.field.Kind == "button" && strings.TrimSpace(value) == "" {
			return problem(400, "请输入主按钮文案，或恢复模版默认文案")
		}
	}
	return nil
}

func diyContentPrimaryLabel(content DIYLandingContent) string {
	return strings.TrimSpace(content["primary-label"])
}

// A deliberate empty override has title semantics too: it clears the browser
// title instead of silently falling back to the application's display name.
func diyContentDocumentTitle(prepared string, _ DIYLandingOptions, content DIYLandingContent) (string, bool) {
	if value, exists := content["document-title"]; exists {
		return value, true
	}
	if len(prepared) > 32<<20 {
		return "", false
	}
	document, err := html.Parse(strings.NewReader(prepared))
	if err != nil {
		return "", false
	}
	_, title := diyContentHeadTitle(document)
	if title == nil {
		return "", false
	}
	return diyContentTitleText(title), true
}

func applyDIYContent(prepared string, options DIYLandingOptions, content DIYLandingContent) (string, error) {
	if len(content) == 0 {
		return prepared, nil
	}
	doc, err := parseDIYContentFields(prepared, options)
	if err != nil {
		return "", err
	}
	if err = validateDIYContentTargets(doc, content); err != nil {
		return "", err
	}
	for _, target := range doc.targets {
		value, changed := content[target.field.ID]
		if !changed || target.field.Kind == "button" || target.css != nil {
			continue
		}
		if target.field.ID == "document-title" {
			if doc.title == nil {
				doc.title = &html.Node{Type: html.ElementNode, Data: "title"}
				doc.head.AppendChild(doc.title)
			}
			for child := doc.title.FirstChild; child != nil; {
				next := child.NextSibling
				doc.title.RemoveChild(child)
				child = next
			}
			diyRemoveAttr(doc.title, "data-d1-bind")
			doc.title.AppendChild(&html.Node{Type: html.TextNode, Data: value})
			continue
		}
		if target.field.Kind == "image" {
			if value == "" {
				diyRemoveAttr(target.element, target.attribute)
			} else {
				diySetAttr(target.element, target.attribute, value)
			}
			continue
		}
		setDIYContentText(target.text, value)
	}
	for _, css := range doc.css {
		var out strings.Builder
		position := 0
		changed := false
		for _, target := range css.targets {
			value, exists := content[target.field.ID]
			if !exists {
				continue
			}
			changed = true
			out.WriteString(css.value[position:target.cssStart])
			out.WriteString(`url("` + value + `")`)
			position = target.cssEnd
		}
		if !changed {
			continue
		}
		out.WriteString(css.value[position:])
		if css.attribute == "" {
			css.node.Data = out.String()
		} else {
			diySetAttr(css.node, css.attribute, out.String())
		}
	}
	label := diyContentPrimaryLabel(content)
	for _, button := range doc.buttons {
		// Content editing can only request the current application's trusted CTA.
		// Its URL, resource ID, installation token and accounting stay outside.
		diySetAttr(button, "data-d1-action", "primary")
		if label != "" {
			setDIYContentButtonLabel(button, label)
		}
	}
	var out bytes.Buffer
	if err := html.Render(&out, doc.document); err != nil {
		return "", err
	}
	return out.String(), nil
}

func parseDIYContentFields(prepared string, options DIYLandingOptions) (*diyContentDocumentFields, error) {
	if len(prepared) > 32<<20 {
		return nil, problem(413, "落地页内容过大，无法编辑")
	}
	root, err := html.Parse(strings.NewReader(prepared))
	if err != nil {
		return nil, problem(400, "落地页 HTML 无法解析")
	}
	selectors, err := diySelectors(options.PrimarySelector)
	if err != nil {
		return nil, err
	}
	head, title := diyContentHeadTitle(root)
	doc := &diyContentDocumentFields{document: root, head: head, title: title}
	counts := map[string]int{}
	var parseErr error
	add := func(target *diyContentTarget) {
		if parseErr != nil {
			return
		}
		// Preserve the original 199 automatic field slots and IDs. The browser
		// title and application-bound main button have independent fixed IDs.
		if len(doc.targets) >= maxDIYContentFields-2 {
			parseErr = problem(400, "落地页可编辑内容最多 201 项")
			return
		}
		counts[target.field.Kind]++
		target.field.ID = target.field.Kind + "-" + strconv.Itoa(counts[target.field.Kind])
		doc.targets = append(doc.targets, target)
	}
	addCSS := func(node *html.Node, attribute, value string) {
		css := &diyContentCSS{node: node, attribute: attribute, value: value}
		for offset := 0; offset < len(value) && parseErr == nil; {
			match := diyCSSURL.FindStringSubmatchIndex(value[offset:])
			if match == nil {
				break
			}
			for index := range match {
				if match[index] >= 0 {
					match[index] += offset
				}
			}
			offset = match[1]
			if !diyContentBackgroundURL(value, match[0]) {
				continue
			}
			raw := ""
			for i := 2; i+1 < len(match); i += 2 {
				if match[i] >= 0 {
					raw = value[match[i]:match[i+1]]
					if i == 2 {
						if decoded, decodeErr := strconv.Unquote(`"` + raw + `"`); decodeErr == nil {
							raw = decoded
						}
					}
					break
				}
			}
			if raw == "" || strings.HasPrefix(raw, "#") {
				continue
			}
			target := &diyContentTarget{field: DIYLandingContentField{Kind: "image", Value: raw, Label: diyContentLabel(node)}, css: css, cssStart: match[0], cssEnd: match[1]}
			add(target)
			css.targets = append(css.targets, target)
		}
		if len(css.targets) > 0 {
			doc.css = append(doc.css, css)
		}
	}
	var walk func(*html.Node, bool, int)
	walk = func(n *html.Node, suppressed bool, depth int) {
		if parseErr != nil {
			return
		}
		if depth > 128 {
			parseErr = problem(400, "落地页结构过深，无法编辑")
			return
		}
		if n.Type == html.ElementNode {
			if diyContentInvisible(n) {
				return
			}
			if n.Data == "style" {
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					if child.Type == html.TextNode {
						addCSS(child, "", child.Data)
					}
				}
				return
			}
			if value := diyAttr(n, "style"); value != "" {
				addCSS(n, "style", value)
			}
			if n.Data == "img" || n.Data == "input" && strings.EqualFold(diyAttr(n, "type"), "image") {
				label := diyContentLabel(n)
				if label == "" {
					label = diyAttr(n, "alt")
				}
				if utf8.RuneCountInString(label) > 100 {
					label = string([]rune(label)[:100])
				}
				add(&diyContentTarget{field: DIYLandingContentField{Kind: "image", Label: label, Value: diyAttr(n, "src")}, element: n, attribute: "src"})
			}
			button := diyContentButton(n, selectors)
			if diyContentNonText(n) {
				suppressed = true
			}
			if button {
				doc.buttons = append(doc.buttons, n)
				suppressed = true
			} else if !suppressed && diyContentTextContainer(n) {
				nodes := diyContentTextNodes(n)
				if value := diyContentJoinedText(nodes); value != "" {
					kind, limit := "text", 16000
					if len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6' {
						kind, limit = "title", 300
					}
					add(&diyContentTarget{field: DIYLandingContentField{Kind: kind, Value: value, Label: diyContentLabel(n), MaxLength: limit}, text: nodes})
					suppressed = true
				}
			}
		}
		if n.Type == html.TextNode && !suppressed && strings.TrimSpace(n.Data) != "" {
			add(&diyContentTarget{field: DIYLandingContentField{Kind: "text", Value: strings.TrimSpace(n.Data), Label: diyContentLabel(n.Parent), MaxLength: 16000}, text: []*html.Node{n}})
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child, suppressed, depth+1)
		}
	}
	walk(root, false, 0)
	if parseErr != nil {
		return nil, parseErr
	}
	label := ""
	for _, button := range doc.buttons {
		if button.Data == "input" {
			label = strings.TrimSpace(diyAttr(button, "value"))
		} else {
			label = diyContentJoinedText(diyContentTextNodes(button))
		}
		if label != "" {
			break
		}
	}
	doc.targets = append(doc.targets, &diyContentTarget{field: DIYLandingContentField{ID: "document-title", Kind: "title", Value: diyContentTitleText(title), MaxLength: 300}})
	doc.targets = append(doc.targets, &diyContentTarget{field: DIYLandingContentField{ID: "primary-label", Kind: "button", Value: label, MaxLength: 80}})
	return doc, nil
}

func diyContentHeadTitle(document *html.Node) (*html.Node, *html.Node) {
	var head, title *html.Node
	var findHead func(*html.Node)
	findHead = func(n *html.Node) {
		if head != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "head" {
			head = n
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				if child.Type == html.ElementNode && child.Data == "title" {
					title = child
					break
				}
			}
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			findHead(child)
		}
	}
	findHead(document)
	return head, title
}

func diyContentTitleText(title *html.Node) string {
	if title == nil {
		return ""
	}
	var text strings.Builder
	for child := title.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			text.WriteString(child.Data)
		}
	}
	return strings.TrimSpace(text.String())
}

func diyContentBackgroundURL(css string, position int) bool {
	start, colon, nesting := 0, -1, 0
	var quote byte
	for index := 0; index < position; index++ {
		ch := css[index]
		if quote != 0 {
			if ch == '\\' {
				index++
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '/' && index+1 < position && css[index+1] == '*' {
			end := strings.Index(css[index+2:position], "*/")
			if end < 0 {
				return false
			}
			index += end + 3
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == '(' {
			nesting++
			continue
		}
		if ch == ')' && nesting > 0 {
			nesting--
			continue
		}
		if nesting != 0 {
			continue
		}
		switch ch {
		case ';', '{', '}':
			start, colon = index+1, -1
		case ':':
			if colon < 0 {
				colon = index
			}
		}
	}
	if colon < start {
		return false
	}
	var propertyText strings.Builder
	for index := start; index < colon; index++ {
		if css[index] == '/' && index+1 < colon && css[index+1] == '*' {
			end := strings.Index(css[index+2:colon], "*/")
			if end < 0 {
				return false
			}
			index += end + 3
			continue
		}
		propertyText.WriteByte(css[index])
	}
	property := strings.ToLower(strings.TrimSpace(propertyText.String()))
	return property == "background" || property == "background-image" || property == "border-image" || property == "border-image-source" || property == "list-style-image" || property == "mask-image" || property == "-webkit-mask-image"
}

func diyContentButton(n *html.Node, selectors []diySelector) bool {
	if n.Type != html.ElementNode {
		return false
	}
	if diyAttr(n, "data-d1-action") == "primary" {
		return true
	}
	for _, selector := range selectors {
		if selector.matches(n) {
			return true
		}
	}
	// In-page navigation keeps its original role unless the template explicitly
	// binds it through data-d1-action or the configured primary selector.
	if n.Data == "a" && strings.HasPrefix(strings.TrimSpace(diyAttr(n, "href")), "#") {
		return false
	}
	if n.Data == "button" || strings.EqualFold(diyAttr(n, "role"), "button") {
		return true
	}
	if n.Data == "input" {
		switch strings.ToLower(diyAttr(n, "type")) {
		case "button", "submit", "image", "reset":
			return true
		}
	}
	if n.Data != "a" {
		return false
	}
	classes := map[string]bool{}
	for _, class := range strings.Fields(strings.ToLower(diyAttr(n, "class"))) {
		classes[class] = true
		switch class {
		case "download-btn", "download-button", "btn-download", "install-btn", "install-button", "btn-install", "cta", "cta-button":
			return true
		}
	}
	return (classes["btn"] || classes["button"]) &&
		(classes["primary"] || classes["btn-primary"] || classes["button-primary"] || classes["primary-button"] || classes["primary-btn"] || classes["download"] || classes["install"])
}

func diyContentLabel(n *html.Node) string {
	if n == nil {
		return ""
	}
	label := strings.TrimSpace(diyAttr(n, "data-d1-label"))
	if utf8.RuneCountInString(label) > 100 {
		label = string([]rune(label)[:100])
	}
	return label
}

func diyContentInvisible(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.Data {
	case "title", "script", "noscript", "template", "svg", "math", "canvas", "iframe", "object", "embed":
		return true
	}
	for _, attr := range n.Attr {
		if attr.Key == "hidden" {
			return true
		}
	}
	style := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(diyAttr(n, "style"), " ", ""), "\t", ""))
	if strings.Contains(style, "display:none") || strings.Contains(style, "visibility:hidden") {
		return true
	}
	for _, class := range strings.Fields(strings.ToLower(diyAttr(n, "class"))) {
		if class == "sr-only" || class == "visually-hidden" {
			return true
		}
	}
	return false
}

func diyContentNonText(n *html.Node) bool {
	if strings.EqualFold(diyAttr(n, "aria-hidden"), "true") {
		return true
	}
	for _, class := range strings.Fields(strings.ToLower(diyAttr(n, "class"))) {
		if class == "icon" || class == "iconfont" || class == "material-icons" || strings.HasPrefix(class, "material-symbols") || class == "fa" || class == "fas" || class == "far" || class == "fab" {
			return true
		}
	}
	return false
}

func diyContentTextContainer(n *html.Node) bool {
	switch n.Data {
	case "h1", "h2", "h3", "h4", "h5", "h6", "p", "li", "blockquote", "figcaption", "label", "td", "th", "dt", "dd", "summary":
		return true
	}
	return diyAttr(n, "data-d1-bind") == "app-name"
}

func diyContentTextNodes(n *html.Node) []*html.Node {
	var nodes []*html.Node
	var walk func(*html.Node, int)
	walk = func(current *html.Node, depth int) {
		if depth > 128 || current != n && (diyContentInvisible(current) || diyContentNonText(current) || current.Type == html.ElementNode && (current.Data == "style" || diyContentButton(current, nil))) {
			return
		}
		if current.Type == html.TextNode && strings.TrimSpace(current.Data) != "" {
			nodes = append(nodes, current)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child, depth+1)
		}
	}
	walk(n, 0)
	return nodes
}

func diyContentJoinedText(nodes []*html.Node) string {
	var value strings.Builder
	for _, node := range nodes {
		value.WriteString(node.Data)
	}
	return strings.TrimSpace(value.String())
}

func setDIYContentText(nodes []*html.Node, value string) {
	for index, node := range nodes {
		if index == 0 {
			node.Data = value
		} else {
			node.Data = ""
		}
		for parent := node.Parent; parent != nil; parent = parent.Parent {
			if diyAttr(parent, "data-d1-bind") == "app-name" {
				diyRemoveAttr(parent, "data-d1-bind")
			}
		}
	}
}

func setDIYContentButtonLabel(button *html.Node, label string) {
	diySetAttr(button, "data-d1-custom-label", label)
	diySetAttr(button, "aria-label", label)
	if button.Data == "input" {
		diySetAttr(button, "value", label)
		diySetAttr(button, "data-d1-bind", "primary-label")
		return
	}
	var removeBindings func(*html.Node)
	removeBindings = func(n *html.Node) {
		if n.Type == html.ElementNode && diyAttr(n, "data-d1-bind") == "primary-label" {
			diyRemoveAttr(n, "data-d1-bind")
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			removeBindings(child)
		}
	}
	removeBindings(button)
	nodes := diyContentTextNodes(button)
	span := &html.Node{Type: html.ElementNode, Data: "span", Attr: []html.Attribute{{Key: "data-d1-bind", Val: "primary-label"}}}
	span.AppendChild(&html.Node{Type: html.TextNode, Data: label})
	if len(nodes) == 0 {
		button.AppendChild(span)
		return
	}
	first := nodes[0]
	first.Parent.InsertBefore(span, first)
	first.Parent.RemoveChild(first)
	for _, node := range nodes[1:] {
		node.Data = ""
	}
}

func validDIYContentImage(value string) bool {
	if value == "" {
		return true
	}
	if diyManagedImagePath(value) != "" {
		return true
	}
	if strings.IndexFunc(value, unicode.IsSpace) >= 0 || strings.ContainsAny(value, "<>\"'\\") {
		return false
	}
	if strings.HasPrefix(value, "data:") {
		parts := strings.SplitN(value, ",", 2)
		if len(parts) != 2 || len(parts[1]) > base64.StdEncoding.EncodedLen(maxDIYContentImageBytes) {
			return false
		}
		formats := map[string]string{"data:image/png;base64": "png", "data:image/jpeg;base64": "jpeg", "data:image/gif;base64": "gif", "data:image/webp;base64": "webp"}
		expected := formats[strings.ToLower(parts[0])]
		if expected == "" {
			return false
		}
		data, err := base64.StdEncoding.Strict().DecodeString(parts[1])
		if err != nil || len(data) > maxDIYContentImageBytes {
			return false
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || format != expected || config.Width <= 0 || config.Height <= 0 || config.Width > 4096 || config.Height > 4096 {
			return false
		}
		_, format, err = image.Decode(bytes.NewReader(data))
		return err == nil && format == expected
	}
	if len(value) > 4096 {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Port() != "" && u.Port() != "443" {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".lan") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return diyPublicIP(ip)
	}
	return true
}
