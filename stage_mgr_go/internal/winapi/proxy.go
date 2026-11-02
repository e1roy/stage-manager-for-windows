//go:build windows

package winapi

import (
	"syscall"
	"unsafe"
)

// SystemProxy returns the proxy of Windows' Internet settings, as clients
// such as v2rayN and Clash set it when they take the system proxy: server
// is ProxyServer ("127.0.0.1:10809", or "https=...;socks=..."), bypass is
// ProxyOverride, and both are empty while the proxy is off. Go's own
// http.ProxyFromEnvironment reads only environment variables.
func SystemProxy() (server, bypass string) {
	const settings = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	if on, ok := regDWORD(hkeyCurrentUser, settings, "ProxyEnable"); !ok || on == 0 {
		return "", ""
	}
	server, _ = regString(hkeyCurrentUser, settings, "ProxyServer")
	bypass, _ = regString(hkeyCurrentUser, settings, "ProxyOverride")
	return server, bypass
}

// regString reads a string value, reporting whether there is one.
func regString(root uintptr, path, name string) (string, bool) {
	p, _ := syscall.UTF16PtrFromString(path)
	n, _ := syscall.UTF16PtrFromString(name)
	const rrfRtRegSz = 0x2
	var size uint32
	r, _, _ := procRegGetValueW.Call(root, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(n)), rrfRtRegSz, 0,
		0, uintptr(unsafe.Pointer(&size)))
	if r != 0 || size < 2 {
		return "", false
	}
	buf := make([]uint16, size/2)
	r, _, _ = procRegGetValueW.Call(root, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(n)), rrfRtRegSz, 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}
