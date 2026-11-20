//go:build darwin

package tray

/*
#cgo darwin LDFLAGS: -framework Cocoa -framework Foundation
#include <dispatch/dispatch.h>
#include <stdlib.h>
#include <stdbool.h>
#include <pthread.h>

extern void daakTrayMainThreadTrampoline(void *ctx);

// daakTrayDispatchMain queues ctx for execution on the macOS main thread.
// It returns immediately; the block runs once the main run loop drains
// the main queue.
static void daakTrayDispatchMain(void *ctx) {
	dispatch_async_f(dispatch_get_main_queue(), ctx, daakTrayMainThreadTrampoline);
}

// daakTrayIsMainThread reports whether the calling thread is the macOS
// main thread. AppKit objects must only be created on that thread.
// Pure C: pthread_main_np() is the standard macOS C function for this.
// (An earlier version used an Objective-C message send with a Foundation
// import — Objective-C in a cgo preamble, which Go compiles as plain C,
// so the darwin build failed at "Generating bindings" with
// "expected identifier or '('" in NSObjCRuntime.h.)
static bool daakTrayIsMainThread(void) {
	return pthread_main_np();
}
*/
import "C"

import (
	"runtime/cgo"
	"time"
	"unsafe"
)

//export daakTrayMainThreadTrampoline
func daakTrayMainThreadTrampoline(ctx unsafe.Pointer) {
	h := cgo.Handle(uintptr(ctx))
	defer h.Delete()
	fn, ok := h.Value().(func())
	if !ok || fn == nil {
		return
	}
	fn()
}

// runOnMainThread runs fn on the macOS main thread and waits for it to
// complete (up to timeout), returning false on timeout.
//
// This exists because Wails runs App.Startup — and dispatches binding
// calls such as SetRunBackground — on worker goroutines, not the main
// thread (Wails darwin frontend.go runs OnStartup in a `go func()` and
// the callback processor on its own goroutine). fyne.io/systray's darwin
// nativeStart creates AppKit objects (NSApplication, NSStatusItem, event
// monitors) and assumes the caller is on the main thread; calling it from
// a worker goroutine crashes the app on launch whenever Run in
// Background is on — the Dock icon bounces, then the app never opens.
//
// The timeout is the fail-open path: if the main run loop never drains
// the queue, the caller must treat the tray as not started and show the
// window instead of leaving the app hidden with no way back. A late
// dispatch still runs the block afterwards (the handle is deleted by the
// trampoline when it does), so at worst the tray appears late.
func runOnMainThread(fn func(), timeout time.Duration) bool {
	if C.daakTrayIsMainThread() {
		fn()
		return true
	}
	done := make(chan struct{})
	h := cgo.NewHandle(func() {
		defer close(done)
		fn()
	})
	C.daakTrayDispatchMain(unsafe.Pointer(uintptr(h)))
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
