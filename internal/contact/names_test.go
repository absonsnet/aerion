package contact

import "testing"

func TestSplitDisplayName(t *testing.T) {
	cases := []struct {
		in         string
		wantGiven  string
		wantFamily string
	}{
		{"", "", ""},
		{"   ", "", ""},
		{"Madonna", "Madonna", ""},
		{"John Smith", "John", "Smith"},
		{"Mary Jane Watson", "Mary Jane", "Watson"},
		{"  John   Smith  ", "John", "Smith"},
		{"Jean-Luc Picard", "Jean-Luc", "Picard"},
	}
	for _, c := range cases {
		given, family := SplitDisplayName(c.in)
		if given != c.wantGiven || family != c.wantFamily {
			t.Errorf("SplitDisplayName(%q) = (%q, %q), want (%q, %q)", c.in, given, family, c.wantGiven, c.wantFamily)
		}
	}
}
