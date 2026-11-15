package credentials

import (
	"testing"
)

// TestOAuthActiveChoice_LegacyValuesMigrated verifies that choice values
// stored by pre-rename builds are translated to the new IDs on read (and
// rewritten in place), and that Set also normalizes a legacy value
// instead of storing it. The legacy literals live in legacy.go; this test
// references them through those constants.
func TestOAuthActiveChoice_LegacyValuesMigrated(t *testing.T) {
	store := openTestStore(t)

	// Seed a stored row exactly as a pre-rename build would have left it.
	if _, err := store.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)`,
		oauthActiveChoiceKey("google-contacts"), legacyOAuthChoiceShipped,
	); err != nil {
		t.Fatalf("seed legacy choice: %v", err)
	}

	got, err := store.GetOAuthActiveChoice("google-contacts")
	if err != nil {
		t.Fatalf("GetOAuthActiveChoice: %v", err)
	}
	if got != "daak-shipped" {
		t.Fatalf("GetOAuthActiveChoice = %q, want %q", got, "daak-shipped")
	}

	// The stored row was rewritten to the new ID.
	var stored string
	if err := store.db.QueryRow(
		`SELECT value FROM settings WHERE key = ?`,
		oauthActiveChoiceKey("google-contacts"),
	).Scan(&stored); err != nil {
		t.Fatalf("read stored choice: %v", err)
	}
	if stored != "daak-shipped" {
		t.Fatalf("stored choice = %q, want %q", stored, "daak-shipped")
	}

	// Set with a legacy value stores the new ID.
	if err := store.SetOAuthActiveChoice("google-calendar", legacyOAuthChoiceMail); err != nil {
		t.Fatalf("SetOAuthActiveChoice: %v", err)
	}
	got, err = store.GetOAuthActiveChoice("google-calendar")
	if err != nil {
		t.Fatalf("GetOAuthActiveChoice: %v", err)
	}
	if got != "daak-mail" {
		t.Fatalf("GetOAuthActiveChoice = %q, want %q", got, "daak-mail")
	}
}
