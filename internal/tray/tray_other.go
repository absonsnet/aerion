//go:build !darwin

package tray

// invokeTrayStart runs the systray library's native start directly.
// Only macOS needs the main-thread dispatch (see tray_darwin.go).
func invokeTrayStart(start func()) bool {
	start()
	return true
}
