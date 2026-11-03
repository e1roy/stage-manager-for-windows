//go:build windows

package winapi

import (
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procPeekMessageW        = user32.NewProc("PeekMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
)

const (
	whKeyboardLL = 13
	hcAction     = 0
	wmQuit       = 0x0012
	wmKeyDown    = 0x0100
	wmSysKeyDown = 0x0104
	pmNoRemove   = 0

	vkTab     = 0x09
	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12
	vkLWin    = 0x5B
	vkRWin    = 0x5C
	// vkMask is a key no keyboard has. Pressed while the Win key is held,
	// it tells the shell the Win key went with another key, so that
	// letting go of it does not open Start, which it would with the Tab
	// taken; no app knows it to act on it. AutoHotkey masks the Win key
	// with the same one.
	vkMask         = 0xE8
	inputKeyboard  = 1
	keyEventfKeyUp = 0x0002
	// maskTag is the dwExtraInfo of the keys maskWinKey sends, "SB", so that
	// the hook, which sees them too, lets them through at once.
	maskTag = 0x5342
)

// kbdllHookStruct is KBDLLHOOKSTRUCT.
type kbdllHookStruct struct {
	VkCode, ScanCode, Flags, Time uint32
	ExtraInfo                     uintptr
}

// keyInput is an INPUT of type INPUT_KEYBOARD: 40 bytes on 64-bit
// Windows, the size of the union's largest member, a mouse's input.
type keyInput struct {
	typ   uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	extra uintptr
	_     [8]byte
}

type threadMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      Point
	_       uint32
}

// winTab is the state of the hook, which runs on its own thread. held is
// set from a Tab taken with the Win key till its release, so that its
// repeats and its release are taken too, and only the press acts.
//
// Neither is behind a lock: the hook is called again from inside itself.
// The keys maskWinKey sends pass through it, and while the thread waits for
// them Windows hands it the next key of the user's, a Tab let go of quickly
// among them. A mutex held across the send then waited for itself, for good,
// with the hook thread stuck and shutting down stuck behind it.
var winTab struct {
	fn   atomic.Pointer[func()]
	held atomic.Bool
}

var winTabCallback = syscall.NewCallback(func(code, wParam uintptr, k *kbdllHookStruct) uintptr {
	if int32(code) == hcAction && k.ExtraInfo != maskTag && k.VkCode == vkTab {
		taken, fire := winTabKey(wParam == wmKeyDown || wParam == wmSysKeyDown)
		if fire != nil {
			fire()
		}
		if taken {
			return 1
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, code, wParam, uintptr(unsafe.Pointer(k)))
	return r
})

// The test swaps these for ones that press and read no key.
var (
	keyIsDown = keyHeld
	sendMask  = maskWinKey
)

// winTabKey reports whether Tab going down or up is taken from Windows,
// and returns the function to call for it, if any: only Win+Tab, without
// Shift, Ctrl or Alt, which open nothing of Windows' own with it. It may be
// called again from inside sendMask, and must not wait for anything.
func winTabKey(down bool) (taken bool, fire func()) {
	if !down {
		return winTab.held.Swap(false), nil
	}
	if winTab.held.Load() {
		return true, nil
	}
	fn := winTab.fn.Load()
	if fn == nil || !keyIsDown(vkLWin) && !keyIsDown(vkRWin) || keyIsDown(vkShift) || keyIsDown(vkControl) || keyIsDown(vkMenu) {
		return false, nil
	}
	// Held before the mask goes out, so that the release that may arrive
	// while it does is taken as well.
	winTab.held.Store(true)
	sendMask()
	return true, *fn
}

func keyHeld(vk uintptr) bool {
	r, _, _ := procGetAsyncKeyState.Call(vk)
	return r&0x8000 != 0
}

// maskWinKey presses and lets go of vkMask, which reaches the shell after
// the Tab it stands in for and before the Win key's release.
func maskWinKey() {
	in := [2]keyInput{
		{typ: inputKeyboard, vk: vkMask, extra: maskTag},
		{typ: inputKeyboard, vk: vkMask, flags: keyEventfKeyUp, extra: maskTag},
	}
	procSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
}

// HookWinTab takes Win+Tab from Windows, which opens Task View with it, and
// calls fn once per press instead. fn runs on the hook's thread and must
// not block: Windows holds every key of the desktop till the hook returns.
// Only one hook exists at a time; the returned function removes it.
//
// The hook is low level, as a hot key cannot be had for a combination the
// shell keeps for itself, and it runs on a thread of its own, whose message
// loop does nothing else: on the main thread, anything that kept it busy
// would hold up typing everywhere, and Windows drops a hook that keeps
// keys waiting longer than LowLevelHooksTimeout.
func HookWinTab(fn func()) (unhook func(), err error) {
	type started struct {
		tid uint32
		err error
	}
	done := make(chan started)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		var m threadMsg
		// The thread gets its message queue here, for the quit to reach.
		procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmNoRemove)
		mod, _, _ := procGetModuleHandleW.Call(0)
		h, _, e := procSetWindowsHookExW.Call(whKeyboardLL, winTabCallback, mod, 0)
		if h == 0 {
			done <- started{err: e}
			return
		}
		winTab.held.Store(false)
		winTab.fn.Store(&fn)
		done <- started{tid: CurrentThreadID()}
		for {
			if r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0); int32(r) <= 0 {
				break
			}
		}
		procUnhookWindowsHookEx.Call(h)
	}()
	s := <-done
	if s.err != nil {
		return nil, s.err
	}
	return func() {
		winTab.fn.Store(nil)
		winTab.held.Store(false)
		procPostThreadMessageW.Call(uintptr(s.tid), wmQuit, 0, 0)
	}, nil
}
