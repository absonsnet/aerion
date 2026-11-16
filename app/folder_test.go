package app

import (
	"path/filepath"
	"testing"

	"github.com/absonsnet/daak/internal/account"
	"github.com/absonsnet/daak/internal/database"
	"github.com/absonsnet/daak/internal/folder"
)

// newSpecialFolderTestApp opens a migrated temp DB with one account and the
// given folders, and returns a minimal App wired to just the two stores
// GetSpecialFolder needs.
func newSpecialFolderTestApp(t *testing.T, folders [][3]string) *App {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO accounts (id, name, email, imap_host, smtp_host, username) VALUES (?, ?, ?, ?, ?, ?)`,
		"acct-1", "Test", "test@example.com", "imap.example.com", "smtp.example.com", "test@example.com",
	); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	for _, f := range folders {
		if _, err := db.Exec(
			`INSERT INTO folders (id, account_id, name, path, folder_type) VALUES (?, ?, ?, ?, ?)`,
			f[0], "acct-1", f[1], f[1], f[2],
		); err != nil {
			t.Fatalf("seed folder %s: %v", f[0], err)
		}
	}

	return &App{accountStore: account.NewStore(db), folderStore: folder.NewStore(db)}
}

// TestGetSpecialFolder_ArchiveFallsBackToAllMail: Gmail has no Archive
// folder — archiving there is a move to All Mail. The sync-time mapping
// default (#464) covers accounts that have synced since it landed, but an
// account whose mapping was never persisted must still archive instead of
// failing with "no archive folder configured".
func TestGetSpecialFolder_ArchiveFallsBackToAllMail(t *testing.T) {
	a := newSpecialFolderTestApp(t, [][3]string{
		{"inbox-1", "INBOX", "inbox"},
		{"allmail-1", "All Mail", "all"},
	})

	f, err := a.GetSpecialFolder("acct-1", folder.TypeArchive)
	if err != nil {
		t.Fatalf("GetSpecialFolder(archive): %v", err)
	}
	if f == nil {
		t.Fatalf("GetSpecialFolder(archive) = nil, want the All Mail folder")
	}
	if f.Type != folder.TypeAll || f.ID != "allmail-1" {
		t.Fatalf("GetSpecialFolder(archive) = %s (%s), want allmail-1 (all)", f.ID, f.Type)
	}
}

// TestGetSpecialFolder_ArchivePrefersRealArchive: when a real Archive folder
// exists, it wins over the All Mail fallback.
func TestGetSpecialFolder_ArchivePrefersRealArchive(t *testing.T) {
	a := newSpecialFolderTestApp(t, [][3]string{
		{"inbox-1", "INBOX", "inbox"},
		{"archive-1", "Archive", "archive"},
		{"allmail-1", "All Mail", "all"},
	})

	f, err := a.GetSpecialFolder("acct-1", folder.TypeArchive)
	if err != nil {
		t.Fatalf("GetSpecialFolder(archive): %v", err)
	}
	if f == nil || f.ID != "archive-1" {
		t.Fatalf("GetSpecialFolder(archive) = %v, want archive-1", f)
	}
}

// TestGetSpecialFolder_ArchiveNilWithoutAllMail: with neither an Archive nor
// an All Mail folder, the lookup still returns nil (no folder to archive
// to) rather than erroring.
func TestGetSpecialFolder_ArchiveNilWithoutAllMail(t *testing.T) {
	a := newSpecialFolderTestApp(t, [][3]string{
		{"inbox-1", "INBOX", "inbox"},
	})

	f, err := a.GetSpecialFolder("acct-1", folder.TypeArchive)
	if err != nil {
		t.Fatalf("GetSpecialFolder(archive): %v", err)
	}
	if f != nil {
		t.Fatalf("GetSpecialFolder(archive) = %v, want nil", f)
	}
}
