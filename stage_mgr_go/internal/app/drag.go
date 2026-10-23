package app

import (
	"slices"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

// dwell is how long a window dragged onto another display rests there
// before the stage there makes way, so that one only passing over, on to
// a third display or back, does not set it flying.
const dwell = 150 * time.Millisecond

// drag is a window the user is moving, from the start of the move to its
// end.
type drag struct {
	h    winapi.HWND
	from winapi.Rect // where it was as the move began
	home *display    // the display it was on then
	// at is the display it is over, since since; over the one whose stage
	// made way for it, and stage what was on that stage.
	at, over *display
	since    time.Time
	stage    []string
	dwell    *time.Timer
}

// startDrag follows window h, which the user began to move.
func (a *App) startDrag(h winapi.HWND) {
	if a.drag != nil && a.drag.dwell != nil {
		a.drag.dwell.Stop()
	}
	home := a.displayByHandle(winapi.MonitorFromRect(winapi.FrameBounds(h)))
	a.drag = &drag{h: h, from: winapi.WindowRect(h), home: home, at: home}
}

// dragOver makes way for a window dragged onto another display, as macOS
// does: the stage there flies into the strip once the window comes over,
// and back out should the window leave again before it is let go.
func (a *App) dragOver() {
	g := a.drag
	if !a.ready || g == nil {
		return
	}
	// By its visible frame, as displayOf goes: a switch must not take the
	// window for one of the stage it leaves, and park it under the pointer.
	// Not by its size: crossing to a display of another DPI resizes it.
	d := a.displayByHandle(winapi.MonitorFromRect(winapi.FrameBounds(g.h)))
	if d != g.at {
		g.at, g.since = d, time.Now()
	}
	if d == g.over {
		return
	}
	if o := g.over; o != nil {
		g.over = nil
		a.switchStage(o, g.stage, g.h, false, false)
	}
	if d == nil || d == g.home || !a.managed(d) || !a.settings.HideInactive {
		return
	}
	if wait := dwell - time.Since(g.since); wait > 0 {
		if g.dwell == nil {
			g.dwell = after(wait, func() {
				g.dwell = nil
				if a.drag == g {
					a.dragOver()
				}
			})
		}
		return
	}
	i := slices.IndexFunc(a.windows, func(w catalog.Window) bool { return w.HWND == g.h })
	if i < 0 {
		return
	}
	w := a.windows[i]
	stage := a.heading(d)
	if slices.Contains(stage, w.App) {
		return
	}
	g.over, g.stage = d, stage
	a.switchStage(d, []string{w.App}, g.h, false, false)
}

// windowMoved puts the app of a window moved to another display on that
// display's stage: the user put it there to use it, so it must not be taken
// for a stray and parked. A window dragged into a strip takes its app off
// the stage instead.
func (a *App) windowMoved(h winapi.HWND) {
	g := a.drag
	moved := g != nil && g.h == h
	if moved {
		// Where it was let go.
		a.dragOver()
	}
	if g != nil && g.dwell != nil {
		g.dwell.Stop()
	}
	a.drag = nil
	if !moved {
		// A move whose start went unseen.
		g = &drag{}
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
		case d != g.home && a.settings.HideInactive:
			stage := g.stage
			if d != g.over {
				stage = a.heading(d)
			}
			a.leave(g.home, w)
			a.sendAcross(d, w, stage)
			return
		case d == g.home && slices.Contains(a.heading(d), w.App):
			// A resize that ends over the strip is no drag into it.
			if r := winapi.WindowRect(h); r.W() == g.from.W() && r.H() == g.from.H() {
				a.sendToStrip(d, w, g.from)
				return
			}
		}
	}
	d := a.displayOf(w)
	if d != g.home {
		a.leave(g.home, w)
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
	if !a.settings.HideInactive || a.drag != nil || a.fg.h != 0 || a.fg.timer != nil {
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
