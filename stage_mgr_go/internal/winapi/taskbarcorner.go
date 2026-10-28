//go:build windows

package winapi

import (
	"sync"
	"syscall"
	"unsafe"
)

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	ntdll    = syscall.NewLazyDLL("ntdll.dll")

	procRegGetValueW  = advapi32.NewProc("RegGetValueW")
	procRtlGetVersion = ntdll.NewProc("RtlGetVersion")
)

const (
	hkeyCurrentUser  = 0x80000001
	hkeyLocalMachine = 0x80000002
)

// WindowsBuild returns the build number of Windows: 22000 and up is
// Windows 11. RtlGetVersion tells the truth whatever the manifest says.
func WindowsBuild() uint32 { return windowsBuild() }

var windowsBuild = sync.OnceValue(func() uint32 {
	var vi struct {
		size, major, minor, build, platform uint32
		csd                                 [128]uint16
	}
	vi.size = uint32(unsafe.Sizeof(vi))
	procRtlGetVersion.Call(uintptr(unsafe.Pointer(&vi)))
	return vi.build
})

// regDWORD reads a DWORD value, reporting whether there is one.
func regDWORD(root uintptr, path, name string) (uint32, bool) {
	p, _ := syscall.UTF16PtrFromString(path)
	n, _ := syscall.UTF16PtrFromString(name)
	const rrfRtRegDword = 0x10
	var v, size uint32 = 0, 4
	r, _, _ := procRegGetValueW.Call(root, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(n)), rrfRtRegDword, 0,
		uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&size)))
	return v, r == 0
}

// TaskbarCornerFree reports whether the taskbar leaves its bottom-left
// corner empty: on Windows 11, with its buttons centered, as they are
// unless set to the left, and no widgets, which take that corner. On
// Windows 10 the Start button is there.
func TaskbarCornerFree() bool {
	if WindowsBuild() < 22000 {
		return false
	}
	const advanced = `Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`
	if al, ok := regDWORD(hkeyCurrentUser, advanced, "TaskbarAl"); ok && al == 0 {
		return false
	}
	// Widgets show unless turned off, by the user or by policy.
	if allow, ok := regDWORD(hkeyLocalMachine, `SOFTWARE\Policies\Microsoft\Dsh`, "AllowNewsAndInterests"); ok && allow == 0 {
		return true
	}
	da, ok := regDWORD(hkeyCurrentUser, advanced, "TaskbarDa")
	return ok && da == 0
}

// IsTaskbar reports whether a top-level window is a taskbar: the main
// one, or that of another display.
func IsTaskbar(h HWND) bool {
	c := ClassName(h)
	return c == "Shell_TrayWnd" || c == "Shell_SecondaryTrayWnd"
}

// TaskbarLight reports whether the taskbar is light, as the theme of
// Windows (not that of apps) makes it.
func TaskbarLight() bool {
	v, ok := regDWORD(hkeyCurrentUser, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "SystemUsesLightTheme")
	return ok && v != 0
}
