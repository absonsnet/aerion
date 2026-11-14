//go:build windows

package platform

// Windows always puts the window controls on the right, in the order
// minimize, maximize, close, drawn as glyph buttons (─ ▢ ✕).
func detectWindowControlsLayout() WindowControlsLayout {
	return WindowControlsLayout{
		Side:  WindowControlsRight,
		Order: append([]string(nil), canonicalRightOrder...),
		Style: WindowControlsGlyphs,
	}
}
