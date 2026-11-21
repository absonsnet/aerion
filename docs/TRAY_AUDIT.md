# Tray interaction audit (v0.7.26, corrected v0.7.31)

Audited Nov 20, 2026, after "Open Daak" from the tray menu still crashed on
v0.7.25. The tray only appears when Run in Background is on (v0.7.21).
Nothing in this audit was live-tested on a Mac, Windows, or Linux desktop
— this VM has none — so every verdict below is "safe by construction"
from the source, and the first real test is on Abid's machines.

## v0.7.31 correction (Nov 21, 2026): the Windows menu never showed

Abid reported (Nov 21, 14:48 PKT, on the v0.7.30 Windows build, and
also on v0.7.26): the tray menu does not show on any click. The
v0.7.26 Windows verdicts above ("Safe") were wrong — the
by-construction reasoning missed a Win32 requirement, and no Windows
tray click had ever been live-tested (every earlier report's Windows
cells were "first real test is Abid on Windows", and none came back).

**Windows root cause — the message pump ran on the wrong OS thread.**
Win32 delivers a window's messages to the message queue of the
thread that created the window, and `GetMessage` only retrieves
messages for the calling thread. Daak starts the tray from
`App.Startup`, which Wails calls on a worker goroutine
(`internal/frontend/desktop/windows/frontend.go`: `go func() {
f.frontendOptions.OnStartup(f.ctx) }()`), so `Register` creates
the tray window there — but `nativeStart` spawns the pump on a new
goroutine (`systray_windows.go:924-930`: `go func() { for
doNativeTick() {} }()`), which the scheduler can place on another
OS thread. Nothing pins either goroutine to a thread
(systray's `init()` `LockOSThread` only pins the main goroutine).
When the pump lands elsewhere, `wndProc` never runs: the icon still
shows (`Shell_NotifyIcon` NIM_ADD is a one-shot registration) but
every click — left, right, double — is silently dropped, and the
menu never appears. The tray code on Windows was unchanged from
v0.7.17 (tray introduction) through v0.7.30 except the badge, so
this is not a v0.7.25/v0.7.26 regression and the badge is exonerated
(broken on v0.7.26, which predates it); the Windows tray clicks were
most likely never working, and Abid is the first to actually click
the tray on Windows.

**Windows fix (v0.7.31):** `Tray.Start` runs the tray on a
dedicated goroutine locked to its OS thread via `systray.Run`
(`internal/tray/tray_windows.go: startTrayWindows`), which pumps on
the calling goroutine — so window creation (`Register`) and pumping
(`nativeLoop`) share the thread, deterministically.
`setInternalLoop` is a no-op on Windows, so `Run` is exactly
`Register` + pump on the caller. `Start` waits up to 5 s for
onReady to finish registering the menu, mirroring the macOS
main-thread wait. No click behaviour changes on Windows:
double-click still opens, right-click still shows the menu — both
now actually reach the code.

**Linux — `ItemIsMenu` is false, menu on right-click unverified.**
`nativeStart` (`systray_unix.go:190`) calls `systrayReady()` first
and only then builds the D-Bus property spec, where `ItemIsMenu`
is `tappedLeft == nil && tappedRight == nil`
(`systray_unix.go:382`). The onReady goroutine's `SetOnTapped` (a
single assignment) wins that race against the D-Bus session setup
by orders of magnitude, so `ItemIsMenu` is false on essentially
every launch — since v0.7.17, when `SetOnTapped(onShow)` was first
set. Consequence: left-click is `Activate` (detector tap; double
opens, single does nothing — by design), and right-click is the
`ContextMenu` D-Bus call, which errors with no secondary handler
set. Whether the host still renders the `Menu` property on
right-click anyway is host-dependent and unverified here (no tray
host on this VM). No Linux behaviour change in v0.7.31: the
double-click-open Abid chose in v0.7.19 stays, and the menu
question needs his live test on Linux. If he confirms the Linux
menu never shows, the fix is one line — drop `SetOnTapped` on
Linux so `ItemIsMenu` is true and the host shows the menu on click
(like macOS), at the cost of double-click-to-open on Linux.

**macOS — unaffected.** The menu demonstrably worked there
(Abid clicked Open Daak from it on v0.7.25); the click path is
AppKit on the main thread, and nothing since v0.7.25 touches menu
registration. Abid's "may be same on Mac" is unconfirmed
speculation; the code says it works.

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
| Left single-click | Detector tap; a lone tap never fires — now that the tap reaches the detector (v0.7.31: tray on a dedicated locked thread, window and pump on the same thread) | Safe in v0.7.31 (never reached the detector before — the pump ran on another thread) |
| Left double-click | Detector pairs taps → `onShow` → `ShowWindow` → Wails `Invoke` to the UI thread | Safe in v0.7.31 (same cause as above) |
| Right-click | No right-click handler → library `showMenu()` (TrackPopupMenu), reached via `wndProc` on the pump thread | Safe in v0.7.31 (`wndProc` never ran before) |
| Open Daak | Tray message-pump goroutine → `ShowWindow` (`Invoke`) + emit (WebView2, no ObjC hazard) | Safe in v0.7.31 |
| Compose new message | Same as Open Daak + `tray:compose` emit | Safe in v0.7.31 |
| Check mail now | Own goroutine, no UI calls | Safe in v0.7.31 |
| Settings | Same as Open Daak + `tray:settings` emit | Safe in v0.7.31 |
| Quit Daak | Menu goroutine → `QuitApp` → `Quit` via `Invoke` | Safe in v0.7.31 |

### Linux

| Interaction | What happens | Verdict |
|---|---|---|
| Left single-click | `Activate` → detector tap; a lone tap never fires (`ItemIsMenu` is false — tap handler set — so left-click is Activate by design) | Safe (does nothing) |
| Left double-click | Detector pairs taps → `ShowWindow` → `ExecuteOnMainThread` (GTK) | Safe |
| Right-click | Host-dependent: `ItemIsMenu` is false, so the host calls `ContextMenu`, which errors with no secondary handler set; whether the host still renders the exported D-Bus menu anyway is host-dependent and unverified here | Unverified — needs Abid's live test on Linux (see v0.7.31 note) |
| Open Daak | D-Bus signal goroutine → `ShowWindow` (GTK main thread) + emit (`invokeOnMainThread`) — if the host shows the menu | Safe if the menu shows |
| Compose new message | Same as Open Daak + `tray:compose` emit | Safe if the menu shows |
| Check mail now | Own goroutine, no UI calls | Safe |
| Settings | Same as Open Daak + `tray:settings` emit | Safe if the menu shows |
| Quit Daak | D-Bus goroutine → `QuitApp` → frontend quit on the GTK main thread | Safe if the menu shows |

Linux without a D-Bus session bus: the tray is skipped with a
warning; there is no tray to click.

## What still needs a live test

Every cell above, on Abid's Mac (and Windows where it applies):
left single/double-click, right-click, and each of the five menu
items, with the window visible and with it hidden. The double-click
and Open Daak cells on macOS are the two that actually crashed, so
those two are the ones that prove the fix.
