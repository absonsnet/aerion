# Tray interaction audit (v0.7.26)

Audited Nov 20, 2026, after "Open Daak" from the tray menu still crashed on
v0.7.25. The tray only appears when Run in Background is on (v0.7.21).
Nothing in this audit was live-tested on a Mac, Windows, or Linux desktop
— this VM has none — so every verdict below is "safe by construction"
from the source, and the first real test is on Abid's machines.

## Root cause of the Mac tray crashes (v0.7.24 double-click, v0.7.25 Open Daak)

Both crashes are the same defect, not two. The native window show was
never the crasher; the crasher is the `"window:show"` event emit.

- Wails v2.16.0, `internal/frontend/desktop/darwin/Application.m:67-74`
  (`ExecJS`): builds the script string with `safeInit` (`:252-257`),
  which is `[NSString stringWithUTF8String:]` — an **autoreleased**
  string — and then releases it explicitly inside the `ON_MAIN_THREAD`
  block (`[nsscript release]`).
- Called from the macOS main thread that is a **double release**: the
  run loop's autorelease pool drains and messages the freed string, so
  the app segfaults in `objc_msgSend` (both crashes: same PC
  `0x7ff80a4dddca` in libobjc, goroutine 1 inside `RunMainLoop`, wild
  fault addresses `0x692daeb96720` and `0x19f8e6447380`, `rax == rdx`
  with the fault at `+0x20`).
- Called from a Go worker goroutine there is no autorelease pool, so
  the explicit release simply balances the creation and nothing ever
  messages the freed string — which is why the thousands of
  worker-thread emits in normal use never crash.
- v0.7.24: the double-click tap callback ran on the main thread and
  called `ShowWindow` directly, so its emit ran on the main thread.
- v0.7.25: `ShowWindow` dispatched its whole body to the main thread
  and skipped the native show when the window was already visible —
  yet Open Daak still crashed at the same PC right after the log line
  "Showing window: already visible, skipping native show". The native
  show was skipped and it still died, which proves the show was not
  the crasher; the dispatch had moved the emit onto the main thread.
- v0.7.26 fix: no main-thread dispatch in `ShowWindow` (Wails
  dispatches the native calls itself on every platform), the native
  show is unconditional again, and on darwin the emit goes out on a
  fresh worker goroutine, never on the main thread. The removed
  `tray.OnMainThread` export is gone with it.
- Latent same-class issue in Wails, not ours to fix here:
  `Application.m:439-454` (`Run`) does `[_url release]` on a
  `safeInit` string once at startup. It has never been observed
  crashing; if the v0.7.26 fix ever fails to hold, a debug build with
  `NSZombieEnabled` (or Instruments Zombies) would name the freed
  object directly.

The other emit sites are safe: every other `EventsEmit` in `app/`
runs on a worker goroutine (`BeforeClose` reaches Go through
`processMessage("Q")` from `WindowDelegate windowShouldClose:`,
tray menu handlers are goroutines, sync/FTS/actions run on their own
goroutines). The only main-thread-reachable emit was ShowWindow's.

## How the pieces thread

- systray darwin: left-click and right-click both go through the
  library's `show_menu` when no tap handler is set (`systray_darwin.go`
  / `systray_darwin.m`); `show_menu` attaches the menu and calls
  `performClick:`, detaching in `menuDidClose:` via `dispatch_async`.
  Menu-item selection is `menuHandler:` → `systray_menu_item_selected`
  → a **non-blocking** send on `ClickedCh` (`systray.go:318-331`),
  so it can never block the main thread.
- systray windows: tap callbacks run on the tray's message-pump
  goroutine; left-click → `tappedLeft` if set else `showMenu()`,
  right-click → `tappedRight` if set else `showMenu()`
  (`systray_windows.go:1132-1146`).
- systray linux: D-Bus signals arrive on a Go channel and are handled
  on a goroutine; if neither tap handler is set the host shows the
  menu on Activate (`systray_unix.go:382`).
- Wails dispatches native window calls itself on every platform:
  darwin `ON_MAIN_THREAD` (`dispatch_async`), windows `Invoke` to the
  UI thread, linux `ExecuteOnMainThread` to the GTK main thread.
  Linux `ExecJS` also dispatches (`invokeOnMainThread`); Windows
  `ExecJS` is WebView2. Only darwin `ExecJS` has the over-release
  hazard, and only when called on the main thread.
- Our tray menu handlers (`internal/tray/tray.go`) are five goroutines
  ranging over the items' `ClickedCh`; the double-click detector
  (Windows/Linux only) pairs taps within 500 ms on the tap-callback
  goroutine.

## Matrix

States: window visible (V) vs window hidden in background mode (H);
tray fresh at startup (F) vs after the window has been hidden a
while (L). The code paths are identical in V/H and F/L — the only
state the code reads is `windowHidden`, used by `saveWindowGeometry`
and `watchTerminationSignals`, not by `ShowWindow` — so each cell
covers all four state combinations unless noted.

### macOS

| Interaction | What happens | Verdict |
|---|---|---|
| Left single-click | No tap handler → library `show_menu` → menu, on the main thread, AppKit-native | Safe |
| Left double-click | First click opens the menu; second click's mouse-down goes to the menu (tracking) — dismisses or picks. No tap handler, no `ShowWindow` | Safe (v0.7.24 crash path removed) |
| Right-click | Event monitor fires `leftMouseClicked` on mouse-down → `show_menu`; `rightMouseUp` would fire a second `show_menu`, but during menu tracking the button does not receive mouse-up, so it does not fire in practice. Library quirk, not observed misbehaving | Safe |
| Open Daak | Menu goroutine → `ShowWindow`: native calls self-dispatched, emit on a worker goroutine | Safe in v0.7.26 (crashed in v0.7.24 and v0.7.25) |
| Compose new message | Menu goroutine → `ShowWindow` + `tray:compose` emit (worker goroutine) | Safe |
| Check mail now | Own goroutine, `SyncAllComplete`, no UI calls | Safe |
| Settings | Menu goroutine → `ShowWindow` + `tray:settings` emit (worker goroutine) | Safe |
| Quit Daak | Menu goroutine → `QuitApp`: emit (worker) + `Quit` after 150 ms, frontend quits on the main thread | Safe |

### Windows

| Interaction | What happens | Verdict |
|---|---|---|
| Left single-click | Detector tap; a lone tap never fires | Safe (does nothing) |
| Left double-click | Detector pairs taps → `onShow` → `ShowWindow` → Wails `Invoke` to the UI thread | Safe |
| Right-click | No right-click handler → library `showMenu()` (TrackPopupMenu) | Safe |
| Open Daak | Tray message-pump goroutine → `ShowWindow` (`Invoke`) + emit (WebView2, no ObjC hazard) | Safe |
| Compose new message | Same as Open Daak + `tray:compose` emit | Safe |
| Check mail now | Own goroutine, no UI calls | Safe |
| Settings | Same as Open Daak + `tray:settings` emit | Safe |
| Quit Daak | Menu goroutine → `QuitApp` → `Quit` via `Invoke` | Safe |

### Linux

| Interaction | What happens | Verdict |
|---|---|---|
| Left single-click | `Activate` → detector tap; a lone tap never fires | Safe (does nothing) |
| Left double-click | Detector pairs taps → `ShowWindow` → `ExecuteOnMainThread` (GTK) | Safe |
| Right-click | Tray host renders the exported D-Bus menu | Safe |
| Open Daak | D-Bus signal goroutine → `ShowWindow` (GTK main thread) + emit (`invokeOnMainThread`) | Safe |
| Compose new message | Same as Open Daak + `tray:compose` emit | Safe |
| Check mail now | Own goroutine, no UI calls | Safe |
| Settings | Same as Open Daak + `tray:settings` emit | Safe |
| Quit Daak | D-Bus goroutine → `QuitApp` → frontend quit on the GTK main thread | Safe |

Linux without a D-Bus session bus: the tray is skipped with a
warning; there is no tray to click.

## What still needs a live test

Every cell above, on Abid's Mac (and Windows where it applies):
left single/double-click, right-click, and each of the five menu
items, with the window visible and with it hidden. The double-click
and Open Daak cells on macOS are the two that actually crashed, so
those two are the ones that prove the fix.
