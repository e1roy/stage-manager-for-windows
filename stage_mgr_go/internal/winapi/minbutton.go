//go:build windows

package winapi

import (
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const (
	whMouseLL     = 14
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmNcHitTest   = 0x0084
	htMinButton   = 8
	llmhfInjected = 0x1
	// minBand is how far down from the top of a window, in DIPs, a click
	// may be on its minimize button. Below it no app is asked where the
	// click is: every click waits for the answer, the whole system's.
	minBand = 64
	// minAsk is how long an app has to say where a click is. A busy one
	// that cannot say in time gets the click as if there were no hook.
	minAsk = 50
)

type msllHookStruct struct {
	Pt    Point
	Data  uint32
	Flags uint32
	Time  uint32
	Extra uintptr
}

// minButton is the state of the minimize-button hook. It runs on a
// thread of its own and must not lock: the hook holds up all mouse input
// while it runs.
var minButton struct {
	want  atomic.Pointer[func(HWND) bool]
	fn    atomic.Pointer[func(HWND)]
	taken atomic.Uintptr // the window whose button press was taken
	// at is the taskbar button the press was taken on, if it was.
	at atomic.Pointer[Rect]
	// buttons are the taskbar buttons to take (SetTaskbarTargets).
	buttons atomic.Pointer[[]TaskbarTarget]
}

// TaskbarTarget is a taskbar button that stands for one window alone.
type TaskbarTarget struct {
	Rect Rect
	HWND HWND
}

// SetTaskbarTargets has the minimize-button hook take the clicks on these
// taskbar buttons too, while the window of one is in front: Windows
// minimizes it, as its minimize button does. Any thread.
func SetTaskbarTargets(ts []TaskbarTarget) {
	if len(ts) == 0 {
		minButton.buttons.Store(nil)
		return
	}
	minButton.buttons.Store(&ts)
}

// onTaskbarTarget returns the button at p that would minimize the window
// in front, if it is one of the targets.
func onTaskbarTarget(p Point) (HWND, Rect) {
	ts := minButton.buttons.Load()
	if ts == nil {
		return 0, Rect{}
	}
	r, _, _ := procWindowFromPoint.Call(uintptr(*(*uint64)(unsafe.Pointer(&p))))
	if r == 0 || !IsTaskbar(Root(HWND(r))) {
		return 0, Rect{}
	}
	// The window in front may be a dialog the target owns.
	fg := Foreground()
	owner, _, _ := procGetAncestor.Call(uintptr(fg), gaRootOwner)
	for _, t := range *ts {
		if t.Rect.Contains(p) && (t.HWND == Root(fg) || uintptr(t.HWND) == owner) {
			return t.HWND, t.Rect
		}
	}
	return 0, Rect{}
}

// onMinButton returns the top-level window whose minimize button is at p,
// if want takes it.
func onMinButton(p Point, want func(HWND) bool) HWND {
	r, _, _ := procWindowFromPoint.Call(uintptr(*(*uint64)(unsafe.Pointer(&p))))
	if r == 0 {
		return 0
	}
	under := HWND(r)
	root := Root(under)
	if root == 0 || !want(root) {
		return 0
	}
	// Caption buttons are at the top right (top left in right-to-left
	// layouts, which this leaves to Windows).
	wr := WindowRect(root)
	band := int32(minBand * DpiForWindow(root) / 96)
	if p.Y >= wr.Top+band || p.X < wr.Left+wr.W()/2 {
		return 0
	}
	const smtoAbortIfHung = 0x0002
	lp := uintptr(uint16(p.X)) | uintptr(uint16(p.Y))<<16
	var hit uintptr
	if ok, _, _ := procSendMessageTimeoutW.Call(uintptr(under), wmNcHitTest, 0, lp, smtoAbortIfHung, minAsk, uintptr(unsafe.Pointer(&hit))); ok == 0 || hit != htMinButton {
		return 0
	}
	return root
}

var minButtonCallback = syscall.NewCallback(func(code, wParam uintptr, m *msllHookStruct) uintptr {
	if int32(code) == hcAction && m.Flags&llmhfInjected == 0 {
		want, fn := minButton.want.Load(), minButton.fn.Load()
		switch wParam {
		case wmLButtonDown:
			minButton.taken.Store(0)
			minButton.at.Store(nil)
			if want != nil && fn != nil {
				if h, r := onTaskbarTarget(m.Pt); h != 0 {
					minButton.at.Store(&r)
					minButton.taken.Store(uintptr(h))
					return 1
				}
				if h := onMinButton(m.Pt, *want); h != 0 {
					minButton.taken.Store(uintptr(h))
					return 1
				}
			}
		case wmLButtonUp:
			if h := HWND(minButton.taken.Swap(0)); h != 0 {
				at := minButton.at.Swap(nil)
				// Let go off the button, the press is called off, as
				// Windows calls off a minimize.
				var still bool
				if at != nil {
					still = at.Contains(m.Pt)
				} else {
					still = want != nil && onMinButton(m.Pt, *want) == h
				}
				if fn != nil && still {
					(*fn)(h)
				}
				return 1
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, code, wParam, uintptr(unsafe.Pointer(m)))
	return r
})

// HookMinButton takes left clicks on the minimize button of the windows
// want accepts, and on the taskbar buttons of SetTaskbarTargets, and calls
// fn with the window instead of letting it minimize. Windows hides a minimized window at once, before anyone can
// be told; the click has to be taken before it gets to the window.
// Windows that do not answer WM_NCHITTEST with HTMINBUTTON (some draw
// their own title bar) still minimize.
//
// want and fn run on the hook thread, with all mouse input waiting: they
// must return at once, without locking. Call unhook outside them: it
// waits for the hook to be removed.
func HookMinButton(want func(HWND) bool, fn func(HWND)) (unhook func(), err error) {
	type started struct {
		tid uint32
		err error
	}
	ready := make(chan started)
	stopped := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(stopped)
		var m threadMsg
		procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmNoRemove)
		mod, _, _ := procGetModuleHandleW.Call(0)
		minButton.taken.Store(0)
		minButton.want.Store(&want)
		minButton.fn.Store(&fn)
		h, _, e := procSetWindowsHookExW.Call(whMouseLL, minButtonCallback, mod, 0)
		if h == 0 {
			minButton.want.Store(nil)
			minButton.fn.Store(nil)
			ready <- started{err: e}
			return
		}
		ready <- started{tid: CurrentThreadID()}
		for {
			if r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0); int32(r) <= 0 {
				break
			}
		}
		minButton.want.Store(nil)
		minButton.fn.Store(nil)
		procUnhookWindowsHookEx.Call(h)
		minButton.taken.Store(0)
		minButton.buttons.Store(nil)
	}()
	s := <-ready
	if s.err != nil {
		return nil, s.err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			minButton.want.Store(nil)
			minButton.fn.Store(nil)
			procPostThreadMessageW.Call(uintptr(s.tid), wmQuit, 0, 0)
			<-stopped
		})
	}, nil
}
