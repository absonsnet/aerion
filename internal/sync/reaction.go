package sync

import (
	"bytes"
	"encoding/json"
	"github.com/absonsnet/daak/internal/reaction"
	gomessage "github.com/emersion/go-message"
	"io"
	"mime"
	"strings"
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

// isSingleEmoji delegates to the shared validator in internal/reaction so
// the receive path and the send path agree on what counts as one emoji.
func isSingleEmoji(s string) bool {
	return reaction.IsSingleEmoji(s)
}
