package app

import (
	"strconv"
	"strings"
	"time"

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
	if winapi.IsTaskbar(root) {
		a.taskbar = taskbarPoint{tray: root, at: cur}
		a.aimTaskbar(root)
	} else if a.aim.tray != 0 {
		a.aim.tray = 0
		winapi.SetTaskbarTargets(nil)
	}
}

// taskbarAim is the taskbar whose buttons the minimize-button hook takes
// clicks on, while the pointer is over it.
type taskbarAim struct {
	tray winapi.HWND
	at   time.Time
	busy bool
}

// aimEvery is how often the buttons of a taskbar under the pointer are
// listed again: they move as apps open and close.
const aimEvery = 500 * time.Millisecond

// aimTaskbar has the minimize-button hook take the clicks on the taskbar
// buttons that minimize a window on a stage (MinimizeToStrip). Clicked,
// the button of the window in front minimizes it, which Windows does at
// once: it vanished, came back, and flew to the strip. Taken, it flies
// from where it is, as its minimize button has it. Which button stands for
// which window only UI Automation tells, in tens of milliseconds, so the
// buttons are listed as the pointer comes onto the taskbar, not as it
// clicks: one clicked before they are, the hook leaves to Windows.
func (a *App) aimTaskbar(tray winapi.HWND) {
	if a.unhookMinButton == nil || a.aim.busy || tray == a.aim.tray && time.Since(a.aim.at) < aimEvery {
		return
	}
	a.aim.busy = true
	go func() {
		buttons, err := winapi.TaskbarButtons(tray)
		post(func() {
			a.aim.busy = false
			if a.taskbar.tray != tray || a.unhookMinButton == nil {
				return
			}
			if a.aim.tray != tray && a.aim.tray != 0 {
				winapi.SetTaskbarTargets(nil)
			}
			a.aim.tray, a.aim.at = tray, time.Now()
			if err != nil {
				debugf("taskbar buttons: %v", err)
				winapi.SetTaskbarTargets(nil)
				return
			}
			winapi.SetTaskbarTargets(a.taskbarTargets(buttons))
		})
	}()
}

// taskbarTargets are the buttons that stand for one window alone, on a
// stage. A button with more windows shows their thumbnails when clicked,
// rather than minimize one.
func (a *App) taskbarTargets(buttons []winapi.TaskbarButton) []winapi.TaskbarTarget {
	var out []winapi.TaskbarTarget
	for _, b := range buttons {
		if buttonCount(b.Name) != 1 {
			continue
		}
		var h winapi.HWND
		n := 0
		for _, w := range a.windows {
			if buttonOf(b, w, a.cat.Name(w)) {
				h, n = w.HWND, n+1
			}
		}
		if n == 1 && a.calm[h] {
			out = append(out, winapi.TaskbarTarget{Rect: b.Rect, HWND: h})
		}
	}
	return out
}

// buttonCount is how many windows a button reads out it has, 0 for one
// with none or that does not say ("微信 - 1 个运行窗口 已固定" reads 1).
func buttonCount(name string) int {
	i := strings.LastIndex(name, " - ")
	if i < 0 {
		return 0
	}
	n := 0
	for _, c := range name[i+3:] {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
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
