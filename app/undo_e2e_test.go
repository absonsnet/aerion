package app

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	imapPkg "github.com/absonsnet/daak/internal/imap"
	"github.com/absonsnet/daak/internal/database"
	"github.com/absonsnet/daak/internal/message"
	"github.com/absonsnet/daak/internal/undo"
)

// localUndoContext is a minimal undo.UndoContext for testing MoveCommand
// end-to-end: lookups go through the real App.FindLocalMessageIDs (the
// fixed path), and the move back is the local store move only — the full
// App.MoveMessagesToFolder cannot run in a test binary because it emits a
// Wails event, and Wails' runtime calls log.Fatal (os.Exit) on a non-Wails
// context.
type localUndoContext struct {
	app          *App
	messageStore *message.Store
}

func (f *localUndoContext) GetIMAPConnectionForUndo(context.Context, string) (*imapPkg.Client, func(), error) {
	return nil, nil, fmt.Errorf("no IMAP in test")
}
func (f *localUndoContext) UpdateLocalFlags([]string, *bool, *bool) error { return nil }
func (f *localUndoContext) MoveLocalMessages(ids []string, folderID string) error {
	return f.messageStore.MoveMessages(ids, folderID)
}
func (f *localUndoContext) DeleteLocalMessages([]string) error { return nil }
func (f *localUndoContext) FindLocalMessageIDs(accountID, folderID string, rfc822 []string) ([]string, error) {
	return f.app.FindLocalMessageIDs(accountID, folderID, rfc822)
}
func (f *localUndoContext) MoveMessagesToFolder(ids []string, destFolderID string) error {
	return f.messageStore.MoveMessages(ids, destFolderID)
}
func (f *localUndoContext) ResolveMessageUID(string) (uint32, string, error) {
	return 0, "", fmt.Errorf("unused in test")
}

// TestMoveCommandUndoEndToEnd drives the real MoveCommand.Undo (the path
// the toast Undo button calls via App.Undo) against a temp DB: move a
// message to All Mail / Trash (which stamps the negative temp UID of an
// in-flight move), then undo it and assert the message is back in the
// inbox. Pre-fix this fails with "messages not found in destination
// folder" because the undo lookup filtered out temp (negative) UIDs.
func TestMoveCommandUndoEndToEnd(t *testing.T) {
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
	for _, f := range [][3]string{
		{"inbox-1", "INBOX", "inbox"},
		{"allmail-1", "All Mail", "all"},
		{"trash-1", "Trash", "trash"},
	} {
		if _, err := db.Exec(`INSERT INTO folders (id, account_id, name, path, folder_type) VALUES (?, 'acc-1', ?, ?, ?)`, f[0], f[1], f[1], f[2]); err != nil {
			t.Fatal(err)
		}
	}
	seed := func(id, msgID string, uid int) {
		if _, err := db.Exec(`INSERT INTO messages (id, account_id, folder_id, uid, message_id, subject, from_name, from_email, to_list, cc_list, bcc_list) VALUES (?, 'acc-1', 'inbox-1', ?, ?, 'Subject', 'Me', 'me@example.com', '[]', '[]', '[]')`, id, uid, msgID); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed("msg-1", "<m1@example.com>", 42)
	seed("msg-2", "<m2@example.com>", 43)

	ms := message.NewStore(db)
	a := &App{messageStore: ms}
	fake := &localUndoContext{app: a, messageStore: ms}
	folderOf := func(id string) string {
		var folderID string
		if err := db.QueryRow(`SELECT folder_id FROM messages WHERE id = ?`, id).Scan(&folderID); err != nil {
			t.Fatalf("folderOf %s: %v", id, err)
		}
		return folderID
	}
	uidOf := func(id string) int {
		var uid int
		if err := db.QueryRow(`SELECT uid FROM messages WHERE id = ?`, id).Scan(&uid); err != nil {
			t.Fatalf("uidOf %s: %v", id, err)
		}
		return uid
	}

	// Archive path: local move to All Mail (temp negative UID), then undo.
	if err := ms.MoveMessages([]string{"msg-1"}, "allmail-1"); err != nil {
		t.Fatalf("move to All Mail: %v", err)
	}
	if got := folderOf("msg-1"); got != "allmail-1" {
		t.Fatalf("after move, msg-1 folder = %q, want allmail-1", got)
	}
	if uid := uidOf("msg-1"); uid >= 0 {
		t.Fatalf("expected a negative temp UID after the move, got %d", uid)
	}
	cmd := undo.NewMoveCommand(fake, "acc-1", []string{"<m1@example.com>"}, "inbox-1", "allmail-1", "Archive 1 message")
	if err := cmd.Undo(); err != nil {
		t.Fatalf("Undo after archive: %v", err)
	}
	if got := folderOf("msg-1"); got != "inbox-1" {
		t.Fatalf("after undo, msg-1 folder = %q, want inbox-1", got)
	}

	// Trash path: local move to Trash, then undo.
	if err := ms.MoveMessages([]string{"msg-2"}, "trash-1"); err != nil {
		t.Fatalf("move to Trash: %v", err)
	}
	if got := folderOf("msg-2"); got != "trash-1" {
		t.Fatalf("after move, msg-2 folder = %q, want trash-1", got)
	}
	cmd = undo.NewMoveCommand(fake, "acc-1", []string{"<m2@example.com>"}, "inbox-1", "trash-1", "Delete 1 message")
	if err := cmd.Undo(); err != nil {
		t.Fatalf("Undo after trash: %v", err)
	}
	if got := folderOf("msg-2"); got != "inbox-1" {
		t.Fatalf("after undo, msg-2 folder = %q, want inbox-1", got)
	}
}
