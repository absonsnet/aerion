package translate

import (
	"strings"
)

// Bergamot publishes one model per direction, always with English on
// one side: X→en and en→X. A pair with English on either side is a
// direct route; anything else pivots through English (two models, one
// engine call — translateViaPivoting — exactly as Firefox does).
//
// Route returns the legs for from→to, each leg a [2]string{from, to}.
// A nil return means the pair cannot be served by Bergamot at all.
func BergamotRoute(from, to string) [][2]string {
	from = NormalizeLang(from)
	to = NormalizeLang(to)
	if from == "" || to == "" || from == to {
		return nil
	}
	if from == "en" || to == "en" {
		return [][2]string{{from, to}}
	}
	return [][2]string{{from, "en"}, {"en", to}}
}

// NormalizeLang lower-cases a language code and maps the common
// regional variants to the base codes Bergamot and the online providers
// use. Chinese keeps its script tag: zh-Hans / zh-Hant are distinct
// Bergamot models.
func NormalizeLang(code string) string {
	code = strings.TrimSpace(strings.ToLower(code))
	if code == "" {
		return ""
	}
	if strings.HasPrefix(code, "zh") {
		if strings.Contains(code, "hant") || strings.Contains(code, "tw") || strings.Contains(code, "hk") {
			return "zh-Hant"
		}
		return "zh-Hans"
	}
	if i := strings.IndexAny(code, "-_"); i >= 0 {
		code = code[:i]
	}
	return code
}

// NormalizeLangForGoogle maps a code to what the Google endpoint
// expects (zh-Hans → zh-CN).
func NormalizeLangForGoogle(code string) string {
	switch NormalizeLang(code) {
	case "zh-Hans":
		return "zh-CN"
	case "zh-Hant":
		return "zh-TW"
	default:
		return NormalizeLang(code)
	}
}

// TargetLanguages is the picker list for translate_target_language:
// "system" first, then the languages Bergamot and the online providers
// cover well. The code is what is stored; the name is shown.
var TargetLanguages = []struct {
	Code string
	Name string
}{
	{"system", "System language"},
	{"en", "English"},
	{"ur", "Urdu"},
	{"de", "German"},
	{"fr", "French"},
	{"es", "Spanish"},
	{"it", "Italian"},
	{"pt", "Portuguese"},
	{"nl", "Dutch"},
	{"ar", "Arabic"},
	{"hi", "Hindi"},
	{"fa", "Persian"},
	{"tr", "Turkish"},
	{"ru", "Russian"},
	{"ja", "Japanese"},
	{"zh-Hans", "Chinese (Simplified)"},
}

// ValidTargetLanguage reports whether a stored target-language value is
// one the picker offers.
func ValidTargetLanguage(code string) bool {
	for _, l := range TargetLanguages {
		if l.Code == code {
			return true
		}
	}
	return false
}

// ResolveTargetLanguage turns the stored translate_target_language
// ("system" or a code) into a concrete code, using the UI language
// when the setting says "system". The UI language is a locale such as
// "en" or "de"; an empty one means English.
func ResolveTargetLanguage(stored, uiLanguage string) string {
	if stored != "" && stored != DefaultTargetLanguage {
		return NormalizeLang(stored)
	}
	if uiLanguage == "" {
		return "en"
	}
	return NormalizeLang(uiLanguage)
}
