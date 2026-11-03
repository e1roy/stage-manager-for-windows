//go:build windows

package winapi

import "testing"

// shellTray returns the taskbar, a window of Explorer's process, or 0 where
// there is no desktop session to have one.
func shellTray() HWND {
	var tray HWND
	EnumWindows(func(h HWND) bool {
		if ClassName(h) == "Shell_TrayWnd" {
			tray = h
			return false
		}
		return true
	})
	return tray
}

func TestInExplorer(t *testing.T) {
	if InExplorer(0) {
		t.Fatal("no window is in Explorer")
	}
	tray := shellTray()
	if tray == 0 {
		t.Skip("no taskbar in this session")
	}
	if !InExplorer(tray) {
		t.Fatal("the taskbar is not in Explorer")
	}
}
