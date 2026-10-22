//go:build windows

// Package catalog finds the application windows Stage Manager arranges and
// tells which app each belongs to.
package catalog

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/renkai9418/stage-bar/internal/winapi"
)

// Window is one application window as it was when listed.
type Window struct {
	HWND  winapi.HWND
	Title string
	// App identifies the application: windows of one app go on and off
	// the stage together.
	App string
	Exe string
	PID uint32
	// Rect is the window rectangle, invisible resize borders included,
	// and Frame the visible frame DWM draws.
	Rect, Frame winapi.Rect
	Foreground  bool
}

// Aspect is the window's width over its height.
func (w Window) Aspect() float64 {
	if w.Frame.Empty() {
		return 16.0 / 10
	}
	return float64(w.Frame.W()) / float64(w.Frame.H())
}

const (
	applicationFrameClass = "ApplicationFrameWindow"
	coreWindowClass       = "Windows.UI.Core.CoreWindow"
)

var excludedClasses = map[string]bool{
	"Progman":                true,
	"WorkerW":                true,
	"Shell_TrayWnd":          true,
	"Shell_SecondaryTrayWnd": true,
	coreWindowClass:          true,
	// The Start menu, search and the notification center of Windows 11.
	"Windows.UI.Composition.DesktopWindowContentBridge": true,
	"XamlExplorerHostIslandWindow":                      true,
}

// IsDesktop reports whether a window is the wallpaper.
func IsDesktop(h winapi.HWND) bool {
	c := winapi.ClassName(h)
	return c == "Progman" || c == "WorkerW"
}

type identity struct {
	key, exe string
	pid      uint32
}

// Catalog lists windows and remembers which app each process is.
type Catalog struct {
	self   uint32
	apps   map[uint32]identity
	filter map[string]bool
	names  map[string]string
}

// New returns a catalog. STAGEBAR_APP_FILTER, a comma-separated list of
// executable names, limits it to those apps, so that a test run leaves
// the developer's own windows alone.
func New() *Catalog {
	c := &Catalog{self: uint32(os.Getpid()), apps: map[uint32]identity{}, names: map[string]string{}}
	if f := os.Getenv("STAGEBAR_APP_FILTER"); f != "" {
		c.filter = map[string]bool{}
		for _, name := range strings.Split(f, ",") {
			if name = strings.TrimSpace(strings.ToLower(strings.TrimSuffix(name, ".exe"))); name != "" {
				c.filter[name] = true
			}
		}
	}
	return c
}

// Windows lists application windows front-most first. Owned windows such
// as dialogs are left out: they go wherever their owner goes.
func (c *Catalog) Windows() []Window {
	fg := winapi.Foreground()
	var out []Window
	seen := map[uint32]bool{}
	winapi.EnumWindows(func(h winapi.HWND) bool {
		if w, ok := c.inspect(h, fg); ok {
			out = append(out, w)
			seen[w.PID] = true
		}
		return true
	})
	for pid := range c.apps {
		if !seen[pid] {
			delete(c.apps, pid)
		}
	}
	listed := make(map[winapi.HWND]bool, len(out))
	for _, w := range out {
		listed[w.HWND] = true
	}
	kept := out[:0]
	for _, w := range out {
		if !ownedByListed(w.HWND, listed) {
			kept = append(kept, w)
		}
	}
	return kept
}

func ownedByListed(h winapi.HWND, listed map[winapi.HWND]bool) bool {
	o := winapi.Owner(h)
	for i := 0; o != 0 && i < 16; i++ {
		if listed[o] {
			return true
		}
		o = winapi.Owner(o)
	}
	return false
}

func (c *Catalog) inspect(h, fg winapi.HWND) (Window, bool) {
	if !winapi.IsWindowVisible(h) || winapi.Root(h) != h {
		return Window{}, false
	}
	// A minimized window belongs to the taskbar: leaving it out keeps an
	// app the user minimized off the strip as well as off the stage.
	if winapi.IsIconic(h) {
		return Window{}, false
	}
	pid, _ := winapi.ProcessID(h)
	if pid == 0 || pid == c.self {
		return Window{}, false
	}
	ex := winapi.ExStyle(h)
	appWindow := ex&winapi.WsExAppWindow != 0
	// Some apps show their main window as an owned tool window and opt in
	// with WS_EX_APPWINDOW; genuine tool windows stay out.
	if ex&winapi.WsExToolWindow != 0 && !appWindow {
		return Window{}, false
	}
	// Always-on-top utilities and overlays float above the stage.
	if ex&winapi.WsExTopmost != 0 || ex&winapi.WsExNoActivate != 0 && !appWindow {
		return Window{}, false
	}
	if winapi.IsCloaked(h) {
		return Window{}, false
	}
	class := winapi.ClassName(h)
	if excludedClasses[class] {
		return Window{}, false
	}
	title := strings.TrimSpace(winapi.Title(h))
	if title == "" {
		return Window{}, false
	}
	w := Window{HWND: h, Title: title, Foreground: h == fg}
	w.Rect, w.Frame = winapi.WindowRect(h), winapi.FrameBounds(h)
	if w.Frame.W() < 40 || w.Frame.H() < 30 {
		return Window{}, false
	}
	id := c.resolve(h, class, pid)
	if c.filter != nil && !c.filter[strings.ToLower(strings.TrimSuffix(filepath.Base(id.exe), filepath.Ext(id.exe)))] {
		return Window{}, false
	}
	w.App, w.Exe, w.PID = id.key, id.exe, id.pid
	return w, true
}

// resolve finds the app of a window. UWP windows are hosted by
// ApplicationFrameHost, and the app is the process of the CoreWindow
// inside; a suspended app has detached it and stays apart.
func (c *Catalog) resolve(h winapi.HWND, class string, pid uint32) identity {
	if class == applicationFrameClass {
		hosted := uint32(0)
		winapi.EnumChildWindows(h, func(child winapi.HWND) bool {
			if winapi.ClassName(child) != coreWindowClass {
				return true
			}
			if p, _ := winapi.ProcessID(child); p != 0 && p != pid {
				hosted = p
				return false
			}
			return true
		})
		if hosted == 0 {
			return identity{key: "frame:" + strings.ToUpper(winapi.Title(h)), pid: pid}
		}
		pid = hosted
	}
	if id, ok := c.apps[pid]; ok {
		return id
	}
	exe := winapi.ProcessPath(pid)
	id := identity{key: strings.ToUpper(exe), exe: exe, pid: pid}
	if exe == "" {
		id.key = "pid:" + itoa(pid)
	}
	c.apps[pid] = id
	return id
}

// Name returns the name to show for an app: the product name of its
// executable, else the executable's name, else a window's title.
func (c *Catalog) Name(w Window) string {
	if w.Exe == "" {
		return w.Title
	}
	if n, ok := c.names[w.Exe]; ok {
		return n
	}
	n := winapi.FileDescription(w.Exe)
	// Packaged apps often describe themselves by their file name; their
	// windows end their titles with the name the user knows ("… - 画图").
	if n == "" || strings.HasSuffix(strings.ToLower(n), ".exe") {
		n = ""
		if i := strings.LastIndex(w.Title, " - "); i >= 0 {
			if t := strings.TrimSpace(w.Title[i+3:]); t != "" && len([]rune(t)) <= 32 {
				n = t
			}
		}
	}
	if n == "" {
		n = strings.TrimSuffix(filepath.Base(w.Exe), filepath.Ext(w.Exe))
	}
	c.names[w.Exe] = n
	return n
}

// OwnedWindows lists the visible windows whose owner chain leads to owner.
func OwnedWindows(owner winapi.HWND) []winapi.HWND {
	var out []winapi.HWND
	winapi.EnumWindows(func(h winapi.HWND) bool {
		if h == owner || !winapi.IsWindowVisible(h) {
			return true
		}
		o := winapi.Owner(h)
		for i := 0; o != 0 && i < 16; i++ {
			if o == owner {
				out = append(out, h)
				break
			}
			o = winapi.Owner(o)
		}
		return true
	})
	return out
}

// RootOwner walks up visible owners to the window that stands for the app.
func RootOwner(h winapi.HWND) winapi.HWND {
	r := winapi.Root(h)
	if r == 0 {
		r = h
	}
	for i := 0; i < 16; i++ {
		o := winapi.Owner(r)
		if o == 0 || !winapi.IsWindowVisible(o) {
			break
		}
		r = o
	}
	return r
}

func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
