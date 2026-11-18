//go:build !windows

package platform

import "testing"

// The quit-event watcher is a no-op outside Windows: it must return at once
// and never invoke the callback.
func TestStartQuitEventWatcherNoop(t *testing.T) {
	called := false
	StartQuitEventWatcher(func() { called = true })
	if called {
		t.Fatal("StartQuitEventWatcher invoked the callback on a non-Windows build")
	}
}
