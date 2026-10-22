//go:build windows

package winapi

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	version                     = syscall.NewLazyDLL("version.dll")
	procGetFileVersionInfoSizeW = version.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfoW     = version.NewProc("GetFileVersionInfoW")
	procVerQueryValueW          = version.NewProc("VerQueryValueW")
)

// FileDescription returns the product name an executable gives itself,
// such as "Google Chrome" for chrome.exe, or "".
func FileDescription(path string) string {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	size, _, _ := procGetFileVersionInfoSizeW.Call(uintptr(unsafe.Pointer(p)), 0)
	if size == 0 {
		return ""
	}
	buf := make([]byte, size)
	if r, _, _ := procGetFileVersionInfoW.Call(uintptr(unsafe.Pointer(p)), 0, size, uintptr(unsafe.Pointer(&buf[0]))); r == 0 {
		return ""
	}
	query := func(q string) (unsafe.Pointer, uint32) {
		qp, _ := syscall.UTF16PtrFromString(q)
		var ptr unsafe.Pointer
		var n uint32
		if r, _, _ := procVerQueryValueW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(qp)), uintptr(unsafe.Pointer(&ptr)), uintptr(unsafe.Pointer(&n))); r == 0 {
			return nil, 0
		}
		return ptr, n
	}
	langs := []string{}
	if ptr, n := query(`\VarFileInfo\Translation`); ptr != nil && n >= 4 {
		pairs := unsafe.Slice((*uint16)(ptr), n/2)
		for i := 0; i+1 < len(pairs); i += 2 {
			langs = append(langs, fmt.Sprintf("%04x%04x", pairs[i], pairs[i+1]))
		}
	}
	langs = append(langs, "040904b0", "040904e4", "080404b0")
	for _, lang := range langs {
		for _, key := range []string{"FileDescription", "ProductName"} {
			if ptr, n := query(`\StringFileInfo\` + lang + `\` + key); ptr != nil && n > 1 {
				if s := syscall.UTF16ToString(unsafe.Slice((*uint16)(ptr), n)); s != "" {
					return s
				}
			}
		}
	}
	return ""
}
