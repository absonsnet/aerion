// Package tray provides Daak's system tray icon (Windows, macOS, Linux).
//
// The icon is shown whenever "Run in background" is on — including
// start-hidden launches — so a window hidden with CloseWindow or
// BeforeClose can always be reopened from the tray. Interaction:
//   - Double left-click shows the window; a single left-click does
//     nothing.
//   - Right-click opens the menu, in order: Open Daak (show the
//     window), Compose new message (show the window, then open the
//     composer), Check mail now (sync all accounts in the background,
//     window stays as it is), Settings (show the window, then open
//     the Settings dialog), Quit Daak (quit for real).
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
	"time"

	"fyne.io/systray"
	"github.com/godbus/dbus/v5"

	"github.com/absonsnet/daak/internal/logging"
)

// doubleClickInterval is the maximum gap between two left-clicks that
// counts as a double-click (the Windows/macOS default double-click
// time is 500 ms).
const doubleClickInterval = 500 * time.Millisecond

// doubleClickDetector turns the library's per-click tap callback into a
// double-click callback: a lone tap does nothing, two taps within
// interval fire onDoubleClick. The library (fyne.io/systray v1.12.2) has
// no double-click callback of its own — SetOnTapped fires on every
// left-click — so the pairing is done here.
//
// No timer is needed: a stale first tap can never fire, because the
// interval check fails once it is old; it is simply overwritten by the
// next tap. A third rapid tap starts a new pair instead of re-firing.
type doubleClickDetector struct {
	mu            sync.Mutex
	interval      time.Duration
	lastTap       time.Time
	onDoubleClick func()
}

func (d *doubleClickDetector) tap() {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	if !d.lastTap.IsZero() && now.Sub(d.lastTap) <= d.interval {
		d.lastTap = time.Time{}
		d.onDoubleClick()
		return
	}
	d.lastTap = now
}

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
// icon is used. onShow shows the window (double left-click or Open
// Daak); onQuit quits the app for real (Quit Daak); onCompose opens a
// new message (Compose new message); onCheckMail syncs all accounts in
// the background (Check mail now); onSettings opens the Settings dialog
// (Settings). A single left-click does nothing.
//
// It returns false when the tray could not be started — the caller must
// treat that as "no tray" and never leave the window hidden without one
// (a hidden window with no tray icon leaves the user locked out). A panic
// during start is recovered and reported the same way: a tray failure
// must never crash startup. Note this only catches Go panics, not a
// native crash — the real crash guard is invokeTrayStart, which runs the
// library's native start on the macOS main thread.
func (t *Tray) Start(iconICO, iconPNG []byte, onShow, onQuit, onCompose, onCheckMail, onSettings func()) (ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return true
	}

	log := logging.WithComponent("tray")

	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Msg("System tray failed to start; continuing without it")
			t.started = false
			t.end = nil
			ok = false
		}
	}()

	// On Linux the tray is a D-Bus StatusNotifierItem: without a session
	// bus there is no tray host, and the library panics in its shutdown
	// path (it closes a nil connection) if started anyway. Skip starting
	// entirely in that case; a later Start retries.
	if runtime.GOOS == "linux" {
		conn, err := dbus.SessionBus()
		if err != nil {
			log.Warn().Err(err).Msg("No D-Bus session bus, skipping system tray icon")
			return false
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
		// Double left-click shows the window; a single left-click does
		// nothing. The library has no double-click callback, so taps
		// are paired by doubleClickDetector.
		detector := &doubleClickDetector{interval: doubleClickInterval, onDoubleClick: onShow}
		systray.SetOnTapped(detector.tap)
		// No SetOnSecondaryTapped: with no right-click handler the
		// library shows the menu itself on right-click (Windows and
		// macOS; on Linux the tray host renders the exported menu).
		// Setting one would replace the menu with the handler.

		openItem := systray.AddMenuItem("Open Daak", "Show the Daak window")
		composeItem := systray.AddMenuItem("Compose new message", "Write a new message")
		checkMailItem := systray.AddMenuItem("Check mail now", "Sync all accounts now")
		systray.AddSeparator()
		settingsItem := systray.AddMenuItem("Settings", "Open Daak settings")
		quitItem := systray.AddMenuItem("Quit Daak", "Quit Daak")
		go func() {
			for range openItem.ClickedCh {
				onShow()
			}
		}()
		go func() {
			for range composeItem.ClickedCh {
				onCompose()
			}
		}()
		go func() {
			for range checkMailItem.ClickedCh {
				onCheckMail()
			}
		}()
		go func() {
			for range settingsItem.ClickedCh {
				onSettings()
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
	// On macOS the library's native start creates AppKit objects and
	// must run on the main thread; invokeTrayStart dispatches it there
	// (elsewhere it runs directly). A late dispatch still creates the
	// tray afterwards, but the caller is told it is not up yet.
	if !invokeTrayStart(start) {
		log.Warn().Msg("System tray did not start on the main thread in time; continuing without it")
		t.started = false
		return false
	}
	t.end = end
	return true
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
