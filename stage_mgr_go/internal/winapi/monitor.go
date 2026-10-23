//go:build windows

package winapi

import (
	"sync"
	"syscall"
	"unsafe"
)

// HMONITOR identifies a display.
type HMONITOR uintptr

// Monitor describes a display in physical pixels.
type Monitor struct {
	Handle  HMONITOR
	Device  string // \\.\DISPLAY1, stable while the display stays connected
	Monitor Rect
	Work    Rect
	DPI     uint32
	Primary bool
}

// Scale is the display's scale factor, 1 at 96 DPI.
func (m Monitor) Scale() float64 { return float64(m.DPI) / 96 }

type monitorInfoEx struct {
	Size    uint32
	Monitor Rect
	Work    Rect
	Flags   uint32
	Device  [32]uint16
}

const (
	monitorDefaultToNearest = 2
	monitorInfoPrimary      = 1
)

func monitorInfo(h HMONITOR) monitorInfoEx {
	mi := monitorInfoEx{Size: uint32(unsafe.Sizeof(monitorInfoEx{}))}
	procGetMonitorInfoW.Call(uintptr(h), uintptr(unsafe.Pointer(&mi)))
	return mi
}

func monitorDPI(h HMONITOR) uint32 {
	var x, y uint32
	if hr, _, _ := procGetDpiForMonitor.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y))); hr != 0 || x == 0 {
		return 96
	}
	return x
}

// MonitorInfo describes one display.
func MonitorInfo(h HMONITOR) Monitor {
	mi := monitorInfo(h)
	return Monitor{
		Handle:  h,
		Device:  syscall.UTF16ToString(mi.Device[:]),
		Monitor: mi.Monitor,
		Work:    mi.Work,
		DPI:     monitorDPI(h),
		Primary: mi.Flags&monitorInfoPrimary != 0,
	}
}

var monMu sync.Mutex
var monList []HMONITOR
var monCallback = syscall.NewCallback(func(h, _, _, _ uintptr) uintptr {
	monList = append(monList, HMONITOR(h))
	return 1
})

// Monitors lists the connected displays, the primary one first.
func Monitors() []Monitor {
	monMu.Lock()
	monList = nil
	procEnumDisplayMonitors.Call(0, 0, monCallback, 0)
	handles := monList
	monList = nil
	monMu.Unlock()
	out := make([]Monitor, 0, len(handles))
	for _, h := range handles {
		m := MonitorInfo(h)
		if m.Primary {
			out = append([]Monitor{m}, out...)
		} else {
			out = append(out, m)
		}
	}
	return out
}

func MonitorFromRect(r Rect) HMONITOR {
	h, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), monitorDefaultToNearest)
	return HMONITOR(h)
}

func MonitorFromPoint(p Point) HMONITOR {
	h, _, _ := procMonitorFromPoint.Call(uintptr(*(*uint64)(unsafe.Pointer(&p))), monitorDefaultToNearest)
	return HMONITOR(h)
}

func MonitorFromWindow(w HWND) HMONITOR {
	h, _, _ := procMonitorFromWindow.Call(uintptr(w), monitorDefaultToNearest)
	return HMONITOR(h)
}

// Cursor returns the pointer's position in physical screen pixels.
func Cursor() Point {
	var p Point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

// VirtualScreen returns the rectangle around every display.
func VirtualScreen() Rect {
	const smX, smY, smW, smH = 76, 77, 78, 79
	m := func(i uintptr) int32 { r, _, _ := procGetSystemMetrics.Call(i); return int32(r) }
	x, y := m(smX), m(smY)
	return Rect{x, y, x + m(smW), y + m(smH)}
}

// OnAnyMonitor reports whether a rectangle overlaps a display.
func OnAnyMonitor(r Rect) bool {
	for _, m := range Monitors() {
		if m.Monitor.Intersects(r) {
			return true
		}
	}
	return false
}

// MostlyOffscreen reports whether the displays show less than half of a
// rectangle.
func MostlyOffscreen(r Rect) bool {
	var areas []Rect
	for _, m := range Monitors() {
		areas = append(areas, m.Monitor)
	}
	return mostlyOutside(r, areas)
}

// mostlyOutside reports whether areas that do not overlap one another
// cover less than half of r.
func mostlyOutside(r Rect, areas []Rect) bool {
	total := int64(r.W()) * int64(r.H())
	if total <= 0 {
		return false
	}
	var shown int64
	for _, a := range areas {
		w := int64(min(r.Right, a.Right) - max(r.Left, a.Left))
		h := int64(min(r.Bottom, a.Bottom) - max(r.Top, a.Top))
		if w > 0 && h > 0 {
			shown += w * h
		}
	}
	return 2*shown < total
}

// FitOnScreen moves a rectangle as little as it takes into the work area
// of its nearest display.
func FitOnScreen(r Rect) Rect {
	return fitInto(r, MonitorInfo(MonitorFromRect(r)).Work)
}

// FitInto moves a rectangle as little as it takes into the work area of
// display m.
func FitInto(r Rect, m HMONITOR) Rect {
	return fitInto(r, MonitorInfo(m).Work)
}

// fitInto moves r as little as it takes into area. One larger than area
// keeps its top left corner there, where its title bar is.
func fitInto(r, area Rect) Rect {
	x := max(min(r.Left, area.Right-r.W()), area.Left)
	y := max(min(r.Top, area.Bottom-r.H()), area.Top)
	return r.Offset(x-r.Left, y-r.Top)
}
