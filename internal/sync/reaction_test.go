package sync

import (
	"testing"

	"github.com/hkdb/aerion/internal/reaction"
)

// Reaction-format detection tests. The parser is exercised through
// parseMessageBodyInternal on a bare Engine — detection only needs the
// MIME tree and the logger (zerolog's zero value is a disabled logger),
// so no DB or IMAP fixtures are required.

func testEngine() *Engine {
	return &Engine{}
}

func TestDetectReactionGmailValid(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"To: bob@example.com\r\n" +
		"Subject: 👍\r\n" +
		"Message-ID: <react-1@example.com>\r\n" +
		"In-Reply-To: <orig@example.com>\r\n" +
		"Content-Type: multipart/mixed; boundary=\"BOUNDARY\"\r\n" +
		"\r\n" +
		"--BOUNDARY\r\n" +
		"Content-Type: text/vnd.google.email-reaction+json; charset=utf-8\r\n" +
		"\r\n" +
		"{\"version\": 1, \"emoji\": \"👍\"}\r\n" +
		"--BOUNDARY--\r\n"

	parsed := testEngine().parseMessageBodyInternal([]byte(raw), "msg-1")
	if parsed.Reaction == nil {
		t.Fatal("Reaction = nil, want a detected Gmail reaction")
	}
	if parsed.Reaction.Emoji != "👍" {
		t.Errorf("Reaction.Emoji = %q, want %q", parsed.Reaction.Emoji, "👍")
	}
	if parsed.Reaction.Format != reactionFormatGmail {
		t.Errorf("Reaction.Format = %q, want %q", parsed.Reaction.Format, reactionFormatGmail)
	}
}

func TestDetectReactionGmailSinglePart(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"To: bob@example.com\r\n" +
		"Subject: 👍\r\n" +
		"Message-ID: <react-1@example.com>\r\n" +
		"In-Reply-To: <orig@example.com>\r\n" +
		"Content-Type: text/vnd.google.email-reaction+json; charset=utf-8\r\n" +
		"\r\n" +
		"{\"version\": 1, \"emoji\": \"🎉\"}\r\n"

	parsed := testEngine().parseMessageBodyInternal([]byte(raw), "msg-1")
	if parsed.Reaction == nil {
		t.Fatal("Reaction = nil, want a detected Gmail reaction in a single-part message")
	}
	if parsed.Reaction.Emoji != "🎉" {
		t.Errorf("Reaction.Emoji = %q, want %q", parsed.Reaction.Emoji, "🎉")
	}
}

func TestDetectReactionGmailNested(t *testing.T) {
	// Reaction part nested inside multipart/alternative inside
	// multipart/mixed — the Gmail walk is recursive.
	raw := "From: alice@example.com\r\n" +
		"To: bob@example.com\r\n" +
		"Message-ID: <react-1@example.com>\r\n" +
		"In-Reply-To: <orig@example.com>\r\n" +
		"Content-Type: multipart/mixed; boundary=\"OUTER\"\r\n" +
		"\r\n" +
		"--OUTER\r\n" +
		"Content-Type: multipart/alternative; boundary=\"INNER\"\r\n" +
		"\r\n" +
		"--INNER\r\n" +
		"Content-Type: text/vnd.google.email-reaction+json; charset=utf-8\r\n" +
		"\r\n" +
		"{\"version\": 1, \"emoji\": \"❤️\"}\r\n" +
		"--INNER--\r\n" +
		"--OUTER--\r\n"

	parsed := testEngine().parseMessageBodyInternal([]byte(raw), "msg-1")
	if parsed.Reaction == nil {
		t.Fatal("Reaction = nil, want a detected nested Gmail reaction")
	}
	if parsed.Reaction.Emoji != "❤️" {
		t.Errorf("Reaction.Emoji = %q, want %q", parsed.Reaction.Emoji, "❤️")
	}
}

func TestDetectReactionGmailMalformed(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"bad version", `{"version": 2, "emoji": "👍"}`},
		{"version zero", `{"version": 0, "emoji": "👍"}`},
		{"missing version", `{"emoji": "👍"}`},
		{"multiple emoji", `{"version": 1, "emoji": "👍👍"}`},
		{"two different emoji", `{"version": 1, "emoji": "👍❤️"}`},
		{"empty emoji", `{"version": 1, "emoji": ""}`},
		{"plain text emoji", `{"version": 1, "emoji": "hello"}`},
		{"bare digit", `{"version": 1, "emoji": "1"}`},
		{"invalid JSON", `{not json`},
		{"empty body", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := "From: alice@example.com\r\n" +
				"To: bob@example.com\r\n" +
				"Message-ID: <react-1@example.com>\r\n" +
				"In-Reply-To: <orig@example.com>\r\n" +
				"Content-Type: text/vnd.google.email-reaction+json; charset=utf-8\r\n" +
				"\r\n" +
				tc.json + "\r\n"
			parsed := testEngine().parseMessageBodyInternal([]byte(raw), "msg-1")
			if parsed.Reaction != nil {
				t.Errorf("Reaction = %+v, want nil for malformed payload %q", parsed.Reaction, tc.json)
			}
		})
	}
}

func TestDetectReactionRFC9078Multipart(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"To: bob@example.com\r\n" +
		"Subject: Re: Hello\r\n" +
		"Message-ID: <react-1@example.com>\r\n" +
		"In-Reply-To: <orig@example.com>\r\n" +
		"Content-Type: multipart/mixed; boundary=\"BOUNDARY\"\r\n" +
		"\r\n" +
		"--BOUNDARY\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Disposition: reaction\r\n" +
		"\r\n" +
		"🎉\r\n" +
		"--BOUNDARY--\r\n"

	parsed := testEngine().parseMessageBodyInternal([]byte(raw), "msg-1")
	if parsed.Reaction == nil {
		t.Fatal("Reaction = nil, want a detected RFC 9078 reaction")
	}
	if parsed.Reaction.Emoji != "🎉" {
		t.Errorf("Reaction.Emoji = %q, want %q", parsed.Reaction.Emoji, "🎉")
	}
	if parsed.Reaction.Format != reactionFormatRFC9078 {
		t.Errorf("Reaction.Format = %q, want %q", parsed.Reaction.Format, reactionFormatRFC9078)
	}
}

func TestDetectReactionRFC9078SinglePart(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"To: bob@example.com\r\n" +
		"Message-ID: <react-1@example.com>\r\n" +
		"In-Reply-To: <orig@example.com>\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Disposition: reaction\r\n" +
		"\r\n" +
		"😮\r\n"

	parsed := testEngine().parseMessageBodyInternal([]byte(raw), "msg-1")
	if parsed.Reaction == nil {
		t.Fatal("Reaction = nil, want a detected single-part RFC 9078 reaction")
	}
	if parsed.Reaction.Emoji != "😮" {
		t.Errorf("Reaction.Emoji = %q, want %q", parsed.Reaction.Emoji, "😮")
	}
}

func TestDetectReactionRFC9078Malformed(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"multiple emoji on one line", "👍❤️"},
		{"two lines", "👍\r\n❤️"},
		{"plain text", "thanks!"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := "From: alice@example.com\r\n" +
				"To: bob@example.com\r\n" +
				"Message-ID: <react-1@example.com>\r\n" +
				"In-Reply-To: <orig@example.com>\r\n" +
				"Content-Type: text/plain; charset=utf-8\r\n" +
				"Content-Disposition: reaction\r\n" +
				"\r\n" +
				tc.body + "\r\n"
			parsed := testEngine().parseMessageBodyInternal([]byte(raw), "msg-1")
			if parsed.Reaction != nil {
				t.Errorf("Reaction = %+v, want nil for malformed content %q", parsed.Reaction, tc.body)
			}
		})
	}
}

func TestDetectReactionRFC9078WrongContentType(t *testing.T) {
	// Disposition reaction but not text/plain: not an RFC 9078 reaction.
	raw := "From: alice@example.com\r\n" +
		"To: bob@example.com\r\n" +
		"Message-ID: <react-1@example.com>\r\n" +
		"In-Reply-To: <orig@example.com>\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"Content-Disposition: reaction\r\n" +
		"\r\n" +
		"👍\r\n"

	parsed := testEngine().parseMessageBodyInternal([]byte(raw), "msg-1")
	if parsed.Reaction != nil {
		t.Errorf("Reaction = %+v, want nil for non-text/plain reaction part", parsed.Reaction)
	}
}

func TestDetectReactionRegularEmail(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"To: bob@example.com\r\n" +
		"Subject: Hello\r\n" +
		"Message-ID: <msg-1@example.com>\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Hello Bob, 👍 for your message.\r\n"

	parsed := testEngine().parseMessageBodyInternal([]byte(raw), "msg-1")
	if parsed.Reaction != nil {
		t.Errorf("Reaction = %+v, want nil for a regular email", parsed.Reaction)
	}
	if parsed.BodyText == "" {
		t.Error("BodyText is empty, want the regular body preserved")
	}
}

// TestDetectReactionOwnBuiltMessage is the send/receive round-trip: a
// reaction email built by the Phase 2 sender must be detected by the
// Phase 1 receive parser, so a reaction saved to Sent (or arriving from
// another client built the same way) is recognised.
func TestDetectReactionOwnBuiltMessage(t *testing.T) {
	raw, _, err := reaction.BuildMessage(reaction.BuildOptions{
		Emoji:       "👍",
		SenderName:  "Me",
		SenderEmail: "me@example.com",
		Original: reaction.Original{
			MessageID: "<orig@example.com>",
			Subject:   "Hello there",
			FromName:  "Alice",
			FromEmail: "alice@example.com",
			To:        []string{"me@example.com"},
		},
	})
	if err != nil {
		t.Fatalf("reaction.BuildMessage() error = %v", err)
	}
	parsed := testEngine().parseMessageBodyInternal(raw, "msg-1")
	if parsed.Reaction == nil {
		t.Fatal("Reaction = nil, want the built reaction email to be detected")
	}
	if parsed.Reaction.Emoji != "👍" {
		t.Errorf("Reaction.Emoji = %q, want 👍", parsed.Reaction.Emoji)
	}
}

func TestIsSingleEmoji(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"👍", true},
		{"❤️", true},    // heart + VS16
		{"👍🏽", true},    // skin tone
		{"1️⃣", true},   // keycap
		{"👨‍👩‍👧", true}, // ZWJ family
		{"🇺🇸", true},    // flag (two regional indicators)
		{"🏳️‍🌈", true},  // rainbow flag (ZWJ)
		{"👍👍", false},   // two emoji, no joiner
		{"👍❤️", false},
		{"hello", false},
		{"1", false}, // bare digit is not an emoji
		{"🇺", false}, // lone regional indicator
		{"", false},
		{"  ", false},
		{"👍 ", true}, // surrounding whitespace is trimmed
		{"👍\n❤️", false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := isSingleEmoji(tc.input); got != tc.want {
				t.Errorf("isSingleEmoji(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
