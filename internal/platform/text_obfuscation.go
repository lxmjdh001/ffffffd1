package platform

import (
	"html"
	"html/template"
	"strings"
	"unicode"
)

// This is a presentation effect, not encryption or an AI/review guarantee.
// HTML is always escaped; only our generated, aria-hidden noise is trusted.
func (d PageDesign) TitleForDisplay() template.HTML {
	if !normalizedAPKLanding(d.Landing).TextObfuscation {
		return template.HTML(html.EscapeString(d.Title))
	}
	var out strings.Builder
	out.WriteString(`<span class="d1-obfuscated-text" data-d1-display="` + html.EscapeString(d.Title) + `"><span class="d1-obfuscated-value" aria-hidden="true">`)
	runes := []rune(d.Title)
	for i, r := range runes {
		out.WriteString(html.EscapeString(string(r)))
		// Keep combining marks, joining characters and emoji sequences together.
		if i+1 < len(runes) {
			next := runes[i+1]
			if unicode.IsMark(next) || r == '\u200d' || next == '\u200d' || (next >= 0x1f3fb && next <= 0x1f3ff) || (r >= 0x1f1e6 && r <= 0x1f1ff && next >= 0x1f1e6 && next <= 0x1f1ff) {
				continue
			}
		}
		out.WriteString(`<span class="d1-text-noise" aria-hidden="true">` + randomHex(1) + `</span>`)
	}
	out.WriteString(`</span></span>`)
	return template.HTML(out.String())
}
