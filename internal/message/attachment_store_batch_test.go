package message

import "testing"

// TestCreateBatch_ReplacesOnRefetch guards the duplicate-attachment bug: a
// body re-fetch (Force Resync, a second folder holding the same message, a
// retried body fetch) must replace the message's attachment set, not add a
// second copy of it under fresh ids. Regression for the Nov 18 bank-email
// case, where the same files were stored twice after two body fetches.
func TestCreateBatch_ReplacesOnRefetch(t *testing.T) {
	s, db := newExpandCopiesTestStore(t)
	as := NewAttachmentStore(db)
	seedCopy(t, s, "m1", "inbox-1", "<m1@example.com>", 10)
	seedCopy(t, s, "m2", "inbox-1", "<m2@example.com>", 11)

	att := func(id, msgID, filename string) *Attachment {
		return &Attachment{ID: id, MessageID: msgID, Filename: filename, ContentType: "application/pdf", Size: 100}
	}

	// First fetch: m1 has two attachments, m2 has one.
	if err := as.CreateBatch([]*Attachment{
		att("a1", "m1", "one.pdf"),
		att("a2", "m1", "two.pdf"),
		att("b1", "m2", "other.pdf"),
	}); err != nil {
		t.Fatalf("first CreateBatch: %v", err)
	}

	// Second fetch of m1 only, with fresh ids and a changed set: "one.pdf"
	// is gone, "three.pdf" is new. The old m1 rows must be replaced, not
	// doubled, and m2 must be untouched.
	if err := as.CreateBatch([]*Attachment{
		att("a3", "m1", "two.pdf"),
		att("a4", "m1", "three.pdf"),
	}); err != nil {
		t.Fatalf("second CreateBatch: %v", err)
	}

	got, err := as.GetByMessage("m1")
	if err != nil {
		t.Fatalf("GetByMessage m1: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("m1 attachments = %d, want 2 (re-fetch must replace, not duplicate)", len(got))
	}
	names := map[string]bool{}
	for _, a := range got {
		names[a.Filename] = true
	}
	if !names["two.pdf"] || !names["three.pdf"] {
		t.Fatalf("m1 filenames = %v, want [two.pdf three.pdf]", names)
	}
	if names["one.pdf"] {
		t.Fatalf("m1 still has one.pdf, which the second fetch did not produce")
	}

	other, err := as.GetByMessage("m2")
	if err != nil {
		t.Fatalf("GetByMessage m2: %v", err)
	}
	if len(other) != 1 || other[0].Filename != "other.pdf" {
		t.Fatalf("m2 attachments = %v, want [other.pdf] (untouched by m1's re-fetch)", other)
	}
}
