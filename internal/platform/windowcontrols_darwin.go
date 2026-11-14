//go:build darwin

package platform

// macOS always puts the window controls on the left, in the order
// close, minimize, maximize, drawn as traffic-light circles.
func detectWindowControlsLayout() WindowControlsLayout {
	return WindowControlsLayout{
		Side:  WindowControlsLeft,
		Order: append([]string(nil), canonicalLeftOrder...),
		Style: WindowControlsTraffic,
	}
}
