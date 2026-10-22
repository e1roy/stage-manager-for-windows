package app

import (
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/renkai9418/stage-bar/internal/catalog"
	"github.com/renkai9418/stage-bar/internal/settings"
	"github.com/renkai9418/stage-bar/internal/winapi"
)

// hookEvents listens to the system's window events, so that the stage
// follows Alt+Tab, taskbar clicks and new windows at once rather than on a
// polling tick. The hooks deliver on the main thread.
func (a *App) hookEvents() {
	a.unhook = winapi.HookWinEvents(a.onWinEvent,
		[2]uint32{winapi.EventSystemForeground, winapi.EventSystemForeground},
		[2]uint32{winapi.EventSystemMoveSizeEnd, winapi.EventSystemMoveSizeEnd},
		[2]uint32{winapi.EventSystemMinimizeEnd, winapi.EventSystemMinimizeEnd},
		[2]uint32{winapi.EventObjectDestroy, winapi.EventObjectHide},
		[2]uint32{winapi.EventObjectLocationChange, winapi.EventObjectNameChange},
		[2]uint32{winapi.EventObjectCloaked, winapi.EventObjectUncloaked},
	)
}

func (a *App) onWinEvent(event uint32, h winapi.HWND) {
	if a.quitting || a.settings.Paused {
		return
	}
	switch event {
	case winapi.EventSystemForeground:
		a.onForeground(h)
		return
	case winapi.EventSystemMoveSizeEnd:
		// A window the user moved to another display joins the stage there.
		a.windowMoved(h)
	case winapi.EventObjectLocationChange:
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

// windowMoved puts the app of a window that was moved to another display on
// that display's stage, beside what is there: the user put it there to use
// it, and it must not be taken for a stray and parked.
func (a *App) windowMoved(h winapi.HWND) {
	if a.busy || !a.ready || a.park.IsParked(h) {
		return
	}
	a.windows = a.cat.Windows()
	i := slices.IndexFunc(a.windows, func(w catalog.Window) bool { return w.HWND == h })
	if i < 0 {
		return
	}
	w := a.windows[i]
	if d := a.displayOf(w); d != nil && a.managed(d) && !d.model.OnStage(w.App) {
		d.model.Join(w.App, time.Now())
	}
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
	if prev := a.fg.prev; prev != 0 && prev != h && a.isListed(prev) && !a.park.IsParked(prev) {
		if s := a.media.still(prev); s == nil || time.Since(s.at) > 1500*time.Millisecond {
			a.media.capture([]winapi.HWND{prev}, false, time.Second, a.redraw)
		}
	}
	a.fg.prev = catalog.RootOwner(h)
	debugf("foreground %x %q", h, winapi.ClassName(h))
	if catalog.IsDesktop(h) {
		// Clicking the wallpaper sends the stage to the strip, as on macOS.
		// The button check keeps Win+D and the like out of it.
		d := a.desktopClick()
		debugf("desktop clicked, clears %v", d != nil)
		if d != nil {
			a.switchStage(d, nil, 0, false)
		}
		return
	}
	a.fg.h, a.fg.tries = h, 0
	if a.fg.timer != nil {
		a.fg.timer.Stop()
	}
	a.fg.timer = after(70*time.Millisecond, a.settleForeground)
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
	a.switchStage(d, []string{w.App}, w.HWND, false)
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
