package platform

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed templates/apk-language-buttons.json
var apkLanguageButtonsJSON []byte

var apkLanguageButtons = func() map[string]string {
	var out map[string]string
	if err := json.Unmarshal(apkLanguageButtonsJSON, &out); err != nil {
		panic(err)
	}
	return out
}()

func apkLocaleCode(code string) string {
	aliases := map[string]string{"en": "en-US", "ja": "ja-JP", "ko": "ko-KR", "de": "de-DE"}
	if alias := aliases[code]; alias != "" {
		return alias
	}
	return code
}

func validateAPKLocales(d PageDesign, origin string) error {
	if len(d.Localizations) == 0 {
		if len(d.LanguagePriority) > 0 {
			return problem(400, "请选择支持的详情页语言")
		}
		return nil
	}
	if len(d.Localizations) > len(apkLanguageButtons) || len(d.LanguagePriority) != len(d.Localizations) {
		return problem(400, "请选择支持的详情页语言")
	}
	seen := map[string]bool{}
	for _, locale := range d.Localizations {
		code := apkLocaleCode(locale.Language)
		if seen[code] || apkLanguageButtons[code] == "" || len(locale.Localizations) > 0 || len(locale.LanguagePriority) > 0 {
			return problem(400, "请选择支持的详情页语言")
		}
		seen[code] = true
		locale.ExternalBrowser = d.ExternalBrowser
		if err := validatePageDesign(locale, origin); err != nil {
			return err
		}
	}
	priority := map[string]bool{}
	for _, code := range d.LanguagePriority {
		key := apkLocaleCode(code)
		if !seen[key] || priority[key] {
			return problem(400, "请选择支持的详情页语言")
		}
		priority[key] = true
	}
	return nil
}

// Share the PWA browser-language matching and configured fallback order.
func (d PageDesign) localized(requested string) PageDesign {
	if len(d.Localizations) == 0 {
		return d
	}
	c := PWALandingConfig{LanguagePriority: append([]string(nil), d.LanguagePriority...)}
	for _, v := range d.Localizations {
		c.StoreInfos = append(c.StoreInfos, PWALandingLanguage{Language: apkLocaleCode(v.Language)})
	}
	for i := range c.LanguagePriority {
		c.LanguagePriority[i] = apkLocaleCode(c.LanguagePriority[i])
	}
	selected := pwaLandingLanguage(c, strings.TrimSpace(requested)).Language
	for _, v := range d.Localizations {
		if apkLocaleCode(v.Language) == selected {
			// Like PWA, return-flow behavior is app-wide, not per language.
			// Keep the rendered switch consistent with its existing billing gate.
			v.ReturnFlowConfig = d.ReturnFlowConfig
			v.ExternalBrowser = d.ExternalBrowser
			return v
		}
	}
	fallback := d.Localizations[0]
	fallback.ReturnFlowConfig = d.ReturnFlowConfig
	fallback.ExternalBrowser = d.ExternalBrowser
	return fallback
}
