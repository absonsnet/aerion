package platform

import "strings"

// WindowControlsLayout describes the placement, order and style of the
// window control buttons (close, minimize, maximize) in the custom title
// bar. It is computed once at startup by DetectWindowControlsLayout and
// reflects the operating system and, on Linux, the desktop environment's
// own button layout.
type WindowControlsLayout struct {
	// Side is where the buttons sit: "left" or "right".
	Side string `json:"side"`
	// Order is the display order of the buttons, e.g.
	// ["close", "minimize", "maximize"]. Always contains all three
	// buttons; see completeOrder.
	Order []string `json:"order"`
	// Style is "traffic" (macOS-style coloured circles) or "glyphs"
	// (Windows-style ─ ▢ ✕ buttons).
	Style string `json:"style"`
}

const (
	// WindowControlsLeft places the buttons on the left of the title bar.
	WindowControlsLeft = "left"
	// WindowControlsRight places the buttons on the right of the title bar.
	WindowControlsRight = "right"
	// WindowControlsTraffic draws macOS-style coloured circles.
	WindowControlsTraffic = "traffic"
	// WindowControlsGlyphs draws Windows-style glyph buttons.
	WindowControlsGlyphs = "glyphs"
)

// Canonical button orders per side. The left order is the macOS/Ubuntu
// convention (close first); the right order is the Windows/GNOME/KDE
// convention (close last).
var (
	canonicalLeftOrder  = []string{"close", "minimize", "maximize"}
	canonicalRightOrder = []string{"minimize", "maximize", "close"}
)

// DetectWindowControlsLayout returns the window-controls layout for the
// current OS and desktop environment. The per-OS implementation lives in
// windowcontrols_<goos>.go; on Linux it reads the desktop environment's
// own button-layout setting (gsettings, kwinrc, xfconf) and falls back to
// the right side when nothing is detected.
func DetectWindowControlsLayout() WindowControlsLayout {
	return detectWindowControlsLayout()
}

// fallbackLayout is used when no desktop environment setting could be
// read: buttons on the right, Windows/GNOME order, traffic-light style.
func fallbackLayout() WindowControlsLayout {
	return WindowControlsLayout{
		Side:  WindowControlsRight,
		Order: append([]string(nil), canonicalRightOrder...),
		Style: WindowControlsTraffic,
	}
}

// completeOrder returns the full three-button order for a side, starting
// with the buttons the desktop environment listed for that side (in the
// listed order) and appending any missing buttons in the canonical order
// for the side. Desktop settings sometimes list only a subset (e.g. just
// "close"), and the app still needs all three window controls.
func completeOrder(side string, found []string) []string {
	canonical := canonicalRightOrder
	if side == WindowControlsLeft {
		canonical = canonicalLeftOrder
	}
	seen := make(map[string]bool, 3)
	order := make([]string, 0, 3)
	for _, b := range found {
		if b == "close" || b == "minimize" || b == "maximize" {
			if !seen[b] {
				seen[b] = true
				order = append(order, b)
			}
		}
	}
	for _, b := range canonical {
		if !seen[b] {
			order = append(order, b)
		}
	}
	return order
}

// parseGsettingsButtonLayout parses the value of
// org.gnome.desktop.wm.preferences button-layout, as returned by
// `gsettings get` (a quoted GVariant string, e.g.
// 'close,minimize,maximize:appmenu' or 'appmenu:close,minimize,maximize').
// The part before ':' is the left side, the part after ':' the right side;
// the side containing "close" is where the buttons go, and the order
// within that side is the button order. Returns ok=false when the value
// is empty or lists no close button on either side.
func parseGsettingsButtonLayout(raw string) (side string, order []string, ok bool) {
	value := strings.Trim(raw, "'\" \t\r\n")
	if value == "" {
		return "", nil, false
	}
	parts := strings.SplitN(value, ":", 2)
	left := parseButtonNames(parts[0])
	var right []string
	if len(parts) == 2 {
		right = parseButtonNames(parts[1])
	}
	if containsButton(left, "close") {
		return WindowControlsLeft, completeOrder(WindowControlsLeft, left), true
	}
	if containsButton(right, "close") {
		return WindowControlsRight, completeOrder(WindowControlsRight, right), true
	}
	return "", nil, false
}

// parseButtonNames splits a comma-separated gsettings button list and
// keeps only the three window-control names, in listed order.
func parseButtonNames(list string) []string {
	var out []string
	for _, name := range strings.Split(list, ",") {
		switch strings.TrimSpace(name) {
		case "close", "minimize", "maximize":
			out = append(out, strings.TrimSpace(name))
		}
	}
	return out
}

// parseKwinrcButtons parses a kwinrc file ([org.kde.kdecoration2]
// section, ButtonsOnLeft / ButtonsOnRight keys) and returns the side
// and order of the window controls. KDE stores the buttons as a string
// of single-letter codes (M menu, S sticky, H help, I minimize,
// A maximize, X close), e.g. ButtonsOnLeft=MS, ButtonsOnRight=HIAX.
// Commas between codes are tolerated. Returns ok=false when the section
// is missing or neither side lists a close button.
func parseKwinrcButtons(content string) (side string, order []string, ok bool) {
	inSection := false
	var left, right string
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inSection = line == "[org.kde.kdecoration2]"
			continue
		}
		if !inSection {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "ButtonsOnLeft":
			left = strings.Trim(strings.TrimSpace(value), "'\"")
		case "ButtonsOnRight":
			right = strings.Trim(strings.TrimSpace(value), "'\"")
		}
	}
	leftButtons := parseKDEButtonCodes(left)
	rightButtons := parseKDEButtonCodes(right)
	if containsButton(leftButtons, "close") {
		return WindowControlsLeft, completeOrder(WindowControlsLeft, leftButtons), true
	}
	if containsButton(rightButtons, "close") {
		return WindowControlsRight, completeOrder(WindowControlsRight, rightButtons), true
	}
	return "", nil, false
}

// parseKDEButtonCodes maps a kwinrc button-code string to button names.
// Unknown codes (menu, sticky, help, …) and separators are ignored.
func parseKDEButtonCodes(codes string) []string {
	var out []string
	for _, c := range codes {
		switch c {
		case 'X':
			out = append(out, "close")
		case 'I':
			out = append(out, "minimize")
		case 'A':
			out = append(out, "maximize")
		}
	}
	return out
}

// parseXfconfButtonLayout parses the value of xfwm4's button_layout, as
// returned by `xfconf-query -c xfwm4 -p /general/button_layout`
// (e.g. "O|HMC" or "CHM|O"). The part before '|' is the left side, the
// part after '|' the right side. Codes: O menu, H hide (minimize),
// M maximize, C close. Returns ok=false when the value is empty or
// lists no close button on either side.
func parseXfconfButtonLayout(raw string) (side string, order []string, ok bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil, false
	}
	parts := strings.SplitN(value, "|", 2)
	left := parseXfceButtonCodes(parts[0])
	var right []string
	if len(parts) == 2 {
		right = parseXfceButtonCodes(parts[1])
	}
	if containsButton(left, "close") {
		return WindowControlsLeft, completeOrder(WindowControlsLeft, left), true
	}
	if containsButton(right, "close") {
		return WindowControlsRight, completeOrder(WindowControlsRight, right), true
	}
	return "", nil, false
}

// parseXfceButtonCodes maps an xfwm4 button-layout side to button names.
// Unknown codes (menu, shade, stick, …) are ignored.
func parseXfceButtonCodes(codes string) []string {
	var out []string
	for _, c := range codes {
		switch c {
		case 'C':
			out = append(out, "close")
		case 'H':
			out = append(out, "minimize")
		case 'M':
			out = append(out, "maximize")
		}
	}
	return out
}

func containsButton(buttons []string, name string) bool {
	for _, b := range buttons {
		if b == name {
			return true
		}
	}
	return false
}
