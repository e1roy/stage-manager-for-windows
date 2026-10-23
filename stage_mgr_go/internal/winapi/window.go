//go:build windows

package winapi

import (
	"sync"
	"syscall"
	"unsafe"
)

// HWND is a window handle. It is a plain integer so that it can key maps
// and travel through JSON.
type HWND uintptr

// Rect is a rectangle in physical screen pixels, as Win32 reports them.
type Rect struct{ Left, Top, Right, Bottom int32 }

func (r Rect) W() int32    { return r.Right - r.Left }
func (r Rect) H() int32    { return r.Bottom - r.Top }
func (r Rect) Empty() bool { return r.W() <= 0 || r.H() <= 0 }
func (r Rect) Offset(dx, dy int32) Rect {
	return Rect{r.Left + dx, r.Top + dy, r.Right + dx, r.Bottom + dy}
}

// Intersects reports whether two rectangles share any pixel.
func (r Rect) Intersects(o Rect) bool {
	return r.Left < o.Right && o.Left < r.Right && r.Top < o.Bottom && o.Top < r.Bottom
}

// Contains reports whether the point lies inside the rectangle.
func (r Rect) Contains(p Point) bool {
	return p.X >= r.Left && p.X < r.Right && p.Y >= r.Top && p.Y < r.Bottom
}

type Point struct{ X, Y int32 }

const (
	gaRoot     = 2
	gwOwner    = 4
	gwlStyle   = -16
	gwlExStyle = -20

	WsExTopmost     = 0x00000008
	WsExTransparent = 0x00000020
	WsExToolWindow  = 0x00000080
	WsExAppWindow   = 0x00040000
	WsExLayered     = 0x00080000
	WsExNoActivate  = 0x08000000

	WsCaption = 0x00C00000

	dwmwaNCRenderingEnabled       = 1
	dwmwaExtendedFrameBounds      = 9
	dwmwaCloaked                  = 14
	dwmwaTransitionsForceDisabled = 3

	SwpNoSize         = 0x0001
	SwpNoMove         = 0x0002
	SwpNoZOrder       = 0x0004
	SwpNoRedraw       = 0x0008
	SwpNoActivate     = 0x0010
	SwpShowWindow     = 0x0040
	SwpHideWindow     = 0x0080
	SwpNoOwnerZOrder  = 0x0200
	SwpAsyncWindowPos = 0x4000
	SwpFrameChanged   = 0x0020

	HwndTop       HWND = 0
	HwndTopmost   HWND = ^HWND(0)     // -1
	HwndNoTopmost HWND = ^HWND(0) - 1 // -2

	swRestore         = 9
	swShowNoActivate  = 4
	swMinimize        = 6
	swShowMinimized   = 2
	swShowMinNoActive = 7
	swHide            = 0

	wsActiveCaption = 0x0001
)

var enumMu sync.Mutex
var enumFn func(HWND) bool
var enumCallback = syscall.NewCallback(func(h, _ uintptr) uintptr {
	if enumFn(HWND(h)) {
		return 1
	}
	return 0
})

// EnumWindows calls fn for every top-level window, front-most first, until
// fn returns false. Calls are serialized: fn must not enumerate again.
func EnumWindows(fn func(HWND) bool) {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumFn = fn
	procEnumWindows.Call(enumCallback, 0)
	enumFn = nil
}

var childMu sync.Mutex
var childFn func(HWND) bool
var childCallback = syscall.NewCallback(func(h, _ uintptr) uintptr {
	if childFn(HWND(h)) {
		return 1
	}
	return 0
})

// EnumChildWindows calls fn for the descendants of parent until it returns false.
func EnumChildWindows(parent HWND, fn func(HWND) bool) {
	childMu.Lock()
	defer childMu.Unlock()
	childFn = fn
	procEnumChildWindows.Call(uintptr(parent), childCallback, 0)
	childFn = nil
}

func boolCall(p *syscall.LazyProc, args ...uintptr) bool {
	r, _, _ := p.Call(args...)
	return r != 0
}

func IsWindow(h HWND) bool        { return h != 0 && boolCall(procIsWindow, uintptr(h)) }
func IsWindowVisible(h HWND) bool { return boolCall(procIsWindowVisible, uintptr(h)) }
func IsIconic(h HWND) bool        { return boolCall(procIsIconic, uintptr(h)) }
func IsZoomed(h HWND) bool        { return boolCall(procIsZoomed, uintptr(h)) }

// IsHung reports whether a window's app has stopped taking messages, as
// Windows tells when it shows a ghost window in its place.
func IsHung(h HWND) bool { return boolCall(procIsHungAppWindow, uintptr(h)) }

// WindowRect returns the window's rectangle, invisible resize borders included.
func WindowRect(h HWND) Rect {
	var r Rect
	procGetWindowRect.Call(uintptr(h), uintptr(unsafe.Pointer(&r)))
	return r
}

// FrameBounds returns the visible frame DWM draws, without the invisible
// resize borders around it, or the window rectangle where DWM has none.
func FrameBounds(h HWND) Rect {
	var r Rect
	hr, _, _ := procDwmGetWindowAttribute.Call(uintptr(h), dwmwaExtendedFrameBounds, uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r))
	if hr != 0 || r.Empty() {
		return WindowRect(h)
	}
	return r
}

// CastsFrameShadow reports whether DWM draws its shadow around the window.
// It draws none where it draws no frame, nor around a window shaped by a
// window region, as apps that draw their own rounded frame and shadow are.
func CastsFrameShadow(h HWND) bool {
	var enabled int32
	if hr, _, _ := procDwmGetWindowAttribute.Call(uintptr(h), dwmwaNCRenderingEnabled, uintptr(unsafe.Pointer(&enabled)), 4); hr != 0 || enabled == 0 {
		return false
	}
	const rgnError = 0
	rgn, _, _ := procCreateRectRgn.Call(0, 0, 0, 0)
	defer procDeleteObject.Call(rgn)
	kind, _, _ := procGetWindowRgn.Call(uintptr(h), rgn)
	return kind == rgnError
}

// IsCloaked reports whether DWM hides the window, as it hides windows of
// other virtual desktops and suspended UWP apps.
func IsCloaked(h HWND) bool {
	var v uint32
	hr, _, _ := procDwmGetWindowAttribute.Call(uintptr(h), dwmwaCloaked, uintptr(unsafe.Pointer(&v)), 4)
	return hr == 0 && v != 0
}

// SetTransitionsDisabled turns DWM's show, hide and restore animations of
// a window off or back on.
func SetTransitionsDisabled(h HWND, disabled bool) {
	var v int32
	if disabled {
		v = 1
	}
	procDwmSetWindowAttribute.Call(uintptr(h), dwmwaTransitionsForceDisabled, uintptr(unsafe.Pointer(&v)), 4)
}

// SetDwmAttribute sets a 32-bit DWM window attribute.
func SetDwmAttribute(h HWND, attr uint32, v int32) {
	procDwmSetWindowAttribute.Call(uintptr(h), uintptr(attr), uintptr(unsafe.Pointer(&v)), 4)
}

// DwmFlush waits for the next composition pass.
func DwmFlush() { procDwmFlush.Call() }

func Title(h HWND) string {
	n, _, _ := procGetWindowTextLengthW.Call(uintptr(h))
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

func ClassName(h HWND) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf[:n])
}

// ProcessID returns the process and thread that own the window.
func ProcessID(h HWND) (pid, tid uint32) {
	r, _, _ := procGetWindowThreadProcessId.Call(uintptr(h), uintptr(unsafe.Pointer(&pid)))
	return pid, uint32(r)
}

// Root returns the top-level window that contains h.
func Root(h HWND) HWND {
	r, _, _ := procGetAncestor.Call(uintptr(h), gaRoot)
	return HWND(r)
}

func Owner(h HWND) HWND {
	r, _, _ := procGetWindow.Call(uintptr(h), gwOwner)
	return HWND(r)
}

func ExStyle(h HWND) uint32 {
	r, _, _ := procGetWindowLongPtrW.Call(uintptr(h), uintptr(int32ToUintptr(gwlExStyle)))
	return uint32(r)
}

func Style(h HWND) uint32 {
	r, _, _ := procGetWindowLongPtrW.Call(uintptr(h), uintptr(int32ToUintptr(gwlStyle)))
	return uint32(r)
}

// SetExStyle adds and removes extended styles and makes the frame take them.
func SetExStyle(h HWND, add, remove uint32) {
	ex := (ExStyle(h) | add) &^ remove
	procSetWindowLongPtrW.Call(uintptr(h), uintptr(int32ToUintptr(gwlExStyle)), uintptr(ex))
	procSetWindowPos.Call(uintptr(h), 0, 0, 0, 0, 0, SwpNoMove|SwpNoSize|SwpNoZOrder|SwpNoActivate|SwpFrameChanged)
}

func int32ToUintptr(v int32) uintptr { return uintptr(int64(v)) }

func Foreground() HWND {
	r, _, _ := procGetForegroundWindow.Call()
	return HWND(r)
}

// SetWindowPos wraps the Win32 call; insertAfter is a window or one of the
// Hwnd* constants.
func SetWindowPos(h, insertAfter HWND, x, y, w, ht int32, flags uint32) bool {
	return boolCall(procSetWindowPos, uintptr(h), uintptr(insertAfter),
		uintptr(int64(x)), uintptr(int64(y)), uintptr(int64(w)), uintptr(int64(ht)), uintptr(flags))
}

func ShowNoActivate(h HWND) { procShowWindowAsync.Call(uintptr(h), swShowNoActivate) }
func Hide(h HWND)           { procShowWindow.Call(uintptr(h), swHide) }
func Minimize(h HWND)       { procShowWindowAsync.Call(uintptr(h), swMinimize) }

// Redraw asks a window to repaint everything, frame and children included,
// without erasing what it shows first.
func Redraw(h HWND) {
	const rdwInvalidate, rdwNoErase, rdwAllChildren, rdwFrame = 0x1, 0x20, 0x80, 0x400
	procRedrawWindow.Call(uintptr(h), 0, 0, rdwInvalidate|rdwNoErase|rdwAllChildren|rdwFrame)
}

// PaintNow has a window of this thread paint at once, if it asked to be
// painted, rather than when the message loop gets to it.
func PaintNow(h HWND) { procUpdateWindow.Call(uintptr(h)) }

// Close asks a window to close, as its close button does.
func Close(h HWND) {
	const wmClose = 0x0010
	procPostMessageW.Call(uintptr(h), wmClose, 0, 0)
}

type windowPlacement struct {
	Length         uint32
	Flags          uint32
	ShowCmd        uint32
	MinPosition    Point
	MaxPosition    Point
	NormalPosition Rect
	Device         Rect
}

// RestoredRect returns where a minimized window goes back to, in screen
// coordinates, and whether it returns maximized.
func RestoredRect(h HWND) (r Rect, maximized bool, ok bool) {
	wp := windowPlacement{Length: uint32(unsafe.Sizeof(windowPlacement{}))}
	if !boolCall(procGetWindowPlacement, uintptr(h), uintptr(unsafe.Pointer(&wp))) {
		return Rect{}, false, false
	}
	const restoreToMaximized = 0x2
	n := wp.NormalPosition
	mon := MonitorFromRect(n)
	mi := monitorInfo(mon)
	if wp.Flags&restoreToMaximized != 0 {
		return mi.Work, true, true
	}
	// Placement is in workspace coordinates unless the window is a tool window.
	if ExStyle(h)&WsExToolWindow == 0 {
		n = n.Offset(mi.Work.Left-mi.Monitor.Left, mi.Work.Top-mi.Monitor.Top)
	}
	return n, false, true
}

// MoveRestored changes where a minimized window goes back to, p in screen
// coordinates, leaving it minimized and inactive. Moving a minimized window
// only moves its icon, so it would come back where it was.
func MoveRestored(h HWND, p Point) bool {
	wp := windowPlacement{Length: uint32(unsafe.Sizeof(windowPlacement{}))}
	if IsHung(h) || !boolCall(procGetWindowPlacement, uintptr(h), uintptr(unsafe.Pointer(&wp))) {
		return false
	}
	n := wp.NormalPosition
	at := n.Offset(p.X-n.Left, p.Y-n.Top)
	// Placement is in workspace coordinates unless the window is a tool window.
	if ExStyle(h)&WsExToolWindow == 0 {
		mi := monitorInfo(MonitorFromRect(at))
		at = at.Offset(mi.Monitor.Left-mi.Work.Left, mi.Monitor.Top-mi.Work.Top)
	}
	wp.NormalPosition = at
	if wp.ShowCmd == swShowMinimized {
		wp.ShowCmd = swShowMinNoActive
	}
	return boolCall(procSetWindowPlacement, uintptr(h), uintptr(unsafe.Pointer(&wp)))
}

type windowInfo struct {
	Size           uint32
	Window         Rect
	Client         Rect
	Style          uint32
	ExStyle        uint32
	WindowStatus   uint32
	BorderX        uint32
	BorderY        uint32
	WindowType     uint16
	CreatorVersion uint16
}

// HasActiveFrame reports whether the window draws its frame as active.
func HasActiveFrame(h HWND) bool {
	wi := windowInfo{Size: uint32(unsafe.Sizeof(windowInfo{}))}
	return boolCall(procGetWindowInfo, uintptr(h), uintptr(unsafe.Pointer(&wi))) && wi.WindowStatus&wsActiveCaption != 0
}

// WindowFromPoint returns the top-level window under a screen point.
func WindowFromPoint(p Point) HWND {
	r, _, _ := procWindowFromPoint.Call(uintptr(*(*uint64)(unsafe.Pointer(&p))))
	if r == 0 {
		return 0
	}
	return Root(HWND(r))
}

// DpiForWindow returns the window's DPI, 96 at 100%.
func DpiForWindow(h HWND) uint32 {
	r, _, _ := procGetDpiForWindow.Call(uintptr(h))
	if r == 0 {
		return 96
	}
	return uint32(r)
}

var procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")

// SetLayered makes a window layered and fully opaque, which leaves what it
// draws as it was and lets WS_EX_TRANSPARENT pass clicks through it.
func SetLayered(h HWND) {
	SetExStyleBits(h, WsExLayered, 0)
	const lwaAlpha = 0x2
	procSetLayeredWindowAttributes.Call(uintptr(h), 0, 255, lwaAlpha)
}

// SetExStyleBits adds and removes extended styles without touching the frame.
func SetExStyleBits(h HWND, add, remove uint32) {
	ex := (ExStyle(h) | add) &^ remove
	procSetWindowLongPtrW.Call(uintptr(h), uintptr(int32ToUintptr(gwlExStyle)), uintptr(ex))
}
