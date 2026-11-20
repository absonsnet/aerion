//go:build darwin

package tray

import "time"

// invokeTrayStart runs the systray library's native start on the macOS
// main thread (see mainthread_darwin.go — the library creates AppKit
// objects and crashes if started from a worker goroutine). Returns false
// if the main thread did not run it within 5 seconds.
func invokeTrayStart(start func()) bool {
	return runOnMainThread(start, 5*time.Second)
}
