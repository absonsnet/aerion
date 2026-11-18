//go:build !windows

package platform

// StartQuitEventWatcher is a no-op outside Windows: the installer quit
// handshake is a Windows-only feature, so there is nothing to watch for.
func StartQuitEventWatcher(onQuit func()) {}
