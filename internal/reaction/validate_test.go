package reaction

import "testing"

// IsSingleEmoji is also covered end-to-end by the sync parser tests
// (internal/sync TestIsSingleEmoji); these cases pin the exported
// validator the sender relies on.
func TestIsSingleEmoji(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"👍", true},
		{"👍🏽", true},     // skin tone
		{"❤️", true},     // variation selector
		{"1️⃣", true},    // keycap
		{"👨‍👩‍👧", true},  // ZWJ family
		{"🇺🇸", true},     // flag (regional indicator pair)
		{"👍👍", false},    // two emoji
		{"hello", false}, // text
		{"", false},      // empty
		{"1", false},     // bare digit is not an emoji
		{"🇺", false},     // lone regional indicator
		{"👍 ", true},     // surrounding whitespace is trimmed
		{"👍\x01", false}, // interior control character rejected
	}
	for _, tc := range cases {
		if got := IsSingleEmoji(tc.input); got != tc.want {
			t.Errorf("IsSingleEmoji(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
