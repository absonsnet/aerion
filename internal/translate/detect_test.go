package translate

import (
	"strings"
	"testing"
)

// Fixed sample messages with expected banner/no-banner outcomes. The
// samples are long enough to pass the MinDetectRunes gate unless the
// case is about the gate itself.
func TestDetect_Samples(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		target   string
		wantLang string // "" = no banner
		wantSrc  string
	}{
		{
			name: "Urdu script",
			text: "السلام علیکم، میں اپنے آرڈر کے بارے میں لکھ رہا ہوں۔ کیا آپ براہ کرم ڈیلیوری کی تاریخ کی تصدیق کر سکتے ہیں؟ بہت شکریہ، میں آپ کے جواب کا انتظار کروں گا۔",
			wantLang: "ur", wantSrc: "detector",
		},
		{
			name: "German",
			text: "Guten Morgen. Wie geht es Ihnen heute? Ich habe eine Frage zu meiner Bestellung und würde gerne wissen, wann sie geliefert wird. Vielen Dank für Ihre Hilfe.",
			wantLang: "de", wantSrc: "detector",
		},
		{
			name: "French",
			text: "Bonjour, je vous écris au sujet de ma commande. Pourriez-vous me confirmer la date de livraison s'il vous plaît ? Merci beaucoup pour votre aide.",
			wantLang: "fr", wantSrc: "detector",
		},
		{
			name: "Roman Urdu",
			text: "aap kal aa rahe hain? mujhe batao ke meeting kab hai aur kya karna hai, mein ghar par hoon aur aap ka intezar kar raha hoon, batao kab milte hain",
			wantLang: "ur", wantSrc: "roman-urdu",
		},
		{
			name: "Roman Urdu at realistic density",
			text: "Assalam-o-Alaikum, kal subah das baje hum milte hain aur us ke baad hum us project par kaam karenge jo pichle hafte se ruka hua hai. Aap apni file le kar aaiye ga.",
			wantLang: "ur", wantSrc: "roman-urdu",
		},
		{
			name: "English gets no banner",
			text: "Hello, I am writing about my order. Could you please confirm the delivery date? Thank you very much for your help and support.",
			wantLang: "",
		},
		{
			name:     "short Roman Urdu gets no banner",
			text:     "Ok, kal milte hain",
			wantLang: "",
		},
		{
			name:     "short text gets no banner",
			text:     "Merci beaucoup !",
			wantLang: "",
		},
		{
			name: "mixed English with a little Roman Urdu stays English",
			text: "Hi team, the meeting is at ten in the office tomorrow. Please bring the report and the slides. Let me know if anything changes today.",
			wantLang: "",
		},
		{
			name: "German text with German target gets no banner",
			text: "Guten Morgen. Wie geht es Ihnen heute? Ich habe eine Frage zu meiner Bestellung und würde gerne wissen, wann sie geliefert wird. Vielen Dank für Ihre Hilfe.",
			target:   "de",
			wantLang: "",
		},
		{
			name: "quoted history is ignored",
			text: "Vielen Dank für Ihre schnelle Antwort, ich freue mich sehr darüber und werde Ihnen bald schreiben.\n\nOn 14 November 2026, John wrote:\n> Hello, I am writing about my order. Could you please confirm the delivery date? Thank you very much for your help and support today.",
			wantLang: "de", wantSrc: "detector",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := tc.target
			if target == "" {
				target = "en"
			}
			got := Detect(tc.text, target)
			if tc.wantLang == "" {
				if got.ShowBanner {
					t.Errorf("Detect(%q) shows banner for %q (conf %.2f, src %s), want none", tc.text, got.Language, got.Confidence, got.Source)
				}
				return
			}
			if !got.ShowBanner {
				t.Fatalf("Detect(%q) shows no banner, want %q (conf %.2f, src %s)", tc.text, tc.wantLang, got.Confidence, got.Source)
			}
			if got.Language != tc.wantLang {
				t.Errorf("Detect language = %q, want %q", got.Language, tc.wantLang)
			}
			if tc.wantSrc != "" && got.Source != tc.wantSrc {
				t.Errorf("Detect source = %q, want %q", got.Source, tc.wantSrc)
			}
		})
	}
}

func TestLanguageName(t *testing.T) {
	// Regression: whatlanggo.LangToString returns ISO 639-2/3 codes
	// ("fra", "deu"), which used to leak into the banner as
	// "This message is in fra." Names must be English words.
	for code, want := range map[string]string{
		"fr": "French", "de": "German", "ur": "Urdu", "en": "English",
		"es": "Spanish", "zh-Hans": "Chinese", "xx": "xx",
	} {
		if got := LanguageName(code); got != want {
			t.Errorf("LanguageName(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestDetect_LanguageNameIsEnglish(t *testing.T) {
	got := Detect("Bonjour, je vous écris au sujet de ma commande. Pourriez-vous me confirmer la date de livraison s'il vous plaît ? Merci beaucoup pour votre aide.", "en")
	if !got.ShowBanner || got.Language != "fr" {
		t.Fatalf("Detect French: banner=%v lang=%q", got.ShowBanner, got.Language)
	}
	if got.LanguageName != "French" {
		t.Errorf("Detect French LanguageName = %q, want %q", got.LanguageName, "French")
	}
}

func TestStripQuotedText(t *testing.T) {
	text := "Merci pour votre message, je vais vérifier cela.\n\nOn 1 Jan 2026, Alice wrote:\n> Hello there, this is a long English quoted message that would otherwise dominate detection completely.\n> Second quoted line here."
	got := StripQuotedText(text)
	if strings.Contains(got, "Hello there") {
		t.Errorf("quoted lines not stripped: %q", got)
	}
	if !strings.Contains(got, "Merci") {
		t.Errorf("own text lost: %q", got)
	}
}

func TestStripHTMLTags(t *testing.T) {
	got := StripHTMLTags("<p>Guten <b>Morgen</b></p><ul><li>eins</li><li>zwei</li></ul>")
	if strings.Contains(got, "<") || strings.Contains(got, ">") {
		t.Errorf("tags remain: %q", got)
	}
	if !strings.Contains(got, "Guten Morgen") || !strings.Contains(got, "eins zwei") {
		t.Errorf("text mangled: %q", got)
	}
}

func TestBergamotRoute(t *testing.T) {
	if got := BergamotRoute("de", "en"); len(got) != 1 || got[0] != [2]string{"de", "en"} {
		t.Errorf("de→en route = %v, want one direct leg", got)
	}
	if got := BergamotRoute("de", "fr"); len(got) != 2 || got[0] != [2]string{"de", "en"} || got[1] != [2]string{"en", "fr"} {
		t.Errorf("de→fr route = %v, want pivot via English", got)
	}
	if got := BergamotRoute("en", "en"); got != nil {
		t.Errorf("en→en route = %v, want nil", got)
	}
}

func TestResolveTargetLanguage(t *testing.T) {
	if got := ResolveTargetLanguage("system", "de"); got != "de" {
		t.Errorf("system + UI de = %q, want de", got)
	}
	if got := ResolveTargetLanguage("system", ""); got != "en" {
		t.Errorf("system + no UI language = %q, want en", got)
	}
	if got := ResolveTargetLanguage("ur", "de"); got != "ur" {
		t.Errorf("explicit ur = %q, want ur", got)
	}
	if got := ResolveTargetLanguage("system", "zh-TW"); got != "zh-Hant" {
		t.Errorf("system + UI zh-TW = %q, want zh-Hant", got)
	}
}
