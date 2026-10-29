package app

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	flightTime = 520 * time.Millisecond
	// A window the user left by way of the system fades where it is: as
	// quickly as the system's own animation of the window they went to,
	// so that the two do not linger over each other.
	fadeTime = 200 * time.Millisecond
	// The flights and the real windows overlap for a moment at each end:
	// both show the same picture, so the overlap cannot be seen, while
	// swapping them within one frame can. Their shadows differ, so a flight
	// casts none while the real window under it casts its own.
	takeOffDelay = 40 * time.Millisecond
	landingDelay = 70 * time.Millisecond
	// How far into a flight the icons of the cards it lands in fade in, so
	// that they are in place by the time the windows are.
	iconRevealPoint = 0.3
	// How long a switch waits for its flights to be drawn before it hands
	// the real windows over anyway.
	flightTimeout = 1500 * time.Millisecond
)

// flightEase is a critically damped spring launched at its target and
// normalised to land on exactly 1: brisk at the start, most of the way
// early, and settling without overshoot.
func flightEase(p float64) float64 {
	const stiffness, launch = 7.5, 0.6
	spring := func(x float64) float64 { return 1 - (1+(1-launch)*x)*math.Exp(-x) }
	return spring(stiffness*min(max(p, 0), 1)) / spring(stiffness)
}

// pose is where a window shows, in screen pixels, and how far it is turned.
type pose struct {
	x, y, w, h float64
	tilt       float32
}

func poseOf(r winapi.Rect) pose {
	return pose{x: float64(r.Left), y: float64(r.Top), w: float64(r.W()), h: float64(r.H())}
}

// shade is the shadow under a flight: a tight ambient glow around the
// window, plus a key shadow dropped below it from the outline shrunk by
// keyInset, so that it reaches far down but hardly past the sides. Blurs
// are CSS blurs in DIPs, twice the standard deviation.
type shade struct {
	ambient, ambientBlur         float32
	key, keyBlur, keyY, keyInset float32
}

// cardShade is the shadow of a card h DIPs tall; it grows less than the card.
func cardShade(h float32) shade {
	return shade{key: 0.34, keyBlur: min(h*0.16, h*0.04+14), keyY: min(h*0.05, h*0.02+4)}
}

// windowShade is the shadow DWM draws under a Windows 11 window, fitted to
// captures taken against the bare wallpaper and measured from the frame
// bounds; it is darker while the frame is drawn active. A flight hands over
// to the real window, so any difference shows as the shadow jumping.
func windowShade(active bool) shade {
	if active {
		return shade{ambient: 0.176, ambientBlur: 28.4, key: 0.235, keyBlur: 40.2, keyY: 37.9, keyInset: 5.7}
	}
	return shade{ambient: 0.119, ambientBlur: 14, key: 0.164, keyBlur: 42, keyY: 38.1, keyInset: 7.6}
}

// castsShadow reports whether DWM draws a shadow around window h on the
// stage: none while it is maximized, nor around a window shaped by a
// region, as apps that draw a frame and shadow of their own are.
func castsShadow(h winapi.HWND) bool {
	maximized := winapi.IsZoomed(h)
	if winapi.IsIconic(h) {
		_, maximized, _ = winapi.RestoredRect(h)
	}
	return !maximized && winapi.CastsFrameShadow(h)
}

// ownShade is the shadow a window casts on the stage, if it casts one.
func ownShade(casts, active bool) shade {
	if !casts {
		return shade{}
	}
	return windowShade(active)
}

func lerpShade(a, b shade, t float32) shade {
	l := func(a, b float32) float32 { return a + (b-a)*t }
	return shade{
		ambient: l(a.ambient, b.ambient), ambientBlur: l(a.ambientBlur, b.ambientBlur),
		key: l(a.key, b.key), keyBlur: l(a.keyBlur, b.keyBlur), keyY: l(a.keyY, b.keyY), keyInset: l(a.keyInset, b.keyInset),
	}
}

// cast gives el the shadow; the zero shade casts none.
func (sh shade) cast(el ui.Element) ui.Element {
	if sh.ambient > 0 {
		el = el.Shadow(0, 0, sh.ambientBlur, 0, ui.RGBA(0, 0, 0, sh.ambient))
	}
	if sh.key > 0 {
		el = el.Shadow(0, sh.keyY, sh.keyBlur, -sh.keyInset, ui.RGBA(0, 0, 0, sh.key))
	}
	return el
}

// flight carries one window's still between the strip and the stage.
type flight struct {
	still          *ui.Bitmap
	hwnd           winapi.HWND
	from, to       pose
	shade, toShade shade
	// How opaque it shows at its from and its to. A flight with no slot of
	// its own to land in fades away; one turned back mid-way starts as
	// faded as it was.
	fromOp, toOp float32
	fadeIn       bool // comes from nowhere: it fades in early on the way
	// It fades where it is, done pace times as fast as the trip.
	fadeOut bool
	pace    float64
	// It starts over its real window, which casts its own shadow until the
	// flight takes off.
	holdsShadow bool
	// It lands as its real window, casting the shadow that window casts:
	// none unless casts, darker while its frame is drawn active, which is
	// only known once its activation comes in. A window that draws its own
	// frame, as Chromium's do, never draws it active.
	lands, casts bool
	own          *ownShadow // the shadow its app draws it in, if it draws one
}

// badge is an app's icon on a card that flights land in, in screen pixels.
type badge struct {
	app  string
	icon *ui.Bitmap
	at   pose
}

// toward is the shadow f casts where it lands, as things stand now, with
// fg in the foreground: only that window may have its frame drawn active.
func (f *flight) toward(fg winapi.HWND) shade {
	switch {
	case !f.lands:
		return f.toShade
	case !f.casts:
		return shade{}
	}
	return windowShade(f.hwnd == fg && winapi.HasActiveFrame(f.hwnd))
}

// opacity is how opaque f shows p of its flight time in, e of the way.
func (f *flight) opacity(p, e float64) float32 {
	switch {
	case f.fadeIn:
		e = min(p*3, 1)
	case f.fadeOut:
		// Eased out, with no tail to linger.
		q := min(p*f.pace, 1)
		e = 1 - (1-q)*(1-q)
	}
	return f.fromOp + (f.toOp-f.fromOp)*float32(e)
}

// trip is the course of one switch's flights: they show at their start,
// take off once they cover the real windows, and land.
type trip struct {
	hold    time.Duration // how long they show at their start
	held    bool          // the flights were drawn at their start
	flying  bool          // they took off
	start   time.Time     // the frame they left in; zero until it is drawn
	arrived bool          // they were drawn where they land
	landed  bool          // the real windows and cards under them cast their own shadows
	// Another switch took over mid-way: the flights hold at stoppedAt of
	// the way until it flies them on.
	stopped   bool
	stoppedAt float64
	span      time.Duration // flightTime, or fadeTime with nothing but fades
	// stats measures the frames drawn while flying, for the debug log.
	stats frameStats

	takeOff, land, stop func()
}

// progress is how far into its flight time a trip is at now.
func (t *trip) progress(now time.Time) float64 {
	switch {
	case t.stopped:
		return t.stoppedAt
	case !t.flying || t.start.IsZero():
		return 0
	}
	return min(float64(now.Sub(t.start))/float64(t.span), 1)
}

// aloft is where a flight stopped mid-way shows, the shadow it casts and
// how opaque it is.
type aloft struct {
	at    pose
	shade shade
	op    float32
}

// at is where f shows e of the way, eased, from its from to its to.
func (f *flight) at(e float64) pose { return lerpPose(f.from, f.to, e) }

// lerpPose is e of the way from a to b.
func lerpPose(a, b pose, e float64) pose {
	return pose{
		x: lerp(a.x, b.x, e), y: lerp(a.y, b.y, e),
		w: lerp(a.w, b.w, e), h: lerp(a.h, b.h, e),
		tilt: a.tilt + (b.tilt-a.tilt)*float32(e),
	}
}

// layer is a transparent window over a whole display, the strip included,
// that lets every click through. It shows the flights while a switch runs,
// and the windows of a card dragged out of the strip, which the strip's own
// window could not show beyond its edge. Another such window right under
// it, under, shows the shadows apps draw in windows of their own.
type layer struct {
	win     *mygo.Window
	hwnd    winapi.HWND
	under   *mygo.Window
	uhwnd   winapi.HWND
	rect    winapi.Rect
	scale   float64
	shown   bool
	low     bool     // under a window rather than above all
	list    []flight // flying, from their from to their to
	badges  []badge  // the icons of the cards they land in, over them
	trip    *trip
	drag    []flight // held where their from is, under the pointer
	dragGen int
	// framed runs after each frame the layer draws, for the strips below
	// it to draw theirs (App.paintStrips).
	framed func()
	// hole is the window the flights stay clear of, rather than show
	// over, while the layer is above all.
	hole winapi.HWND
	// name is its display's, and frames measures the frames of a card
	// dragged on it, for the debug log.
	name   string
	frames frameStats
	pace   pacer
}

// flights runs the flights of switches, on a layer per display.
type flights struct {
	a      *App
	layers map[string]*layer
}

func newFlights(a *App) *flights { return &flights{a: a, layers: map[string]*layer{}} }

func (f *flights) layer(d *display) *layer {
	l := f.layers[d.mon.Device]
	if l != nil {
		return l
	}
	l = &layer{framed: f.a.paintStrips, name: d.mon.Device}
	pane := func(view func(*ui.Context)) (*mygo.Window, winapi.HWND) {
		win := mygo.NewFlyout(mygo.FlyoutOptions{Tray: f.a.tray, Hidden: true, Width: 400, Height: 400, Content: ui.View(view)})
		h := winapi.HWND(win.NativeHandle())
		win.OnClose(func(e *mygo.CloseEvent) {
			if !f.a.quitting {
				e.PreventDefault()
			}
		})
		winapi.SetLayered(h)
		winapi.SetExStyleBits(h, winapi.WsExTransparent, 0)
		winapi.SetTransitionsDisabled(h, true)
		return win, h
	}
	l.win, l.hwnd = pane(l.view)
	l.under, l.uhwnd = pane(func(c *ui.Context) { c.Root().Background(ui.Transparent) })
	f.layers[d.mon.Device] = l
	return l
}

// raise puts the layer above every other window, the strip included, and
// its under window right beneath it.
func (l *layer) raise() {
	const flags = winapi.SwpNoActivate | winapi.SwpNoMove | winapi.SwpNoSize
	winapi.SetWindowPos(l.hwnd, winapi.HwndTopmost, 0, 0, 0, 0, flags)
	winapi.SetWindowPos(l.uhwnd, l.hwnd, 0, 0, 0, 0, flags)
	l.low = false
}

// lower puts the layer right under window h, and its under window beneath
// it. Ordered after a window that is not topmost, it is no longer topmost
// either, until raised.
func (l *layer) lower(h winapi.HWND) {
	const flags = winapi.SwpNoActivate | winapi.SwpNoMove | winapi.SwpNoSize
	winapi.SetWindowPos(l.hwnd, h, 0, 0, 0, 0, flags)
	winapi.SetWindowPos(l.uhwnd, l.hwnd, 0, 0, 0, 0, flags)
	l.low = true
}

// release lets go of the shadows the flights show of their apps' own.
func (l *layer) release() {
	for _, fl := range l.list {
		if fl.own != nil {
			fl.own.release()
		}
	}
}

// ownShadow is the shadow an app draws around one of its windows in a
// window of its own (catalog.ShadowWindow), which only that window's own
// picture matches: it may be darker below than on the sides and change as
// the window is activated. DWM shows it on the layer's under window as it
// is, one thumbnail per side outside the frame, so it never covers a flight.
type ownShadow struct {
	frame  winapi.Rect // the window's frame as the flight begins
	insets winapi.Rect // how far the shadow reaches past each edge
	w, h   int32       // the size of the shadow's window
	sides  [4]winapi.Thumbnail
}

func newOwnShadow(under, h winapi.HWND) *ownShadow {
	src, insets, ok := catalog.ShadowWindow(h)
	if !ok {
		return nil
	}
	r := winapi.WindowRect(src)
	o := &ownShadow{frame: winapi.FrameBounds(h), insets: insets, w: r.W(), h: r.H()}
	for i := range o.sides {
		t, ok := winapi.RegisterThumbnail(under, src)
		if !ok {
			o.release()
			return nil
		}
		o.sides[i] = t
	}
	return o
}

// show draws the shadow around a flight at at, scaled as the flight is
// from the window's frame. Coordinates are relative to origin, where the
// under window is.
func (o *ownShadow) show(origin winapi.Rect, at pose, opacity float64) {
	if opacity <= 0 || o.frame.Empty() {
		o.hide()
		return
	}
	kx, ky := at.w/float64(o.frame.W()), at.h/float64(o.frame.H())
	x, y := at.x-float64(origin.Left), at.y-float64(origin.Top)
	px := func(v float64) int32 { return int32(math.Round(v)) }
	in := o.insets
	left, top, right, bottom := px(x), px(y), px(x+at.w), px(y+at.h)
	outL, outT := px(x-float64(in.Left)*kx), px(y-float64(in.Top)*ky)
	outR, outB := px(x+at.w+float64(in.Right)*kx), px(y+at.h+float64(in.Bottom)*ky)
	// The top and bottom take the corners along.
	o.sides[0].Show(winapi.Rect{Left: outL, Top: outT, Right: outR, Bottom: top},
		winapi.Rect{Right: o.w, Bottom: in.Top}, opacity)
	o.sides[1].Show(winapi.Rect{Left: outL, Top: bottom, Right: outR, Bottom: outB},
		winapi.Rect{Top: o.h - in.Bottom, Right: o.w, Bottom: o.h}, opacity)
	o.sides[2].Show(winapi.Rect{Left: outL, Top: top, Right: left, Bottom: bottom},
		winapi.Rect{Top: in.Top, Right: in.Left, Bottom: o.h - in.Bottom}, opacity)
	o.sides[3].Show(winapi.Rect{Left: right, Top: top, Right: outR, Bottom: bottom},
		winapi.Rect{Left: o.w - in.Right, Top: in.Top, Right: o.w, Bottom: o.h - in.Bottom}, opacity)
}

func (o *ownShadow) hide() {
	for _, t := range o.sides {
		if t != 0 {
			t.Hide()
		}
	}
}

func (o *ownShadow) release() {
	for i, t := range o.sides {
		if t != 0 {
			t.Unregister()
			o.sides[i] = 0
		}
	}
}

// cardPoses returns where the given windows show in a display's strip, as
// it is laid out now. Live poses are the cards as last drawn, turned and
// lifted by the pointer over them; otherwise the cards at rest.
func (f *flights) cardPoses(d *display, ws []catalog.Window, live bool) map[winapi.HWND]pose {
	s := d.strip
	out := map[winapi.HWND]pose{}
	if s == nil || !s.visible || d.viewW == 0 {
		return out
	}
	want := map[winapi.HWND]bool{}
	for _, w := range ws {
		// A card dropped on the stage flies on from where it was let go.
		if p, ok := d.dropPoses[w.HWND]; ok {
			out[w.HWND] = p
			continue
		}
		want[w.HWND] = true
	}
	right := s.right()
	l := layoutStrip(len(d.sets), d.viewW, d.viewH, d.scroll, float32(f.a.settings.Spacing), f.a.panel(), right)
	rest, _ := cardTilts(f.a.settings.Tilt)
	for i, set := range d.sets {
		tilt, lift := rest, float32(1)
		if st := d.cards[set.key]; live && st != nil && st.lift > 0 {
			tilt, lift = st.tilt, st.lift
		}
		card := l.cards[i]
		for j, r := range set.stack(card, right) {
			if h := set.windows[j].HWND; want[h] {
				x, y, w, ht := s.toScreen(lifted(card, r, lift, right))
				out[h] = pose{x: x, y: y, w: w, h: ht, tilt: tilt * turnSign(right)}
			}
		}
	}
	return out
}

// landingIcons returns the icons of the cards windows are flying into,
// where the strip shows them at rest, the first app's last so it is on top.
func (f *flights) landingIcons(d *display) []badge {
	s := d.strip
	if s == nil || !s.visible || d.viewW == 0 {
		return nil
	}
	right := s.right()
	l := layoutStrip(len(d.sets), d.viewW, d.viewH, d.scroll, float32(f.a.settings.Spacing), f.a.panel(), right)
	var out []badge
	for i, set := range d.sets {
		if !d.landingOf(set) {
			continue
		}
		card := l.cards[i]
		row := iconRow(set.stack(card, right)[0], card, len(set.apps), float32(f.a.settings.IconScale), right)
		for j := len(row) - 1; j >= 0; j-- {
			g := set.apps[j]
			if icon := f.a.media.icon(g.front()); icon != nil {
				x, y, w, h := s.toScreen(row[j])
				out = append(out, badge{app: g.app, icon: icon, at: pose{x: x, y: y, w: w, h: h}})
			}
		}
	}
	return out
}

// run plays a switch: the departing windows fly into their cards, the
// arriving ones out of theirs (from) and home. takeOff runs once the flights
// cover the real windows, land as they are drawn where they land, and done
// once they are gone. Should another switch take over mid-way, stop runs
// instead of land and done and its flights carry on from where they are.
// With fade, the departing windows fade where they are instead of flying
// into their cards; with below, the layer shows right under that window
// rather than above all. With hole, the layer stays above all and the
// flights show nowhere over that window, as if they flew under it. With
// nothing to fly, or with reduced motion, it all runs at once.
func (f *flights) run(d *display, departing, arriving []catalog.Window, from map[winapi.HWND]pose, fade bool, below, hole winapi.HWND, takeOff, land, stop, done func()) {
	scale := d.mon.Scale()
	cardShadeAt := func(p pose) shade { return cardShade(float32(p.h / scale)) }
	air := f.airborne(d)
	// A window with no slot of its own in its card, as the fourth of an
	// app, shrinks into the card's front window and fades on the way.
	front := map[string]winapi.HWND{}
	want := slices.Clone(departing)
	for _, set := range d.sets {
		for _, g := range set.apps {
			front[g.app] = set.front().HWND
		}
		want = append(want, set.front())
	}
	to := f.cardPoses(d, want, false)
	// The flights are drawn in turn, the last on top: the windows, given
	// front first, go in from the back so that they overlap as on the
	// desktop and in the cards.
	var list []flight
	for _, w := range slices.Backward(departing) {
		st := f.a.media.still(w.HWND)
		if st == nil {
			continue
		}
		casts := castsShadow(w.HWND)
		fl := flight{still: st.fly, hwnd: w.HWND, from: poseOf(w.Frame), shade: ownShade(casts, winapi.HasActiveFrame(w.HWND)), casts: casts, holdsShadow: true, fromOp: 1, toOp: 1}
		if target, ok := to[w.HWND]; ok {
			fl.to = target
		} else if p, ok := to[front[w.App]]; ok {
			fl.to, fl.toOp = p, 0
		} else {
			fl.to, fl.toOp = f.collapse(d, fl.from), 0
		}
		fl.toShade = cardShadeAt(fl.to)
		if a, ok := air[w.HWND]; ok {
			// Turned back on its way home: its real window is long gone.
			fl.from, fl.shade, fl.fromOp, fl.holdsShadow = a.at, a.shade, a.op, false
		}
		if fade {
			// Its shadow goes with it.
			fl.to, fl.toOp, fl.toShade, fl.fadeOut = fl.from, 0, fl.shade, true
		}
		list = append(list, fl)
	}
	// The arriving windows are added last so that they fly above.
	fg := winapi.Foreground()
	for _, w := range slices.Backward(arriving) {
		st := f.a.media.still(w.HWND)
		if st == nil {
			continue
		}
		home := poseOf(f.a.home(w))
		start, ok := from[w.HWND]
		fl := flight{still: st.fly, hwnd: w.HWND, from: start, to: home, shade: cardShadeAt(start), lands: true, casts: castsShadow(w.HWND), fromOp: 1, toOp: 1}
		if a, aloft := air[w.HWND]; aloft {
			// Turned back on its way into its card.
			fl.from, fl.shade, fl.fromOp = a.at, a.shade, a.op
		} else if !ok {
			// Not in a card: it grows in place.
			fl.from = pose{x: home.x + home.w*0.03, y: home.y + home.h*0.03, w: home.w * 0.94, h: home.h * 0.94}
			fl.shade, fl.fromOp, fl.fadeIn = fl.toward(fg), 0, true
		}
		list = append(list, fl)
	}
	if len(list) == 0 || mygo.Theme.Preferences().ReduceMotion {
		d.dropPoses = nil
		f.endDrag(d, 0)
		takeOff()
		land()
		done()
		return
	}
	sw := newStopwatch()
	l := f.show(d)
	sw.lap("show layer")
	l.hole = hole
	if below != 0 && winapi.IsWindow(below) {
		l.lower(below)
	} else {
		// Above the strip, which a floating strip that just came out may be.
		l.raise()
	}
	l.release()
	sw.lap("order layer")
	for i := range list {
		// Without a shadow from DWM, a window may have one its app draws.
		if fl := &list[i]; !fl.casts {
			fl.own = newOwnShadow(l.uhwnd, fl.hwnd)
		}
	}
	sw.lap("app shadows")
	defer sw.report("flights: setting off")
	t := &trip{hold: takeOffDelay, span: flightTime}
	if !slices.ContainsFunc(list, func(fl flight) bool { return !fl.fadeOut }) {
		t.span = fadeTime
	}
	for i := range list {
		list[i].pace = float64(t.span) / float64(fadeTime)
	}
	if len(air) > 0 && !slices.ContainsFunc(list, func(fl flight) bool { return fl.holdsShadow }) {
		// Turned back mid-way, they cover no real windows: they fly on
		// at once rather than stop in the air.
		t.hold = 0
	}
	finished := false
	finish := func() {
		if finished {
			return
		}
		finished = true
		if l.trip == t {
			l.release()
			l.trip, l.list, l.badges = nil, nil, nil
			l.win.Invalidate()
			f.hideLater(l)
		}
		done()
	}
	t.takeOff = func() {
		if t.flying || finished {
			return
		}
		takeOff()
		// The windows that left took the shadows their apps draw along, so
		// the flights show them from where the windows were.
		for _, fl := range list {
			if fl.own != nil && fl.holdsShadow {
				fl.own.show(l.rect, fl.from, 1)
			}
		}
		t.flying = true
		l.win.Invalidate()
		// A switch asked for meanwhile takes over now that every window
		// is on the layer.
		if f.a.queued[d] != nil {
			t.stop()
		}
	}
	t.stop = func() {
		// Before take-off the real windows are still where they were, and
		// once landed they are home again: the switch after picks up as
		// this one takes off or is done.
		if !t.flying || t.landed || finished {
			return
		}
		finished = true
		t.stoppedAt, t.stopped = t.progress(time.Now()), true
		l.badges = nil
		l.win.Invalidate()
		stop()
		if l.trip == t {
			// Nothing flew on.
			l.release()
			l.trip, l.list = nil, nil
			f.hideLater(l)
		}
	}
	t.land = func() {
		if t.landed || finished {
			return
		}
		// The real windows and cards cast their own shadows from the frame
		// they arrive in, so the flights drop theirs in that same frame.
		t.landed = true
		for _, fl := range list {
			if fl.own != nil {
				fl.own.hide()
			}
		}
		land()
		l.win.Invalidate()
		after(landingDelay, finish)
	}
	l.list, l.trip = list, t
	l.badges = f.landingIcons(d)
	l.drag = nil // a dropped card's windows fly on from where they are
	d.dropPoses = nil
	l.win.Invalidate()
	// Should the layer not draw, the windows still change places.
	after(takeOffDelay+flightTime+flightTimeout, func() {
		if !finished {
			debugf("flights: timed out, held=%v flying=%v arrived=%v", t.held, t.flying, t.arrived)
			t.takeOff()
			t.land()
		}
	})
}

// interrupt stops the switch in flight on a display, for the one asked for
// meanwhile (App.queued) to fly its windows on from where they are. One on
// another display runs its course.
func (f *flights) interrupt(d *display) {
	if l := f.layers[d.mon.Device]; l != nil && l.trip != nil {
		l.trip.stop()
	}
}

// airborne returns where the flights of a display's stopped switch hold.
func (f *flights) airborne(d *display) map[winapi.HWND]aloft {
	out := map[winapi.HWND]aloft{}
	l := f.layers[d.mon.Device]
	if l == nil || l.trip == nil || !l.trip.stopped {
		return out
	}
	p := l.trip.stoppedAt
	e := flightEase(p)
	fg := winapi.Foreground()
	for _, fl := range l.list {
		out[fl.hwnd] = aloft{at: fl.at(e), shade: lerpShade(fl.shade, fl.toward(fg), float32(e)), op: fl.opacity(p, e)}
	}
	return out
}

// collapse is where a window with no card to land in goes: a small copy of
// it at the middle of the edge of the display its strip keeps to.
func (f *flights) collapse(d *display, p pose) pose {
	work := winapi.MonitorInfo(d.mon.Handle).Work
	w, h := p.w/6, p.h/6
	x := float64(work.Left)
	if f.a.right(d) {
		x = float64(work.Right) - w
	}
	return pose{x: x, y: float64(work.Top) + (float64(work.H())-h)/2, w: w, h: h}
}

// show puts a display's layer over the whole display: the flights go into
// and come out of the strip, which a fixed strip keeps out of the work area.
func (f *flights) show(d *display) *layer {
	l := f.layer(d)
	if l.shown {
		return l
	}
	area := winapi.MonitorInfo(d.mon.Handle).Monitor
	// One pixel short of the display, so that Windows never takes the
	// layer for an app in full screen.
	area.Bottom--
	l.rect, l.scale, l.shown, l.low = area, d.mon.Scale(), true, false
	l.win.ShowInactive()
	l.under.ShowInactive()
	winapi.SetWindowPos(l.hwnd, winapi.HwndTopmost, area.Left, area.Top, area.W(), area.H(), winapi.SwpNoActivate)
	winapi.SetWindowPos(l.uhwnd, l.hwnd, area.Left, area.Top, area.W(), area.H(), winapi.SwpNoActivate)
	return l
}

// layerLinger is how long a layer stays out, empty, once nothing shows on
// it. Showing one over a whole display holds the main thread up for some
// 10 ms, and hiding it as long: switches one after another, and a card
// dragged to and fro over the edge of two displays, would pay it every
// time, right as their frames begin.
const layerLinger = 2 * time.Second

// hideLater hides a layer once it has been empty for layerLinger, unless
// it shows something again meanwhile. While a card is dragged it waits on:
// the card may come back over its display.
func (f *flights) hideLater(l *layer) {
	after(layerLinger, func() {
		if l.list != nil || l.drag != nil || !l.shown {
			return
		}
		if f.a.cardDrag != nil {
			f.hideLater(l)
			return
		}
		l.shown = false
		l.win.Hide()
		l.under.Hide()
	})
}

// standBy makes a display's layer and shows it, empty, ahead of what will
// show on it; it goes again once it has been left empty for layerLinger.
// Showing a layer holds the main thread up for some 15 ms, and the first
// time some 50 ms on a 5K display, as the GPU sets its buffers up: not in
// the middle of an animation.
func (f *flights) standBy(d *display) {
	if l := f.layer(d); !l.shown {
		f.show(d)
		f.hideLater(l)
	}
}

// standByAll has the layer of every display with a strip out, for a card
// about to be dragged out of one: it may reach any of them.
func (f *flights) standByAll() {
	for _, d := range f.a.displays {
		if d.strip != nil {
			f.standBy(d)
		}
	}
}

// dragged shows the windows of a card being dragged out of display d's
// strip where they are now: on d's layer, and on that of every other
// display they reach, as they are dragged toward its strip.
func (f *flights) dragged(d *display, list []flight) {
	reach := winapi.Rect{Left: math.MaxInt32, Top: math.MaxInt32, Right: math.MinInt32, Bottom: math.MinInt32}
	for _, fl := range list {
		p := fl.from
		reach.Left, reach.Top = min(reach.Left, int32(p.x)), min(reach.Top, int32(p.y))
		reach.Right, reach.Bottom = max(reach.Right, int32(p.x+p.w)), max(reach.Bottom, int32(p.y+p.h))
	}
	for _, o := range f.a.displays {
		if o != d && !o.mon.Monitor.Intersects(reach) {
			f.clearDrag(o, 0)
			continue
		}
		if o != d && f.layers[o.mon.Device] == nil {
			continue
		}
		l := f.show(o)
		if l.low {
			l.raise()
		}
		l.hole = 0
		l.drag = list
		l.dragGen++
		// At the pace the display shows it: the strip draws a frame for
		// every move of the pointer, many more.
		l.pace.frame(l.win)
	}
}

// endDrag takes a dragged card's windows away: after delay on the layer
// of display d, the one it was dragged out of, and at once on the others.
// A switch may take them over meanwhile to fly them on, or a new drag
// begin.
func (f *flights) endDrag(d *display, delay time.Duration) {
	if f == nil {
		return
	}
	for _, o := range f.a.displays {
		if o != d {
			f.clearDrag(o, 0)
		}
	}
	f.clearDrag(d, delay)
}

// clearDrag takes the windows of a dragged card off display d's layer
// after delay.
func (f *flights) clearDrag(d *display, delay time.Duration) {
	l := f.layers[d.mon.Device]
	if l == nil || l.drag == nil {
		return
	}
	gen := l.dragGen
	clear := func() {
		if l.dragGen == gen && l.drag != nil {
			l.drag = nil
			l.win.Invalidate()
			f.hideLater(l)
		}
	}
	if delay <= 0 {
		clear()
		return
	}
	after(delay, clear)
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func (l *layer) view(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	if l.framed != nil {
		// Posted, it runs once this frame is done.
		post(l.framed)
	}
	if debugOn {
		t0 := time.Now()
		if t := l.trip; t != nil && t.flying {
			defer func() { t.stats.built(time.Since(t0)) }()
		}
		if len(l.drag) > 0 {
			// A card dragged out of a strip shows here, frame by frame.
			l.frames.frame(c.Now(), "card dragged")
			l.frames.burst("flight layer " + l.name)
			defer func() { l.frames.built(time.Since(t0)) }()
		}
	}
	for i, f := range l.drag {
		l.draw(c, i+len(l.list), f.still, f.from, 1, cardShade(float32(f.from.h/l.scale)), 0, 0)
	}
	if len(l.list) == 0 || l.trip == nil {
		return
	}
	t := l.trip
	now := c.Now()
	p := 0.0
	switch {
	case !t.held:
		// The flights show where they start; once they surely cover
		// the real windows, those may go.
		t.held = true
		after(t.hold, t.takeOff)
	case t.stopped:
		p = t.stoppedAt
	case t.flying:
		// The clock starts with the first frame that moves, so a late
		// frame cannot eat into the flight and make it jump.
		if t.start.IsZero() {
			t.start = now
		}
		p = t.progress(now)
		if debugOn {
			t.stats.frame(now, progress(p))
		}
		if p < 1 {
			l.pace.frame(l.win)
		} else if !t.arrived {
			t.arrived = true
			if debugOn {
				l.reportTrip(t)
			}
			post(t.land)
		}
	}
	e := flightEase(p)
	fg := winapi.Foreground()
	hole, cut := l.holeRect()
	type still struct {
		at pose
		op float32
		sh shade
	}
	stills := make([]still, len(l.list))
	for i, f := range l.list {
		at := f.at(e)
		op := f.opacity(p, e)
		var sh shade
		casting := !t.landed && (t.flying || !f.holdsShadow)
		if casting {
			sh = lerpShade(f.shade, f.toward(fg), float32(e))
		}
		if f.own != nil {
			// The app's own shadow shows the more, the nearer the flight
			// is to the window's size and place. Its window is under the
			// layer, above the hole's: none shows while there is one.
			near := 0.0
			switch {
			case !casting || cut:
			case f.lands:
				near = e
			default:
				near = 1 - e
			}
			f.own.show(l.rect, at, near*float64(op))
		}
		stills[i] = still{at, op, sh}
	}
	if !cut {
		for i, s := range stills {
			l.draw(c, i, l.list[i].still, s.at, s.op, s.sh, 0, 0)
		}
	} else {
		// The flights show in the bands around the hole, each clipping
		// them to itself.
		for b, r := range around(ui.Rect{W: float32(float64(l.rect.W()) / l.scale), H: float32(float64(l.rect.H()) / l.scale)}, hole) {
			ui.Box(c.Key(-1-b)).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Clip().Children(func() {
				for i, s := range stills {
					l.draw(c, i, l.list[i].still, s.at, s.op, s.sh, r.X, r.Y)
				}
			})
		}
	}
	// The icons fade in part of the way and hold until the strip draws its own.
	iconOp := float32(min(max((p-iconRevealPoint)*float64(flightTime)/float64(fadeQuick.Duration), 0), 1))
	if iconOp > 0 {
		for _, b := range l.badges {
			x := float32((b.at.x - float64(l.rect.Left)) / l.scale)
			y := float32((b.at.y - float64(l.rect.Top)) / l.scale)
			ui.Image(c.Key(b.app), b.icon).Absolute().Left(x).Top(y).Size(float32(b.at.w/l.scale), float32(b.at.h/l.scale)).Opacity(iconOp)
		}
	}
}

// holeRect is where the hole's window shows on the layer, in its DIPs, and
// whether the flights must stay clear of it now. It follows the window as
// it is dragged, frame by frame.
func (l *layer) holeRect() (ui.Rect, bool) {
	if l.hole == 0 || !winapi.IsWindow(l.hole) {
		return ui.Rect{}, false
	}
	r := winapi.FrameBounds(l.hole)
	if r.Empty() || !r.Intersects(l.rect) {
		return ui.Rect{}, false
	}
	k := float32(l.scale)
	return ui.Rect{
		X: float32(r.Left-l.rect.Left) / k, Y: float32(r.Top-l.rect.Top) / k,
		W: float32(r.W()) / k, H: float32(r.H()) / k,
	}, true
}

// around splits area less hole into the bands above and below the hole,
// full width, and those left and right of it; empty ones are left out.
func around(area, hole ui.Rect) []ui.Rect {
	x0, y0 := max(hole.X, area.X), max(hole.Y, area.Y)
	x1, y1 := min(hole.X+hole.W, area.X+area.W), min(hole.Y+hole.H, area.Y+area.H)
	bands := []ui.Rect{
		{X: area.X, Y: area.Y, W: area.W, H: y0 - area.Y},
		{X: area.X, Y: y1, W: area.W, H: area.Y + area.H - y1},
		{X: area.X, Y: y0, W: x0 - area.X, H: y1 - y0},
		{X: x1, Y: y0, W: area.X + area.W - x1, H: y1 - y0},
	}
	return slices.DeleteFunc(bands, func(r ui.Rect) bool { return r.W <= 0 || r.H <= 0 })
}

// reportTrip logs how evenly the flights of trip t drew, and what they
// drew: the size of the stills, and of the layer.
func (l *layer) reportTrip(t *trip) {
	px := 0
	for _, fl := range l.list {
		if fl.still != nil {
			w, h := fl.still.Size()
			px += w * h
		}
	}
	t.stats.report(fmt.Sprintf("flights: %d windows, %.1f MP of stills, layer %dx%d, low=%v hole=%v",
		len(l.list), float64(px)/1e6, l.rect.W(), l.rect.H(), l.low, l.hole != 0))
}

// draw shows a window's still at a pose given in screen pixels, casting
// sh; the zero shade casts none. ox and oy are where the element it is
// drawn in sits on the layer.
func (l *layer) draw(c *ui.Context, key int, still *ui.Bitmap, at pose, op float32, sh shade, ox, oy float32) {
	x := float32((at.x-float64(l.rect.Left))/l.scale) - ox
	y := float32((at.y-float64(l.rect.Top))/l.scale) - oy
	w, h := float32(at.w/l.scale), float32(at.h/l.scale)
	radius := max(min(h*0.045, 8), 3)
	img := sh.cast(ui.Image(c.Key(key), still).Absolute().Left(x).Top(y).Size(w, h).Fit(ui.FillBox).Radius(radius))
	// Cards turn about their outer edge: a negative tilt is a card of a
	// strip on the right, turned about its right edge.
	originX := float32(0)
	if at.tilt < 0 {
		originX = 1
	}
	img.TransformOrigin(originX, 0.5).RotateY(at.tilt).Perspective(cardPerspective).Opacity(op)
}
