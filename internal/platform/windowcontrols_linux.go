//go:build linux

package platform

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// detectWindowControlsLayout reads the Linux desktop environment's own
// button-layout setting. The primary source depends on the desktop
// environment (gsettings for GNOME-family, kwinrc for KDE, xfconf for
// XFCE); each source is tried in turn until one yields a layout. Inside a
// Flatpak, gsettings reads the sandboxed dconf instead of the host's, so
// the desktop-environment hint is trusted there instead. When nothing is
// detected, the fallback is buttons on the right.
func detectWindowControlsLayout() WindowControlsLayout {
	de := strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP"))

	// Inside a Flatpak the host's gsettings is not visible (the sandboxed
	// dconf returns its own default), so the DE hint is the only reliable
	// signal. Ubuntu's default layout is left; everything else falls back
	// to right.
	if isFlatpak() {
		if side, order, ok := layoutFromDesktopHint(de); ok {
			return WindowControlsLayout{Side: side, Order: order, Style: WindowControlsTraffic}
		}
		return fallbackLayout()
	}

	// Try the DE's own source first, then the other two, so a KDE user
	// with GNOME apps installed (whose dconf still holds GNOME's default)
	// is not mis-detected.
	switch {
	case strings.Contains(de, "kde"):
		if l, ok := layoutFromKwinrc(); ok {
			return l
		}
		if l, ok := layoutFromGsettings(); ok {
			return l
		}
		if l, ok := layoutFromXfconf(); ok {
			return l
		}
	case strings.Contains(de, "xfce"):
		if l, ok := layoutFromXfconf(); ok {
			return l
		}
		if l, ok := layoutFromGsettings(); ok {
			return l
		}
		if l, ok := layoutFromKwinrc(); ok {
			return l
		}
	default: // GNOME, Unity, Cinnamon, MATE, and anything unrecognised
		if l, ok := layoutFromGsettings(); ok {
			return l
		}
		if l, ok := layoutFromKwinrc(); ok {
			return l
		}
		if l, ok := layoutFromXfconf(); ok {
			return l
		}
	}

	if side, order, ok := layoutFromDesktopHint(de); ok {
		return WindowControlsLayout{Side: side, Order: order, Style: WindowControlsTraffic}
	}
	return fallbackLayout()
}

// layoutFromGsettings reads org.gnome.desktop.wm.preferences
// button-layout via the gsettings CLI.
func layoutFromGsettings() (WindowControlsLayout, bool) {
	out, err := runCommand(2*time.Second, "gsettings", "get", "org.gnome.desktop.wm.preferences", "button-layout")
	if err != nil {
		return WindowControlsLayout{}, false
	}
	side, order, ok := parseGsettingsButtonLayout(out)
	if !ok {
		return WindowControlsLayout{}, false
	}
	return WindowControlsLayout{Side: side, Order: order, Style: WindowControlsTraffic}, true
}

// layoutFromKwinrc reads ~/.config/kwinrc ([org.kde.kdecoration2]).
func layoutFromKwinrc() (WindowControlsLayout, bool) {
	path := kwinrcPath()
	content, err := os.ReadFile(path)
	if err != nil {
		return WindowControlsLayout{}, false
	}
	side, order, ok := parseKwinrcButtons(string(content))
	if !ok {
		return WindowControlsLayout{}, false
	}
	return WindowControlsLayout{Side: side, Order: order, Style: WindowControlsTraffic}, true
}

// layoutFromXfconf reads xfwm4's button_layout via xfconf-query.
func layoutFromXfconf() (WindowControlsLayout, bool) {
	out, err := runCommand(2*time.Second, "xfconf-query", "-c", "xfwm4", "-p", "/general/button_layout")
	if err != nil {
		return WindowControlsLayout{}, false
	}
	side, order, ok := parseXfconfButtonLayout(out)
	if !ok {
		return WindowControlsLayout{}, false
	}
	return WindowControlsLayout{Side: side, Order: order, Style: WindowControlsTraffic}, true
}

// layoutFromDesktopHint derives the layout from XDG_CURRENT_DESKTOP
// alone. Only Ubuntu is recognised (its default layout is left);
// every other desktop falls through to the right-side fallback.
func layoutFromDesktopHint(de string) (string, []string, bool) {
	for _, part := range strings.Split(de, ":") {
		if strings.TrimSpace(part) == "ubuntu" {
			return WindowControlsLeft, append([]string(nil), canonicalLeftOrder...), true
		}
	}
	return "", nil, false
}

// isFlatpak reports whether the process runs inside a Flatpak sandbox.
func isFlatpak() bool {
	if os.Getenv("FLATPAK_ID") != "" {
		return true
	}
	if _, err := os.Stat("/.flatpak-info"); err == nil {
		return true
	}
	return false
}

// kwinrcPath returns the kwinrc location, honouring XDG_CONFIG_HOME.
func kwinrcPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "kwinrc")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "kwinrc")
}

// runCommand runs a command with a timeout and returns its trimmed
// standard output. Any error (missing binary, timeout, non-zero exit)
// yields an error.
func runCommand(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
