package app

import (
	"slices"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

// switchStage makes next the apps on a display's stage. The windows
// leaving shrink into their cards while the arriving ones grow out of
// theirs; then the real windows take over.
func (a *App) switchStage(d *display, next []string, focus winapi.HWND, activate bool) {
	if !a.managed(d) {
		return
	}
	if a.busy {
		// A click does not wait for the switch running to end: it takes
		// over, and the windows in the air turn where they are, as on macOS.
		a.queued = &switchRequest{d: d, next: next, focus: focus, activate: activate}
		a.flights.interrupt(d)
		return
	}
	a.windows = a.cat.Windows()
	groups := a.groupsOf(d)
	// The windows of a switch that was taken over are parked, and show on
	// the flight layer on their way.
	air := a.flights.airborne(d)
	var leaving []string
	var departing, arriving []catalog.Window
	for _, g := range groups {
		in := slices.Contains(next, g.app)
		stays := in || !a.settings.HideInactive
		for _, w := range g.windows {
			_, aloft := air[w.HWND]
			switch {
			case a.hidden(w) && (in || aloft && stays):
				arriving = append(arriving, w)
			case !stays && (aloft || !a.hidden(w)):
				departing = append(departing, w)
				if !slices.Contains(leaving, g.app) {
					leaving = append(leaving, g.app)
				}
			}
		}
	}
	if activate && focus == 0 {
		// The front-most window of the apps coming in.
		for _, g := range groups {
			if slices.Contains(next, g.app) {
				focus = g.front().HWND
				break
			}
		}
	}
	if len(departing)+len(arriving) == 0 {
		d.model.Switch(next, time.Now())
		if activate && focus != 0 {
			winapi.Activate(focus)
		}
		a.refresh()
		return
	}
	a.busy = true
	a.running = &switchRequest{d: d, next: next, focus: focus, activate: activate}
	// Stills taken as the pointer came to the strip are fresh enough, and
	// the windows in the air have theirs; the rest make do with what they
	// have rather than hold the flights still, unless they have no still.
	var hs []winapi.HWND
	for _, w := range departing {
		if _, aloft := air[w.HWND]; aloft {
			continue
		}
		if st := a.media.still(w.HWND); st == nil || len(air) == 0 && (time.Since(st.at) > 2*time.Second || a.media.pending[w.HWND]) {
			hs = append(hs, w.HWND)
		}
	}
	// Each leaving window gets a fresh still to fly into its card with.
	t0 := time.Now()
	a.media.capture(hs, false, 150*time.Millisecond, func() {
		debugf("switch: captured %d in %v", len(hs), time.Since(t0))
		// The arriving windows grow out of the cards as they show now,
		// still turned and lifted by the pointer over them.
		from := a.flights.cardPoses(d, arriving, true)
		d.model.Switch(next, time.Now())
		for _, app := range leaving {
			d.landing[app] = true
		}
		a.arrange(d, groups, time.Now())
		if d.strip != nil && len(departing) > 0 {
			// Hold a floating strip open so the windows visibly land.
			d.strip.holdOpen(1200 * time.Millisecond)
		}
		a.redraw()
		h := handover{a: a, d: d, departing: departing, arriving: arriving, focus: focus, activate: activate}
		a.flights.run(d, departing, arriving, from, h.takeOff, h.land, h.stop, h.done)
	})
}

// handover swaps the real windows in under the flights.
type handover struct {
	a                   *App
	d                   *display
	departing, arriving []catalog.Window
	focus               winapi.HWND
	activate            bool
	early               bool
}

// takeOff runs once the flights cover the windows: the leaving windows go,
// and, as Windows does, the arriving window is activated before it flies
// rather than after it lands, so its title bar does not change color on
// arrival.
func (h *handover) takeOff() {
	for _, w := range h.departing {
		h.a.park.Park(w.HWND)
		// A window dragged into the strip comes back where the drag began.
		if r, ok := h.a.dropHomes[w.HWND]; ok {
			h.a.park.Rehome(w.HWND, r)
		}
	}
	clear(h.a.dropHomes)
	if h.activate && h.focus != 0 && !winapi.IsIconic(h.focus) {
		h.early = true
		ok := winapi.Activate(h.focus)
		debugf("activate early %x ok=%v fg=%x", h.focus, ok, winapi.Foreground())
	}
}

// land brings the arriving windows home under the flights, and shows the
// cards the departing ones flew into under theirs.
func (h *handover) land() {
	clear(h.d.landing)
	h.a.redraw()
	for _, w := range h.arriving {
		// Coming back from outside the desktop, a window is faded in by
		// DWM; the flight already showed it arrive.
		winapi.SetTransitionsDisabled(w.HWND, true)
		switch {
		case h.a.park.IsParked(w.HWND):
			h.a.park.Unpark(w.HWND)
		case winapi.IsIconic(w.HWND) && !(h.activate && !h.early && w.HWND == h.focus):
			winapi.ShowNoActivate(w.HWND)
		}
	}
	if h.activate && !h.early && h.focus != 0 {
		ok := winapi.Activate(h.focus)
		debugf("activate late %x ok=%v fg=%x", h.focus, ok, winapi.Foreground())
	}
	arrived := h.arriving
	after(700*time.Millisecond, func() {
		for _, w := range arrived {
			if winapi.IsWindow(w.HWND) {
				winapi.SetTransitionsDisabled(w.HWND, false)
			}
		}
	})
}

// stop hands the switch over to the one asked for meanwhile, mid-flight:
// the windows stay parked, for it to fly on.
func (h *handover) stop() {
	clear(h.d.landing)
	h.a.busy, h.a.running = false, nil
	h.a.runQueued()
}

// done ends the switch once the flights are gone.
func (h *handover) done() {
	clear(h.d.landing)
	h.a.busy, h.a.running = false, nil
	h.a.refresh()
	if h.a.runQueued() {
		return
	}
	// A foreground change that came in meanwhile is handled now.
	if h.a.fg.h != 0 && h.a.fg.timer == nil {
		h.a.fg.timer = after(10*time.Millisecond, h.a.settleForeground)
	}
}

// switchRequest is a switch asked for while another ran.
type switchRequest struct {
	d        *display
	next     []string
	focus    winapi.HWND
	activate bool
}

// runQueued starts the switch asked for while one ran, if any.
func (a *App) runQueued() bool {
	q := a.queued
	if q == nil {
		return false
	}
	a.queued = nil
	a.switchStage(q.d, q.next, q.focus, q.activate)
	return true
}

// heading is the stage a display is on its way to: that of the switch
// asked for meanwhile, else that of the switch running, which may not
// have changed the model yet.
func (a *App) heading(d *display) []string {
	for _, r := range []*switchRequest{a.queued, a.running} {
		if r != nil && r.d == d {
			return slices.Clone(r.next)
		}
	}
	return d.model.Stage()
}

// freshenStage takes new stills of the windows on a display's stage.
func (a *App) freshenStage(d *display) {
	var hs []winapi.HWND
	for _, g := range d.onStage {
		for _, w := range g.windows {
			if a.hidden(w) || a.media.pending[w.HWND] {
				continue
			}
			if st := a.media.still(w.HWND); st == nil || time.Since(st.at) > time.Second {
				hs = append(hs, w.HWND)
			}
		}
	}
	a.media.capture(hs, false, time.Second, nil)
}

// What a card asks for.
type action int

const (
	actPick action = iota
	actJoin
	actSplit
	actClose
)

// request carries out what a card asks for once the frame is built. Tests
// set App.act to see the requests instead.
func (a *App) request(act action, d *display, s *set) {
	debugf("card request %d for %q", act, s.title())
	if a.act != nil {
		a.act(act, d, s)
		return
	}
	post(func() {
		switch act {
		case actPick:
			a.pick(d, s)
		case actJoin:
			a.join(d, s)
		case actSplit:
			d.model.Split(s.key)
			a.refresh()
		case actClose:
			a.closeApps(s)
		}
	})
}

// pick puts the apps of a card on the stage, in place of those there.
func (a *App) pick(d *display, s *set) {
	a.switchStage(d, s.names(), s.front().HWND, true)
}

// join adds the apps of a card to the stage, as dragging it there or
// Shift-clicking it does, to the stage as it will be once the switch
// running is done.
func (a *App) join(d *display, s *set) {
	next := append(a.heading(d), s.names()...)
	a.switchStage(d, next, s.front().HWND, true)
}

// closeApps asks every window of a card's apps to close.
func (a *App) closeApps(s *set) {
	for _, w := range s.windows {
		winapi.Close(w.HWND)
	}
	a.refreshSoon()
}

// arrange brings a display's strip and stage lists up to date with the
// model, for windows already listed.
func (a *App) arrange(d *display, groups []*group, now time.Time) {
	var present []string
	by := map[string]*group{}
	for _, g := range groups {
		present = append(present, g.app)
		by[g.app] = g
	}
	d.sets = d.sets[:0]
	for _, names := range d.model.Strip(present) {
		apps := make([]*group, len(names))
		for i, app := range names {
			apps[i] = by[app]
		}
		d.sets = append(d.sets, newSet(apps))
	}
	d.onStage = d.onStage[:0]
	for _, app := range d.model.Stage() {
		if g := by[app]; g != nil {
			d.onStage = append(d.onStage, g)
		}
	}
}
