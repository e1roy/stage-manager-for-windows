//go:build windows

package winapi

import (
	"syscall"
	"unsafe"
)

// ProcessPath returns the executable of a process, or "" when the process
// cannot be opened, as protected ones cannot.
func ProcessPath(pid uint32) string {
	const processQueryLimitedInformation = 0x1000
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if r, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

// CurrentThreadID returns the calling OS thread's ID.
func CurrentThreadID() uint32 {
	r, _, _ := procGetCurrentThreadId.Call()
	return uint32(r)
}

// LeftButtonDown reports whether the primary mouse button is held.
func LeftButtonDown() bool {
	const vkLButton, vkRButton, smSwapButton = 0x01, 0x02, 23
	vk := uintptr(vkLButton)
	if r, _, _ := procGetSystemMetrics.Call(smSwapButton); r != 0 {
		vk = vkRButton
	}
	r, _, _ := procGetAsyncKeyState.Call(vk)
	return r&0x8000 != 0
}

// Activate brings a window to the foreground. Windows grants that only to
// the process that owns the foreground or received the last input, so two
// workarounds are tried in turn: an empty mouse input, which makes StageBar
// the process with the last input without a key or a move reaching any
// app, then borrowing the foreground thread's input state.
func Activate(h HWND) bool {
	if !IsWindow(h) {
		return false
	}
	if IsIconic(h) {
		procShowWindowAsync.Call(uintptr(h), swRestore)
	}
	if boolCall(procSetForegroundWindow, uintptr(h)) {
		return true
	}
	emptyInput()
	if boolCall(procSetForegroundWindow, uintptr(h)) {
		return true
	}
	fg := Foreground()
	if fg == 0 {
		return false
	}
	_, fgThread := ProcessID(fg)
	own := CurrentThreadID()
	if fgThread == 0 || fgThread == own || !boolCall(procAttachThreadInput, uintptr(own), uintptr(fgThread), 1) {
		return false
	}
	defer procAttachThreadInput.Call(uintptr(own), uintptr(fgThread), 0)
	return boolCall(procSetForegroundWindow, uintptr(h))
}

// RegisterWindowMessage returns the message number for a name.
func RegisterWindowMessage(name string) uint32 {
	p, _ := syscall.UTF16PtrFromString(name)
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(p)))
	return uint32(r)
}

var procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")

// AllowAnyForeground lets any process take the foreground, as a second
// launch does before it hands over to the running instance.
func AllowAnyForeground() {
	const asfwAny = ^uintptr(0)
	procAllowSetForegroundWindow.Call(asfwAny)
}

var procSendInput = user32.NewProc("SendInput")

// emptyInput sends a mouse input that neither moves nor clicks.
func emptyInput() {
	// INPUT with type INPUT_MOUSE (0) and a zeroed MOUSEINPUT: 40 bytes on
	// 64-bit Windows.
	var in [40]byte
	procSendInput.Call(1, uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in))
}
