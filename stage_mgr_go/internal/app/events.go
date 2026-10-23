package app

import (
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

// hookEvents listens to the system's window events, so that the stage
// follows Alt+Tab, taskbar clicks and new windows at once rather than on a
// polling tick. The hooks deliver on the main thread.
func (a *App) hookEvents() {
	a.unhook = winapi.HookWinEvents(a.onWinEvent,
		[2]uint32{winapi.EventSystemForeground, winapi.EventSystemForeground},
		[2]uint32{winapi.EventSystemMoveSizeStart, winapi.EventSystemMoveSizeEnd},
		[2]uint32{winapi.EventSystemMinimizeEnd, winapi.EventSystemMinimizeEnd},
		[2]uint32{winapi.EventObjectCreate, winapi.EventObjectHide},
		[2]uint32{winapi.EventObjectLocationChange, winapi.EventObjectNameChange},
		[2]uint32{winapi.EventObjectCloaked, winapi.EventObjectUncloaked},
	)
}

func (a *App) onWinEvent(event uint32, h winapi.HWND) {
	if a.quitting {
		return
	}
	switch event {
	case winapi.EventSystemForeground:
		a.onForeground(h)
		return
	case winapi.EventSystemMoveSizeStart:
		a.startDrag(h)
		return
	case winapi.EventObjectCreate:
		if winapi.ClassName(h) == peekClass {
			a.peekStart(h)
		}
		return
	case winapi.EventObjectDestroy:
		if h == a.peek {
			a.peekEnd()
			return
		}
	case winapi.EventSystemMoveSizeEnd:
		a.windowMoved(h)
	case winapi.EventObjectLocationChange:
		if a.drag != nil && h == a.drag.h {
			a.dragOver()
		}
		// Moves of parked windows are ours. Others only matter for whether
		// a floating strip is covered.
		if !a.park.IsParked(h) && a.settings.Mode == settings.Floating && a.coverTimer == nil && winapi.Root(h) == h {
			a.coverTimer = after(50*time.Millisecond, a.updateCovered)
		}
		return
	}
	// Destroyed windows can no longer be asked anything: let them through.
	if event != winapi.EventObjectDestroy && winapi.Root(h) != h {
		return
	}
	a.refreshSoon()
}

func (a *App) isListed(h winapi.HWND) bool {
	return slices.ContainsFunc(a.windows, func(w catalog.Window) bool { return w.HWND == h })
}

func (a *App) updateCovered() {
	a.coverTimer = nil
	changed := false
	for _, d := range a.displays {
		if c := a.covered(d); c != d.covered {
			d.covered, changed = c, true
		}
	}
	if changed {
		a.redraw()
	}
}

// onForeground follows the user to another window: its app takes the stage.
func (a *App) onForeground(h winapi.HWND) {
	if !a.ready {
		return
	}
	// The window the user left gets a fresh still, for when its app goes.
	// Not when the focus only moves to a popup or dialog of the same app:
	// the app stays, and some apps flash when asked to draw themselves, as
	// JetBrains IDEs do on every Find Usages popup. Should the app go after
	// all, the switch takes the still.
	if prev := a.fg.prev; prev != 0 && prev != h && a.isListed(prev) && !a.park.IsParked(prev) && !sameProcess(prev, h) {
		if s := a.media.still(prev); s == nil || time.Since(s.at) > 1500*time.Millisecond {
			a.media.capture([]winapi.HWND{prev}, false, time.Second, a.redraw)
		}
	}
	a.fg.prev = catalog.RootOwner(h)
	debugf("foreground %x %q at %v iconic=%v parked=%v", h, winapi.ClassName(h), winapi.WindowRect(h), winapi.IsIconic(h), a.park.IsParked(catalog.RootOwner(h)))
	if catalog.IsDesktop(h) {
		// Clicking the wallpaper sends the stage to the strip, as on macOS.
		// The button check keeps Win+D and the like out of it.
		d := a.desktopClick()
		debugf("desktop clicked, clears %v", d != nil)
		if d != nil {
			a.switchStage(d, nil, 0, false, false)
		}
		return
	}
	a.fg.h, a.fg.tries = h, 0
	if a.fg.timer != nil {
		a.fg.timer.Stop()
	}
	a.fg.timer = after(70*time.Millisecond, a.settleForeground)
}

func sameProcess(a, b winapi.HWND) bool {
	pa, _ := winapi.ProcessID(a)
	pb, _ := winapi.ProcessID(b)
	return pa != 0 && pa == pb
}

// desktopClick returns the display whose wallpaper was just clicked, if
// that should clear its stage.
func (a *App) desktopClick() *display {
	if !a.settings.ClickDesktopClears || !a.settings.HideInactive || a.busy || !winapi.LeftButtonDown() {
		return nil
	}
	cur := winapi.Cursor()
	d := a.displayByHandle(winapi.MonitorFromPoint(cur))
	if d == nil || !a.managed(d) || d.model.Empty() {
		return nil
	}
	// The strip lets clicks through wherever no card is: those reach the
	// wallpaper and count.
	if d.strip != nil && d.strip.visible && d.strip.rect.Contains(cur) && d.strip.overCard(cur) {
		return nil
	}
	return d
}

func (a *App) settleForeground() {
	a.fg.timer = nil
	h := a.fg.h
	// The events come late and out of order: Alt+Tab's switcher reports
	// itself in front once more after the window it switched to. The window
	// in front now is the one to follow.
	if fg := winapi.Foreground(); h != 0 && fg != 0 && fg != h {
		debugf("foreground %x is %x now", h, fg)
		h, a.fg.h = fg, fg
	}
	if h == 0 || a.busy {
		debugf("foreground %x waits: busy=%v", h, a.busy)
		return
	}
	root := catalog.RootOwner(h)
	a.windows = a.cat.Windows()
	i := slices.IndexFunc(a.windows, func(w catalog.Window) bool { return w.HWND == root })
	if i < 0 {
		// New windows often get their title, or show, a moment after
		// they are activated.
		if a.fg.tries++; a.fg.tries < 6 && winapi.IsWindow(h) {
			a.fg.timer = after(70*time.Millisecond, a.settleForeground)
		} else {
			a.fg.h = 0
		}
		return
	}
	a.fg.h = 0
	w := a.windows[i]
	d := a.displayOf(w)
	debugf("foreground settled on %q (%s) busy=%v", w.Title, func() string {
		if d == nil {
			return "no display"
		}
		return d.mon.Device
	}(), a.busy)
	if d == nil || !a.managed(d) {
		return
	}
	if d.model.OnStage(w.App) {
		if a.park.IsParked(w.HWND) {
			a.park.Unpark(w.HWND)
			a.refreshSoon()
		}
		return
	}
	// The app comes with the apps of its set, as on macOS. The system
	// already brought its window up: the stage it leaves fades under it.
	// A companion, as a TortoiseSVN dialog opened from the file manager,
	// comes beside the stage instead and leaves it with its windows.
	a.switchStage(d, d.model.Coming(w.App, a.settings.Companion(w.Exe)), w.HWND, false, true)
}

// startPolling follows the pointer for the strips: revealing a floating
// strip at the screen's edge, and letting clicks through where no card is.
func (a *App) startPolling() {
	a.stopPoll = make(chan struct{})
	go func() {
		t := time.NewTicker(16 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-a.stopPoll:
				return
			case <-t.C:
				t0 := time.Now()
				mygo.RunOnMain(a.poll)
				pollStats.add(time.Since(t0))
			}
		}
	}()
}

func (a *App) poll() {
	if a.quitting {
		return
	}
	cur := winapi.Cursor()
	now := time.Now()
	a.watchTaskbar(cur)
	for _, d := range a.displays {
		if d.strip != nil {
			d.strip.poll(cur, now)
		}
	}
}

// pollRate measures how long the pointer poll waits for the main thread,
// in debug runs.
type pollRate struct {
	n         int
	sum, peak time.Duration
	since     time.Time
}

var pollStats pollRate

func (ps *pollRate) add(d time.Duration) {
	if !debugOn {
		return
	}
	ps.n++
	ps.sum += d
	ps.peak = max(ps.peak, d)
	if ps.since.IsZero() {
		ps.since = time.Now()
	}
	if el := time.Since(ps.since); el > 5*time.Second {
		debugf("poll: %.1f/s, wait avg %v, peak %v", float64(ps.n)/el.Seconds(), ps.sum/time.Duration(ps.n), ps.peak)
		*ps = pollRate{since: time.Now()}
	}
}
