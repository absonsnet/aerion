//go:build windows

package platform

import (
	"golang.org/x/sys/windows"

	"github.com/absonsnet/daak/internal/logging"
)

// QuitEventName is the named auto-reset event the Windows installer signals
// to ask a running Daak to quit before an update or uninstall.
//
// The event lives in the session-local namespace (Local\), so it is visible
// to every process in the user's logon session — including an elevated
// installer — but never to other sessions. Any process in the session can
// signal it, the same exposure as the localhost single-instance port.
const QuitEventName = `Local\DaakQuit`

// StartQuitEventWatcher creates the named quit event and waits for it on a
// background goroutine. When the installer signals the event, onQuit runs.
//
// The watcher is started once, by the primary instance, from App.Startup —
// after the Wails context exists, so onQuit (App.QuitApp) can shut the app
// down through the normal quit path, including in background mode.
func StartQuitEventWatcher(onQuit func()) {
	log := logging.WithComponent("singleinstance")

	name, err := windows.UTF16PtrFromString(QuitEventName)
	if err != nil {
		log.Error().Err(err).Msg("Quit event name is not valid UTF-16")
		return
	}

	// Auto-reset event, initially non-signalled. If an event with this name
	// already exists (a second instance cannot normally happen — the
	// single-instance lock prevents it), CreateEvent opens the existing one.
	handle, err := windows.CreateEvent(nil, 0, 0, name)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create quit event, installer quit handshake unavailable")
		return
	}

	go func() {
		_, _ = windows.WaitForSingleObject(handle, windows.INFINITE)
		// Deliberately do NOT close the handle here. The installer uses
		// the event's existence as "a Daak 0.7.11+ is running": it polls
		// for the event while waiting for the app to exit, so the handle
		// must stay open until the process itself exits (the OS then
		// reclaims it). Closing it on signal would make the event vanish
		// while the app is still shutting down.
		log.Info().Msg("Quit event signalled by installer, shutting down")
		onQuit()
	}()
}
