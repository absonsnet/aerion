package message

import "testing"

// TestOnDemandRefetch_Replaces guards the Nov 21 duplicate-attachment
// report: a message with 5 files showed each one twice (10 rows). The
// batch body-fetch path stored via CreateBatch (delete-first), but the
// on-demand body fetch (Engine.FetchMessageBody) stored via plain
// Create, which appends. The app-level BodyFetched guard is
// check-then-act, so a batch applying mid-fetch — or two on-demand
// fetches racing — both pass it and each stores a full second copy
// under fresh ids. Proven during the investigation: CreateBatch(5)
// then plain Create x5 yields 10 rows, each filename exactly twice.
// The fix makes the on-demand path (and the server-search fetch) store
// via CreateBatch too, so a re-fetch replaces instead of doubling.
func TestOnDemandRefetch_Replaces(t *testing.T) {
	s, db := newExpandCopiesTestStore(t)
	as := NewAttachmentStore(db)
	seedCopy(t, s, "m1", "inbox-1", "<m1@example.com>", 10)

	five := func(idPrefix string) []*Attachment {
		names := []string{"1000210286.jpg", "1000210287.jpg", "1000212485.jpg", "Muhammad Umar CV-1.pdf", "douctoment.pdf"}
		out := make([]*Attachment, 0, len(names))
		for i, n := range names {
			out = append(out, &Attachment{ID: idPrefix + string(rune('a'+i)), MessageID: "m1", Filename: n, ContentType: "image/jpeg", Size: 100})
		}
		return out
	}

	// First fetch stores 5 (batch path).
	if err := as.CreateBatch(five("b")); err != nil {
		t.Fatalf("first CreateBatch: %v", err)
	}

	// Second fetch of the same 5 under fresh ids — the exact sequence
	// the fixed on-demand path performs — must replace, not double.
	if err := as.CreateBatch(five("o")); err != nil {
		t.Fatalf("second CreateBatch: %v", err)
	}

	got, err := as.GetByMessage("m1")
	if err != nil {
		t.Fatalf("GetByMessage: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("attachments = %d, want 5 (re-fetch must replace, not duplicate)", len(got))
	}
	names := map[string]int{}
	for _, a := range got {
		names[a.Filename]++
	}
	for n, c := range names {
		if c != 1 {
			t.Fatalf("filename %q appears %d times, want 1", n, c)
		}
	}
}
