package platform

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// Shared with the editor so its one-click defaults match the public renderer.
//
//go:embed templates/page-locales.json
var pageLocaleJSON []byte

type pageLanguageCopy struct {
	DownloadButton string `json:"download_button"`
	PageTitle      string `json:"page_title"`
	Tagline        string `json:"tagline"`
	Platform       string `json:"platform"`
	PackageSize    string `json:"package_size"`
	Downloads      string `json:"downloads"`
	Rating         string `json:"rating"`
	Reviews        string `json:"reviews"`
	Screenshots    string `json:"screenshots"`
	AboutApp       string `json:"about_app"`
	AppInfo        string `json:"app_info"`
	Version        string `json:"version"`
	VersionNumber  string `json:"version_number"`
	Package        string `json:"package"`
	AppIcon        string `json:"app_icon"`
	AdConsent      string `json:"ad_consent"`
	ResumeNote     string `json:"resume_note"`
	OpenApp        string `json:"open_app"`
}

var pageLanguages = func() map[string]pageLanguageCopy {
	var languages map[string]pageLanguageCopy
	if err := json.Unmarshal(pageLocaleJSON, &languages); err != nil {
		panic("invalid embedded page languages: " + err.Error())
	}
	return languages
}()

// Legacy designs have no language field and keep their original Chinese copy.
func (d PageDesign) LanguageCode() string {
	if _, supported := pageLanguages[d.Language]; supported {
		return d.Language
	}
	if apkLanguageButtons[apkLocaleCode(d.Language)] != "" {
		return apkLocaleCode(d.Language)
	}
	return "zh-CN"
}

func (d PageDesign) Copy() pageLanguageCopy {
	code := d.LanguageCode()
	if copy, ok := pageLanguages[code]; ok {
		return copy
	}
	if copy, ok := pageLanguages[strings.Split(code, "-")[0]]; ok {
		return copy
	}
	return pageLanguages["en"]
}

func (d PageDesign) DownloadButtonText() string {
	text := strings.TrimSpace(d.ButtonText)
	defaultText := apkLanguageButtons[apkLocaleCode(d.Language)]
	if defaultText == "" {
		defaultText = d.Copy().DownloadButton
	}
	for _, copy := range apkLanguageButtons {
		if text == copy {
			return defaultText
		}
	}
	for _, copy := range pageLanguages {
		if text == copy.DownloadButton {
			return defaultText
		}
	}
	return d.ButtonText // Preserve a publisher's explicitly customized wording.
}
