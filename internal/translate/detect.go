package translate

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/abadojack/whatlanggo"
)

// DetectionResult is what Detect returns for one message body.
type DetectionResult struct {
	// Language is the detected ISO 639-1 code ("" when nothing was
	// detected with enough confidence).
	Language string `json:"language"`
	// LanguageName is the English name ("Urdu", "German").
	LanguageName string `json:"languageName"`
	// Confidence is the detector's confidence, 0-1.
	Confidence float64 `json:"confidence"`
	// Source names how the language was found: "detector" or
	// "roman-urdu" (the local wordlist rule below).
	Source string `json:"source"`
	// ShowBanner is true when the viewer should offer translation.
	ShowBanner bool `json:"showBanner"`
}

// romanUrduWords is the local wordlist for the Roman-Urdu rule. Roman
// Urdu is Urdu written in Latin letters, so a trigram detector reads it
// as English (or guesses at random). When the detector says English but
// a large share of the words are on this list, the text is treated as
// Urdu. This is a clearly-marked local rule for one known case, not a
// general Roman-Urdu claim: it only fires when Roman-Urdu words dominate
// the text (see romanUrduMinHits / romanUrduMinRatio).
var romanUrduWords = map[string]bool{
	"hai": true, "hain": true, "hoon": true, "ho": true, "tha": true,
	"thi": true, "the": true, "hoga": true, "hogi": true, "honge": true,
	"ka": true, "ki": true, "ke": true, "ko": true, "se": true,
	"mein": true, "main": true, "mera": true, "meri": true, "mere": true,
	"aap": true, "aapka": true, "aapki": true, "tum": true, "tumhara": true,
	"nahi": true, "nahin": true, "na": true, "mat": true,
	"kar": true, "karo": true, "karna": true, "karta": true, "karti": true,
	"karte": true, "kiya": true, "kiye": true, "hua": true, "hui": true,
	"raha": true, "rahi": true, "rahe": true,
	"ja": true, "jao": true, "jana": true, "jata": true, "aa": true,
	"aao": true, "aana": true, "aata": true,
	"kya": true, "kab": true, "kahan": true, "kaise": true, "kyun": true,
	"kyon": true, "kis": true, "kisko": true, "kitna": true, "kitne": true,
	"aur": true, "ya": true, "lekin": true, "magar": true, "phir": true,
	"ab": true, "abhi": true, "kal": true, "aaj": true, "parson": true,
	"mujhe": true, "mujh": true, "tujhe": true, "usko": true, "isko": true,
	"usne": true, "isne": true, "yeh": true, "ye": true, "woh": true,
	"wo": true, "aisa": true, "waisa": true,
	"batao": true, "bata": true, "sun": true, "suno": true, "dekho": true,
	"chahiye": true, "chahte": true, "milte": true, "milna": true,
	"baat": true, "kaam": true, "ghar": true,
	"paise": true, "acha": true, "theek": true,
	"shukriya": true, "bhai": true, "jaan": true, "yaar": true,
	"bohat": true, "bahut": true, "zaroor": true, "bilkul": true,
	"subah": true, "shaam": true, "raat": true, "din": true,
	"likh": true, "likha": true, "parh": true, "parha": true,
	"bhej": true, "bhejo": true,
}

// languageNames maps ISO 639-1 codes to English display names. It exists
// because whatlanggo.LangToString returns the ISO 639-2/3 code ("fra",
// "deu") rather than a name, so using it directly put codes in the
// banner ("This message is in fra."). Unknown codes fall back to the
// code itself in LanguageName.
var languageNames = map[string]string{
	"en": "English", "ur": "Urdu", "de": "German", "fr": "French",
	"es": "Spanish", "it": "Italian", "pt": "Portuguese", "nl": "Dutch",
	"ar": "Arabic", "hi": "Hindi", "fa": "Persian", "tr": "Turkish",
	"ru": "Russian", "ja": "Japanese", "zh": "Chinese",
	"pl": "Polish", "uk": "Ukrainian", "ko": "Korean", "vi": "Vietnamese",
	"id": "Indonesian", "ms": "Malay", "th": "Thai", "sv": "Swedish",
	"da": "Danish", "fi": "Finnish", "nb": "Norwegian", "no": "Norwegian",
	"cs": "Czech", "hu": "Hungarian", "el": "Greek", "he": "Hebrew",
	"bn": "Bengali", "ta": "Tamil", "te": "Telugu", "mr": "Marathi",
	"pa": "Punjabi", "gu": "Gujarati", "kn": "Kannada", "ml": "Malayalam",
	"ro": "Romanian", "bg": "Bulgarian", "hr": "Croatian", "sr": "Serbian",
	"sk": "Slovak", "sl": "Slovenian", "lt": "Lithuanian", "lv": "Latvian",
	"et": "Estonian", "sw": "Swahili", "am": "Amharic", "ne": "Nepali",
	"si": "Sinhala", "km": "Khmer", "lo": "Lao", "my": "Burmese",
	"ka": "Georgian", "hy": "Armenian", "az": "Azerbaijani", "kk": "Kazakh",
	"uz": "Uzbek", "mn": "Mongolian", "is": "Icelandic", "ga": "Irish",
	"cy": "Welsh", "eu": "Basque", "ca": "Catalan", "gl": "Galician",
	"af": "Afrikaans", "sq": "Albanian", "mk": "Macedonian", "bs": "Bosnian",
	"be": "Belarusian", "tl": "Tagalog", "ug": "Uyghur",
	"ht": "Haitian Creole", "yo": "Yoruba", "ig": "Igbo", "ha": "Hausa",
	"jw": "Javanese", "su": "Sundanese", "la": "Latin", "eo": "Esperanto",
}

const (
	// romanUrduMinHits is the fewest wordlist hits that can flip a
	// detector-English result to Urdu.
	romanUrduMinHits = 4
	// romanUrduMinRatio is the smallest share of the text's words that
	// must be on the wordlist for the flip. Mixed mail (a few Roman-Urdu
	// words inside English) stays English: the banner names the dominant
	// detected language. 0.30, not 0.40: realistic Roman Urdu mixes in
	// English nouns (project, file, meeting), so a 0.40 share is rarely
	// reached — a genuine Roman-Urdu mail scored 0.353 in the smoke
	// test and got no banner. The hits floor (4) still keeps short
	// code-switched asides from flipping.
	romanUrduMinRatio = 0.30
)

// Detect detects the language of one message body and says whether the
// viewer should offer translation. The input is plain text (BodyText, or
// BodyHTML with tags stripped); the quoted-history part is ignored.
// target is the resolved target language ("en" when the UI is English):
// text already in the target language gets no banner.
//
// Rules, in order:
//  1. Short text (under MinDetectRunes) or empty text: no banner.
//  2. The local detector (whatlanggo, pure Go, no cgo). Under
//     MinDetectConfidence: no banner.
//  3. The Roman-Urdu wordlist rule (see romanUrduWords).
//  4. Detected language == target: no banner.
func Detect(text, target string) DetectionResult {
	clean := StripQuotedText(text)
	clean = strings.TrimSpace(clean)
	if len([]rune(clean)) < MinDetectRunes {
		return DetectionResult{}
	}

	info := whatlanggo.Detect(clean)
	target = NormalizeLang(target)

	// Roman-Urdu rule first: the detector reads Latin-script Urdu as
	// English (or guesses a language with no ISO 639-1 code, such as
	// Ilocano). Only when Roman-Urdu words dominate the text.
	if hits, ratio := romanUrduScore(clean); hits >= romanUrduMinHits && ratio >= romanUrduMinRatio {
		return DetectionResult{
			Language:     "ur",
			LanguageName: "Urdu",
			Confidence:   ratio,
			Source:       "roman-urdu",
			ShowBanner:   target != "ur",
		}
	}

	lang := info.Lang.Iso6391()
	if lang == "" || lang == "und" {
		return DetectionResult{}
	}

	if info.Confidence < MinDetectConfidence {
		return DetectionResult{
			Language:     lang,
			LanguageName: LanguageName(lang),
			Confidence:   info.Confidence,
			Source:       "detector",
			ShowBanner:   false,
		}
	}
	return DetectionResult{
		Language:     lang,
		LanguageName: LanguageName(lang),
		Confidence:   info.Confidence,
		Source:       "detector",
		ShowBanner:   target == "" || lang != target,
	}
}

// romanUrduScore counts wordlist hits over the text's words.
func romanUrduScore(text string) (hits int, ratio float64) {
	words := splitWords(text)
	if len(words) == 0 {
		return 0, 0
	}
	for _, w := range words {
		if romanUrduWords[w] {
			hits++
		}
	}
	return hits, float64(hits) / float64(len(words))
}

// splitWords lower-cases the text and splits on anything that is not a
// letter, so "hain?" and "hain," both match the wordlist.
func splitWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
}

var (
	// quoteHeader matches the "On <date>, <someone> wrote:" line that
	// introduces quoted history in a plain-text body.
	quoteHeader = regexp.MustCompile(`(?m)^On .{0,120}wrote:\s*$`)
	// originalMessageHeader matches the "-----Original Message-----"
	// separator Outlook and others put above quoted history.
	originalMessageHeader = regexp.MustCompile(`(?m)^\s*-{3,}\s*Original Message\s*-{3,}\s*$`)
	// forwardedHeader matches the "Begin forwarded message:" line.
	forwardedHeader = regexp.MustCompile(`(?m)^\s*Begin forwarded message:\s*$`)
)

// StripQuotedText removes the quoted-history part of a plain-text body
// for detection: lines starting with ">" and everything after a quote
// header. The quoted part is still translated with the rest of the
// message; it is only left out of detection, so a long English thread
// under a short Urdu reply does not read as English.
func StripQuotedText(text string) string {
	// Cut at the first quote header.
	if loc := quoteHeader.FindStringIndex(text); loc != nil {
		text = text[:loc[0]]
	}
	if loc := originalMessageHeader.FindStringIndex(text); loc != nil {
		text = text[:loc[0]]
	}
	if loc := forwardedHeader.FindStringIndex(text); loc != nil {
		text = text[:loc[0]]
	}
	// Drop ">" quoted lines.
	lines := strings.Split(text, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), ">") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// StripHTMLTags reduces an HTML body to plain text for detection when no
// plain-text body exists. Tags are replaced with a space so words on
// either side of a tag do not join.
func StripHTMLTags(html string) string {
	var b strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
			b.WriteRune(' ')
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// LanguageName returns the English name for an ISO 639-1 code (or a
// locale such as "zh-Hans", whose base code is used), or the code itself
// when unknown.
func LanguageName(code string) string {
	if i := strings.Index(code, "-"); i >= 0 {
		code = code[:i]
	}
	if name, ok := languageNames[strings.ToLower(code)]; ok {
		return name
	}
	return code
}
