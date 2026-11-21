//go:build !darwin && !windows

package tray

// setDockBadge is a no-op off macOS (no Dock badge API).
func setDockBadge(count int) bool {
	return true
}

// setTaskbarOverlay is a no-op off Windows (no taskbar overlay API).
// Linux has no standard taskbar/dock badge API on GNOME or KDE; the
// tray icon badge below is the Linux surface.
func setTaskbarOverlay(count int) bool {
	return true
}
