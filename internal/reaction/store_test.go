package reaction

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hkdb/aerion/internal/database"
)

func openTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustSeedMessage(t *testing.T, db *database.DB, id, messageID string) {
	t.Helper()
	// Minimal account/folder rows so the messages row satisfies its FKs.
	if _, err := db.Exec(`INSERT OR IGNORE INTO accounts (id, name, email, imap_host, smtp_host, username) VALUES ('acc-1', 'Test', 'me@example.com', 'imap.example.com', 'smtp.example.com', 'me@example.com')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO folders (id, account_id, name, path, folder_type) VALUES ('folder-1', 'acc-1', 'INBOX', 'INBOX', 'inbox')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO messages (id, account_id, folder_id, uid, message_id) VALUES (?, 'acc-1', 'folder-1', 1, ?)`,
		id, messageID,
	); err != nil {
		t.Fatal(err)
	}
}

func TestRecordAndListByMessage(t *testing.T) {
	db := openTestDB(t)
	mustSeedMessage(t, db, "msg-1", "<orig@example.com>")
	store := NewStore(db)

	err := store.Record(&Reaction{
		MessageID:       "msg-1",
		TargetMessageID: "<orig@example.com>",
		ReactorEmail:    "Alice@Example.com",
		ReactorName:     "Alice",
		Emoji:           "👍",
		CreatedAt:       time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC),
		SourceMessageID: "<react-1@example.com>",
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	got, err := store.ListByMessage("msg-1")
	if err != nil {
		t.Fatalf("ListByMessage() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListByMessage() returned %d reactions, want 1", len(got))
	}
	r := got[0]
	if r.Emoji != "👍" {
		t.Errorf("Emoji = %q, want %q", r.Emoji, "👍")
	}
	if r.ReactorEmail != "alice@example.com" {
		t.Errorf("ReactorEmail = %q, want lowercased %q", r.ReactorEmail, "alice@example.com")
	}
	if r.ReactorName != "Alice" {
		t.Errorf("ReactorName = %q, want %q", r.ReactorName, "Alice")
	}
	if r.MessageID != "msg-1" {
		t.Errorf("MessageID = %q, want %q", r.MessageID, "msg-1")
	}
	if r.TargetMessageID != "orig@example.com" {
		t.Errorf("TargetMessageID = %q, want angle brackets stripped", r.TargetMessageID)
	}
	if r.SourceMessageID != "react-1@example.com" {
		t.Errorf("SourceMessageID = %q, want angle brackets stripped", r.SourceMessageID)
	}
	if r.CreatedAt.Year() != 2026 || r.CreatedAt.Hour() != 10 {
		t.Errorf("CreatedAt = %v, want the recorded date", r.CreatedAt)
	}
	if r.ID == "" {
		t.Error("ID is empty, want a generated UUID")
	}
}

func TestRecordReplacesSameReactor(t *testing.T) {
	db := openTestDB(t)
	mustSeedMessage(t, db, "msg-1", "<orig@example.com>")
	store := NewStore(db)

	first := &Reaction{
		MessageID:       "msg-1",
		TargetMessageID: "<orig@example.com>",
		ReactorEmail:    "alice@example.com",
		Emoji:           "👍",
		SourceMessageID: "<react-1@example.com>",
	}
	second := &Reaction{
		MessageID:       "msg-1",
		TargetMessageID: "<orig@example.com>",
		ReactorEmail:    "alice@example.com",
		Emoji:           "❤️",
		SourceMessageID: "<react-2@example.com>",
	}
	if err := store.Record(first); err != nil {
		t.Fatalf("Record(first) error = %v", err)
	}
	if err := store.Record(second); err != nil {
		t.Fatalf("Record(second) error = %v", err)
	}

	got, err := store.ListByMessage("msg-1")
	if err != nil {
		t.Fatalf("ListByMessage() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListByMessage() returned %d reactions, want 1 (replacement)", len(got))
	}
	if got[0].Emoji != "❤️" {
		t.Errorf("Emoji = %q, want the newest reaction %q", got[0].Emoji, "❤️")
	}
	if got[0].SourceMessageID != "react-2@example.com" {
		t.Errorf("SourceMessageID = %q, want %q", got[0].SourceMessageID, "react-2@example.com")
	}
}

func TestRecordKeepsDifferentReactors(t *testing.T) {
	db := openTestDB(t)
	mustSeedMessage(t, db, "msg-1", "<orig@example.com>")
	store := NewStore(db)

	for _, r := range []*Reaction{
		{MessageID: "msg-1", TargetMessageID: "<orig@example.com>", ReactorEmail: "alice@example.com", Emoji: "👍", SourceMessageID: "<react-1@example.com>"},
		{MessageID: "msg-1", TargetMessageID: "<orig@example.com>", ReactorEmail: "bob@example.com", Emoji: "🎉", SourceMessageID: "<react-2@example.com>"},
	} {
		if err := store.Record(r); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}

	got, err := store.ListByMessage("msg-1")
	if err != nil {
		t.Fatalf("ListByMessage() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListByMessage() returned %d reactions, want 2", len(got))
	}
}

func TestRecordDedupsSameSourceMessage(t *testing.T) {
	db := openTestDB(t)
	mustSeedMessage(t, db, "msg-1", "<orig@example.com>")
	store := NewStore(db)

	r := &Reaction{
		MessageID:       "msg-1",
		TargetMessageID: "<orig@example.com>",
		ReactorEmail:    "alice@example.com",
		Emoji:           "👍",
		SourceMessageID: "<react-1@example.com>",
	}
	if err := store.Record(r); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	// Same reaction email recorded again (e.g. body re-fetched): no duplicate.
	if err := store.Record(r); err != nil {
		t.Fatalf("Record() again error = %v", err)
	}

	got, err := store.ListByMessage("msg-1")
	if err != nil {
		t.Fatalf("ListByMessage() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListByMessage() returned %d reactions, want 1 (dedup)", len(got))
	}
}

func TestRecordValidation(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)

	cases := []struct {
		name string
		r    *Reaction
	}{
		{"nil", nil},
		{"no target", &Reaction{ReactorEmail: "a@example.com", Emoji: "👍", SourceMessageID: "<r@example.com>"}},
		{"no reactor", &Reaction{TargetMessageID: "<o@example.com>", Emoji: "👍", SourceMessageID: "<r@example.com>"}},
		{"no emoji", &Reaction{TargetMessageID: "<o@example.com>", ReactorEmail: "a@example.com", SourceMessageID: "<r@example.com>"}},
		{"no source", &Reaction{TargetMessageID: "<o@example.com>", ReactorEmail: "a@example.com", Emoji: "👍"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := store.Record(tc.r); err == nil {
				t.Error("Record() error = nil, want an error")
			}
		})
	}
}

func TestOutOfOrderLink(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)

	// Reaction arrives before its original: recorded pending (no message_id).
	err := store.Record(&Reaction{
		TargetMessageID: "<orig@example.com>",
		ReactorEmail:    "alice@example.com",
		Emoji:           "😂",
		SourceMessageID: "<react-1@example.com>",
	})
	if err != nil {
		t.Fatalf("Record(pending) error = %v", err)
	}
	if n, err := store.CountPending(); err != nil || n != 1 {
		t.Fatalf("CountPending() = %d, err = %v, want 1", n, err)
	}

	// Original syncs later; linking fills in message_id.
	mustSeedMessage(t, db, "msg-1", "<orig@example.com>")
	linked, err := store.LinkPending("<orig@example.com>", "msg-1")
	if err != nil {
		t.Fatalf("LinkPending() error = %v", err)
	}
	if len(linked) != 1 || linked[0] != "react-1@example.com" {
		t.Fatalf("LinkPending() = %v, want [<react-1@example.com>]", linked)
	}
	if n, err := store.CountPending(); err != nil || n != 0 {
		t.Fatalf("CountPending() after link = %d, err = %v, want 0", n, err)
	}

	got, err := store.ListByMessage("msg-1")
	if err != nil {
		t.Fatalf("ListByMessage() error = %v", err)
	}
	if len(got) != 1 || got[0].Emoji != "😂" {
		t.Fatalf("ListByMessage() = %+v, want the linked 😂 reaction", got)
	}
}

func TestLinkPendingNoMatch(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)

	linked, err := store.LinkPending("<nobody@example.com>", "msg-1")
	if err != nil {
		t.Fatalf("LinkPending() error = %v", err)
	}
	if len(linked) != 0 {
		t.Fatalf("LinkPending() = %v, want empty", linked)
	}
}

func TestLinkPendingReplacesStalePendingReaction(t *testing.T) {
	db := openTestDB(t)
	store := NewStore(db)

	// Same reactor reacted while the original was missing (pending 👍),
	// then reacted again after it synced (linked ❤️). The pending row
	// must not survive alongside the linked one.
	if err := store.Record(&Reaction{
		TargetMessageID: "<orig@example.com>",
		ReactorEmail:    "alice@example.com",
		Emoji:           "👍",
		SourceMessageID: "<react-1@example.com>",
	}); err != nil {
		t.Fatalf("Record(pending) error = %v", err)
	}
	mustSeedMessage(t, db, "msg-1", "<orig@example.com>")
	if err := store.Record(&Reaction{
		MessageID:       "msg-1",
		TargetMessageID: "<orig@example.com>",
		ReactorEmail:    "alice@example.com",
		Emoji:           "❤️",
		SourceMessageID: "<react-2@example.com>",
	}); err != nil {
		t.Fatalf("Record(linked) error = %v", err)
	}

	got, err := store.ListByMessage("msg-1")
	if err != nil {
		t.Fatalf("ListByMessage() error = %v", err)
	}
	if len(got) != 1 || got[0].Emoji != "❤️" {
		t.Fatalf("ListByMessage() = %+v, want only the linked ❤️", got)
	}
	if n, err := store.CountPending(); err != nil || n != 0 {
		t.Fatalf("CountPending() = %d, err = %v, want 0 (stale pending replaced)", n, err)
	}
}
