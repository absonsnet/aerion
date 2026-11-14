package mailmerge

import (
	"errors"
	"testing"
)

func TestRenderPlain(t *testing.T) {
	out, err := Render("Hello {{FirstName}} {{LastName}}!", map[string]string{
		"FirstName": "John", "LastName": "Doe",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "Hello John Doe!" {
		t.Fatalf("got %q", out)
	}
}

func TestRenderCaseSensitive(t *testing.T) {
	// Headers are case-sensitive: {{firstname}} does NOT match "FirstName".
	_, err := Render("{{firstname}}", map[string]string{"FirstName": "John"})
	var me *MissingError
	if !errors.As(err, &me) {
		t.Fatalf("expected MissingError, got %v", err)
	}
	if len(me.Names) != 1 || me.Names[0] != "firstname" {
		t.Fatalf("unexpected missing names %v", me.Names)
	}
}

func TestRenderEmptyValue(t *testing.T) {
	out, err := Render("X{{Middle}}Y", map[string]string{"Middle": ""})
	if err != nil {
		t.Fatal(err)
	}
	if out != "XY" {
		t.Fatalf("got %q", out)
	}
}

func TestRenderConditionalEquality(t *testing.T) {
	row := map[string]string{"Gender": "m", "FirstName": "John"}
	cases := []struct{ tmpl, want string }{
		{"{{Gender|m|Mr|Ms}}", "Mr"},
		{"{{Gender|f|Mr|Ms}}", "Ms"},
		{"{{Gender|m|Mr}}", "Mr"},
		{"{{Gender|f|Mr}}", ""},
		{"{{FirstName|John|yes|no}}", "yes"},
		{"{{FirstName|Jane|yes|no}}", "no"},
	}
	for _, c := range cases {
		out, err := Render(c.tmpl, row)
		if err != nil {
			t.Fatalf("%s: %v", c.tmpl, err)
		}
		if out != c.want {
			t.Fatalf("%s: got %q want %q", c.tmpl, out, c.want)
		}
	}
}

func TestRenderOperators(t *testing.T) {
	row := map[string]string{
		"Email": "john.doe@example.com",
		"Name":  "John Doe",
		"Age":   "42",
		"Score": "9.5",
	}
	cases := []struct{ tmpl, want string }{
		// * includes
		{"{{Email|*|example|yes|no}}", "yes"},
		{"{{Email|*|other|yes|no}}", "no"},
		// ^ starts-with
		{"{{Name|^|John|yes|no}}", "yes"},
		{"{{Name|^|Doe|yes|no}}", "no"},
		// $ ends-with
		{"{{Email|$|.com|yes|no}}", "yes"},
		{"{{Email|$|.org|yes|no}}", "no"},
		// == numeric
		{"{{Age|==|42|yes|no}}", "yes"},
		{"{{Age|==|43|yes|no}}", "no"},
		{"{{Score|==|9.5|yes|no}}", "yes"},
		// > >= < <= numeric
		{"{{Age|>|40|yes|no}}", "yes"},
		{"{{Age|>|42|yes|no}}", "no"},
		{"{{Age|>=|42|yes|no}}", "yes"},
		{"{{Age|<|43|yes|no}}", "yes"},
		{"{{Age|<|42|yes|no}}", "no"},
		{"{{Age|<=|42|yes|no}}", "yes"},
		{"{{Age|<=|41|yes|no}}", "no"},
		// operator without else
		{"{{Age|>|40|adult}}", "adult"},
		{"{{Age|>|50|adult}}", ""},
		// non-numeric side: numeric comparison is false, not an error
		{"{{Name|>|5|yes|no}}", "no"},
		{"{{Age|>|abc|yes|no}}", "no"},
	}
	for _, c := range cases {
		out, err := Render(c.tmpl, row)
		if err != nil {
			t.Fatalf("%s: %v", c.tmpl, err)
		}
		if out != c.want {
			t.Fatalf("%s: got %q want %q", c.tmpl, out, c.want)
		}
	}
}

func TestRenderNested(t *testing.T) {
	row := map[string]string{
		"Premium": "x",
		"Email":   "vip@example.com",
		"Gender":  "m",
	}
	out, err := Render("{{Premium|x|{{Email}}}}", row)
	if err != nil {
		t.Fatal(err)
	}
	if out != "vip@example.com" {
		t.Fatalf("got %q", out)
	}
	// Nested inside a branch: premium filter from the TB docs.
	out, err = Render("{{FirstName}} {{LastName}} <{{Premium|x|{{Email}}}}>", map[string]string{
		"FirstName": "John", "LastName": "Doe", "Premium": "x", "Email": "john@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "John Doe <john@example.com>" {
		t.Fatalf("got %q", out)
	}
	// Nested conditional as the branch value.
	out, err = Render("{{Gender|m|{{Email}}|none}}", map[string]string{"Gender": "m", "Email": "m@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "m@example.com" {
		t.Fatalf("got %q", out)
	}
	// Non-premium row resolves to empty (the TB premium-filter trick).
	out, err = Render("{{Premium|x|{{Email}}}}", map[string]string{"Premium": "", "Email": "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("got %q want empty", out)
	}
}

func TestRenderEmptyTest(t *testing.T) {
	row := map[string]string{"DisplayName": "", "FirstName": "John"}
	out, err := Render("{{DisplayName||Empty|NotEmpty}}", row)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Empty" {
		t.Fatalf("got %q", out)
	}
	out, err = Render("{{FirstName||Empty|NotEmpty}}", row)
	if err != nil {
		t.Fatal(err)
	}
	if out != "NotEmpty" {
		t.Fatalf("got %q", out)
	}
}

func TestRenderMissingVariable(t *testing.T) {
	_, err := Render("Hello {{Nope}} and {{AlsoNope}}", map[string]string{"FirstName": "John"})
	var me *MissingError
	if !errors.As(err, &me) {
		t.Fatalf("expected MissingError, got %v", err)
	}
	if len(me.Names) != 2 || me.Names[0] != "Nope" || me.Names[1] != "AlsoNope" {
		t.Fatalf("unexpected missing names %v", me.Names)
	}
	// Missing inside a conditional branch is reported too.
	_, err = Render("{{Gender|m|{{Missing}}|x}}", map[string]string{"Gender": "m"})
	if !errors.As(err, &me) {
		t.Fatalf("expected MissingError for nested, got %v", err)
	}
	// Missing variable in an untaken branch does not fail the render
	// (validation via Variables is the conservative check that blocks).
	out, err := Render("{{Gender|m|Mr|{{Missing}}}}", map[string]string{"Gender": "m"})
	if err != nil {
		t.Fatalf("untaken branch must not fail: %v", err)
	}
	if out != "Mr" {
		t.Fatalf("got %q", out)
	}
}

func TestRenderMalformed(t *testing.T) {
	for _, tmpl := range []string{
		"{{Name|only-two}}",
		"{{Name|a|b|c|d|e}}",
		"{{Name|??|a|b|c}}",
		"{{Name",
		"{{}}",
	} {
		if _, err := Render(tmpl, map[string]string{"Name": "x"}); err == nil {
			t.Fatalf("%q: expected error", tmpl)
		}
	}
}

func TestRenderHTMLEscaping(t *testing.T) {
	row := map[string]string{"FirstName": "<b>John</b> & \"friends\""}
	// Raw render keeps the value literal (correct for text/headers).
	out, err := Render("Hi {{FirstName}}", row)
	if err != nil {
		t.Fatal(err)
	}
	if out != `Hi <b>John</b> & "friends"` {
		t.Fatalf("raw: got %q", out)
	}
	// HTML render escapes the VALUE, not the template markup.
	out, err = RenderHTML("<p>Hi {{FirstName}}</p>", row)
	if err != nil {
		t.Fatal(err)
	}
	if out != "<p>Hi &lt;b&gt;John&lt;/b&gt; &amp; &quot;friends&quot;</p>" {
		t.Fatalf("html: got %q", out)
	}
	// Escaping must not break conditional comparison: a value containing
	// "<" still compares raw.
	out, err = RenderHTML("{{FirstName|<b>John</b>|match|no}}", map[string]string{"FirstName": "<b>John</b>"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "match" {
		t.Fatalf("conditional compare: got %q", out)
	}
	// A branch value containing markup is escaped in HTML mode.
	out, err = RenderHTML("{{Premium|x|{{Email}}}}", map[string]string{"Premium": "x", "Email": "a<b@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "a&lt;b@example.com" {
		t.Fatalf("nested html: got %q", out)
	}
}

func TestVariables(t *testing.T) {
	got := Variables("Dear {{FirstName}} {{LastName}}, {{Gender|m|Mr|Ms}} {{LastName}} — {{Premium|x|{{Email}}}}")
	want := []string{"FirstName", "LastName", "Gender", "Premium", "Email"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if got := Variables("no placeholders"); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	// Names inside untaken branches are still reported (conservative:
	// validation blocks on them even if a given row would not render them).
	got = Variables("{{Gender|m|Mr|{{Missing}}}}")
	found := false
	for _, n := range got {
		if n == "Missing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Missing in %v", got)
	}
}
