package app

import (
	"slices"
	"time"

	"github.com/renkai9418/stage-bar/internal/catalog"
	"github.com/renkai9418/stage-bar/internal/winapi"
)

// switchStage makes next the apps on a display's stage. The windows
// leaving shrink into their cards while the arriving ones grow out of
// theirs; then the real windows take over.
func (a *App) switchStage(d *display, next []string, focus winapi.HWND, activate bool) {
	if a.busy || !a.managed(d) {
		return
	}
	a.windows = a.cat.Windows()
	groups := a.groupsOf(d)
	var leaving []string
	var departing, arriving []catalog.Window
	for _, g := range groups {
		in := slices.Contains(next, g.app)
		for _, w := range g.windows {
			switch {
			case in && a.hidden(w):
				arriving = append(arriving, w)
			case !in && !a.hidden(w) && a.settings.HideInactive:
				departing = append(departing, w)
				if !slices.Contains(leaving, g.app) {
					leaving = append(leaving, g.app)
				}
			}
		}
	}
	if activate && focus == 0 {
		for _, g := range groups {
			if len(next) > 0 && g.app == next[0] {
				focus = g.front().HWND
			}
		}
	}
	if len(departing)+len(arriving) == 0 {
		d.model.Switch(next, leaving, time.Now())
		if activate && focus != 0 {
			winapi.Activate(focus)
		}
		a.refresh()
		return
	}
	a.busy = true
	// Stills taken as the pointer came to the strip are fresh enough.
	var hs []winapi.HWND
	for _, w := range departing {
		if st := a.media.still(w.HWND); st == nil || time.Since(st.at) > 2*time.Second || a.media.pending[w.HWND] {
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
		d.model.Switch(next, leaving, time.Now())
		for _, app := range leaving {
			d.landing[app] = false
		}
		a.arrange(d, groups, time.Now())
		if d.strip != nil && len(departing) > 0 {
			// Hold a floating strip open so the windows visibly land.
			d.strip.holdOpen(1200 * time.Millisecond)
		}
		a.redraw()
		h := handover{a: a, d: d, departing: departing, arriving: arriving, focus: focus, activate: activate}
		a.flights.run(d, departing, arriving, from, h.takeOff, h.reveal, h.land, h.done)
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
	}
	if h.activate && h.focus != 0 && !winapi.IsIconic(h.focus) {
		h.early = true
		ok := winapi.Activate(h.focus)
		debugf("activate early %x ok=%v fg=%x", h.focus, ok, winapi.Foreground())
	}
}

// reveal brings in the icons of the cards the departing windows fly into,
// so that they are in place as the windows land.
func (h *handover) reveal() {
	for app := range h.d.landing {
		h.d.landing[app] = true
	}
	h.a.redraw()
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

// done ends the switch once the flights are gone.
func (h *handover) done() {
	clear(h.d.landing)
	h.a.busy = false
	h.a.refresh()
	// A foreground change that came in meanwhile is handled now.
	if h.a.fg.h != 0 && h.a.fg.timer == nil {
		h.a.fg.timer = after(10*time.Millisecond, h.a.settleForeground)
	}
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
	actClose
)

// request carries out what a card asks for once the frame is built. Tests
// set App.act to see the requests instead.
func (a *App) request(act action, d *display, g *group) {
	debugf("card request %d for %q", act, g.name)
	if a.act != nil {
		a.act(act, d, g)
		return
	}
	post(func() {
		switch act {
		case actPick:
			a.pick(d, g)
		case actJoin:
			a.join(d, g)
		case actClose:
			a.closeApp(g)
		}
	})
}

// pick puts the app of a card on the stage.
func (a *App) pick(d *display, g *group) {
	a.switchStage(d, []string{g.app}, g.front().HWND, true)
}

// join adds the app of a card to the stage, as dragging it there does.
func (a *App) join(d *display, g *group) {
	next := append(d.model.Stage(), g.app)
	a.switchStage(d, next, g.front().HWND, true)
}

// closeApp asks every window of a card's app to close.
func (a *App) closeApp(g *group) {
	for _, w := range g.windows {
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
	order := d.model.Strip(present)
	d.groups = d.groups[:0]
	for _, app := range order {
		d.groups = append(d.groups, by[app])
	}
	d.onStage = d.onStage[:0]
	for _, app := range d.model.Stage() {
		if g := by[app]; g != nil {
			d.onStage = append(d.onStage, g)
		}
	}
}
