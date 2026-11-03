package app

import (
	"testing"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

// Explorer's windows are never asked to draw themselves: the capture of one
// answers at once, takes nothing and starts nothing, and is not asked for
// again before the refresh waits out its three seconds.
func TestCaptureLeavesExplorerAlone(t *testing.T) {
	var tray winapi.HWND
	winapi.EnumWindows(func(h winapi.HWND) bool {
		if winapi.ClassName(h) == "Shell_TrayWnd" {
			tray = h
			return false
		}
		return true
	})
	if tray == 0 {
		t.Skip("no taskbar in this session")
	}
	m := newMedia()
	answered := false
	m.capture([]winapi.HWND{tray}, true, time.Second, func() { answered = true })
	if !answered {
		t.Fatal("the capture of an Explorer window did not answer at once")
	}
	if m.pending[tray] || m.still(tray) != nil {
		t.Fatal("an Explorer window was captured")
	}
	if time.Since(m.tried[tray]) > time.Second {
		t.Fatal("the refresh would ask for the Explorer window again at once")
	}
}
