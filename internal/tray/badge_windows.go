//go:build windows

package tray

import (
	"fmt"
	"image"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/absonsnet/daak/internal/logging"
)

// Windows taskbar badge: an overlay icon on the taskbar button, via
// ITaskbarList3::SetOverlayIcon — the same API Outlook uses. The overlay
// is the badge alone (red circle/pill + number); the taskbar button
// already shows the envelope.
//
// Everything here goes through lazy DLL procedures: golang.org/x/sys
// does not wrap CoCreateInstance, CreateDIBSection, CreateIconIndirect
// or GetWindowTextW. CoInitializeEx is also called via a lazy proc —
// x/sys's wrapper reports S_FALSE ("COM already initialised on this
// thread") as an error, and S_FALSE is a success that still needs a
// balancing CoUninitialize.

// CLSCTX_ALL for CoCreateInstance.
const clsctxAll = 0x17

// COINIT_APARTMENTTHREADED for CoInitializeEx.
const coinitApartmentThreaded = 0x2

var (
	modOle32  = windows.NewLazySystemDLL("ole32.dll")
	modUser32 = windows.NewLazySystemDLL("user32.dll")
	modGdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procCoCreateInstance   = modOle32.NewProc("CoCreateInstance")
	procCoInitializeEx     = modOle32.NewProc("CoInitializeEx")
	procGetWindowTextW     = modUser32.NewProc("GetWindowTextW")
	procCreateIconIndirect = modUser32.NewProc("CreateIconIndirect")
	procDestroyIcon        = modUser32.NewProc("DestroyIcon")
	procCreateDIBSection   = modGdi32.NewProc("CreateDIBSection")
	procCreateBitmap       = modGdi32.NewProc("CreateBitmap")
	procDeleteObject       = modGdi32.NewProc("DeleteObject")
)

var (
	clsidTaskbarList = windows.GUID{
		Data1: 0x56FDF344, Data2: 0xFD6D, Data3: 0x11D0,
		Data4: [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA9, 0x90},
	}
	iidTaskbarList3 = windows.GUID{
		Data1: 0xEA1AFB91, Data2: 0x9E28, Data3: 0x4B86,
		Data4: [8]byte{0x90, 0xE9, 0x9E, 0x9F, 0x8A, 0x5E, 0xEF, 0xA},
	}
)

// taskbarList3 is ITaskbarList3, called through its vtable. SetOverlayIcon
// is vtable index 18: IUnknown (0-2), ITaskbarList (3-7: HrInit, AddTab,
// DeleteTab, ActivateTab, SetActiveAlt), ITaskbarList2 (8:
// MarkFullscreenWindow), ITaskbarList3 (9 SetProgressValue, 10
// SetProgressState, 11 RegisterTab, 12 UnregisterTab, 13 SetTabOrder, 14
// SetTabActive, 15 ThumbBarAddButtons, 16 ThumbBarUpdateButtons, 17
// ThumbBarSetImageList, 18 SetOverlayIcon, 19 SetThumbnailTooltip, 20
// SetThumbnailClip).
type taskbarList3Vtbl struct {
	QueryInterface        uintptr
	AddRef                uintptr
	Release               uintptr
	HrInit                uintptr
	AddTab                uintptr
	DeleteTab             uintptr
	ActivateTab           uintptr
	SetActiveAlt          uintptr
	MarkFullscreenWindow  uintptr
	SetProgressValue      uintptr
	SetProgressState      uintptr
	RegisterTab           uintptr
	UnregisterTab         uintptr
	SetTabOrder           uintptr
	SetTabActive          uintptr
	ThumbBarAddButtons    uintptr
	ThumbBarUpdateButtons uintptr
	ThumbBarSetImageList  uintptr
	SetOverlayIcon        uintptr
	SetThumbnailTooltip   uintptr
	SetThumbnailClip      uintptr
}

type taskbarList3 struct {
	vtbl *taskbarList3Vtbl
}

func (t *taskbarList3) release() {
	callVtbl(t.vtbl.Release, uintptr(unsafe.Pointer(t)))
}

func (t *taskbarList3) hrInit() uintptr {
	return callVtbl(t.vtbl.HrInit, uintptr(unsafe.Pointer(t)))
}

func (t *taskbarList3) setOverlayIcon(hwnd windows.HWND, hicon windows.Handle, desc *uint16) uintptr {
	return callVtbl(t.vtbl.SetOverlayIcon, uintptr(unsafe.Pointer(t)), uintptr(hwnd), uintptr(hicon), uintptr(unsafe.Pointer(desc)))
}

func callVtbl(fn uintptr, args ...uintptr) uintptr {
	r1, _, _ := syscall.SyscallN(fn, args...)
	return r1
}

// findMainWindow returns the HWND of Daak's main window: a top-level
// window of this process, Wails' "wailsWindow" class, titled "Daak".
// The class alone is not enough — the detached viewer is a second
// wailsWindow in this process ("Daak - Message") — and the title alone
// is not enough either (the single-instance event-target window shares
// the process but has its own class). The result is cached: the main
// window handle is stable for the session.
var (
	mainWindowMu   sync.Mutex
	mainWindow     windows.HWND
	mainWindowDone bool
)

func findMainWindow() windows.HWND {
	mainWindowMu.Lock()
	defer mainWindowMu.Unlock()
	if mainWindowDone {
		return mainWindow
	}
	mainWindowDone = true
	pid := uint32(os.Getpid())
	var found windows.HWND
	cb := windows.NewCallback(func(hwnd windows.HWND, lParam unsafe.Pointer) uintptr {
		var wpid uint32
		if _, err := windows.GetWindowThreadProcessId(hwnd, &wpid); err != nil || wpid != pid {
			return 1 // continue
		}
		var classBuf [256]uint16
		n, err := windows.GetClassName(hwnd, &classBuf[0], int32(len(classBuf)))
		if err != nil || n == 0 || windows.UTF16ToString(classBuf[:n]) != "wailsWindow" {
			return 1
		}
		var titleBuf [256]uint16
		tn, _, _ := procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&titleBuf[0])), uintptr(len(titleBuf)))
		if tn == 0 || windows.UTF16ToString(titleBuf[:tn]) != "Daak" {
			return 1
		}
		found = hwnd
		return 0 // stop
	})
	_ = windows.EnumWindows(cb, nil)
	mainWindow = found
	return found
}

// bitmapInfoHeader is the 40-byte BITMAPINFOHEADER for a 32bpp DIB.
type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// iconInfo mirrors ICONINFO: fIcon, hotspots, mask and colour bitmaps.
type iconInfo struct {
	Icon     uint32
	XHotspot uint32
	YHotspot uint32
	_        uint32 // padding before the pointer fields
	Mask     windows.Handle
	Color    windows.Handle
}

// hiconFromImage converts a Go image to a Windows icon handle: a 32bpp
// DIB section (alpha channel honoured) plus an empty 1bpp mask, via
// CreateIconIndirect. The caller must DestroyIcon the result. Pixels are
// written premultiplied (opaque badge pixels are unaffected; transparent
// pixels become zero).
func hiconFromImage(img *image.RGBA) (windows.Handle, error) {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	bmi := bitmapInfoHeader{
		Size:        40,
		Width:       int32(w),
		Height:      int32(h), // positive: bottom-up DIB
		Planes:      1,
		BitCount:    32,
		Compression: 0, // BI_RGB
	}
	var bits unsafe.Pointer
	hbmColor, _, err := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bmi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbmColor == 0 {
		return 0, fmt.Errorf("CreateDIBSection failed: %v", err)
	}
	// Copy pixels, flipping rows (DIB is bottom-up) and premultiplying.
	pixels := (*[1 << 30]byte)(bits)[: w*h*4 : w*h*4]
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := img.RGBAAt(x, y)
			a := uint32(c.A)
			// DIB row (h-1-y), pixel x: BGRA, premultiplied.
			o := ((h-1-y)*w + x) * 4
			pixels[o+0] = byte(uint32(c.B) * a / 255)
			pixels[o+1] = byte(uint32(c.G) * a / 255)
			pixels[o+2] = byte(uint32(c.R) * a / 255)
			pixels[o+3] = c.A
		}
	}
	hbmMask, _, err := procCreateBitmap.Call(uintptr(w), uintptr(h), 1, 1, 0)
	if hbmMask == 0 {
		_, _, _ = procDeleteObject.Call(hbmColor)
		return 0, fmt.Errorf("CreateBitmap (mask) failed: %v", err)
	}
	info := iconInfo{Icon: 1, Mask: windows.Handle(hbmMask), Color: windows.Handle(hbmColor)}
	hicon, _, err := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&info)))
	// CreateIconIndirect copies the bitmaps; they can be freed now.
	_, _, _ = procDeleteObject.Call(hbmColor)
	_, _, _ = procDeleteObject.Call(hbmMask)
	if hicon == 0 {
		return 0, fmt.Errorf("CreateIconIndirect failed: %v", err)
	}
	return windows.Handle(hicon), nil
}

// setTaskbarOverlay sets (or, at count <= 0, clears) the taskbar button
// overlay icon. COM is initialised on a locked OS thread for the call;
// S_OK and S_FALSE both count as initialised (S_FALSE still needs its
// balancing CoUninitialize).
func setTaskbarOverlay(count int) bool {
	log := logging.WithComponent("tray")
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	if hr != 0 && hr != 1 { // S_OK and S_FALSE
		log.Error().Uint64("hresult", uint64(hr)).Msg("Taskbar overlay: CoInitializeEx failed")
		return false
	}
	defer windows.CoUninitialize()

	hwnd := findMainWindow()
	if hwnd == 0 {
		log.Warn().Msg("Taskbar overlay: main window not found")
		return false
	}

	var tbl *taskbarList3
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidTaskbarList)),
		0,
		clsctxAll,
		uintptr(unsafe.Pointer(&iidTaskbarList3)),
		uintptr(unsafe.Pointer(&tbl)),
	)
	if hr != 0 || tbl == nil {
		log.Error().Uint64("hresult", uint64(hr)).Msg("Taskbar overlay: CoCreateInstance(ITaskbarList3) failed")
		return false
	}
	defer tbl.release()
	if hr := tbl.hrInit(); hr != 0 {
		log.Error().Uint64("hresult", uint64(hr)).Msg("Taskbar overlay: ITaskbarList3 HrInit failed")
		return false
	}

	if count <= 0 {
		// NULL icon and NULL description clear the overlay.
		if hr := tbl.setOverlayIcon(hwnd, 0, nil); hr != 0 {
			log.Error().Uint64("hresult", uint64(hr)).Msg("Taskbar overlay: clear failed")
			return false
		}
		return true
	}

	img, err := RenderBadgeOnly(count, 64)
	if err != nil {
		log.Error().Err(err).Msg("Taskbar overlay: badge render failed")
		return false
	}
	hicon, err := hiconFromImage(img)
	if err != nil {
		log.Error().Err(err).Msg("Taskbar overlay: HICON creation failed")
		return false
	}
	// The taskbar copies the icon; destroy ours after the call.
	defer func() { _, _, _ = procDestroyIcon.Call(uintptr(hicon)) }()
	desc, err := windows.UTF16PtrFromString(fmt.Sprintf("%d unread emails", count))
	if err != nil {
		log.Error().Err(err).Msg("Taskbar overlay: description failed")
		return false
	}
	if hr := tbl.setOverlayIcon(hwnd, hicon, desc); hr != 0 {
		log.Error().Uint64("hresult", uint64(hr)).Msg("Taskbar overlay: SetOverlayIcon failed")
		return false
	}
	return true
}

// setDockBadge is a no-op on Windows (no Dock); the taskbar overlay is
// the Windows surface.
func setDockBadge(count int) bool {
	return true
}
