package sync

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"strings"
	"unicode"
	"unicode/utf8"

	gomessage "github.com/emersion/go-message"
)

// Reaction wire formats detected during body parsing. A reaction is a
// separate email pointing back at the original via In-Reply-To; detection
// here only validates the reaction payload itself. Linking to the original
// and hiding the reaction email happen in fetch.go (handleReaction).
const (
	// gmailReactionContentType is the MIME part Gmail uses for reactions.
	gmailReactionContentType = "text/vnd.google.email-reaction+json"

	// reactionFormatGmail is the DetectedReaction.Format for Gmail's format.
	reactionFormatGmail = "gmail"
	// reactionFormatRFC9078 is the DetectedReaction.Format for RFC 9078.
	reactionFormatRFC9078 = "rfc9078"
)

// maxReactionPartSize bounds how much of a candidate reaction part is read.
// A reaction payload is a few dozen bytes; anything larger is malformed.
const maxReactionPartSize = 4096

// DetectedReaction is a validated email reaction found while parsing a
// message body. Format is "gmail" or "rfc9078". Emoji is exactly one
// Unicode emoji.
type DetectedReaction struct {
	Emoji  string
	Format string
}

// detectReaction inspects raw message bytes for a reaction in either
// supported format. Gmail is checked first; a malformed reaction part is
// skipped rather than vetoing a valid one elsewhere in the tree, so a
// message with only malformed reaction parts is not a reaction at all.
// Returns nil when nothing valid is found.
//
// Detection re-parses raw instead of reusing the entity the body walk
// already consumed, and is gated on two marker strings so ordinary
// messages pay only a byte scan: Gmail always emits its content type
// verbatim and RFC 9078 senders emit "Content-Disposition: reaction".
// A sender that folds or re-cases those headers is missed and the message
// falls back to a regular email — the safe default.
//
// The tree is walked exactly once: a MIME part's body can only be read
// once, so separate Gmail and RFC 9078 passes would starve each other.
func detectReaction(raw []byte) *DetectedReaction {
	if !bytes.Contains(raw, []byte("email-reaction+json")) &&
		!bytes.Contains(raw, []byte("Content-Disposition: reaction")) {
		return nil
	}
	entity, err := gomessage.Read(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	return searchReactionParts(entity, 0)
}

// searchReactionParts walks the MIME tree once, checking every part for a
// Gmail reaction part (valid at any depth) and — at the outermost level
// only, per RFC 9078 — for a reaction part. depth 0 is the message itself,
// depth 1 its direct children. Returns the first valid reaction found.
func searchReactionParts(entity *gomessage.Entity, depth int) *DetectedReaction {
	if emoji, ok := gmailReactionPartEmoji(entity); ok {
		return &DetectedReaction{Emoji: emoji, Format: reactionFormatGmail}
	}
	if depth <= 1 {
		if emoji, ok := rfc9078ReactionPartEmoji(entity); ok {
			return &DetectedReaction{Emoji: emoji, Format: reactionFormatRFC9078}
		}
	}
	mr := entity.MultipartReader()
	if mr == nil {
		return nil
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			return nil
		}
		if found := searchReactionParts(part, depth+1); found != nil {
			return found
		}
	}
}

// gmailReactionPartEmoji validates a Gmail reaction part:
// text/vnd.google.email-reaction+json with a {"version": 1, "emoji": ...}
// payload. Any other version, a missing or multi-emoji value, or
// unparseable JSON is malformed — not a reaction. Returns false for
// parts that are not Gmail reaction parts at all.
func gmailReactionPartEmoji(entity *gomessage.Entity) (string, bool) {
	contentType, params, _ := mime.ParseMediaType(entity.Header.Get("Content-Type"))
	if contentType != gmailReactionContentType {
		return "", false
	}
	body, err := io.ReadAll(io.LimitReader(entity.Body, maxReactionPartSize))
	if err != nil {
		return "", false
	}
	return parseGmailReactionJSON(decodeCharset(body, params["charset"]))
}

// gmailReactionJSON is the payload of Gmail's reaction part.
type gmailReactionJSON struct {
	Version int    `json:"version"`
	Emoji   string `json:"emoji"`
}

// parseGmailReactionJSON validates a Gmail reaction payload. Version must be
// exactly 1 and emoji exactly one Unicode emoji.
func parseGmailReactionJSON(body string) (string, bool) {
	var payload gmailReactionJSON
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return "", false
	}
	if payload.Version != 1 {
		return "", false
	}
	if !isSingleEmoji(payload.Emoji) {
		return "", false
	}
	return payload.Emoji, true
}

// rfc9078ReactionPartEmoji returns the emoji if the entity is an RFC 9078
// reaction part — Content-Disposition: reaction with Content-Type
// text/plain whose content is a single line holding one emoji — and false
// otherwise (not a reaction part, or malformed content).
func rfc9078ReactionPartEmoji(entity *gomessage.Entity) (string, bool) {
	disposition, _, _ := mime.ParseMediaType(entity.Header.Get("Content-Disposition"))
	if disposition != "reaction" {
		return "", false
	}
	contentType, params, _ := mime.ParseMediaType(entity.Header.Get("Content-Type"))
	if contentType != "text/plain" {
		return "", false
	}
	body, err := io.ReadAll(io.LimitReader(entity.Body, maxReactionPartSize))
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(decodeCharset(body, params["charset"]))
	if strings.ContainsAny(line, "\r\n") {
		return "", false
	}
	if !isSingleEmoji(line) {
		return "", false
	}
	return line, true
}

// isSingleEmoji reports whether s is exactly one Unicode emoji: one
// grapheme cluster made of emoji. Multi-rune forms are accepted when they
// are one emoji — skin-tone modifiers (👍🏽), variation selector-16 (❤️),
// keycap sequences (1️⃣), ZWJ sequences (👨‍👩‍👧), and two regional
// indicators forming one flag (🇺🇸). Two adjacent emoji (👍👍), text, or
// an empty string are rejected.
func isSingleEmoji(s string) bool {
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
// pairs, handled in isSingleEmoji); modifiers are excluded (they follow a
// core, handled in isEmojiSegment).
func isEmojiCore(r rune) bool {
	// A lone regional indicator is a letter, not an emoji — only pairs
	// count, and those are handled in isSingleEmoji.
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
