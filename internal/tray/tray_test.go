package tray

import (
	"testing"
	"time"
)

// Start is idempotent and Stop is safe to call twice; neither may hang.
// On Linux without a D-Bus session the library logs and never becomes
// ready — the test only exercises the wrapper's state machine, not the
// desktop environment.
func TestTrayStartStopIdempotent(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		tr := New()
		tr.Start(nil, nil, func() {}, func() {})
		tr.Start(nil, nil, func() {}, func() {})
		tr.Stop()
		tr.Stop()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Start/Stop did not return within 10s")
	}
}
