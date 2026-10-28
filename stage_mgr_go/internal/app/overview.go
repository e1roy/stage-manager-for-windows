package app

import (
	"slices"
	"strconv"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	// The overview opens and closes about as briskly as Mission Control.
	overviewTime = 420 * time.Millisecond
	// How dark the desktop goes behind the windows spread out.
	overviewDim = 0.45
	// The close button in the top right corner of the window under the
	// pointer: at least this tall, and this at most, in DIPs, else in
	// proportion to the window so that it reads on any size of it.
	overviewCloseSize = 40
	overviewCloseMax  = 76
	// The windows spread over the work area this far in from its edges,
	// further at the bottom, for the icons and titles under the last row,
	// and this far apart, in DIPs. The gap between rows has room for the
	// icons and, under the pointer, a window's title.
	overviewMargin = 44
	overviewBottom = 72
	overviewGapX   = 36
	overviewGapY   = 60
	// How long the overview waits for frames it does not get before it
	// carries on: the windows it parked must not stay away.
	overviewTimeout = 1500 * time.Millisecond
	// How long a switch running may hold up the overview asked for meanwhile.
	overviewWait = 2 * time.Second
)

var (
	// overviewShade is the shadow of a window spread out, in DIPs.
	overviewShade = shade{ambient: 0.12, ambientBlur: 14, key: 0.36, keyBlur: 30, keyY: 12, keyInset: 4}
	// ringColor outlines the window under the pointer, as macOS does.
	ringColor = ui.RGBA(10, 132, 255, 0.95)
	// closeRed is the red of a Windows title bar's close button, which the
	// overview's takes under the pointer; at rest the block is dark and
	// translucent, so that it does not draw the eye from the windows.
	closeRed  = ui.RGB(216, 42, 32)
	closeRest = ui.RGBA(28, 28, 30, 0.55)
)

// accent is the color the overview's buttons take under the pointer: the
// user's, or the blue of the ring. outlined reports whether it outlines
// them, which the user may turn off for the borders they had.
func (a *App) accent() (c ui.Color, outlined bool) {
	if a.settings.Accent == settings.AccentNone {
		return ringColor, false
	}
	if len(a.settings.Accent) == 7 {
		if n, err := strconv.ParseUint(a.settings.Accent[1:], 16, 32); err == nil {
			return ui.RGBA(uint8(n>>16), uint8(n>>8), uint8(n), 1), true
		}
	}
	return ringColor, true
}

type overviewPhase int

const (
	ovHidden  overviewPhase = iota
	ovOpening               // flying out of the stage and the cards
	ovOpen                  // spread out, waiting for the user
	ovClosing               // flying home, or into the cards
	ovLanding               // landed: the real windows and the cards take over
)

// ovTile is a window in the overview and the course it is on, in pixels of
// the screen.
type ovTile struct {
	w     catalog.Window
	name  string // its app's
	still *ui.Bitmap
	icon  *ui.Bitmap
	// origin is where the window showed as the overview opened: on the
	// desktop, where the overview parks it for as long as it shows (parks),
	// or in its card.
	origin   pose
	originOp float32
	originSh shade
	parks    bool
	slot     pose // where it shows spread out
	// The move it is on.
	from, to     pose
	fromOp, toOp float32
	fromSh, toSh shade
	home         bool // it closes onto the desktop rather than into a card
	// closing is set once its close button asked the window to close: the
	// tile fades out and takes the pointer no more.
	closing bool
}

// at is where t shows e of the way, eased, how opaque and with what shadow.
func (t *ovTile) at(e float64) (pose, float32, shade) {
	return lerpPose(t.from, t.to, e), t.fromOp + (t.toOp-t.fromOp)*float32(e), lerpShade(t.fromSh, t.toSh, float32(e))
}

// overview spreads out every window of one display at once, side by side
// over the dimmed desktop, as Mission Control does on macOS: the windows on
// the stage and those of the strip's cards fly out to their places. A click
// on one puts its app on the stage, as a click on its card does, and every
// window flies to where that leaves it; a click elsewhere, Escape, or going
// to another window sends them all back. It runs as a switch does, the app
// busy meanwhile, and parks the windows it takes off the desktop, so that
// they come back should it end unexpectedly.
type overview struct {
	a      *App
	win    *mygo.Window
	hwnd   winapi.HWND
	d      *display // the display it shows, until it hides
	groups []*group // that display's apps as it opened
	rect   winapi.Rect
	scale  float64
	tiles  []ovTile // front first; drawn the other way round
	phase  overviewPhase
	held   bool      // its first frame, every window where it was, was drawn
	flying bool      // the windows it covers are parked, and it moves
	landed bool      // the windows were handed back
	start  time.Time // when the move began: its first frame, zero until drawn
	// spread is how far it is out, from 0 as it opens to 1 spread out: how
	// dark the desktop is and how much the icons show.
	spreadFrom, spreadTo float32
	badges               []badge     // the icons of the cards windows close into
	prev                 winapi.HWND // the window in front as it opened
	focus                winapi.HWND // the window activated as it closes
	gen                  int         // counts its courses, for timers to tell theirs
}

func newOverview(a *App) *overview {
	o := &overview{a: a}
	// Focusable, it takes the keyboard for Escape, and closes as another
	// window is activated.
	o.win = mygo.NewFlyout(mygo.FlyoutOptions{Tray: a.tray, Hidden: true, Focusable: true, Width: 400, Height: 400, Content: ui.View(o.view)})
	o.hwnd = winapi.HWND(o.win.NativeHandle())
	o.win.OnClose(func(e *mygo.CloseEvent) {
		if a.quitting {
			return
		}
		e.PreventDefault()
		post(func() { o.close(nil) })
	})
	winapi.SetTransitionsDisabled(o.hwnd, true)
	return o
}

// openOverview spreads out the windows of display d, asked for at asked.
func (a *App) openOverview(d *display, asked time.Time) {
	if a.quitting || !a.ready || !a.managed(d) || !slices.Contains(a.displays, d) || d.strip == nil {
		return
	}
	if a.ov != nil && a.ov.phase != ovHidden {
		return
	}
	if a.busy {
		// A switch is on its way: the overview waits for it a moment.
		if time.Since(asked) < overviewWait {
			after(100*time.Millisecond, func() { a.openOverview(d, asked) })
		}
		return
	}
	a.windows = a.cat.Windows()
	groups := a.groupsOf(d)
	a.arrange(d, groups, time.Now())
	var stage []catalog.Window
	for _, g := range d.onStage {
		stage = append(stage, g.windows...)
	}
	slices.SortStableFunc(stage, frontFirst)
	ws := stage
	for _, s := range d.sets {
		ws = append(ws, s.windows...)
	}
	if len(ws) == 0 {
		debugf("overview %s: no windows", d.mon.Device)
		return
	}
	if a.ov == nil {
		a.ov = newOverview(a)
	}
	o := a.ov
	a.busy = true
	o.gen++
	o.d, o.groups, o.phase, o.tiles = d, groups, ovOpening, nil
	o.held, o.flying, o.landed = false, false, false
	// The windows on the desktop fly out from where they are: they get
	// fresh stills, at the size they show.
	var shoot []winapi.HWND
	for _, w := range ws {
		a.media.icon(w)
		st := a.media.still(w.HWND)
		if !a.hidden(w) && !a.media.pending[w.HWND] && (st == nil || time.Since(st.at) > time.Second) {
			shoot = append(shoot, w.HWND)
		}
	}
	gen := o.gen
	a.media.capture(shoot, true, 250*time.Millisecond, func() {
		if o.gen == gen && o.d == d {
			o.begin(ws)
		}
	})
}

// begin shows the overview, every window where it is now, for it to take
// off from.
func (o *overview) begin(ws []catalog.Window) {
	a, d := o.a, o.d
	if a.quitting || !slices.Contains(a.displays, d) || d.strip == nil {
		o.phase, o.d = ovHidden, nil
		a.busy = false
		return
	}
	mon := winapi.MonitorInfo(d.mon.Handle)
	o.scale = d.mon.Scale()
	// Over the work area and the strip, which a fixed one keeps out of it;
	// the taskbar stays, as the Dock does.
	area := mon.Work
	if s := d.strip; s.visible {
		area = winapi.Rect{
			Left: min(area.Left, s.rect.Left), Top: min(area.Top, s.rect.Top),
			Right: max(area.Right, s.rect.Right), Bottom: max(area.Bottom, s.rect.Bottom),
		}
	}
	if area == mon.Monitor {
		// One pixel short of the display, so that Windows never takes it
		// for an app in full screen.
		area.Bottom--
	}
	o.rect = area
	fg := winapi.Foreground()
	o.prev, o.focus = catalog.RootOwner(fg), 0
	front := map[string]winapi.HWND{}
	for _, s := range d.sets {
		for _, g := range s.apps {
			front[g.app] = s.front().HWND
		}
	}
	cards := a.flights.cardPoses(d, ws, true)
	k := float32(o.scale)
	items := make([]overviewItem, len(ws))
	o.tiles = make([]ovTile, len(ws))
	for i, w := range ws {
		t := ovTile{w: w, name: a.cat.Name(w), icon: a.media.icons[w.App], originOp: 1}
		if st := a.media.still(w.HWND); st != nil {
			t.still = st.fly
			if t.still == nil {
				t.still = st.card
			}
		}
		if !a.hidden(w) {
			t.origin, t.parks = poseOf(w.Frame), true
			t.originSh = ownShade(castsShadow(w.HWND), w.HWND == fg && winapi.HasActiveFrame(w.HWND))
		} else if p, ok := cards[w.HWND]; ok {
			t.origin, t.originSh = p, cardShade(float32(p.h/o.scale))
		} else {
			// Behind the three a card shows, it comes out of the card's
			// front window, fading in.
			if p, ok := cards[front[w.App]]; ok {
				t.origin = p
			} else {
				t.origin = a.flights.collapse(d, poseOf(a.home(w)))
			}
			t.originOp = 0
		}
		home := a.home(w)
		x, y, pw, ph := o.local(t.origin)
		items[i] = overviewItem{w: float32(home.W()) / k, h: float32(home.H()) / k, cx: x + pw/2, cy: y + ph/2}
		o.tiles[i] = t
	}
	work := mon.Work
	slots := layoutOverview(ui.Rect{
		X: float32(work.Left-o.rect.Left)/k + overviewMargin,
		Y: float32(work.Top-o.rect.Top)/k + overviewMargin,
		W: float32(work.W())/k - 2*overviewMargin,
		H: float32(work.H())/k - overviewMargin - overviewBottom,
	}, items, overviewGapX, overviewGapY)
	for i := range o.tiles {
		t := &o.tiles[i]
		t.slot = o.screen(slots[i])
		t.from, t.fromOp, t.fromSh = t.origin, t.originOp, t.originSh
		t.to, t.toOp, t.toSh = t.slot, 1, overviewShade
	}
	o.phase, o.start = ovOpening, time.Time{}
	o.spreadFrom, o.spreadTo, o.badges = 0, 1, nil
	debugf("overview %s: %d windows over %v", d.mon.Device, len(o.tiles), area)
	place := func() {
		winapi.SetWindowPos(o.hwnd, winapi.HwndTopmost, area.Left, area.Top, area.W(), area.H(), winapi.SwpNoActivate)
	}
	// Placed again once shown: moving onto a display of another scale,
	// the window is resized for it.
	place()
	o.win.Show()
	place()
	if winapi.Foreground() != o.hwnd {
		winapi.Activate(o.hwnd)
	}
	o.win.Invalidate()
	gen := o.gen
	after(overviewTimeout, func() {
		// It never drew: rather than leave an invisible window over the
		// desktop, it gives up.
		if o.gen == gen && !o.held {
			debugf("overview: timed out showing")
			o.abort()
		}
	})
}

// takeOff parks the windows the overview covers once it shows them where
// they are, blanks the strip's cards, which it shows too, and sets off.
func (o *overview) takeOff() {
	a, d := o.a, o.d
	if o.flying || d == nil || a.quitting {
		return
	}
	o.flying = true
	for _, t := range o.tiles {
		if t.parks {
			a.park.Park(t.w.HWND)
		}
	}
	d.inOverview = true
	a.redraw()
	o.win.Invalidate()
}

// eased is how far, eased, the overview is through its move at now.
func (o *overview) eased(now time.Time) float64 {
	switch {
	case o.phase == ovOpen || o.phase == ovLanding:
		return 1
	case !o.flying || o.start.IsZero():
		return 0
	}
	return flightEase(min(float64(now.Sub(o.start))/float64(overviewTime), 1))
}

func (o *overview) spread(e float64) float32 {
	return o.spreadFrom + (o.spreadTo-o.spreadFrom)*float32(e)
}

// close sends the windows back. With pick, its app comes onto the stage
// in place of those there, as a click on its card puts it, and each window
// flies to where that leaves it: onto the desktop, or into its card. With
// none, everything goes back as it was.
func (o *overview) close(pick *ovTile) {
	a, d := o.a, o.d
	if d == nil || o.phase == ovHidden || o.phase >= ovClosing {
		return
	}
	if !o.flying {
		// Nothing took off yet.
		o.abort()
		return
	}
	now := time.Now()
	e := o.eased(now)
	for i := range o.tiles {
		t := &o.tiles[i]
		t.from, t.fromOp, t.fromSh = t.at(e)
	}
	o.spreadFrom, o.spreadTo = o.spread(e), 0
	o.focus = 0
	if pick != nil {
		o.focus = pick.w.HWND
		if !d.model.OnStage(pick.w.App) {
			d.model.Switch(d.model.SetOf(pick.w.App), now)
			a.arrange(d, o.groups, now)
			// The cards take their new places now, blank, so that those
			// the windows fly into are there as they land, rather than
			// slide in after.
			a.redraw()
		}
	} else if fg := winapi.Foreground(); fg == o.hwnd || fg == 0 {
		// Back to where the user was, unless they went elsewhere meanwhile.
		o.focus = o.prev
	}
	debugf("overview %s closes, pick %v, stage %v", d.mon.Device, pick != nil, d.model.Stage())
	var into []catalog.Window
	for i := range o.tiles {
		t := &o.tiles[i]
		if t.closing && winapi.IsWindow(t.w.HWND) {
			// It did not close, or not yet (it may be asking to save):
			// it goes back as the others do.
			t.closing = false
		}
		if t.closing {
			t.home = false
			continue
		}
		t.home = !a.settings.HideInactive || d.model.OnStage(t.w.App)
		if !t.home {
			into = append(into, t.w)
		}
	}
	if len(into) > 0 && d.strip != nil {
		// A floating strip comes out for the windows to land in.
		d.strip.holdOpen(1200 * time.Millisecond)
	}
	cards := a.flights.cardPoses(d, into, false)
	front := map[string]winapi.HWND{}
	for _, s := range d.sets {
		for _, g := range s.apps {
			front[g.app] = s.front().HWND
		}
	}
	for i := range o.tiles {
		t := &o.tiles[i]
		if t.closing {
			// Gone: it stays out of sight where it was.
			t.to, t.toOp, t.toSh = t.from, 0, t.fromSh
			continue
		}
		t.toOp = 1
		if t.home {
			t.to, t.toSh = poseOf(a.home(t.w)), ownShade(castsShadow(t.w.HWND), t.w.HWND == o.focus)
			continue
		}
		p, ok := cards[t.w.HWND]
		if !ok {
			// No slot of its own in its card: it shrinks into the card's
			// front window, fading.
			p, ok = cards[front[t.w.App]]
			t.toOp = 0
		}
		if !ok {
			p = a.flights.collapse(d, t.from)
		}
		t.to, t.toSh = p, cardShade(float32(p.h/o.scale))
	}
	o.badges = a.flights.landingIcons(d)
	o.phase, o.start = ovClosing, time.Time{}
	o.gen++
	o.win.Invalidate()
	gen := o.gen
	after(overviewTime+overviewTimeout, func() {
		if o.gen == gen && o.phase == ovClosing {
			debugf("overview: timed out closing")
			o.phase = ovLanding
			o.land()
		}
	})
}

// land brings home the windows that close onto the desktop, under their
// tiles, the one picked in front, and lets the cards show again under
// theirs; the overview hides a moment later, once those are drawn.
func (o *overview) land() {
	a, d := o.a, o.d
	if d == nil || o.landed || a.quitting {
		return
	}
	o.landed = true
	order := slices.Clone(o.tiles)
	slices.SortStableFunc(order, func(x, y ovTile) int {
		switch {
		case x.w.HWND == o.focus:
			return -1
		case y.w.HWND == o.focus:
			return 1
		}
		return frontFirst(x.w, y.w)
	})
	var back []winapi.HWND
	for _, t := range order {
		if t.home && a.park.IsParked(t.w.HWND) {
			// The overview showed it arrive; DWM would fade it in again.
			winapi.SetTransitionsDisabled(t.w.HWND, true)
			back = append(back, t.w.HWND)
		}
	}
	a.park.Unpark(back...)
	focus := o.focus
	if focus == 0 && winapi.Foreground() == o.hwnd {
		// The overview must not hide with the keyboard: Windows would hand
		// it to the next window down, which may be a parked one.
		focus = winapi.ShellWindow()
		if len(back) > 0 {
			focus = back[0]
		}
	}
	if focus != 0 && winapi.IsWindow(focus) && !a.park.IsParked(focus) {
		ok := winapi.Activate(focus)
		debugf("overview: activate %x ok=%v", focus, ok)
	}
	d.inOverview = false
	a.redraw()
	after(700*time.Millisecond, func() {
		for _, h := range back {
			if winapi.IsWindow(h) {
				winapi.SetTransitionsDisabled(h, false)
			}
		}
	})
	gen := o.gen
	after(landingDelay, func() {
		if o.gen != gen {
			return
		}
		o.phase, o.d, o.tiles, o.badges = ovHidden, nil, nil, nil
		// One last, empty frame, so that it never shows these again.
		o.win.Invalidate()
		after(50*time.Millisecond, func() {
			if o.phase == ovHidden {
				o.win.Hide()
			}
		})
		a.busy = false
		a.ended(d)
	})
}

// abort sends everything back at once, as the display it shows changes or
// it never draws.
func (o *overview) abort() {
	a, d := o.a, o.d
	switch {
	case o.phase == ovHidden || o.phase == ovLanding:
		return
	case !o.flying:
		// Nothing left the desktop yet, nor the strip: it only goes, and
		// the keyboard back where it was.
		o.gen++
		o.phase, o.d, o.tiles = ovHidden, nil, nil
		if winapi.Foreground() == o.hwnd && o.prev != 0 && winapi.IsWindow(o.prev) {
			winapi.Activate(o.prev)
		}
		o.win.Hide()
		a.busy = false
		a.ended(d)
		return
	}
	o.close(nil)
	o.phase = ovLanding
	o.land()
}

// local converts a pose in pixels of the screen to DIPs of the window.
func (o *overview) local(p pose) (x, y, w, h float32) {
	k := o.scale
	return float32((p.x - float64(o.rect.Left)) / k), float32((p.y - float64(o.rect.Top)) / k), float32(p.w / k), float32(p.h / k)
}

// screen converts a rectangle in DIPs of the window to a pose on the screen.
func (o *overview) screen(r ui.Rect) pose {
	k := o.scale
	return pose{x: float64(o.rect.Left) + float64(r.X)*k, y: float64(o.rect.Top) + float64(r.Y)*k, w: float64(r.W) * k, h: float64(r.H) * k}
}

func (o *overview) view(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	if o.d == nil || len(o.tiles) == 0 {
		return
	}
	now := c.Now()
	if !o.held {
		// The windows show where they are; once that is surely on the
		// screen, the real ones may go.
		o.held = true
		gen := o.gen
		after(takeOffDelay, func() {
			if o.gen == gen {
				o.takeOff()
			}
		})
	}
	if o.flying && (o.phase == ovOpening || o.phase == ovClosing) {
		// The strips under it draw too, as under the flight layer
		// (App.paintStrips): the cards take their new places meanwhile.
		post(o.a.paintStrips)
		// The clock starts with the first frame that moves, so a late
		// frame cannot eat into the move and make it jump.
		if o.start.IsZero() {
			o.start = now
		}
		if now.Sub(o.start) < overviewTime {
			c.AnimationFrame()
		} else if o.phase == ovOpening {
			o.phase = ovOpen
			for i := range o.tiles {
				t := &o.tiles[i]
				t.from, t.fromOp, t.fromSh = t.to, t.toOp, t.toSh
			}
			o.spreadFrom = o.spreadTo
		} else {
			o.phase = ovLanding
			post(o.land)
		}
	}
	e := o.eased(now)
	spread := o.spread(e)
	live := o.flying && (o.phase == ovOpening || o.phase == ovOpen)
	if live && c.Shortcut(0, ui.KeyEscape) {
		post(func() { o.close(nil) })
	}
	w, h := c.Size()
	bg := ui.Box(c.Key("dim")).Absolute().Left(0).Top(0).Size(w, h).Background(ui.RGBA(0, 0, 0, overviewDim*spread))
	if live && bg.Clicked() {
		post(func() { o.close(nil) })
	}
	// Front first, drawn the other way round; the window picked flies
	// home above the others.
	picked := -1
	for i := len(o.tiles) - 1; i >= 0; i-- {
		if o.phase >= ovClosing && o.tiles[i].w.HWND == o.focus {
			picked = i
			continue
		}
		o.drawTile(c, &o.tiles[i], e, spread, live)
	}
	if picked >= 0 {
		o.drawTile(c, &o.tiles[picked], e, spread, live)
	}
	if o.phase >= ovClosing {
		// The cards' icons come in over the second half, to be in place as
		// the cards show.
		op := float32(min(max(e*2-1, 0), 1))
		for _, b := range o.badges {
			x, y, bw, bh := o.local(b.at)
			ui.Image(c.Key("badge:"+b.app), b.icon).Absolute().Left(x).Top(y).Size(bw, bh).Opacity(op)
		}
	}
}

// drawTile draws a window of the overview e of the way through its move,
// spread out as far as spread says; live, it takes the pointer.
func (o *overview) drawTile(c *ui.Context, t *ovTile, e float64, spread float32, live bool) {
	at, op, sh := t.at(e)
	x, y, w, h := o.local(at)
	el := ui.Box(c.Key(t.w.HWND)).Absolute().Left(x).Top(y).Size(w, h)
	live = live && !t.closing
	hovered := false
	if live {
		el = el.Cursor(ui.CursorPointer)
		hovered = el.Hovered()
		if el.Clicked() {
			post(func() { o.close(t) })
		}
	}
	ring, lift := float32(0), float32(1)
	if hovered {
		ring, lift = 1, 1.025
	}
	ring = el.AnimateTo("ring", ring, fadeQuick)
	lift = el.AnimateTo("lift", lift, ui.Snappy)
	// A window asked to close shrinks a little as it fades away.
	gone := el.AnimateTo("gone", b2f(t.closing), fadeQuick)
	op *= 1 - gone
	lift *= 1 - 0.08*gone
	el.TransformOrigin(0.5, 0.5).Scale(lift, lift)
	closer := el.AnimateTo("closer", b2f(hovered), fadeQuick)
	// Cards turn about their outer edge: a negative tilt is a card of a
	// strip on the right, turned about its right edge.
	originX := float32(0)
	if at.tilt < 0 {
		originX = 1
	}
	radius := max(min(h*0.03, 10), 3)
	iconSize := min(max(h*0.2, 26), 48)
	el.Children(func() {
		var pane ui.Element
		if t.still != nil {
			pane = ui.Image(c.Key("still"), t.still).Absolute().Left(0).Top(0).Size(w, h).Fit(ui.FillBox).Radius(radius)
		} else {
			// No still: a frosted pane with the app's icon, as its card shows it.
			pane = ui.Column(c.Key("pane")).Absolute().Left(0).Top(0).Size(w, h).Radius(radius).
				Background(ui.RGBA(250, 250, 252, 0.2)).Border(1, ui.RGBA(255, 255, 255, 0.3)).Center()
			pane.Children(func() {
				if t.icon != nil {
					sz := min(h*0.32, 72)
					ui.Image(c, t.icon).Size(sz, sz).Opacity(0.9)
				}
			})
		}
		sh.cast(pane).TransformOrigin(originX, 0.5).RotateY(at.tilt).Perspective(cardPerspective).Opacity(op)
		if ring > 0.01 {
			ui.Box(c.Key("ring")).Absolute().Left(-3).Top(-3).Size(w+6, h+6).Radius(radius+3).
				Border(3, ringColor).Opacity(ring * op)
		}
		if live && closer > 0.01 {
			o.closeButton(c, t, w, h, closer*op)
		}
		// The app's icon sits on the bottom edge of its windows spread out,
		// and its title shows under the one the pointer is on.
		if t.icon != nil && t.still != nil {
			ui.Image(c.Key("icon"), t.icon).Absolute().Left((w-iconSize)/2).Top(h-iconSize*0.6).
				Size(iconSize, iconSize).Opacity(spread * op)
		}
		if ring > 0.01 {
			title := t.w.Title
			if title == "" {
				title = t.name
			}
			ui.Row(c.Key("title")).Absolute().Left(-80).Top(h+iconSize*0.4+6).Size(w+160, 26).Center().
				Opacity(ring * spread).Children(func() {
				ui.Text(c, title).FontSize(12).Bold().TextColor(white).MaxLines(1).MaxWidth(w+160).
					Background(pillColor).Padding(4, 10).Radius(12)
			})
		}
	})
}

// closeButton draws the button in the top right corner of tile t, w by h
// DIPs, that closes its window: the block of a title bar's close button,
// as wide as the bar is tall, dark and translucent until the pointer sets
// it on it, when it turns the red Windows turns it. It shows only on the
// window under the pointer, so that the windows spread out stay as
// uncluttered as Mission Control's.
func (o *overview) closeButton(c *ui.Context, t *ovTile, w, h, op float32) {
	height := min(max(min(h, w)*0.2, overviewCloseSize), overviewCloseMax)
	width := min(height*1.3, w*0.4)
	// Flush with the corner, rounded as the window it covers is at its
	// top right, so that it never shows outside it.
	corner := max(min(h*0.03, 10), 3)
	btn := ui.Box(c.Key("close")).Absolute().Left(w-width).Top(0).Size(width, height).
		Cursor(ui.CursorPointer).Label(o.a.tr().CloseWindow)
	if btn.Clicked() {
		gen := o.gen
		post(func() {
			if o.gen == gen {
				o.closeWindow(t)
			}
		})
	}
	hot := btn.AnimateTo("hot", b2f(btn.Hovered()), fadeQuick)
	press := btn.AnimateTo("press", 1-0.12*b2f(btn.Pressed()), ui.Snappy)
	bg := closeRest.Mix(closeRed, hot).Mix(ui.RGB(0, 0, 0), 0.12*b2f(btn.Pressed()))
	btn.Radius(0, corner, 0, 3).Background(bg).TransformOrigin(0.5, 0.5).Scale(press, press).Opacity(op)
	// An X of two bars crossed at the middle. Children are placed in the
	// padding box, inside any border, so they center in what it leaves.
	bar, thick := min(height, width)*0.55, max(height*0.1, 2)
	btn.Children(func() {
		for i, deg := range []float32{45, -45} {
			ui.Box(c.Key(i)).Absolute().Left((width-bar)/2).Top((height-thick)/2).Size(bar, thick).Radius(thick/2).
				Background(white).TransformOrigin(0.5, 0.5).Rotate(deg)
		}
	})
}

// closeWindow asks the window of tile t to close, as its close button
// does, and fades the tile out. The overview stays open; should the window
// not close (it may ask to save first), it goes back with the others as
// the overview closes.
func (o *overview) closeWindow(t *ovTile) {
	if o.phase != ovOpening && o.phase != ovOpen || t.closing {
		return
	}
	debugf("overview: close %x %q", t.w.HWND, t.w.Title)
	t.closing = true
	winapi.Close(t.w.HWND)
	o.win.Invalidate()
}
