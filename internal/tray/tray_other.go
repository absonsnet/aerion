//go:build !darwin

package tray

import "time"

// invokeTrayStart runs the systray library's native start directly.
// Only macOS needs the main-thread dispatch (see tray_darwin.go).
func invokeTrayStart(start func()) bool {
	start()
	return true
}

// OnMainThread runs fn directly: only macOS needs the main-thread
// dispatch (see mainthread_darwin.go).
func OnMainThread(fn func(), timeout time.Duration) bool {
	fn()
	return true
}
