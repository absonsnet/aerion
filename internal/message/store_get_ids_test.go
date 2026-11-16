package message

import "testing"

// TestGetIDsByMessageIDs_FindsInFlightMove guards the undo path: MoveMessages
// stamps a just-moved row uid = -rowid (negative placeholder) until the
// destination sync replaces it with a server-assigned UID. The undo lookup
// must find the row while it still carries that placeholder — the toast is
// only up for 8 seconds, and on an account whose destination sync cannot
// complete the placeholder never clears at all.
func TestGetIDsByMessageIDs_FindsInFlightMove(t *testing.T) {
	s, _ := newExpandCopiesTestStore(t)

	seedCopy(t, s, "m1", "inbox-1", "<m1@example.com>", 10)
	if err := s.MoveMessages([]string{"m1"}, "sent-1"); err != nil {
		t.Fatalf("MoveMessages: %v", err)
	}

	ids, err := s.GetIDsByMessageIDs("acct-1", "sent-1", []string{"<m1@example.com>"})
	if err != nil {
		t.Fatalf("GetIDsByMessageIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "m1" {
		t.Fatalf("GetIDsByMessageIDs = %v, want [m1] (in-flight move must be found)", ids)
	}

	// A positive-UID row is still found the same way.
	seedCopy(t, s, "m2", "sent-1", "<m2@example.com>", 20)
	ids, err = s.GetIDsByMessageIDs("acct-1", "sent-1", []string{"<m2@example.com>"})
	if err != nil {
		t.Fatalf("GetIDsByMessageIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "m2" {
		t.Fatalf("GetIDsByMessageIDs = %v, want [m2]", ids)
	}

	// And a message in a different folder is not returned.
	ids, err = s.GetIDsByMessageIDs("acct-1", "inbox-1", []string{"<m1@example.com>"})
	if err != nil {
		t.Fatalf("GetIDsByMessageIDs: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("GetIDsByMessageIDs = %v, want [] (moved message left the inbox)", ids)
	}
}
