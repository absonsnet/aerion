package sync

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/absonsnet/daak/internal/database"
	"github.com/absonsnet/daak/internal/enrich"
	"github.com/absonsnet/daak/internal/message"
)

func newEnrichTestEngine(t *testing.T) (*Engine, *message.Store) {
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
		`INSERT INTO accounts (id, name, email, imap_host, smtp_host, username)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"acct-1", "Test", "test@example.com", "imap.example.com", "smtp.example.com", "test@example.com",
	); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO folders (id, account_id, name, path, folder_type)
		 VALUES (?, ?, ?, ?, ?)`,
		"folder-1", "acct-1", "INBOX", "INBOX", "inbox",
	); err != nil {
		t.Fatalf("seed folder: %v", err)
	}
	ms := message.NewStore(db)
	e := NewEngine(nil, nil, nil, ms, nil)
	return e, ms
}

// TestEnrichHeader covers the header-time pass: List-Unsubscribe +
// Precedence: bulk classify the message before any body arrives, and the
// one-click marker is only set when the Post header advertises it.
func TestEnrichHeader(t *testing.T) {
	e, _ := newEnrichTestEngine(t)

	m := &message.Message{FromEmail: "deals@someshop.example", Subject: "20% off everything"}
	raw := []byte("From: deals@someshop.example\r\n" +
		"Subject: 20% off everything\r\n" +
		"List-Unsubscribe: <mailto:unsub@someshop.example>\r\n" +
		"List-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n" +
		"Precedence: bulk\r\n\r\n")
	e.enrichHeader(m, raw, map[string]string{})
	if m.Category != enrich.CategoryAutomated {
		t.Errorf("category = %q, want automated", m.Category)
	}
	if m.ListUnsubscribe != "<mailto:unsub@someshop.example>" {
		t.Errorf("list_unsubscribe = %q", m.ListUnsubscribe)
	}
	if !m.ListUnsubscribePost {
		t.Errorf("list_unsubscribe_post = false, want true (Post header advertises one-click)")
	}

	// Same header without the Post marker: one-click must be false.
	m2 := &message.Message{FromEmail: "deals@someshop.example", Subject: "20% off"}
	raw2 := []byte("From: deals@someshop.example\r\n" +
		"Subject: 20% off\r\n" +
		"List-Unsubscribe: <mailto:unsub@someshop.example>\r\n\r\n")
	e.enrichHeader(m2, raw2, map[string]string{})
	if m2.ListUnsubscribePost {
		t.Errorf("list_unsubscribe_post = true without the Post header")
	}
	if m2.Category != enrich.CategoryAutomated {
		t.Errorf("category = %q, want automated (List-Unsubscribe alone)", m2.Category)
	}

	// An override to Personal beats the bulk header.
	m3 := &message.Message{FromEmail: "deals@someshop.example", Subject: "20% off"}
	e.enrichHeader(m3, raw, map[string]string{"deals@someshop.example": enrich.CategoryPersonal})
	if m3.Category != enrich.CategoryPersonal {
		t.Errorf("override: category = %q, want personal", m3.Category)
	}
}

// TestBackfillEnrichment covers the one-off backfill: unenriched rows get
// classified + extracted from their stored bodies, and a second run is a
// no-op.
func TestBackfillEnrichment(t *testing.T) {
	e, ms := newEnrichTestEngine(t)
	now := time.Now().UTC()

	otp := &message.Message{
		ID: "otp-1", AccountID: "acct-1", FolderID: "folder-1",
		UID: 1, Subject: "Your NBP OTP is 482619", FromEmail: "alerts@nbp.com.pk",
		Date: now.Add(-1 * time.Hour), Snippet: "Your OTP is 482619",
		BodyText:    "Dear customer, your one-time password is 482619.",
		BodyFetched: true,
	}
	plain := &message.Message{
		ID: "plain-1", AccountID: "acct-1", FolderID: "folder-1",
		UID: 2, Subject: "Dinner?", FromEmail: "sara@example.com",
		Date: now.Add(-2 * time.Hour), BodyFetched: true,
	}
	for _, m := range []*message.Message{otp, plain} {
		if err := ms.Create(m); err != nil {
			t.Fatalf("Create %s: %v", m.ID, err)
		}
	}

	n, err := e.BackfillEnrichment(context.Background())
	if err != nil {
		t.Fatalf("BackfillEnrichment: %v", err)
	}
	if n != 2 {
		t.Fatalf("BackfillEnrichment enriched %d, want 2", n)
	}

	got, err := ms.Get(otp.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Category != enrich.CategoryAutomated || got.ExtractValue != "482619" {
		t.Errorf("backfilled OTP: category = %q, extract = %q/%q",
			got.Category, got.ExtractKind, got.ExtractValue)
	}
	gotPlain, err := ms.Get(plain.ID)
	if err != nil {
		t.Fatalf("Get plain: %v", err)
	}
	if gotPlain.Category != enrich.CategoryPersonal {
		t.Errorf("backfilled plain: category = %q, want personal", gotPlain.Category)
	}

	// Second run: nothing left to do.
	n, err = e.BackfillEnrichment(context.Background())
	if err != nil {
		t.Fatalf("BackfillEnrichment (2nd): %v", err)
	}
	if n != 0 {
		t.Errorf("second backfill enriched %d, want 0", n)
	}
}
