package app

import (
	"strconv"
	"strings"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

// peekClass is the window the shell makes while it shows a peek at a window
// from its taskbar thumbnail: one for as long as the pointer rests on the
// thumbnails, destroyed once it leaves them.
const peekClass = "LivePreview"

// taskbarPoint is where the pointer was last over a taskbar: the thumbnails
// that show are those of the button there.
type taskbarPoint struct {
	tray winapi.HWND
	at   winapi.Point
}

// watchTaskbar notes where the pointer is over a taskbar. Main thread.
func (a *App) watchTaskbar(cur winapi.Point) {
	root := winapi.Root(winapi.WindowFromPoint(cur))
	if c := winapi.ClassName(root); c == "Shell_TrayWnd" || c == "Shell_SecondaryTrayWnd" {
		a.taskbar = taskbarPoint{tray: root, at: cur}
	}
}

// peekStart brings home the parked windows of the button whose thumbnails
// show: a peek shows the window peeked at where it is, so a parked one
// would show nothing. The shell hides the others only if they were on the
// desktop as the peek began, so no more come home than the button has.
func (a *App) peekStart(h winapi.HWND) {
	a.peek = h
	tb := a.taskbar
	if tb.tray == 0 {
		return
	}
	go func() {
		buttons, err := winapi.TaskbarButtons(tb.tray)
		post(func() {
			if a.peek != h {
				return // over already
			}
			b, ok := buttonAt(buttons, tb.at)
			debugf("peek at %q (%s), err=%v", b.Name, b.AppID, err)
			if !ok {
				return
			}
			a.windows = a.cat.Windows()
			for _, w := range a.windows {
				if a.park.IsParked(w.HWND) && buttonOf(b, w, a.cat.Name(w)) {
					a.park.Lift(w.HWND)
				}
			}
		})
	}()
}

// peekEnd parks again the windows the peek brought home. One the user
// picked from its thumbnail is activated, and comes onto the stage as
// Alt+Tab brings it.
func (a *App) peekEnd() {
	debugf("peek ends")
	a.peek = 0
	a.park.Lower()
	a.refreshSoon()
}

// buttonAt returns the button at a point, or, between two, the nearest.
func buttonAt(buttons []winapi.TaskbarButton, p winapi.Point) (winapi.TaskbarButton, bool) {
	best, dist := -1, int32(0)
	for i, b := range buttons {
		if b.Rect.Contains(p) {
			return b, true
		}
		if p.Y < b.Rect.Top || p.Y >= b.Rect.Bottom {
			continue
		}
		d := max(b.Rect.Left-p.X, p.X-b.Rect.Right)
		if best < 0 || d < dist {
			best, dist = i, d
		}
	}
	if best < 0 {
		return winapi.TaskbarButton{}, false
	}
	return buttons[best], true
}

// buttonOf reports whether a taskbar button stands for a window of an app
// named name. An AppUserModelID that is a program's path names it; others,
// which apps choose, say nothing of their windows, which then go by the
// name the button reads out.
func buttonOf(b winapi.TaskbarButton, w catalog.Window, name string) bool {
	if pid, ok := strings.CutPrefix(b.AppID, "*PID"); ok {
		v, err := strconv.ParseUint(pid, 16, 32)
		return err == nil && uint32(v) == w.PID
	}
	if strings.HasSuffix(strings.ToLower(b.AppID), ".exe") {
		exe := w.Exe[strings.LastIndexAny(w.Exe, `\/`)+1:]
		return exe != "" && strings.EqualFold(b.AppID[strings.LastIndexAny(b.AppID, `\/`)+1:], exe)
	}
	label := buttonLabel(b.Name)
	return label != "" && (strings.EqualFold(label, name) || w.Title == label || strings.HasSuffix(w.Title, " - "+label))
}

// buttonLabel is the app's name a button reads out, without how many
// windows it has ("微信 - 1 个运行窗口 已固定" reads "微信").
func buttonLabel(name string) string {
	if i := strings.LastIndex(name, " - "); i > 0 && i+3 < len(name) && name[i+3] >= '0' && name[i+3] <= '9' {
		return name[:i]
	}
	return ""
}
