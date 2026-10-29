package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

type DIYHTMLIssue struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

var diyCSSURL = regexp.MustCompile(`(?i)url\(\s*(?:"([^"\r\n]*)"|'([^'\r\n]*)'|([^\s)"']+))\s*\)`)
var diyCSSImport = regexp.MustCompile(`(?im)@import\s+(?:url\([^;]*\)|"[^";]*"|'[^';]*')[^;]*;?`)
var diySelectorTag = regexp.MustCompile(`^(?:[A-Za-z][A-Za-z0-9_-]*|\*)`)
var diySelectorPart = regexp.MustCompile(`^[.#][A-Za-z_][A-Za-z0-9_-]*`)
var diySelectorAttribute = regexp.MustCompile(`^\[([A-Za-z_][A-Za-z0-9_:-]*)(?:=(?:"([^"\]]*)"|'([^'\]]*)'|([A-Za-z0-9_-]+)))?\]`)

type diySelector struct {
	tag, id string
	classes []string
	attrs   map[string]*string
}

func diySelectors(raw string) ([]diySelector, error) {
	if raw == "" {
		return nil, nil
	}
	var selectors []diySelector
	for _, part := range strings.Split(raw, ",") {
		remaining := strings.TrimSpace(part)
		if remaining == "" {
			return nil, problem(400, "主按钮选择器无效，请使用标签、#id、.class 或属性选择器")
		}
		selector := diySelector{attrs: map[string]*string{}}
		if tag := diySelectorTag.FindString(remaining); tag != "" {
			selector.tag = strings.ToLower(tag)
			remaining = remaining[len(tag):]
		}
		for remaining != "" {
			if token := diySelectorPart.FindString(remaining); token != "" {
				if token[0] == '#' {
					selector.id = token[1:]
				} else {
					selector.classes = append(selector.classes, token[1:])
				}
				remaining = remaining[len(token):]
				continue
			}
			if token := diySelectorAttribute.FindStringSubmatch(remaining); token != nil {
				var value *string
				if strings.Contains(token[0], "=") {
					v := token[2] + token[3] + token[4]
					value = &v
				}
				selector.attrs[strings.ToLower(token[1])] = value
				remaining = remaining[len(token[0]):]
				continue
			}
			return nil, problem(400, "主按钮选择器无效，请使用标签、#id、.class 或属性选择器")
		}
		selectors = append(selectors, selector)
	}
	return selectors, nil
}
func (selector diySelector) matches(n *html.Node) bool {
	if n.Type != html.ElementNode || selector.tag != "" && selector.tag != "*" && selector.tag != n.Data || selector.id != "" && selector.id != diyAttr(n, "id") {
		return false
	}
	classes := strings.Fields(diyAttr(n, "class"))
	for _, wanted := range selector.classes {
		found := false
		for _, v := range classes {
			if v == wanted {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	for key, wanted := range selector.attrs {
		found := false
		for _, attr := range n.Attr {
			if attr.Key == key && (wanted == nil || attr.Val == *wanted) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func diyAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func diySetAttr(n *html.Node, key, value string) {
	for i := range n.Attr {
		if n.Attr[i].Key == key {
			n.Attr[i].Val = value
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: value})
}
func diyRemoveAttr(n *html.Node, key string) {
	attrs := n.Attr[:0]
	for _, a := range n.Attr {
		if a.Key != key {
			attrs = append(attrs, a)
		}
	}
	n.Attr = attrs
}
func diyResolveResource(raw, base string, image bool) string {
	raw = strings.TrimSpace(raw)
	if image && strings.HasPrefix(raw, "data:image/") && !strings.HasPrefix(strings.ToLower(raw), "data:image/svg") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		u.Scheme = "https"
	}
	if !u.IsAbs() && base != "" {
		b, e := url.Parse(base)
		if e == nil {
			u = b.ResolveReference(u)
		}
	}
	if u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(u.String()) > 4096 {
		return ""
	}
	u.Fragment = ""
	return u.String()
}
func diyRewriteCSS(css, base string, resource func(string, string) string) string {
	css = diyCSSImport.ReplaceAllString(css, "")
	return diyCSSURL.ReplaceAllStringFunc(css, func(match string) string {
		parts := diyCSSURL.FindStringSubmatch(match)
		raw := ""
		for _, p := range parts[1:] {
			if p != "" {
				raw = p
				break
			}
		}
		if strings.HasPrefix(raw, "#") {
			return match
		}
		if diyManagedImagePath(raw) != "" {
			return "url(" + diyCSSQuotedString(resource(raw, "image")) + ")"
		}
		if strings.HasPrefix(raw, "data:image/") || strings.HasPrefix(raw, "data:font/") {
			return match
		}
		u := diyResolveResource(raw, base, false)
		if u == "" {
			return `url("")`
		}
		kind := "image"
		p, _ := url.Parse(u)
		lower := strings.ToLower(p.Path)
		if strings.HasSuffix(lower, ".woff") || strings.HasSuffix(lower, ".woff2") || strings.HasSuffix(lower, ".ttf") || strings.HasSuffix(lower, ".otf") {
			kind = "font"
		}
		return "url(" + diyCSSQuotedString(resource(u, kind)) + ")"
	})
}

// CSS string escapes differ from JSON's \uXXXX escapes. In particular signed
// proxy URLs must retain '&' query separators instead of becoming \u0026.
func diyCSSQuotedString(value string) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, char := range value {
		switch {
		case char == '\\' || char == '"':
			out.WriteByte('\\')
			out.WriteRune(char)
		case char < 0x20 || char == 0x7f || char == '<' || char == '>':
			fmt.Fprintf(&out, "\\%x ", char)
		default:
			out.WriteRune(char)
		}
	}
	out.WriteByte('"')
	return out.String()
}

// The original source is stored separately. This browser document deliberately
// has no platform credentials or authority; all business actions live outside.
func prepareDIYHTML(raw string, options DIYLandingOptions) (string, []DIYHTMLIssue, error) {
	issues := []DIYHTMLIssue{}
	if len(raw) > maxDIYHTMLBytes {
		return "", issues, problem(413, "落地页代码最多 5 MiB")
	}
	if strings.TrimSpace(raw) == "" {
		return "", issues, problem(400, "请粘贴落地页 HTML 代码")
	}
	if err := normalizeDIYOptions(&options); err != nil {
		return "", issues, err
	}
	selectors, err := diySelectors(options.PrimarySelector)
	if err != nil {
		return "", issues, err
	}
	lower := strings.ToLower(raw)
	if strings.Count(lower, "<script") > strings.Count(lower, "</script") || strings.Count(lower, "<style") > strings.Count(lower, "</style") {
		return "", issues, problem(400, "代码中的 script 或 style 标签未完整闭合，请检查复制内容")
	}
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return "", issues, problem(400, "落地页 HTML 无法解析")
	}
	add := func(code, msg string) {
		for _, v := range issues {
			if v.Code == code {
				return
			}
		}
		issues = append(issues, DIYHTMLIssue{Code: code, Message: msg, Severity: "warning"})
	}
	buttons := 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			if c.Type == html.ElementNode {
				tag := strings.ToLower(c.Data)
				remove := tag == "base" || tag == "iframe" || tag == "frame" || tag == "object" || tag == "embed" || tag == "portal"
				if tag == "meta" && (diyAttr(c, "http-equiv") != "") {
					remove = true
				}
				if tag == "link" && strings.ToLower(diyAttr(c, "rel")) != "stylesheet" && strings.ToLower(diyAttr(c, "rel")) != "icon" {
					remove = true
				}
				if remove {
					n.RemoveChild(c)
					add("restricted_document", "已隔离原页面的框架、重定向、base 或安装声明，安装与跳转由应用管理。")
					c = next
					continue
				}
				if tag == "form" {
					diyRemoveAttr(c, "action")
					diyRemoveAttr(c, "method")
					add("form", "原页面的表单提交已阻止。")
				}
				if tag == "a" && diyAttr(c, "href") != "" {
					diySetAttr(c, "href", strings.TrimSpace(diyAttr(c, "href")))
				}
				if diyAttr(c, "data-d1-action") == "primary" {
					buttons++
				} else {
					for _, selector := range selectors {
						if selector.matches(c) {
							diySetAttr(c, "data-d1-action", "primary")
							buttons++
							break
						}
					}
					if diyAttr(c, "data-d1-action") != "primary" && tag == "a" && diyContentButton(c, nil) {
						diySetAttr(c, "data-d1-action", "primary")
						buttons++
					}
				}
				for _, key := range []string{"target", "formaction", "formtarget", "download", "ping", "srcdoc", "nonce", "integrity", "crossorigin"} {
					diyRemoveAttr(c, key)
				}
				if tag == "a" {
					href := diyAttr(c, "href")
					if href != "" && !strings.HasPrefix(href, "#") {
						diyRemoveAttr(c, "href")
						add("navigation", "普通外链已阻止；下载、安装和业务外跳请接入应用主按钮。")
					}
				}
				if diyAttr(c, "srcset") != "" {
					diyRemoveAttr(c, "srcset")
					add("srcset", "响应式图片请使用 src 或 CSS 背景图片。")
				}
				for _, key := range []string{"src", "href", "poster"} {
					value := diyAttr(c, key)
					if value == "" || strings.HasPrefix(value, "#") {
						continue
					}
					if key == "href" && tag != "link" {
						continue
					}
					resolved := diyResolveResource(value, options.ResourceBase, tag == "img" || key == "poster")
					if resolved == "" {
						diyRemoveAttr(c, key)
						add("resource", "存在无效或缺少基址的相对资源，请填写 HTTPS 资源基址或使用完整地址。")
					} else {
						diySetAttr(c, key, resolved)
					}
				}
				if tag == "script" && diyAttr(c, "type") == "module" {
					add("module", "模块依赖不会自动打包，请粘贴浏览器可直接运行的 HTML。")
				}
				if v := diyAttr(c, "style"); v != "" {
					diySetAttr(c, "style", diyRewriteCSS(v, options.ResourceBase, func(u, _ string) string { return u }))
				}
				if tag == "style" {
					for t := c.FirstChild; t != nil; t = t.NextSibling {
						if t.Type == html.TextNode {
							t.Data = diyRewriteCSS(t.Data, options.ResourceBase, func(u, _ string) string { return u })
						}
					}
				}
			}
			walk(c)
			c = next
		}
	}
	walk(doc)
	if !options.PlatformButton && buttons == 0 {
		return "", issues, problem(400, "请绑定应用主按钮，或开启平台主按钮")
	}
	if buttons == 0 {
		add("primary_button", "未找到页面主按钮，将使用平台主按钮。")
	}
	var out bytes.Buffer
	if err := html.Render(&out, doc); err != nil {
		return "", issues, err
	}
	return out.String(), issues, nil
}

func diyContentDocument(prepared string, options DIYLandingOptions, channel, mode, label, name, assetBase string, preview bool, resource func(string, string) string) (string, error) {
	doc, err := html.Parse(strings.NewReader(prepared))
	if err != nil {
		return "", err
	}
	var head *html.Node
	selectors, err := diySelectors(options.PrimarySelector)
	if err != nil {
		return "", err
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "head" {
				head = n
			}
			// This also adapts old immutable revisions whose obvious download
			// links were stripped before automatic CTA recognition was added.
			if n.Data == "a" && diyContentButton(n, selectors) {
				diySetAttr(n, "data-d1-action", "primary")
				if !diyHasPrimaryLabel(n) {
					text := diyContentJoinedText(diyContentTextNodes(n))
					if text == "" {
						text = label
					}
					setDIYContentButtonLabel(n, text)
				}
			}
			for _, key := range []string{"src", "href", "poster"} {
				raw := diyAttr(n, key)
				if !strings.HasPrefix(raw, "https://") && diyManagedImagePath(raw) == "" {
					continue
				}
				kind := "image"
				if n.Data == "script" {
					kind = "script"
				}
				if n.Data == "link" && strings.EqualFold(diyAttr(n, "rel"), "stylesheet") {
					kind = "style"
				}
				if key != "poster" && (n.Data == "video" || n.Data == "audio" || n.Data == "source") {
					kind = "media"
				}
				diySetAttr(n, key, resource(raw, kind))
			}
			if v := diyAttr(n, "style"); v != "" {
				diySetAttr(n, "style", diyRewriteCSS(v, options.ResourceBase, resource))
			}
			if n.Data == "style" {
				for t := n.FirstChild; t != nil; t = t.NextSibling {
					if t.Type == html.TextNode {
						t.Data = diyRewriteCSS(t.Data, options.ResourceBase, resource)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if head == nil {
		return "", fmt.Errorf("DIY document has no head")
	}
	config, _ := json.Marshal(map[string]any{"channel": channel, "mode": mode, "label": label, "name": name, "preview": preview, "selector": options.PrimarySelector})
	script := &html.Node{Type: html.ElementNode, Data: "script"}
	script.AppendChild(&html.Node{Type: html.TextNode, Data: strings.Replace(diyChildJS, "/*CONFIG*/", string(config), 1)})
	head.InsertBefore(script, head.FirstChild)
	meta := &html.Node{Type: html.ElementNode, Data: "meta", Attr: []html.Attribute{{Key: "name", Val: "referrer"}, {Key: "content", Val: "no-referrer"}}}
	head.InsertBefore(meta, head.FirstChild)
	var out bytes.Buffer
	err = html.Render(&out, doc)
	return out.String(), err
}

func diyHasPrimaryLabel(n *html.Node) bool {
	if n.Type == html.ElementNode && diyAttr(n, "data-d1-bind") == "primary-label" {
		return true
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if diyHasPrimaryLabel(child) {
			return true
		}
	}
	return false
}

const diyChildJS = `(function(){'use strict';const config=/*CONFIG*/;let request=0;const send=data=>parent.postMessage(Object.assign({type:'d1-diy',channel:config.channel},data),'*');
if(window.history){const replace=window.history.replaceState.bind(window.history);try{for(const target of [window.history,Object.getPrototypeOf(window.history)])Object.defineProperties(target,{pushState:{value:(state,title)=>replace(state,title),writable:false,configurable:false},replaceState:{value:(state,title)=>replace(state,title),writable:false,configurable:false},back:{value:()=>{},writable:false,configurable:false},forward:{value:()=>{},writable:false,configurable:false},go:{value:()=>{},writable:false,configurable:false}});}catch(_){}}
function mock(){let status=document.getElementById('d1-diy-preview-status');if(!status){status=document.createElement('p');status.id='d1-diy-preview-status';status.setAttribute('role','status');Object.assign(status.style,{position:'fixed',bottom:'12px',left:'12px',right:'12px',padding:'14px',background:'#182d43',color:'white',zIndex:2147483647,borderRadius:'8px',font:'14px sans-serif'});document.body.append(status);}status.textContent=config.mode==='apk'?'预览：将执行应用下载或已配置的外部跳转。':'预览：将执行应用安装、Play 或已配置的外部跳转。';}
const primary=()=>{if(config.preview){mock();return;}send({action:'primary',request:++request});};Object.defineProperty(window,'D1Landing',{value:Object.freeze({primary}),writable:false});
function matches(target){const node=target?.closest?.('[data-d1-action="primary"]');if(node)return node;if(config.selector){try{return target?.closest?.(config.selector);}catch(_){}}return null;}
document.addEventListener('click',event=>{const node=matches(event.target);if(node){event.preventDefault();event.stopImmediatePropagation();if(event.isTrusted)primary();return;}const link=event.target?.closest?.('a');if(link){event.preventDefault();const href=link.getAttribute('href');if(href?.startsWith('#')){try{document.getElementById(decodeURIComponent(href.slice(1)))?.scrollIntoView();}catch(_){}}}},true);
window.addEventListener('popstate',()=>send({action:'back'}));let interacted=false;const interaction=event=>{if(event.isTrusted&&!interacted){interacted=true;send({action:'interaction'});}};document.addEventListener('pointerdown',interaction,true);document.addEventListener('keydown',interaction,true);
document.addEventListener('submit',event=>{event.preventDefault();event.stopImmediatePropagation();},true);
window.addEventListener('message',event=>{if(event.source!==parent||event.data?.channel!==config.channel)return;if(event.data.type==='d1-diy-hello'){send({action:'ready'});return;}if(event.data.type!=='d1-diy-state')return;const state=event.data;const label=state.label||config.label;document.querySelectorAll('[data-d1-bind="primary-label"]').forEach(node=>{if(node.tagName==='INPUT')node.value=label;else node.textContent=label;});const buttons=new Set(document.querySelectorAll('[data-d1-action="primary"]'));if(config.selector){try{document.querySelectorAll(config.selector).forEach(node=>buttons.add(node));}catch(_){}}buttons.forEach(node=>{if('disabled'in node)node.disabled=!!state.disabled;if(node.hasAttribute('data-d1-custom-label'))node.setAttribute('aria-label',label);});});
// CDN analytics may be injected after origin rendering. A blocked optional
// beacon is not a broken template; keep reporting all other resource failures.
function optionalAnalyticsFailure(event){if(event.message||event.target?.tagName!=='SCRIPT')return false;try{const url=new URL(event.target.src);return url.protocol==='https:'&&url.host==='static.cloudflareinsights.com'&&(url.pathname==='/beacon.min.js'||url.pathname.startsWith('/beacon.min.js/'));}catch(_){return false;}}
window.addEventListener('error',event=>{if(optionalAnalyticsFailure(event))return;const resource=event.target?.src||event.target?.href;send({action:'diagnostic',message:String(event.message||(resource?'Resource unavailable: '+String(resource).split('?')[0]:'Script error')).slice(0,200)});},true);document.addEventListener('DOMContentLoaded',()=>{document.querySelectorAll('[data-d1-bind="app-name"]').forEach(node=>node.textContent=config.name);send({action:'ready'});});})();`
