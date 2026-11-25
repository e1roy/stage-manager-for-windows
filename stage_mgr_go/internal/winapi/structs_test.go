//go:build windows

package winapi

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

// TestStructSizes pins the structures handed to Win32 to the sizes of the
// SDK's on 64-bit Windows. Most calls take a size field and reject any
// other, some (GetWindowPlacement) do not, so a wrong layout can go unseen
// for as long as only those are called.
func TestStructSizes(t *testing.T) {
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"WINDOWPLACEMENT", unsafe.Sizeof(windowPlacement{}), 44},
		{"WINDOWINFO", unsafe.Sizeof(windowInfo{}), 60},
		{"APPBARDATA", unsafe.Sizeof(appBarData{}), 48},
		{"MONITORINFOEXW", unsafe.Sizeof(monitorInfoEx{}), 104},
		{"DWM_THUMBNAIL_PROPERTIES", unsafe.Sizeof(thumbnailProperties{}), 48},
		{"INPUT", unsafe.Sizeof(keyInput{}), 40},
		{"MSG", unsafe.Sizeof(threadMsg{}), 48},
		{"MSLLHOOKSTRUCT", unsafe.Sizeof(msllHookStruct{}), 32},
		{"KBDLLHOOKSTRUCT", unsafe.Sizeof(kbdllHookStruct{}), 24},
		{"BITMAPINFOHEADER", unsafe.Sizeof(bitmapInfoHeader{}), 40},
		{"ICONINFO", unsafe.Sizeof(iconInfo{}), 32},
		{"BITMAP", unsafe.Sizeof(bitmap{}), 32},
		{"POINT", unsafe.Sizeof(Point{}), 8},
		{"RECT", unsafe.Sizeof(Rect{}), 16},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: %d bytes, want %d", c.name, c.got, c.want)
		}
	}
}

// TestSetWindowPlacement checks the placement round trip on a window of
// the test's own: SetWindowPlacement refuses a wrong length, and a layout
// of the right size but wrong order would only show in where it reads.
func TestSetWindowPlacement(t *testing.T) {
	// The window answers on the thread that made it, which pumps no
	// messages: everything must stay on that thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	h := testWindow(t)
	// Deferred after the unlock above, so it runs before it: a window
	// dies only on the thread that made it, and a cleanup would run
	// after the unlock lets the goroutine wander onto another thread.
	defer destroyWindow(h)
	wp := windowPlacement{Length: uint32(unsafe.Sizeof(windowPlacement{}))}
	if !boolCall(procGetWindowPlacement, uintptr(h), uintptr(unsafe.Pointer(&wp))) {
		t.Fatal("GetWindowPlacement failed")
	}
	// A layout of the right size but with its fields in the wrong order
	// passes the size test and the round trip, and only misreads here.
	// The position is in workspace coordinates, the taskbar's width in
	// it: the width and height are what stay the same across machines.
	if got := wp.NormalPosition; got.W() != 200 || got.H() != 100 {
		t.Errorf("placement reads %v, want one 200 by 100", got)
	}
	if !boolCall(procSetWindowPlacement, uintptr(h), uintptr(unsafe.Pointer(&wp))) {
		t.Fatal("SetWindowPlacement failed")
	}
}

// testWindow creates a hidden window of the system's STATIC class, which
// nobody sees and no app owns. It is to be destroyed on this thread, by
// the caller's deferred destroyWindow.
func testWindow(t *testing.T) HWND {
	t.Helper()
	create := user32.NewProc("CreateWindowExW")
	class, _ := syscall.UTF16PtrFromString("STATIC")
	const wsPopup = 0x80000000
	h, _, err := create.Call(0, uintptr(unsafe.Pointer(class)), 0, wsPopup, 0, 0, 200, 100, 0, 0, 0, 0)
	if h == 0 {
		t.Fatal("CreateWindowExW:", err)
	}
	return HWND(h)
}

// destroyWindow takes a window down on the thread that made it, where
// DestroyWindow works.
func destroyWindow(h HWND) {
	user32.NewProc("DestroyWindow").Call(uintptr(h))
}
