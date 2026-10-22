package app

import (
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/renkai9418/stage-bar/internal/catalog"
	"github.com/renkai9418/stage-bar/internal/winapi"
)

const (
	flightTime = 520 * time.Millisecond
	// Time to let the flights show at their start before the real windows
	// go, and the real windows show before the flights go: the two show
	// the same picture, so overlapping them is invisible where swapping
	// them in one frame is not.
	takeOffDelay = 40 * time.Millisecond
	landingDelay = 70 * time.Millisecond
	// How far into a flight the icons of the cards it lands in fade in, so
	// that they are in place when the windows get there.
	iconRevealPoint = 0.3
	// How long a switch may wait for its flights to be drawn before it
	// hands over the real windows anyway.
	flightTimeout = 1500 * time.Millisecond
)

// flightEase is the course of a critically damped spring launched toward
// its target, normalised to land exactly on 1: it leaves briskly, travels
// most of the way early and settles without overshoot, as windows do on
// macOS.
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

// flight carries one window's still between the strip and the stage.
type flight struct {
	still    *ui.Bitmap
	from, to pose
	fadeIn   bool // comes from nowhere: it fades in instead
	fadeOut  bool // has no place of its own to land in: it fades away
}

// trip is the course of one switch's flights: they show at their start,
// take off once they cover the real windows, and land.
type trip struct {
	held    bool      // the flights were drawn at their start
	flying  bool      // they took off
	start   time.Time // the frame they left in; zero until it is drawn
	arrived bool      // they were drawn where they land

	takeOff, land func()
}

// layer is a transparent window over a whole display, the strip included,
// that lets every click through. It shows the flights while a switch runs,
// and the windows of a card dragged out of the strip, which the strip's own
// window could not show beyond its edge.
type layer struct {
	win     *mygo.Window
	hwnd    winapi.HWND
	rect    winapi.Rect
	scale   float64
	shown   bool
	list    []flight // flying, from their from to their to
	trip    *trip
	drag    []flight // held where their from is, under the pointer
	dragGen int
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
	l = &layer{}
	l.win = mygo.NewFlyout(mygo.FlyoutOptions{Tray: f.a.tray, Hidden: true, Width: 400, Height: 400, Content: ui.View(l.view)})
	l.hwnd = winapi.HWND(l.win.NativeHandle())
	l.win.OnClose(func(e *mygo.CloseEvent) {
		if !f.a.quitting {
			e.PreventDefault()
		}
	})
	winapi.SetLayered(l.hwnd)
	winapi.SetExStyleBits(l.hwnd, winapi.WsExTransparent, 0)
	winapi.SetTransitionsDisabled(l.hwnd, true)
	f.layers[d.mon.Device] = l
	return l
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
	l := layoutStrip(len(d.groups), d.viewW, d.viewH, d.scroll)
	rest := float32(0)
	if f.a.settings.Perspective {
		rest = cardTilt
	}
	for i, g := range d.groups {
		tilt, lift := rest, float32(1)
		if st := d.cards[g.app]; live && st != nil && st.lift > 0 {
			tilt, lift = st.tilt, st.lift
		}
		card := l.cards[i]
		aspects := make([]float64, min(len(g.windows), maxStacked))
		for j := range aspects {
			aspects[j] = g.windows[j].Aspect()
		}
		for j, r := range stackWindows(card, aspects) {
			if h := g.windows[j].HWND; want[h] {
				// Lifted around the card's left middle, as the card is.
				r = ui.Rect{
					X: card.X + (r.X-card.X)*lift,
					Y: card.Y + card.H/2 + (r.Y-card.Y-card.H/2)*lift,
					W: r.W * lift, H: r.H * lift,
				}
				x, y, w, ht := s.toScreen(r)
				out[h] = pose{x: x, y: y, w: w, h: ht, tilt: tilt}
			}
		}
	}
	return out
}

// run plays a switch: the departing windows fly from where they are into
// their cards, the arriving ones from their cards (from) home. The flights
// first show where they start; takeOff runs once they cover the windows,
// and they leave as it returns. reveal runs part of the way, for the
// cards the flights land in to show their icons; land as the flights are
// drawn where they land, and done once they are gone. Without anything to
// fly, or while the desktop asks for less motion, they all run at once.
func (f *flights) run(d *display, departing, arriving []catalog.Window, from map[winapi.HWND]pose, takeOff, reveal, land, done func()) {
	to := f.cardPoses(d, departing, false)
	// A window that has no place of its own in its card, as the fourth of
	// an app, shrinks into the card's front window and fades on the way.
	front := map[string]pose{}
	for _, w := range departing {
		if p, ok := to[w.HWND]; ok {
			if _, seen := front[w.App]; !seen {
				front[w.App] = p
			}
		}
	}
	var list []flight
	for _, w := range departing {
		st := f.a.media.still(w.HWND)
		if st == nil {
			continue
		}
		fl := flight{still: st.fly, from: poseOf(w.Frame)}
		if target, ok := to[w.HWND]; ok {
			fl.to = target
		} else if p, ok := front[w.App]; ok {
			fl.to, fl.fadeOut = p, true
		} else {
			fl.to, fl.fadeOut = f.collapse(d, fl.from), true
		}
		list = append(list, fl)
	}
	// The arriving windows are added last so that they fly above.
	for _, w := range arriving {
		st := f.a.media.still(w.HWND)
		if st == nil {
			continue
		}
		home := poseOf(f.a.home(w))
		start, ok := from[w.HWND]
		fl := flight{still: st.fly, from: start, to: home}
		if !ok {
			// Not in a card: it grows in place.
			fl.from = pose{x: home.x + home.w*0.03, y: home.y + home.h*0.03, w: home.w * 0.94, h: home.h * 0.94}
			fl.fadeIn = true
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
	l := f.show(d)
	// Above the strip, which a floating strip that just came out may be.
	winapi.SetWindowPos(l.hwnd, winapi.HwndTopmost, 0, 0, 0, 0, winapi.SwpNoActivate|winapi.SwpNoMove|winapi.SwpNoSize)
	t := &trip{}
	finished := false
	finish := func() {
		if finished {
			return
		}
		finished = true
		if l.trip == t {
			l.trip, l.list = nil, nil
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
		t.flying = true
		l.win.Invalidate()
		after(time.Duration(float64(flightTime)*iconRevealPoint), func() {
			if !finished {
				reveal()
			}
		})
	}
	landed := false
	t.land = func() {
		if landed || finished {
			return
		}
		landed = true
		land()
		after(landingDelay, finish)
	}
	l.list, l.trip = list, t
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

// collapse is where a window with no card to land in goes: a small copy
// of it at the middle of the display's left edge.
func (f *flights) collapse(d *display, p pose) pose {
	work := winapi.MonitorInfo(d.mon.Handle).Work
	w, h := p.w/6, p.h/6
	return pose{x: float64(work.Left), y: float64(work.Top) + (float64(work.H())-h)/2, w: w, h: h}
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
	l.rect, l.scale, l.shown = area, d.mon.Scale(), true
	l.win.ShowInactive()
	winapi.SetWindowPos(l.hwnd, winapi.HwndTopmost, area.Left, area.Top, area.W(), area.H(), winapi.SwpNoActivate)
	return l
}

// hideLater hides a layer once it drew its last, empty frame, unless it
// shows something again meanwhile.
func (f *flights) hideLater(l *layer) {
	after(50*time.Millisecond, func() {
		if l.list == nil && l.drag == nil && l.shown {
			l.shown = false
			l.win.Hide()
		}
	})
}

// dragged shows the windows of a card being dragged where they are now.
func (f *flights) dragged(d *display, list []flight) {
	l := f.show(d)
	l.drag = list
	l.dragGen++
	l.win.Invalidate()
}

// endDrag takes a dragged card's windows away after delay, unless a switch
// took them over to fly them on, or a new drag began.
func (f *flights) endDrag(d *display, delay time.Duration) {
	if f == nil {
		return
	}
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
	for i, f := range l.drag {
		l.draw(c, i+len(l.list), f.still, f.from, 1)
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
		after(takeOffDelay, t.takeOff)
	case t.flying:
		// The clock starts with the first frame that moves, so a late
		// frame cannot eat into the flight and make it jump.
		if t.start.IsZero() {
			t.start = now
		}
		p = min(float64(now.Sub(t.start))/float64(flightTime), 1)
		if p < 1 {
			c.AnimationFrame()
		} else if !t.arrived {
			t.arrived = true
			post(t.land)
		}
	}
	e := flightEase(p)
	for i, f := range l.list {
		at := pose{
			x: lerp(f.from.x, f.to.x, e), y: lerp(f.from.y, f.to.y, e),
			w: lerp(f.from.w, f.to.w, e), h: lerp(f.from.h, f.to.h, e),
			tilt: f.from.tilt + (f.to.tilt-f.from.tilt)*float32(e),
		}
		op := float32(1)
		switch {
		case f.fadeIn:
			op = float32(min(p*3, 1))
		case f.fadeOut:
			op = float32(1 - e)
		}
		l.draw(c, i, f.still, at, op)
	}
}

// draw shows a window's still at a pose given in screen pixels. Its shadow
// is a card's while it is the size of one, and grows less than the window
// as it grows, as a window's own shadow does.
func (l *layer) draw(c *ui.Context, key int, still *ui.Bitmap, at pose, op float32) {
	x := float32((at.x - float64(l.rect.Left)) / l.scale)
	y := float32((at.y - float64(l.rect.Top)) / l.scale)
	w, h := float32(at.w/l.scale), float32(at.h/l.scale)
	radius := max(min(h*0.045, 8), 3)
	ui.Image(c.Key(key), still).Absolute().Left(x).Top(y).Size(w, h).
		Fit(ui.FillBox).Radius(radius).Shadow(0, min(h*0.05, h*0.02+4), min(h*0.16, h*0.04+14), 0, shadowSoft).
		TransformOrigin(0, 0.5).RotateY(at.tilt).Perspective(cardPerspective).Opacity(op)
}
