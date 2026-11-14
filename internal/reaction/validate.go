package reaction

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// IsSingleEmoji reports whether s is exactly one Unicode emoji: one
// grapheme cluster made of emoji. Multi-rune forms are accepted when they
// are one emoji — skin-tone modifiers (👍🏽), variation selector-16 (❤️),
// keycap sequences (1️⃣), ZWJ sequences (👨‍👩‍👧), and two regional
// indicators forming one flag (🇺🇸). Two adjacent emoji (👍👍), text, or
// an empty string are rejected.
//
// Shared by the sync parser (which validates incoming reaction payloads)
// and the sender (which validates the emoji before sending), so the two
// sides can never disagree about what counts as one emoji.
func IsSingleEmoji(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	runes := []rune(s)
	for _, r := range runes {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	// Two regional indicators form a single flag emoji.
	if len(runes) == 2 && isRegionalIndicator(runes[0]) && isRegionalIndicator(runes[1]) {
		return true
	}
	// Split on zero-width joiners: each segment must be one emoji core
	// plus modifiers. Adjacent emoji without a joiner land in one segment
	// with two cores and are rejected.
	for _, segment := range strings.Split(s, "\u200d") {
		if !isEmojiSegment([]rune(segment)) {
			return false
		}
	}
	return true
}

// isEmojiSegment reports whether the runes are one emoji core (a base emoji
// or a keycap base) followed only by modifiers (variation selector,
// skin tone, keycap mark).
func isEmojiSegment(runes []rune) bool {
	if len(runes) == 0 {
		return false
	}
	core := runes[0]
	isKeycapBase := core == '#' || core == '*' || (core >= '0' && core <= '9')
	if isKeycapBase {
		// A bare digit is not an emoji; it only counts inside a keycap
		// sequence (digit + VS16? + U+20E3).
		hasKeycap := false
		for _, r := range runes[1:] {
			if r == 0x20E3 {
				hasKeycap = true
			}
		}
		if !hasKeycap {
			return false
		}
	} else if !isEmojiCore(core) {
		return false
	}
	for _, r := range runes[1:] {
		if r == 0xFE0F || r == 0x20E3 || isSkinToneModifier(r) {
			continue
		}
		return false
	}
	return true
}

// isSkinToneModifier reports whether r is an emoji skin-tone modifier
// (U+1F3FB..U+1F3FF).
func isSkinToneModifier(r rune) bool {
	return r >= 0x1F3FB && r <= 0x1F3FF
}

// isRegionalIndicator reports whether r is a regional indicator
// (U+1F1E6..U+1F1FF); pairs of them form flag emoji.
func isRegionalIndicator(r rune) bool {
	return r >= 0x1F1E6 && r <= 0x1F1FF
}

// isEmojiCore reports whether r can be the base of an emoji: the standard
// emoji presentation ranges, plus the symbol blocks whose members are
// overwhelmingly emoji. Regional indicators are excluded (only valid as
// pairs, handled in IsSingleEmoji); modifiers are excluded (they follow a
// core, handled in isEmojiSegment).
func isEmojiCore(r rune) bool {
	// A lone regional indicator is a letter, not an emoji — only pairs
	// count, and those are handled in IsSingleEmoji.
	if isRegionalIndicator(r) {
		return false
	}
	switch {
	case r == 0x00A9 || r == 0x00AE: // © ®
		return true
	case r == 0x203C || r == 0x2049: // ‼ ⁉
		return true
	case r == 0x2122 || r == 0x2139: // ™ ℹ
		return true
	case r >= 0x2194 && r <= 0x2199, // ↔↕↖↗↘↙
		r >= 0x21A9 && r <= 0x21AA: // ↩↪
		return true
	case r >= 0x231A && r <= 0x231B, // ⌚⌛
		r == 0x2328,                // ⌨
		r == 0x2388,                // ⎈
		r == 0x23CF,                // ⏏
		r >= 0x23E9 && r <= 0x23F3, // ⏩⏪⏫⏬⏭⏮⏯⏰⏱⏲⏳
		r >= 0x23F8 && r <= 0x23FA: // ⏸⏹⏺
		return true
	case r == 0x24C2: // Ⓜ
		return true
	case r >= 0x25AA && r <= 0x25AB, // ▪▫
		r == 0x25B6,                // ▶
		r == 0x25C0,                // ◀
		r >= 0x25FB && r <= 0x25FE: // ◻◼◽◾
		return true
	case r >= 0x2600 && r <= 0x26FF: // miscellaneous symbols (☀..⛿)
		return true
	case r >= 0x2700 && r <= 0x27BF: // dingbats (✀..➿)
		return true
	case r == 0x2934 || r == 0x2935: // ⤴⤵
		return true
	case r >= 0x2B05 && r <= 0x2B07, // ⬅⬆⬇
		r >= 0x2B1B && r <= 0x2B1C, // ⬛⬜
		r == 0x2B50 || r == 0x2B55: // ⭐⭕
		return true
	case r == 0x3030 || r == 0x303D, // 〰〽
		r == 0x3297 || r == 0x3299: // ㊗㊙
		return true
	case r >= 0x1F000 && r <= 0x1FAFF: // emoticons, transport, symbols, pictographs
		return true
	default:
		return false
	}
}
