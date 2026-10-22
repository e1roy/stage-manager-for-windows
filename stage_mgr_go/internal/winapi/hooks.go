//go:build windows

package winapi

import (
	"sync"
	"syscall"
	"unsafe"
)

// WinEvent types StageBar listens to.
const (
	EventSystemForeground     = 0x0003
	EventSystemMoveSizeEnd    = 0x000B
	EventSystemMinimizeStart  = 0x0016
	EventSystemMinimizeEnd    = 0x0017
	EventObjectDestroy        = 0x8001
	EventObjectShow           = 0x8002
	EventObjectHide           = 0x8003
	EventObjectLocationChange = 0x800B
	EventObjectNameChange     = 0x800C
	EventObjectCloaked        = 0x8017
	EventObjectUncloaked      = 0x8018

	objIDWindow            = 0
	winEventOutOfContext   = 0x0000
	winEventSkipOwnProcess = 0x0002
)

// WinEventFunc receives a window event of a top-level window.
type WinEventFunc func(event uint32, h HWND)

var hookMu sync.Mutex
var hookFn WinEventFunc
var hookCallback = syscall.NewCallback(func(hook, event, hwnd, idObject, idChild, thread, time uintptr) uintptr {
	if hwnd == 0 || int32(idObject) != objIDWindow || int32(idChild) != 0 {
		return 0
	}
	if fn := hookFn; fn != nil {
		fn(uint32(event), HWND(hwnd))
	}
	return 0
})

// HookWinEvents installs out-of-context hooks for the given event ranges,
// skipping StageBar's own windows. They are delivered on the calling
// thread, which must pump messages: call it on MyGo's main thread. Only
// one set of hooks exists at a time; the returned function removes it.
func HookWinEvents(fn WinEventFunc, ranges ...[2]uint32) (unhook func()) {
	hookMu.Lock()
	hookFn = fn
	hookMu.Unlock()
	var hooks []uintptr
	for _, r := range ranges {
		h, _, _ := procSetWinEventHook.Call(uintptr(r[0]), uintptr(r[1]), 0, hookCallback, 0, 0,
			winEventOutOfContext|winEventSkipOwnProcess)
		if h != 0 {
			hooks = append(hooks, h)
		}
	}
	return func() {
		for _, h := range hooks {
			procUnhookWinEvent.Call(h)
		}
		hookMu.Lock()
		hookFn = nil
		hookMu.Unlock()
	}
}

// MessageFunc sees the messages of a subclassed window. It returns the
// result and true when it handled the message.
type MessageFunc func(msg uint32, wParam, lParam uintptr) (uintptr, bool)

var subMu sync.Mutex
var subFns = map[HWND]MessageFunc{}
var subCallback = syscall.NewCallback(func(hwnd, msg, wParam, lParam, id, data uintptr) uintptr {
	subMu.Lock()
	fn := subFns[HWND(hwnd)]
	subMu.Unlock()
	if fn != nil {
		if r, ok := fn(uint32(msg), wParam, lParam); ok {
			return r
		}
	}
	r, _, _ := procDefSubclassProc.Call(hwnd, msg, wParam, lParam)
	return r
})

const subclassID = 0x5342 // "SB"

// Subclass routes a window's messages through fn first. Call it on the
// window's thread.
func Subclass(h HWND, fn MessageFunc) {
	subMu.Lock()
	subFns[h] = fn
	subMu.Unlock()
	procSetWindowSubclass.Call(uintptr(h), subCallback, subclassID, 0)
}

// Unsubclass undoes Subclass.
func Unsubclass(h HWND) {
	procRemoveWindowSubclass.Call(uintptr(h), subCallback, subclassID)
	subMu.Lock()
	delete(subFns, h)
	subMu.Unlock()
}

// AppBar edge and notifications.
const (
	abmNew      = 0
	abmRemove   = 1
	abmQueryPos = 2
	abmSetPos   = 3
	abeLeft     = 0

	AbnPosChanged    = 1
	AbnFullScreenApp = 2
)

type appBarData struct {
	Size     uint32
	Wnd      uintptr
	Callback uint32
	Edge     uint32
	Rc       Rect
	LParam   uintptr
}

// AppBar reserves a band of a display's work area for a window, as the
// taskbar reserves its own: maximized windows stay out of it.
type AppBar struct {
	Wnd        HWND
	Callback   uint32
	registered bool
}

func (a *AppBar) data() appBarData {
	return appBarData{Size: uint32(unsafe.Sizeof(appBarData{})), Wnd: uintptr(a.Wnd), Callback: a.Callback}
}

// Reserve takes width pixels along the left edge of monitor and returns
// the band the shell granted.
func (a *AppBar) Reserve(monitor Rect, width int32) Rect {
	if !a.registered {
		d := a.data()
		r, _, _ := procSHAppBarMessage.Call(abmNew, uintptr(unsafe.Pointer(&d)))
		a.registered = r != 0
	}
	d := a.data()
	d.Edge = abeLeft
	d.Rc = monitor
	procSHAppBarMessage.Call(abmQueryPos, uintptr(unsafe.Pointer(&d)))
	d.Rc.Right = d.Rc.Left + width
	procSHAppBarMessage.Call(abmSetPos, uintptr(unsafe.Pointer(&d)))
	return d.Rc
}

// Registered reports whether the band is reserved.
func (a *AppBar) Registered() bool { return a.registered }

// Remove gives the band back.
func (a *AppBar) Remove() {
	if !a.registered {
		return
	}
	d := a.data()
	procSHAppBarMessage.Call(abmRemove, uintptr(unsafe.Pointer(&d)))
	a.registered = false
}
