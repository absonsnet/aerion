package sendqueue

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/absonsnet/daak/internal/database"
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

func testItem(id string) Item {
	now := time.Now().UTC().Truncate(time.Second)
	return Item{
		ID:          id,
		AccountID:   "acc-1",
		DraftID:     "draft-1",
		MessageJSON: `{"subject":"hi"}`,
		Subject:     "hi",
		Recipient:   "a@example.com",
		FromAddress: "me@example.com",
		To:          []string{"a@example.com"},
		Status:      StatusPending,
		Attempts:    0,
		MaxAttempts: DefaultMaxAttempts,
		SendAt:      now.Add(5 * time.Second),
		DueAt:       now.Add(5 * time.Second),
		CreatedAt:   now,
	}
}

func TestEnqueueGetList(t *testing.T) {
	s := NewStore(openTestDB(t))
	if err := s.Enqueue(testItem("q1")); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("q1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("Get returned nil for an enqueued item")
	}
	if got.Subject != "hi" || got.Recipient != "a@example.com" || got.DraftID != "draft-1" {
		t.Fatalf("Get = %+v, want subject hi / recipient a@example.com / draft-1", got)
	}
	if got.MessageJSON != "" {
		t.Fatalf("Get must not load message JSON, got %q", got.MessageJSON)
	}
	full, err := s.GetForSend("q1")
	if err != nil {
		t.Fatal(err)
	}
	if full == nil || full.MessageJSON != `{"subject":"hi"}` {
		t.Fatalf("GetForSend message JSON = %q, want stored JSON", full.MessageJSON)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "q1" {
		t.Fatalf("List = %d items, want 1 with id q1", len(list))
	}
	missing, err := s.Get("nope")
	if err != nil || missing != nil {
		t.Fatalf("Get(missing) = %v, %v, want nil, nil", missing, err)
	}
}

func TestListDue(t *testing.T) {
	s := NewStore(openTestDB(t))
	now := time.Now().UTC()
	early := testItem("early")
	early.SendAt = now.Add(-time.Minute)
	early.DueAt = now.Add(-time.Minute)
	late := testItem("late")
	late.SendAt = now.Add(time.Hour)
	late.DueAt = now.Add(time.Hour)
	for _, it := range []Item{late, early} {
		if err := s.Enqueue(it); err != nil {
			t.Fatal(err)
		}
	}
	due, err := s.ListDue(now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != "early" {
		t.Fatalf("ListDue = %v, want only [early]", due)
	}
}

func TestClaimIsAtomic(t *testing.T) {
	s := NewStore(openTestDB(t))
	if err := s.Enqueue(testItem("q1")); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	claimed, err := s.Claim("q1", now)
	if err != nil || !claimed {
		t.Fatalf("first Claim = %v, %v, want true, nil", claimed, err)
	}
	// Second claim must fail: the item is 'sending', not 'pending'.
	claimed, err = s.Claim("q1", now)
	if err != nil || claimed {
		t.Fatalf("second Claim = %v, %v, want false, nil", claimed, err)
	}
}

func TestCancelOnlyPending(t *testing.T) {
	s := NewStore(openTestDB(t))
	if err := s.Enqueue(testItem("q1")); err != nil {
		t.Fatal(err)
	}
	ok, err := s.Cancel("q1")
	if err != nil || !ok {
		t.Fatalf("Cancel(pending) = %v, %v, want true, nil", ok, err)
	}
	// Already cancelled: a second cancel is a no-op.
	ok, err = s.Cancel("q1")
	if err != nil || ok {
		t.Fatalf("Cancel(cancelled) = %v, %v, want false, nil", ok, err)
	}
	// A sent item cannot be cancelled (the mail is gone).
	if err := s.Enqueue(testItem("q2")); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := s.Claim("q2", now); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkSent("q2", now); err != nil {
		t.Fatal(err)
	}
	ok, err = s.Cancel("q2")
	if err != nil || ok {
		t.Fatalf("Cancel(sent) = %v, %v, want false, nil", ok, err)
	}
}

func TestRetryOnlyFailed(t *testing.T) {
	s := NewStore(openTestDB(t))
	if err := s.Enqueue(testItem("q1")); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// Pending items cannot be "retried".
	ok, err := s.Retry("q1", now)
	if err != nil || ok {
		t.Fatalf("Retry(pending) = %v, %v, want false, nil", ok, err)
	}
	// Fail it, then retry: attempts reset, due now.
	if err := s.RecordAttempt("q1", DefaultMaxAttempts, StatusFailed, now, "boom"); err != nil {
		t.Fatal(err)
	}
	ok, err = s.Retry("q1", now)
	if err != nil || !ok {
		t.Fatalf("Retry(failed) = %v, %v, want true, nil", ok, err)
	}
	got, _ := s.Get("q1")
	if got.Status != StatusPending || got.Attempts != 0 || got.LastError != "" {
		t.Fatalf("after Retry: %+v, want pending/0 attempts/no error", got)
	}
	if got.DueAt.Before(now.Add(-time.Second)) || got.DueAt.After(now.Add(time.Second)) {
		t.Fatalf("after Retry: dueAt %v, want ~now", got.DueAt)
	}
}

func TestResetStaleSending(t *testing.T) {
	s := NewStore(openTestDB(t))
	if err := s.Enqueue(testItem("q1")); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := s.Claim("q1", now.Add(-20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	n, err := s.ResetStaleSending(now.Add(-10 * time.Minute))
	if err != nil || n != 1 {
		t.Fatalf("ResetStaleSending = %d, %v, want 1, nil", n, err)
	}
	got, _ := s.Get("q1")
	if got.Status != StatusPending {
		t.Fatalf("after reset: status %q, want pending", got.Status)
	}
	// A fresh claim is NOT stale.
	if _, err := s.Claim("q1", now); err != nil {
		t.Fatal(err)
	}
	n, err = s.ResetStaleSending(now.Add(-10 * time.Minute))
	if err != nil || n != 0 {
		t.Fatalf("ResetStaleSending(fresh) = %d, %v, want 0, nil", n, err)
	}
}

func TestStatsAndCountPending(t *testing.T) {
	s := NewStore(openTestDB(t))
	for _, id := range []string{"q1", "q2"} {
		if err := s.Enqueue(testItem(id)); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.CountPending(); err != nil || n != 2 {
		t.Fatalf("CountPending = %d, %v, want 2, nil", n, err)
	}
	now := time.Now().UTC()
	if _, err := s.Claim("q1", now); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkSent("q1", now); err != nil {
		t.Fatal(err)
	}
	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != 2 || st.Pending != 1 || st.Sent != 1 {
		t.Fatalf("Stats = %+v, want total 2 / pending 1 / sent 1", st)
	}
}

func TestBackoff(t *testing.T) {
	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{0, time.Minute},
		{1, time.Minute},
		{2, 2 * time.Minute},
		{3, 4 * time.Minute},
		{4, 8 * time.Minute},
		{5, 16 * time.Minute},
		{10, time.Hour}, // capped
	}
	for _, c := range cases {
		if got := Backoff(c.attempts); got != c.want {
			t.Errorf("Backoff(%d) = %v, want %v", c.attempts, got, c.want)
		}
	}
}

func TestIsPermanentError(t *testing.T) {
	permanent := []string{
		"failed to login to SMTP server: 535 Authentication failed",
		"account \"x\" is configured as receive-only (no outgoing server)",
		"no recipients specified",
		"failed to build message: bad header",
		"account not found: abc",
		"OAuth token refresh failed, re-authorization required: unauthorized_client",
	}
	for _, msg := range permanent {
		if !IsPermanentError(errString(msg)) {
			t.Errorf("IsPermanentError(%q) = false, want true", msg)
		}
	}
	transient := []string{
		"failed to connect to SMTP server: connection refused",
		"failed to send message: read tcp: i/o timeout",
		"server busy, try again later",
		"451 greylisted, try again",
		"EOF",
	}
	for _, msg := range transient {
		if IsPermanentError(errString(msg)) {
			t.Errorf("IsPermanentError(%q) = true, want false", msg)
		}
	}
	if IsPermanentError(nil) {
		t.Errorf("IsPermanentError(nil) = true, want false")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
