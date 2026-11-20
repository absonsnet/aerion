// Package tray provides Daak's system tray icon (Windows, macOS, Linux).
//
// The icon is shown whenever "Run in background" is on — including
// start-hidden launches — so a window hidden with CloseWindow or
// BeforeClose can always be reopened from the tray: a left-click (or the
// Open Daak menu item) shows the window, Quit Daak quits for real.
//
// It is built on fyne.io/systray via RunWithExternalLoop, the library's
// embedding API, so the tray coexists with Wails' own main loop (on
// macOS the tray piggybacks on Wails' NSApplication loop instead of
// taking over the app loop, which plain Run would do).
//
// Lifetime: the tray lives for the app session once started. Turning
// background mode off mid-session deliberately does NOT hide it: the
// library's Quit is once-only and its Windows window class cannot be
// re-registered, so a tray hidden that way could never be shown again —
// which would recreate the exact bug the tray exists to fix (a hidden
// window with no way back). The icon disappears when the app really
// quits (Stop, called from App.Shutdown; process exit removes it
// regardless).
package tray

import (
	"runtime"
	"sync"

	"fyne.io/systray"
	"github.com/godbus/dbus/v5"

	"github.com/absonsnet/daak/internal/logging"
)

// Tray owns the system tray icon for one app session.
type Tray struct {
	mu      sync.Mutex
	started bool
	end     func()
}

// New returns a Tray that has not been started yet.
func New() *Tray {
	return &Tray{}
}

// Start shows the tray icon. It is idempotent: later calls are no-ops.
// iconICO is the Windows .ico bytes, iconPNG the PNG bytes used on
// macOS/Linux; either may be empty, in which case the library's default
// icon is used. onShow shows the window (left-click or Open Daak);
// onQuit quits the app for real (Quit Daak).
func (t *Tray) Start(iconICO, iconPNG []byte, onShow, onQuit func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return
	}

	log := logging.WithComponent("tray")

	// On Linux the tray is a D-Bus StatusNotifierItem: without a session
	// bus there is no tray host, and the library panics in its shutdown
	// path (it closes a nil connection) if started anyway. Skip starting
	// entirely in that case; a later Start retries.
	if runtime.GOOS == "linux" {
		conn, err := dbus.SessionBus()
		if err != nil {
			log.Warn().Err(err).Msg("No D-Bus session bus, skipping system tray icon")
			return
		}
		conn.Close()
	}

	t.started = true
	icon := iconPNG
	if runtime.GOOS == "windows" {
		icon = iconICO
	}

	start, end := systray.RunWithExternalLoop(func() {
		if len(icon) > 0 {
			systray.SetIcon(icon)
		}
		systray.SetTooltip("Daak")
		systray.SetOnTapped(onShow)

		openItem := systray.AddMenuItem("Open Daak", "Show the Daak window")
		quitItem := systray.AddMenuItem("Quit Daak", "Quit Daak")
		go func() {
			for range openItem.ClickedCh {
				onShow()
			}
		}()
		go func() {
			for range quitItem.ClickedCh {
				onQuit()
			}
		}()
		log.Info().Msg("System tray icon shown")
	}, func() {
		log.Info().Msg("System tray icon hidden")
	})
	t.end = end
	start()
}

// Stop ends the tray loop. App shutdown only — see the package comment
// for why turning background mode off does not call this.
func (t *Tray) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.started || t.end == nil {
		return
	}
	t.end()
	t.started = false
	t.end = nil
}
