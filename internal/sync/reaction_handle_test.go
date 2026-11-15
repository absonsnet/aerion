package sync

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/absonsnet/daak/internal/database"
	"github.com/absonsnet/daak/internal/message"
	"github.com/absonsnet/daak/internal/reaction"
)

// Engine-level reaction handling tests: handleReaction (linked + pending)
// and linkPendingReactions. Built on a real temp DB so the message-store
// lookups, the reaction store, and the is_hidden flag are all exercised
// together. No IMAP is involved — the body-fetch step that calls
// handleReaction is covered by the detection tests in reaction_test.go.
func newReactionHandleTestEngine(t *testing.T) (*Engine, *database.DB, *message.Store, *reaction.Store) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO accounts (id, name, email, imap_host, smtp_host, username) VALUES ('acc-1', 'Test', 'me@example.com', 'imap.example.com', 'smtp.example.com', 'me@example.com')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO folders (id, account_id, name, path, folder_type) VALUES ('folder-1', 'acc-1', 'INBOX', 'INBOX', 'inbox')`); err != nil {
		t.Fatal(err)
	}

	msgStore := message.NewStore(db)
	reactStore := reaction.NewStore(db)
	engine := &Engine{messageStore: msgStore, reactionStore: reactStore}
	return engine, db, msgStore, reactStore
}

func seedHandleTestMessage(t *testing.T, s *message.Store, id, messageID, inReplyTo, fromEmail, fromName string) {
	t.Helper()
	m := &message.Message{
		ID:        id,
		AccountID: "acc-1",
		FolderID:  "folder-1",
		UID:       1,
		MessageID: messageID,
		InReplyTo: inReplyTo,
		FromEmail: fromEmail,
		FromName:  fromName,
		Date:      time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC),
		Subject:   "test",
	}
	// UIDs must be unique per folder; derive one from the id.
	switch id {
	case "orig-1":
		m.UID = 1
	case "react-1":
		m.UID = 2
	case "react-2":
		m.UID = 3
	case "orig-2":
		m.UID = 4
	}
	if err := s.Create(m); err != nil {
		t.Fatalf("Create %s: %v", id, err)
	}
}

func isHidden(t *testing.T, db *database.DB, id string) bool {
	t.Helper()
	var hidden int
	if err := db.QueryRow(`SELECT is_hidden FROM messages WHERE id = ?`, id).Scan(&hidden); err != nil {
		t.Fatalf("query is_hidden for %s: %v", id, err)
	}
	return hidden == 1
}

func TestHandleReactionLinked(t *testing.T) {
	e, db, msgStore, reactStore := newReactionHandleTestEngine(t)
	seedHandleTestMessage(t, msgStore, "orig-1", "<orig@example.com>", "", "bob@example.com", "Bob")
	seedHandleTestMessage(t, msgStore, "react-1", "<react-1@example.com>", "<orig@example.com>", "alice@example.com", "Alice")

	linked := e.handleReaction("react-1", &DetectedReaction{Emoji: "👍", Format: reactionFormatGmail})
	if !linked {
		t.Fatal("handleReaction() = false, want true (original is in the DB)")
	}

	got, err := reactStore.ListByMessage("orig-1")
	if err != nil {
		t.Fatalf("ListByMessage() error = %v", err)
	}
	if len(got) != 1 || got[0].Emoji != "👍" || got[0].ReactorEmail != "alice@example.com" {
		t.Fatalf("ListByMessage() = %+v, want one 👍 from alice@example.com", got)
	}
	if !isHidden(t, db, "react-1") {
		t.Error("reaction email is_hidden = 0, want 1 (linked reaction emails are hidden)")
	}
	if isHidden(t, db, "orig-1") {
		t.Error("original is_hidden = 1, want 0")
	}

	// The hidden email is out of the list the user sees.
	headers, err := msgStore.ListByFolder("folder-1", 0, 10)
	if err != nil {
		t.Fatalf("ListByFolder() error = %v", err)
	}
	if len(headers) != 1 || headers[0].ID != "orig-1" {
		t.Fatalf("ListByFolder() = %v, want only [orig-1]", headers)
	}
}

func TestHandleReactionPendingThenLinked(t *testing.T) {
	e, db, msgStore, reactStore := newReactionHandleTestEngine(t)
	// Reaction arrives before its original.
	seedHandleTestMessage(t, msgStore, "react-2", "<react-2@example.com>", "<orig-2@example.com>", "alice@example.com", "Alice")

	linked := e.handleReaction("react-2", &DetectedReaction{Emoji: "🎉", Format: reactionFormatRFC9078})
	if linked {
		t.Fatal("handleReaction() = true, want false (original not in the DB yet)")
	}
	if n, err := reactStore.CountPending(); err != nil || n != 1 {
		t.Fatalf("CountPending() = %d, err = %v, want 1", n, err)
	}
	if isHidden(t, db, "react-2") {
		t.Error("pending reaction email is_hidden = 1, want 0 (it stays a regular message until linked)")
	}

	// Original syncs later; linking resolves the pending reaction and
	// hides the reaction email.
	seedHandleTestMessage(t, msgStore, "orig-2", "<orig-2@example.com>", "", "bob@example.com", "Bob")
	e.linkPendingReactions("acc-1", "<orig-2@example.com>", "orig-2")

	got, err := reactStore.ListByMessage("orig-2")
	if err != nil {
		t.Fatalf("ListByMessage() error = %v", err)
	}
	if len(got) != 1 || got[0].Emoji != "🎉" {
		t.Fatalf("ListByMessage() = %+v, want the linked 🎉 reaction", got)
	}
	if n, err := reactStore.CountPending(); err != nil || n != 0 {
		t.Fatalf("CountPending() after link = %d, err = %v, want 0", n, err)
	}
	if !isHidden(t, db, "react-2") {
		t.Error("reaction email is_hidden = 0 after linking, want 1")
	}
}

func TestHandleReactionNoInReplyTo(t *testing.T) {
	e, _, msgStore, reactStore := newReactionHandleTestEngine(t)
	seedHandleTestMessage(t, msgStore, "react-1", "<react-1@example.com>", "", "alice@example.com", "Alice")

	if linked := e.handleReaction("react-1", &DetectedReaction{Emoji: "👍", Format: reactionFormatGmail}); linked {
		t.Error("handleReaction() = true, want false (no In-Reply-To to link against)")
	}
	if n, err := reactStore.CountPending(); err != nil || n != 0 {
		t.Errorf("CountPending() = %d, err = %v, want 0 (nothing recorded without a target)", n, err)
	}
}

func TestHandleReactionWithoutStore(t *testing.T) {
	// Reactions not wired up (nil store) must degrade to regular email,
	// never panic.
	e, _, msgStore, _ := newReactionHandleTestEngine(t)
	e.reactionStore = nil
	seedHandleTestMessage(t, msgStore, "react-1", "<react-1@example.com>", "<orig@example.com>", "alice@example.com", "Alice")

	if linked := e.handleReaction("react-1", &DetectedReaction{Emoji: "👍", Format: reactionFormatGmail}); linked {
		t.Error("handleReaction() = true with nil reaction store, want false")
	}
}
