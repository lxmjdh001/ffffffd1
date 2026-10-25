package platform

import (
	"crypto/rand"
	"fmt"
	"html/template"
	"strings"
)

type landingDecoration struct {
	Class string
	Noise template.HTML
}

// Stable per owner and page kind, without putting the internal user ID in HTML.
// Functional IDs/data attributes remain unchanged so downloads and installs do
// not depend on either this class name or the per-render decorative fragment.
func (s *Server) landingClass(kind, owner string) string {
	return "d1-" + kind + s.mac("landing-class:" + kind + ":" + owner)[:16] + "-reference"
}

func (s *Server) newLandingDecoration(kind, owner string) landingDecoration {
	return landingDecoration{Class: s.landingClass(kind, owner), Noise: landingNoise()}
}

// Entirely generated, never derived from customer text. The template's content
// is inert: no scripts, requests, active styles, layout or event listeners.
// It is only a source-code presentation variation, not a security guarantee.
func landingNoise() template.HTML {
	var entropy [160]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		panic(err)
	}
	letters := func(start, count int) string {
		var text strings.Builder
		for _, b := range entropy[start : start+count] {
			text.WriteByte('a' + b%26)
		}
		return text.String()
	}
	name, variable := letters(0, 16), letters(16, 12)
	sizes := [...]int{1, 2, 3, 5}
	vertical := [...]string{"top", "bottom"}
	horizontal := [...]string{"left", "right"}
	properties := []string{
		"position:fixed", vertical[entropy[28]%2] + ":0", horizontal[entropy[29]%2] + ":0",
		fmt.Sprintf("width:%dpx", sizes[entropy[30]%4]), fmt.Sprintf("height:%dpx", sizes[entropy[31]%4]),
		fmt.Sprintf("--%s:#%x", variable, entropy[32:35]), "background:var(--" + variable + ")",
		fmt.Sprintf("border:%dpx %s #%x", sizes[entropy[35]%4], []string{"solid", "dotted", "dashed"}[entropy[36]%3], entropy[37:40]),
		fmt.Sprintf("border-radius:%dpx", sizes[entropy[40]%4]), "opacity:0", "pointer-events:none",
	}
	for i := len(properties) - 1; i > 0; i-- {
		j := int(entropy[41+i]) % (i + 1)
		properties[i], properties[j] = properties[j], properties[i]
	}
	// Comments contain only random lowercase English letters. This fragment must
	// remain inside template.content; never clone or append it to the live DOM.
	return template.HTML(fmt.Sprintf(`<!--%s--><template hidden aria-hidden="true" data-d1-decoration><style>.%s{%s}</style><!--%s--><span class="%s"></span></template>`, letters(64, 32), name, strings.Join(properties, ";"), letters(96, 48), name))
}

func apkReferenceStyles(class string) template.CSS {
	return template.CSS(strings.ReplaceAll(apkLandingCSS, ".d1-apk-reference", "."+class) + "\n" + apkTextObfuscationCSS + "\n" + returnFlowCSS + "\n" + externalBrowserCSS)
}
