package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/mail"
	"path/filepath"
	"strings"
	"testing"

	"github.com/absonsnet/daak/internal/account"
	"github.com/absonsnet/daak/internal/database"
	"github.com/absonsnet/daak/internal/message"
	"github.com/absonsnet/daak/internal/reaction"
)

// --- SendReaction tests ----------------------------------------------------
// The App under test uses real DB-backed stores (account, message,
// reaction) and fake transport hooks on composeOps, so the whole
// send-then-record flow runs without a server — the same fake-sender
// approach internal/mailmerge uses.

// capturedSend records one outbound send through the fake transport.
type capturedSend struct {
	from       string
	recipients []string
	raw        []byte
}

type reactionTestApp struct {
	app       *App
	db        *database.DB
	sent      []*capturedSend
	savedSent [][]byte
}

func newReactionTestApp(t *testing.T) *reactionTestApp {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`INSERT INTO accounts (id, name, email, imap_host, smtp_host, username) VALUES ('acc-1', 'Me', 'me@example.com', 'imap.example.com', 'smtp.example.com', 'me@example.com')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO identities (id, account_id, email, name, is_default) VALUES ('ident-1', 'acc-1', 'me@example.com', 'Me', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO folders (id, account_id, name, path, folder_type) VALUES ('folder-1', 'acc-1', 'INBOX', 'INBOX', 'inbox')`); err != nil {
		t.Fatal(err)
	}

	rta := &reactionTestApp{db: db}
	accountStore := account.NewStore(db)
	a := &App{
		ctx:           context.Background(),
		db:            db,
		accountStore:  accountStore,
		messageStore:  message.NewStore(db),
		reactionStore: reaction.NewStore(db),
		composeOps: composeOps{
			accountStore: accountStore,
			rawSendOverride: func(_ context.Context, from string, recipients []string, raw []byte) error {
				rta.sent = append(rta.sent, &capturedSend{from: from, recipients: recipients, raw: raw})
				return nil
			},
			sentSaveOverride: func(_ context.Context, _ string, raw []byte) error {
				rta.savedSent = append(rta.savedSent, raw)
				return nil
			},
		},
	}
	rta.app = a
	return rta
}

// seedMessage inserts a message row for the reaction tests. to/cclist are
// address lists in the message store's JSON form ({"name","email"}).
// extraSQL runs after the insert (e.g. to set smime_encrypted).
func (rta *reactionTestApp) seedMessage(t *testing.T, id, messageID, fromName, fromEmail, toList, ccList, bccList string, extraSQL string) {
	t.Helper()
	_, err := rta.db.Exec(`INSERT INTO messages (id, account_id, folder_id, uid, message_id, subject, from_name, from_email, to_list, cc_list, bcc_list) VALUES (?, 'acc-1', 'folder-1', ?, ?, 'Hello there', ?, ?, ?, ?, ?)`,
		id, 100+len(id), messageID, fromName, fromEmail, toList, ccList, bccList)
	if err != nil {
		t.Fatal(err)
	}
	if extraSQL != "" {
		if _, err := rta.db.Exec(extraSQL); err != nil {
			t.Fatal(err)
		}
	}
}

func addrListJSON(t *testing.T, emails ...string) string {
	t.Helper()
	parts := make([]string, 0, len(emails))
	for _, e := range emails {
		parts = append(parts, fmt.Sprintf(`{"name":"","email":%q}`, e))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func TestSendReactionHappyPath(t *testing.T) {
	rta := newReactionTestApp(t)
	rta.seedMessage(t, "msg-1", "<orig@example.com>", "Alice", "alice@example.com",
		addrListJSON(t, "me@example.com"), "", "", "")

	if err := rta.app.SendReaction("msg-1", "👍"); err != nil {
		t.Fatalf("SendReaction() error = %v", err)
	}

	if len(rta.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(rta.sent))
	}
	sent := rta.sent[0]
	if sent.from != "me@example.com" {
		t.Errorf("sent from = %q, want me@example.com", sent.from)
	}
	if len(sent.recipients) != 1 || sent.recipients[0] != "alice@example.com" {
		t.Errorf("sent recipients = %v, want [alice@example.com] (original sender only)", sent.recipients)
	}
	if len(rta.savedSent) != 1 {
		t.Fatalf("saved %d messages to Sent, want 1", len(rta.savedSent))
	}
	if !bytes.Equal(rta.savedSent[0], sent.raw) {
		t.Error("Sent save bytes differ from the sent bytes")
	}

	// The sent bytes are a reaction email for the original.
	msg, err := mail.ReadMessage(bytes.NewReader(sent.raw))
	if err != nil {
		t.Fatalf("mail.ReadMessage(sent) error = %v", err)
	}
	if got := msg.Header.Get("In-Reply-To"); got != "<orig@example.com>" {
		t.Errorf("In-Reply-To = %q, want <orig@example.com>", got)
	}
	if got := msg.Header.Get("Message-ID"); got == "" {
		t.Error("sent reaction has no Message-ID")
	}
	if !strings.Contains(string(sent.raw), "text/vnd.google.email-reaction+json") {
		t.Error("sent bytes lack the Gmail reaction part")
	}
	if !strings.Contains(string(sent.raw), "Content-Disposition: reaction") {
		t.Error("sent bytes lack the RFC 9078 reaction part")
	}

	// The reaction is recorded immediately, with the sent email's own
	// Message-ID as the source id.
	reactions, err := rta.app.reactionStore.ListByMessage("msg-1")
	if err != nil {
		t.Fatalf("ListByMessage() error = %v", err)
	}
	if len(reactions) != 1 {
		t.Fatalf("recorded %d reactions, want 1", len(reactions))
	}
	rec := reactions[0]
	if rec.Emoji != "👍" {
		t.Errorf("recorded emoji = %q, want 👍", rec.Emoji)
	}
	if rec.ReactorEmail != "me@example.com" {
		t.Errorf("recorded reactor = %q, want me@example.com", rec.ReactorEmail)
	}
	if rec.ReactorName != "Me" {
		t.Errorf("recorded reactor name = %q, want Me", rec.ReactorName)
	}
	if rec.TargetMessageID != "orig@example.com" {
		t.Errorf("recorded target = %q, want orig@example.com", rec.TargetMessageID)
	}
	if rec.SourceMessageID == "" || "<"+rec.SourceMessageID+">" != msg.Header.Get("Message-ID") {
		t.Errorf("recorded source_message_id = %q, want the sent Message-ID %q", rec.SourceMessageID, msg.Header.Get("Message-ID"))
	}

	// And the read binding sees it as the user's own reaction.
	listed, err := rta.app.GetMessageReactions("msg-1")
	if err != nil {
		t.Fatalf("GetMessageReactions() error = %v", err)
	}
	if len(listed) != 1 || !listed[0].IsOwn {
		t.Errorf("GetMessageReactions() = %+v, want one own reaction", listed)
	}
}

func TestSendReactionNotFound(t *testing.T) {
	rta := newReactionTestApp(t)
	if err := rta.app.SendReaction("no-such-message", "👍"); err == nil {
		t.Error("SendReaction() error = nil, want an error for an unknown message")
	}
	if len(rta.sent) != 0 {
		t.Errorf("sent %d messages, want 0", len(rta.sent))
	}
}

func TestSendReactionLimits(t *testing.T) {
	many := make([]string, 0, 21)
	for i := 0; i < 21; i++ {
		many = append(many, fmt.Sprintf("person%d@example.com", i))
	}
	cases := []struct {
		name     string
		id       string
		to, cc   string
		bcc      string
		extraSQL string
		wantErr  error
	}{
		{"group address", "msg-group", addrListJSON(t, "me@example.com", "all@example.com"), "", "", "", reaction.ErrGroupRecipient},
		{"too many recipients", "msg-many", addrListJSON(t, many...), "", "", "", reaction.ErrTooManyRecipients},
		{"user BCC'd (in Bcc list)", "msg-bcc", addrListJSON(t, "alice@example.com"), "", addrListJSON(t, "me@example.com"), "", reaction.ErrBCCRecipient},
		{"user BCC'd (not in To/Cc)", "msg-bcc2", addrListJSON(t, "someone@example.com"), "", "", "", reaction.ErrBCCRecipient},
		{"encrypted", "msg-enc", addrListJSON(t, "me@example.com"), "", "", `UPDATE messages SET smime_encrypted = 1 WHERE id = 'msg-enc'`, reaction.ErrEncryptedMessage},
		{"invalid emoji", "msg-emoji", addrListJSON(t, "me@example.com"), "", "", "", reaction.ErrInvalidEmoji},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rta := newReactionTestApp(t)
			emoji := "👍"
			if tc.name == "invalid emoji" {
				emoji = "👍👍"
			}
			rta.seedMessage(t, tc.id, "<orig-"+tc.id+"@example.com>", "Alice", "alice@example.com", tc.to, tc.cc, tc.bcc, tc.extraSQL)
			err := rta.app.SendReaction(tc.id, emoji)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("SendReaction() error = %v, want %v", err, tc.wantErr)
			}
			if len(rta.sent) != 0 {
				t.Errorf("sent %d messages, want 0 (limit must block before sending)", len(rta.sent))
			}
			if len(rta.savedSent) != 0 {
				t.Errorf("saved %d messages to Sent, want 0", len(rta.savedSent))
			}
			reactions, _ := rta.app.reactionStore.ListByMessage(tc.id)
			if len(reactions) != 0 {
				t.Errorf("recorded %d reactions, want 0", len(reactions))
			}
		})
	}
}

// --- RemoveOwnReaction tests ------------------------------------------------
// Removal is local-only: it deletes the stored row and sends nothing.

func TestRemoveOwnReaction(t *testing.T) {
	rta := newReactionTestApp(t)
	rta.seedMessage(t, "msg-1", "<orig@example.com>", "Alice", "alice@example.com",
		addrListJSON(t, "me@example.com"), "", "", "")

	if err := rta.app.SendReaction("msg-1", "👍"); err != nil {
		t.Fatalf("SendReaction() error = %v", err)
	}
	// Another reactor's reaction on the same message, recorded directly.
	if err := rta.app.reactionStore.Record(&reaction.Reaction{
		MessageID:       "msg-1",
		TargetMessageID: "<orig@example.com>",
		ReactorEmail:    "alice@example.com",
		ReactorName:     "Alice",
		Emoji:           "❤️",
		SourceMessageID: "<react-alice@example.com>",
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	if err := rta.app.RemoveOwnReaction("msg-1"); err != nil {
		t.Fatalf("RemoveOwnReaction() error = %v", err)
	}
	if len(rta.sent) != 1 {
		t.Errorf("sent %d messages after removal, want 1 (removal sends nothing)", len(rta.sent))
	}
	listed, err := rta.app.GetMessageReactions("msg-1")
	if err != nil {
		t.Fatalf("GetMessageReactions() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ReactorEmail != "alice@example.com" {
		t.Errorf("GetMessageReactions() = %+v, want only Alice's reaction", listed)
	}
}

func TestRemoveOwnReactionNotFound(t *testing.T) {
	rta := newReactionTestApp(t)
	if err := rta.app.RemoveOwnReaction("no-such-message"); err == nil {
		t.Error("RemoveOwnReaction() error = nil, want an error for an unknown message")
	}
}
