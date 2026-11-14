//go:build !darwin && !windows && !linux

package platform

// Other platforms fall back to buttons on the right.
func detectWindowControlsLayout() WindowControlsLayout {
	return fallbackLayout()
}
