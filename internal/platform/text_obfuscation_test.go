package platform

import (
	"html"
	"regexp"
	"strings"
	"testing"
)

func TestAPKTextObfuscationPreservesVisibleNameAndEscapesHTML(t *testing.T) {
	noise := regexp.MustCompile(`<span class="d1-text-noise" aria-hidden="true">[a-f0-9]{2}</span>`)
	tags := regexp.MustCompile(`<[^>]*>`)
	for _, title := range []string{"LINK APP", "繁體應用", "e\u0301 👩‍💻", `<img src=x onerror=alert(1)> & "brand"`} {
		d := PageDesign{Title: title}
		if got := string(d.TitleForDisplay()); got != html.EscapeString(title) {
			t.Fatal("legacy/off setting changed the visible title")
		}
		d.Landing = &APKLandingOptions{TextObfuscation: true}
		got := string(d.TitleForDisplay())
		if !noise.MatchString(got) || html.UnescapeString(tags.ReplaceAllString(noise.ReplaceAllString(got, ""), "")) != title || strings.Contains(got, "<img") {
			t.Fatal("noise changed visible text or allowed markup injection")
		}
		if strings.Contains(title, "e\u0301") && !strings.Contains(got, "e\u0301") {
			t.Fatal("combining marks were split")
		}
	}
}
