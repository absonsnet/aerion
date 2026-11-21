package tray

import (
	"testing"
	"time"
)

func TestDoubleClickDetector(t *testing.T) {
	calls := 0
	d := &doubleClickDetector{interval: 50 * time.Millisecond, onDoubleClick: func() { calls++ }}

	// A lone tap does nothing.
	d.tap()
	if calls != 0 {
		t.Fatalf("single tap fired onDoubleClick, want 0 calls")
	}

	// Two rapid taps fire exactly once.
	d.tap()
	if calls != 1 {
		t.Fatalf("double tap: got %d calls, want 1", calls)
	}

	// A third rapid tap starts a new pair instead of re-firing.
	d.tap()
	if calls != 1 {
		t.Fatalf("third tap: got %d calls, want 1", calls)
	}

	// Two taps further apart than the interval do not fire.
	d2 := &doubleClickDetector{interval: 50 * time.Millisecond, onDoubleClick: func() { calls++ }}
	d2.tap()
	time.Sleep(100 * time.Millisecond)
	d2.tap()
	if calls != 1 {
		t.Fatalf("slow taps: got %d calls, want 1", calls)
	}
}

// waitForReady (used by the Windows start to wait for the tray's
// menu to be registered): a closed channel reports true at once, an
// open one reports false after the timeout.
func TestWaitForReady(t *testing.T) {
	ready := make(chan struct{})
	close(ready)
	if !waitForReady(ready, time.Second) {
		t.Fatal("closed channel: got false, want true")
	}

	never := make(chan struct{})
	if waitForReady(never, 10*time.Millisecond) {
		t.Fatal("open channel: got true, want false after timeout")
	}
}

// Start is idempotent and Stop is safe to call twice; neither may hang.
// On Linux without a D-Bus session the library logs and never becomes
// ready — the test only exercises the wrapper's state machine, not the
// desktop environment.
func TestTrayStartStopIdempotent(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		tr := New()
		tr.Start(nil, nil, func() {}, func() {}, func() {}, func() {}, func() {})
		tr.Start(nil, nil, func() {}, func() {}, func() {}, func() {}, func() {})
		tr.Stop()
		tr.Stop()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Start/Stop did not return within 10s")
	}
}
