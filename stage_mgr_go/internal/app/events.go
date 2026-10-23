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
		[2]uint32{winapi.EventObjectNameChange, winapi.EventObjectNameChange},
		[2]uint32{winapi.EventObjectCloaked, winapi.EventObjectUncloaked},
	)
	a.watchMoves(true)
}

// watchMoves turns the events of windows moving on or off. A drag sends
// one for every step, and while they keep coming the frames of the flight
// layers, painted only once nothing else waits, starve: during a drag the
// pointer poll follows the window instead.
func (a *App) watchMoves(on bool) {
	switch {
	case on && a.unhookMoves == nil:
		a.unhookMoves = winapi.AddWinEventHook(winapi.EventObjectLocationChange, winapi.EventObjectLocationChange)
	case !on && a.unhookMoves != nil:
		a.unhookMoves()
		a.unhookMoves = nil
	}
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
		a.moving.h, a.moving.from = h, winapi.WindowRect(h)
		a.moving.home = a.displayByHandle(winapi.MonitorFromRect(winapi.FrameBounds(h)))
		a.moving.at, a.moving.over, a.moving.stage = a.moving.home, nil, nil
		a.watchMoves(false)
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

// dwell is how long a window dragged onto another display rests there
// before the stage there makes way, so that one only passing over, on to
// a third display or back, does not set it flying.
const dwell = 150 * time.Millisecond

// dragOver makes way for a window dragged onto another display, as macOS
// does: the stage there flies into the strip once the window comes over,
// and back out should the window leave again before it is let go.
func (a *App) dragOver() {
	if !a.ready || a.moving.h == 0 {
		return
	}
	// By its visible frame, as displayOf goes: a switch must not take the
	// window for one of the stage it leaves, and park it under the pointer.
	// Not by its size: crossing to a display of another DPI resizes it.
	d := a.displayByHandle(winapi.MonitorFromRect(winapi.FrameBounds(a.moving.h)))
	if d != a.moving.at {
		a.moving.at, a.moving.since = d, time.Now()
	}
	if d == a.moving.over {
		return
	}
	if o := a.moving.over; o != nil {
		a.moving.over = nil
		a.switchStage(o, a.moving.stage, a.moving.h, false, false)
	}
	if d == nil || d == a.moving.home || !a.managed(d) || !a.settings.HideInactive {
		return
	}
	if wait := dwell - time.Since(a.moving.since); wait > 0 {
		if a.moving.dwell == nil {
			a.moving.dwell = after(wait, func() {
				a.moving.dwell = nil
				a.dragOver()
			})
		}
		return
	}
	i := slices.IndexFunc(a.windows, func(w catalog.Window) bool { return w.HWND == a.moving.h })
	if i < 0 {
		return
	}
	w := a.windows[i]
	stage := a.heading(d)
	if slices.Contains(stage, w.App) {
		return
	}
	a.moving.over, a.moving.stage = d, stage
	a.switchStage(d, []string{w.App}, a.moving.h, false, false)
}

// windowMoved puts the app of a window moved to another display on that
// display's stage: the user put it there to use it, so it must not be taken
// for a stray and parked. A window dragged into a strip takes its app off
// the stage instead.
func (a *App) windowMoved(h winapi.HWND) {
	from, home, moved := a.moving.from, a.moving.home, a.moving.h == h
	if moved {
		a.dragOver()
	}
	over, stage := a.moving.over, a.moving.stage
	if a.moving.dwell != nil {
		a.moving.dwell.Stop()
	}
	a.moving.h, a.moving.home, a.moving.at, a.moving.over, a.moving.stage, a.moving.dwell = 0, nil, nil, nil, nil, nil
	a.watchMoves(true)
	if a.settings.Mode == settings.Floating && a.coverTimer == nil {
		// Unseen during the drag.
		a.coverTimer = after(50*time.Millisecond, a.updateCovered)
	}
	if !a.ready || a.park.IsParked(h) {
		return
	}
	a.windows = a.cat.Windows()
	i := slices.IndexFunc(a.windows, func(w catalog.Window) bool { return w.HWND == h })
	if i < 0 {
		return
	}
	w := a.windows[i]
	if d := a.stripUnder(winapi.Cursor()); moved && d != nil {
		switch {
		case d != home && a.settings.HideInactive:
			if d != over {
				stage = a.heading(d)
			}
			a.leave(home, w)
			a.sendAcross(d, w, stage)
			return
		case d == home && slices.Contains(a.heading(d), w.App):
			// A resize that ends over the strip is no drag into it.
			if r := winapi.WindowRect(h); r.W() == from.W() && r.H() == from.H() {
				a.sendToStrip(d, w, from)
				return
			}
		}
	}
	d := a.displayOf(w)
	if d != home {
		a.leave(home, w)
	}
	// A stage that did not make way during the drag, as one whose windows
	// are never hidden or one let go of at once, takes the window now.
	if d != nil && a.managed(d) && !slices.Contains(a.heading(d), w.App) {
		a.switchStage(d, []string{w.App}, w.HWND, false, false)
	}
}

// leave lets display d go of the app of window w, just moved off it, at
// once rather than once its stage settles, unless a window of the app
// stays there: should the stage be left empty, the one before comes back.
func (a *App) leave(d *display, w catalog.Window) {
	if d == nil || !d.model.OnStage(w.App) || slices.ContainsFunc(a.windows, func(o catalog.Window) bool {
		return o.App == w.App && o.HWND != w.HWND && a.displayOf(o) == d
	}) {
		return
	}
	d.model.Leave(w.App)
	if d.model.Empty() {
		d.refill = true
	}
}

// followMoved puts the app of the window in front on the stage of its
// display, should it have moved there without a drag, as with
// Win+Shift+Arrow or by itself: it takes the stage as a dropped window
// does, rather than be parked for a stray once it loses the focus. It
// reports whether it started a switch.
func (a *App) followMoved() bool {
	if !a.settings.HideInactive || a.moving.h != 0 || a.fg.h != 0 || a.fg.timer != nil {
		return false
	}
	fg := catalog.RootOwner(winapi.Foreground())
	i := slices.IndexFunc(a.windows, func(w catalog.Window) bool { return w.HWND == fg })
	if i < 0 || a.park.IsParked(fg) {
		return false
	}
	w := a.windows[i]
	d := a.displayOf(w)
	if d == nil || !a.managed(d) || d.model.OnStage(w.App) || d.model.Settling(time.Now()) {
		return false
	}
	debugf("%q moved to %s", w.Title, d.mon.Device)
	a.switchStage(d, []string{w.App}, w.HWND, false, false)
	return true
}

// stripUnder returns the display whose strip shows under a point.
func (a *App) stripUnder(p winapi.Point) *display {
	d := a.displayByHandle(winapi.MonitorFromPoint(p))
	if d == nil || !a.managed(d) || d.strip == nil || !d.strip.shown || !d.strip.rect.Contains(p) {
		return nil
	}
	return d
}

// sendToStrip takes the app of a window dragged into a display's strip off
// the stage, into a card of its own; the others stay. The window goes back
// to where the drag began, for when its app returns.
func (a *App) sendToStrip(d *display, w catalog.Window, from winapi.Rect) {
	debugf("%q dragged into the strip", w.Title)
	next := slices.DeleteFunc(a.heading(d), func(app string) bool { return app == w.App })
	if a.settings.HideInactive {
		a.dropHomes[w.HWND] = from
	} else {
		// Nothing is parked: the window goes back at once.
		winapi.SetWindowPos(w.HWND, 0, from.Left, from.Top, 0, 0,
			winapi.SwpNoSize|winapi.SwpNoZOrder|winapi.SwpNoActivate)
	}
	a.switchStage(d, next, 0, true, false)
}

// sendAcross puts the app of a window dragged into the strip of another
// display into a card there, and the stage there back as it was before
// the window came over. The window moves onto that display, clear of the
// strip, for when its app comes out of the card.
func (a *App) sendAcross(d *display, w catalog.Window, stage []string) {
	debugf("%q dragged into the strip of %s", w.Title, d.mon.Device)
	f := winapi.FitInto(w.Frame, d.mon.Handle)
	winapi.SetWindowPos(w.HWND, 0, w.Rect.Left+f.Left-w.Frame.Left, w.Rect.Top+f.Top-w.Frame.Top, 0, 0,
		winapi.SwpNoSize|winapi.SwpNoZOrder|winapi.SwpNoActivate)
	next := slices.DeleteFunc(slices.Clone(stage), func(app string) bool { return app == w.App })
	a.switchStage(d, next, 0, false, false)
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
	if a.moving.h != 0 {
		a.dragOver()
	}
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
