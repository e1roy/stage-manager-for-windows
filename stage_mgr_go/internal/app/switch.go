package app

import (
	"fmt"
	"slices"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

// switchStage makes next the apps on a display's stage. The windows
// leaving shrink into their cards while the arriving ones grow out of
// theirs; then the real windows take over. A switch that follows the user
// elsewhere (followed), as the taskbar, Alt+Tab or a new app take them,
// has the system already showing the window they went to, often with an
// animation of its own: the leaving windows fade where they are, under
// it, rather than fly over it, and their cards slide into the strip.
func (a *App) switchStage(d *display, next []string, focus winapi.HWND, activate, followed bool) {
	a.start(&switchRequest{d: d, next: next, focus: focus, activate: activate, followed: followed})
}

// start carries out a switch request, or, while another switch runs, has it
// take over from that one.
func (a *App) start(r *switchRequest) {
	d, next, focus, activate, followed := r.d, r.next, r.focus, r.activate, r.followed
	if !a.managed(d) {
		return
	}
	if a.busy {
		// A click does not wait for the switch running to end: it takes
		// over, and the windows in the air turn where they are, as on macOS.
		a.queued[d] = r
		a.flights.interrupt(d)
		return
	}
	sw := newStopwatch()
	a.windows = a.cat.Windows()
	sw.lap("list windows")
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
	// The windows fly and land overlapping as they did on the desktop, and
	// the one activated comes in front of the others, as it does on landing.
	slices.SortStableFunc(departing, frontFirst)
	slices.SortStableFunc(arriving, func(x, y catalog.Window) int {
		switch {
		case x.HWND == y.HWND:
			return 0
		case activate && x.HWND == focus:
			return -1
		case activate && y.HWND == focus:
			return 1
		}
		return frontFirst(x, y)
	})
	if len(departing)+len(arriving) == 0 {
		r.commit(time.Now())
		if activate && focus != 0 {
			winapi.Activate(focus)
		}
		a.refresh()
		return
	}
	a.busy = true
	debugf("switch %s to %v: departing %v arriving %v focus %x activate=%v followed=%v",
		d.mon.Device, next, hwnds(departing), hwnds(arriving), focus, activate, followed)
	r.focus = focus
	a.running = r
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
	sw.lap("sort")
	sw.report("switch: starting")
	// Each leaving window gets a fresh still to fly into its card with.
	t0 := time.Now()
	a.media.capture(hs, false, 150*time.Millisecond, func() {
		debugf("switch: captured %d in %v", len(hs), time.Since(t0))
		sw := newStopwatch()
		defer sw.report("switch: setting the flights off")
		// The arriving windows grow out of the cards as they show now,
		// still turned and lifted by the pointer over them.
		from := a.flights.cardPoses(d, arriving, true)
		sw.lap("card poses")
		r.commit(time.Now())
		// Fading where they are, the windows land in no card: theirs
		// come in as new cards do.
		for _, app := range leaving {
			d.landing[app] = !followed
		}
		a.arrange(d, groups, time.Now())
		if len(leaving) > 0 {
			a.reveal(d, leaving[0])
		}
		if d.strip != nil && len(departing) > 0 {
			// Hold a floating strip open so the windows visibly land, and
			// a moment longer.
			d.strip.holdOpen(a.flightSpan() + 700*time.Millisecond)
		}
		a.redraw()
		sw.lap("arrange")
		// The windows fade under the one the user went to, unless that
		// one has yet to come in from its card itself. They fly as if
		// under one the user holds in a drag: the layer stays above all,
		// as one under a window being dragged gets few frames, and they
		// show nowhere over it.
		var below, hole winapi.HWND
		if focus != 0 && !a.park.IsParked(focus) {
			switch {
			case followed:
				below = focus
			case !activate:
				hole = focus
			}
		}
		front := below
		if hole != 0 {
			front = hole
		}
		h := handover{a: a, d: d, departing: departing, arriving: arriving, focus: focus, activate: activate, front: front}
		a.flights.run(d, departing, arriving, from, followed, below, hole, h.takeOff, h.land, h.stop, h.done)
		sw.lap("flights")
	})
}

func hwnds(ws []catalog.Window) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = fmt.Sprintf("%x", w.HWND)
	}
	return out
}

// handover swaps the real windows in under the flights.
type handover struct {
	a                   *App
	d                   *display
	departing, arriving []catalog.Window
	focus               winapi.HWND
	activate            bool
	early               bool
	front               winapi.HWND // the window the others stay under
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
			delete(h.a.dropHomes, w.HWND)
		}
	}
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
	var parked []winapi.HWND
	for _, w := range h.arriving {
		// Coming back from outside the desktop, a window is faded in by
		// DWM; the flight already showed it arrive.
		winapi.SetTransitionsDisabled(w.HWND, true)
		switch {
		case h.a.park.IsParked(w.HWND):
			parked = append(parked, w.HWND)
		case winapi.IsIconic(w.HWND) && !(h.activate && !h.early && w.HWND == h.focus):
			winapi.ShowNoActivate(w.HWND)
		}
	}
	// Together, so that they come home in the order they flew in, under
	// the window they stay under: they come home on top, and it goes back
	// above them in the same raise, not in one of its own that would race
	// with theirs.
	h.a.park.UnparkUnder(h.front, parked...)
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
	h.a.runQueued(h.d)
}

// done ends the switch once the flights are gone.
func (h *handover) done() {
	clear(h.d.landing)
	h.a.busy, h.a.running = false, nil
	h.a.ended(h.d)
}

// ended takes up, once a switch or the overview on display d is over, what
// waited for it: a switch asked for meanwhile, else a foreground change
// that came in.
func (a *App) ended(d *display) {
	a.refresh()
	if a.runQueued(d) {
		return
	}
	if a.fg.h != 0 && a.fg.timer == nil {
		a.fg.timer = after(10*time.Millisecond, a.settleForeground)
	}
}

// switchRequest is a switch asked for while another ran, one per display.
type switchRequest struct {
	d        *display
	next     []string
	focus    winapi.HWND
	activate bool
	followed bool
	// split is the app whose set breaks into sets of one app each as the
	// stage changes (App.split), or "".
	split string
}

// commit makes the model of the request's display what the switch asks for.
// A set of the stage breaks up only now, together with the switch that takes
// some of its apps off, so that no switch dropped or taken over leaves the
// stage in pieces, each a card of the stage.
func (r *switchRequest) commit(now time.Time) {
	if r.split != "" {
		r.d.model.Split(r.split)
	}
	r.d.model.Switch(r.next, now)
}

// runQueued starts a switch asked for while one ran, if any: first that of
// display first, whose windows may hold in the air for it.
func (a *App) runQueued(first *display) bool {
	q := a.queued[first]
	for i := 0; q == nil && i < len(a.displays); i++ {
		q = a.queued[a.displays[i]]
	}
	if q == nil {
		// Those of displays since gone.
		clear(a.queued)
		return false
	}
	delete(a.queued, q.d)
	a.start(q)
	return true
}

// heading is the stage a display is on its way to: that of the switch
// asked for meanwhile, else that of the switch running, which may not
// have changed the model yet.
func (a *App) heading(d *display) []string {
	for _, r := range []*switchRequest{a.queued[d], a.running} {
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
	// The strip that lists the stage's own card shows the new stills in it.
	var shown func()
	if a.settings.AllStages {
		shown = a.redraw
	}
	a.media.capture(hs, false, time.Second, shown)
}

// What a card asks for.
type action int

const (
	actPick action = iota
	actJoin
	actSplit
	actClose
	actOverview // asked for by the strip's button rather than a card
	actMove     // the card was dropped in the strip of another display
	actReorder  // the card was dragged up or down its strip
	// A window of the list of the stage's windows (lineup.go), as a set
	// of its own, asks for these.
	actFront   // to come in front of the others on the stage
	actUnstage // for its app to leave the stage for the strip
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
			a.split(d, s)
		case actClose:
			a.closeApps(s)
		case actFront:
			a.bringForward(s.front())
		case actUnstage:
			a.unstage(d, s)
		}
	})
}

// pick puts the apps of a card on the stage, in place of those there.
func (a *App) pick(d *display, s *set) {
	a.switchStage(d, s.names(), s.front().HWND, true, false)
}

// requestOverview opens the overview of a display once the frame is
// built. Tests set App.act to see the request instead, with no set.
func (a *App) requestOverview(d *display) {
	if a.act != nil {
		a.act(actOverview, d, nil)
		return
	}
	post(func() { a.openOverview(d, time.Now()) })
}

// join adds the apps of a card to the stage, as dragging it there or
// Shift-clicking it does, to the stage as it will be once the switch
// running is done.
func (a *App) join(d *display, s *set) {
	next := append(a.heading(d), s.names()...)
	a.switchStage(d, next, s.front().HWND, true, false)
}

// closeApps asks every window of a card's apps to close.
func (a *App) closeApps(s *set) {
	for _, w := range s.windows {
		winapi.Close(w.HWND)
	}
	a.refreshSoon()
}

// split breaks a card into cards of one app each. The stage's own card, in a
// strip that lists every stage (settings.AllStages), keeps its front app on
// the stage, and the others leave it for cards of their own: they leave the
// stage as it will be once the switch running is done, as join and unstage
// see it, so that an app joining it meanwhile still does, and the set breaks
// up as they go (switchRequest.commit).
func (a *App) split(d *display, s *set) {
	if !s.staged {
		d.model.Split(s.key)
		a.refresh()
		return
	}
	front := slices.MinFunc(s.apps, func(x, y *group) int { return x.z - y.z })
	next := slices.DeleteFunc(a.heading(d), func(app string) bool {
		return app != front.app && slices.ContainsFunc(s.apps, func(g *group) bool { return g.app == app })
	})
	a.start(&switchRequest{d: d, next: next, activate: len(next) > 0, split: s.key})
}

// reveal scrolls the strip of display d, should its cards run past it, to
// show the card of app. A strip that lists every stage (settings.AllStages)
// sends the windows leaving the stage back to their own slots rather than to
// the one the card coming in leaves, and that may be out of sight: the strip
// scrolls to it as it does by hand, for the windows to fly into.
func (a *App) reveal(d *display, app string) {
	s := d.strip
	if s == nil || d.viewW == 0 || !a.settings.AllStages {
		return
	}
	i := slices.IndexFunc(d.sets, func(g *set) bool {
		return slices.ContainsFunc(g.apps, func(x *group) bool { return x.app == app })
	})
	if i < 0 {
		return
	}
	head := s.lineupFor(d.viewW, d.viewH).extent
	l := layoutStrip(len(d.sets), d.viewW, d.viewH, d.scroll, float32(a.settings.Spacing), a.panel(), head, float32(a.settings.CardsAt), s.right())
	d.scroll = l.scrollTo(i, d.scroll, d.viewH, a.panel(), head)
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
	d.model.Keep(a.settings.AllStages)
	d.sets = d.sets[:0]
	for _, names := range d.model.Strip(present) {
		apps := make([]*group, len(names))
		for i, app := range names {
			apps[i] = by[app]
		}
		s := newSet(apps)
		// A strip that lists every stage has the stage's own set in it.
		s.staged = d.model.OnStage(names[0])
		d.sets = append(d.sets, s)
	}
	d.onStage = d.onStage[:0]
	for _, app := range d.model.Stage() {
		if g := by[app]; g != nil {
			d.onStage = append(d.onStage, g)
		}
	}
}
