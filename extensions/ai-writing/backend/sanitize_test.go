package backend

import (
	"reflect"
	"testing"
)

func TestSanitizeLLMResult(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"html fence pair", "```html<p>x</p>```", "<p>x</p>"},
		// Exact TB behavior: each replace hits only the FIRST occurrence,
		// so a bare opener is consumed by the "```" replace and the
		// closing fence survives. (In practice the model returns
		// ```html ... ```, where the "```html" replace takes the opener
		// and the "```" replace takes the closer — see the case above.)
		{"bare fence pair", "```<p>x</p>```", "<p>x</p>```"},
		{"single-quote html fence", "'''html<p>x</p>'''", "<p>x</p>"},
		{"single-quote fence", "'''<p>x</p>'''", "<p>x</p>'''"},
		{"double-quote html fence", `"""html<p>x</p>"""`, "<p>x</p>"},
		{"double-quote fence", `"""<p>x</p>"""`, "<p>x</p>\"\"\""},
		{"plain unchanged", "<p>x</p>", "<p>x</p>"},
		{"empty", "", ""},
		// TB uses String.replace (first occurrence only): a second fence
		// later in the body survives.
		{"first occurrence only", "```a```b```", "a```b```"},
		// Fence without a closing fence: only the opener is stripped.
		{"opener only", "```html<p>x</p>", "<p>x</p>"},
	}
	for _, c := range cases {
		if got := SanitizeLLMResult(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseSubjects(t *testing.T) {
	cases := []struct {
		name         string
		in           string
		wantSubjects []string
		wantCleaned  string
	}{
		{
			"two subjects then body",
			"<subject>Sub A</subject><subject>Sub B</subject><p>body</p>",
			[]string{"Sub A", "Sub B"}, "<p>body</p>",
		},
		{
			"case insensitive",
			"<SUBJECT>Shout</SUBJECT><p>body</p>",
			[]string{"Shout"}, "<p>body</p>",
		},
		{
			"subjects trimmed",
			"<subject>  Padded  </subject><p>body</p>",
			[]string{"Padded"}, "<p>body</p>",
		},
		{
			"empty subject dropped",
			"<subject>  </subject><subject>Real</subject><p>body</p>",
			[]string{"Real"}, "<p>body</p>",
		},
		{
			"no subjects",
			"<p>body</p>",
			nil, "<p>body</p>",
		},
		{
			"subjects only",
			"<subject>Only</subject>",
			[]string{"Only"}, "",
		},
		{
			"subject mid-body",
			"<p>hi</p><subject>Late</subject>",
			[]string{"Late"}, "<p>hi</p>",
		},
	}
	for _, c := range cases {
		subjects, cleaned := ParseSubjects(c.in)
		if !reflect.DeepEqual(subjects, c.wantSubjects) {
			t.Errorf("%s: subjects = %v, want %v", c.name, subjects, c.wantSubjects)
		}
		if cleaned != c.wantCleaned {
			t.Errorf("%s: cleaned = %q, want %q", c.name, cleaned, c.wantCleaned)
		}
	}
}
