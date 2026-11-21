//go:build darwin

package tray

/*
#cgo darwin LDFLAGS: -framework Cocoa
#include <stdlib.h>

// Dock badge via the Objective-C runtime, in pure C.
//
// No Objective-C syntax and no Foundation import here: the v0.7.23 build
// broke because Objective-C in a cgo preamble is compiled as plain C
// ("expected identifier or '('" in NSObjCRuntime.h). The runtime entry
// points below are plain C functions, declared by hand so no ObjC header
// is needed.
//
// objc_msgSend is declared variadic. That is safe for these three calls:
// every argument is a pointer and the return is a pointer, so the
// variadic and non-variadic calling conventions agree (the arm64
// variadic pitfall only affects floating-point and struct arguments).
// The label string is created autoreleased and deliberately never
// released by this code: setBadgeLabel: retains it, and the run loop's
// pool drains it. Over-releasing it is exactly the v0.7.26 crash
// (Wails ExecJS released its autoreleased script string on the main
// thread), so there is no release here.
typedef struct objc_object *daak_id;
typedef struct objc_selector *daak_sel;
extern daak_id objc_getClass(const char *name);
extern daak_sel sel_registerName(const char *name);
extern daak_id objc_msgSend(daak_id self, daak_sel op, ...);

// daakSetDockBadge sets the macOS Dock badge label; a NULL or empty
// label clears the badge. Must run on the main thread (AppKit).
static void daakSetDockBadge(const char *label) {
	daak_id app = objc_msgSend(objc_getClass("NSApplication"), sel_registerName("sharedApplication"));
	if (app == 0) {
		return;
	}
	daak_id tile = objc_msgSend(app, sel_registerName("dockTile"));
	if (tile == 0) {
		return;
	}
	daak_id str = 0;
	if (label != 0 && label[0] != '\0') {
		str = objc_msgSend(objc_getClass("NSString"), sel_registerName("stringWithUTF8String:"), label);
	}
	objc_msgSend(tile, sel_registerName("setBadgeLabel:"), str);
}
*/
import "C"

import (
	"time"
	"unsafe"
)

// setDockBadge sets the macOS Dock badge to the count's label (cleared at
// zero). Runs on the main thread via the tray's dispatcher; returns false
// if the main thread did not run it in time (the caller leaves the count
// unrecorded so the next refresh retries — the set is idempotent).
func setDockBadge(count int) bool {
	label := FormatBadgeCount(count)
	if label == "" {
		return runOnMainThread(func() {
			C.daakSetDockBadge(nil)
		}, 2*time.Second)
	}
	return runOnMainThread(func() {
		// Allocated, used and freed inside the dispatched func: on a
		// timeout the func still runs later, and a string freed by the
		// caller would be use-after-free by then.
		cstr := C.CString(label)
		defer C.free(unsafe.Pointer(cstr))
		C.daakSetDockBadge(cstr)
	}, 2*time.Second)
}

// setTaskbarOverlay is a no-op on macOS (no taskbar); the Dock badge is
// the macOS surface.
func setTaskbarOverlay(count int) bool {
	return true
}
