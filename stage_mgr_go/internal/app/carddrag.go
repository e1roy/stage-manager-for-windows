package app

import (
	"math"
	"slices"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo/ui"
)

// A card dragged out of a strip goes, once let go, where it was aimed at
// as it was last dragged: back to its place, to another place in its strip,
// onto the stage, or into the strip of another display. The strips that
// would take it make room for it as it is dragged, as on macOS.
type aim int

const (
	aimBack aim = iota // to its place, as when barely moved
	aimSort            // to slot at of its strip
	aimJoin            // onto the stage, beside the apps there
	aimMove            // to slot at of another display's strip
)

// cardDrag is the card being dragged out of a strip, if any, and where it
// would go. Once let go somewhere new it stays, done, until it is there,
// so that the room made for it holds until it fills it.
type cardDrag struct {
	from *display // the display whose strip it is dragged out of
	key  string   // its set's key
	aim  aim
	to   *display // the strip that makes room for it: from's, another's, or none
	at   int      // the slot there, counted in the strip as it is
	// out tells that it follows the pointer out of its strip, though it
	// may still go back there: it heads for the stage or another display.
	out  bool
	done bool
}

// sorts reports whether a card dragged as c stays in its strip: moved up
// or down it, or going back. A card is dragged as nothing before the first
// move.
func (c *cardDrag) sorts() bool {
	return c == nil || !c.out && (c.aim == aimBack || c.aim == aimSort)
}

// joinReach is how far toward the stage, as a share of the strip's width,
// a card is dragged before it goes on the stage when let go.
const joinReach = 0.75

// crossReach is how far, in DIPs, a card must be dragged toward a display
// beside its own before it leaves its strip for that display, following
// the pointer, and may go into the strip there. Two strips may meet at the
// edge between two displays, one on its right and one on its left: a card
// moved up or down one of them would otherwise slip into the other as the
// pointer strays across. It counts the drag, not how far past the edge
// the pointer is: two strips facing each other leave no room beyond the
// edge to go, and a drop just inside the other strip is the natural one.
const crossReach = 32

// moveDrop is how long the windows of a card let go in another display's
// strip stay on the layer: until the card shows under them, there.
const moveDrop = 80 * time.Millisecond

// cardDropAt returns the strip of another display than from that a card
// dragged as st, out of from's strip, would go into, let go at p, and the
// slot there; nil if none. A floating strip counts though it is out: the
// card over its band brings it in (strip.poll).
func (a *App) cardDropAt(from *display, st *cardState, p winapi.Point) (*display, int) {
	d := a.displayByHandle(winapi.MonitorFromPoint(p))
	if d == nil || d == from || !a.managed(d) || d.strip == nil || d.inOverview || !d.strip.rect.Contains(p) {
		return nil, 0
	}
	m := from.mon.Monitor
	if p.X >= m.Left && p.X < m.Right {
		// Across the top or bottom edge, the strips of two displays one
		// above the other run on beyond it: the pointer has room to go in.
		if float64(max(m.Top-p.Y, p.Y-m.Bottom+1)) < crossReach*from.mon.Scale() {
			return nil, 0
		}
	} else if math.Abs(float64(st.dx)) < crossReach {
		return nil, 0
	}
	s := d.strip
	return d, d.layout.slotAt(float32(float64(p.Y-s.rect.Top) / s.scale))
}

// aimCard works out where card g of display d's strip, dragged as st
// tells, would go if let go now.
func (a *App) aimCard(d *display, g *set, st *cardState) {
	c := &cardDrag{from: d, key: g.key}
	i := slices.IndexFunc(d.sets, func(s *set) bool { return s.key == g.key })
	cur := winapi.Cursor()
	switch to, at := a.cardDropAt(d, st, cur); {
	case to != nil:
		c.aim, c.to, c.at = aimMove, to, at
	case st.dx*turnSign(a.right(d)) > float32(a.settings.Width)*joinReach:
		// Toward the stage is rightward, or leftward from a strip on the
		// right.
		c.aim = aimJoin
	case d.strip != nil && !d.strip.rect.Contains(cur) && a.straightToStage(d, cur):
		// The pointer itself left the strip on the stage's side: the card
		// follows it out, rather than wait for the drag to add up to
		// joinReach, which on a narrow strip it does only once the pointer
		// is well past the neighbouring display's strip.
		c.aim = aimJoin
	case i >= 0 && i < len(d.layout.cards):
		b := d.layout.cards[i]
		c.aim, c.to, c.at = aimSort, d, d.layout.nearestSlot(b.Y+b.H/2+st.dy)
	}
	c.out = c.aim == aimMove || c.aim == aimJoin || a.headsAcross(d, st, cur)
	a.setCardDrag(c)
}

// headsAcross reports whether a card of display d's strip, dragged as st
// with the pointer at p, heads across the display's edge by the strip for
// the display beyond it: dragged crossReach that way. It then follows the
// pointer out of the strip ahead of reaching another, rather than stop at
// the strip's edge as if moved up or down it, and jump to the pointer once
// that is across.
func (a *App) headsAcross(d *display, st *cardState, p winapi.Point) bool {
	right := a.right(d)
	if st.dx*turnSign(right) > -crossReach {
		return false
	}
	m := d.mon.Monitor
	beyond := winapi.Point{X: m.Left - 1, Y: p.Y}
	if right {
		beyond.X = m.Right
	}
	return slices.ContainsFunc(a.displays, func(o *display) bool {
		return o != d && o.mon.Monitor.Contains(beyond)
	})
}

// straightToStage reports whether p, off a display's strip, is on the side
// the stage is on: away from the display's edge, not up or down beside the
// strip, where a card dragged to the strip of another display passes.
func (a *App) straightToStage(d *display, p winapi.Point) bool {
	s := d.strip
	// The pointer has to be on the display: a test strip, or one whose
	// window is not where the pointer is, tells nothing by it.
	if s == nil || !d.mon.Monitor.Contains(p) {
		return false
	}
	if a.right(d) {
		return p.X < s.rect.Left
	}
	return p.X >= s.rect.Right
}

// setCardDrag makes c the card dragged, or none. The strip of another
// display that makes room for it, or did, draws again: it hears nothing of
// a drag in another strip.
func (a *App) setCardDrag(c *cardDrag) {
	old := a.cardDrag
	if old != nil && c != nil && *old == *c {
		return
	}
	a.cardDrag = c
	for _, x := range []*cardDrag{old, c} {
		if x != nil && x.to != nil && x.to != x.from && x.to.strip != nil {
			x.to.strip.win.Invalidate()
		}
	}
}

// cardLanded ends the drag of a card let go somewhere new, once it is
// there or could not go: the strips stop making room for it.
func (a *App) cardLanded() {
	if c := a.cardDrag; c != nil && c.done {
		a.setCardDrag(nil)
	}
}

// drag follows card g of the strip as it is held, moved by dx and dy.
func (s *strip) drag(g *set, st *cardState, dx, dy float32) {
	a := s.a
	st.dx += dx
	st.dy += dy
	if !st.dragging {
		if st.dx*st.dx+st.dy*st.dy <= 64 {
			return
		}
		st.dragging, st.moved = true, true
		s.setDragging(true)
		// The card may leave the strip: the layers it would show on come
		// out once this frame is drawn, rather than hold up one mid-way.
		if a.flights != nil {
			post(a.flights.standByAll)
		}
	}
	a.aimCard(s.d, g, st)
}

// letGo sends card g of the strip, at box, where it was aimed at as it was
// let go.
func (s *strip) letGo(g *set, st *cardState, box ui.Rect) {
	a, d := s.a, s.d
	if st.dragging {
		c := a.cardDrag
		if c == nil || c.from != d || c.key != g.key {
			c = &cardDrag{from: d, key: g.key}
		}
		switch c.aim {
		case aimMove:
			// Into the strip of another display: the card moves there,
			// and its apps with it, without coming on a stage. It comes
			// in where it was let go.
			x, y, w, h := s.toScreen(ui.Rect{X: box.X + st.dx, Y: box.Y + st.dy, W: box.W * st.lift, H: box.H * st.lift})
			c.done = true
			a.requestMove(c.to, g, c.at, winapi.Rect{Left: int32(x), Top: int32(y), Right: int32(x + w), Bottom: int32(y + h)})
			a.flights.endDrag(c.to, moveDrop)
		case aimJoin:
			// The windows stay on the layer where they were let go until
			// the switch flies them on.
			a.setCardDrag(nil)
			d.dropPoses = st.poses
			a.request(actJoin, d, g)
			a.flights.endDrag(d, 800*time.Millisecond)
		default:
			a.flights.endDrag(d, 0)
			st.returning = true
			if c.aim == aimSort && slices.Index(d.sets, g) != c.at {
				c.done = true
				a.requestReorder(d, g, c.at)
			} else {
				a.setCardDrag(nil)
			}
		}
	}
	st.dragging = false
	st.dx, st.dy = 0, 0
	st.poses = nil
	s.setDragging(false)
}

// leaving reports whether card g of the strip was let go in another
// display's strip and is on its way there: it shows nothing meanwhile,
// rather than slide back first.
func (s *strip) leaving(g *set) bool {
	c := s.a.cardDrag
	return c != nil && c.done && c.aim == aimMove && c.from == s.d && c.key == g.key
}

// room is how the strip lays its cards out while one is dragged: as if it
// were where it would go already, the others closing up behind it and
// parting before it, or making room for one coming in from another
// display's strip. Where it would go is told by the strip as it is
// (aimCard), so that the cards making room cannot change it. It returns
// the slot of each card, how many slots there are, and the order to draw
// the cards in: the one held keeps its own slot, where its drag is
// measured from, till it is let go, and comes last, over the others.
func (s *strip) room() (slots []int, n int, order []int) {
	d := s.d
	held, from, to := -1, -1, -1
	if c := s.a.cardDrag; c != nil {
		i := slices.IndexFunc(d.sets, func(set *set) bool { return set.key == c.key })
		switch {
		case c.from != d:
			if c.to == d {
				to = c.at
			}
		case i < 0:
		default:
			if !c.done {
				held = i
			}
			if c.to == d {
				from, to = i, c.at
			}
		}
	}
	slots, n = makeRoom(len(d.sets), from, to)
	order = make([]int, 0, len(d.sets))
	for i := range d.sets {
		if i != held {
			order = append(order, i)
		}
	}
	if held >= 0 {
		slots[held] = held
		order = append(order, held)
	}
	return slots, n, order
}

// keepIn moves the offset of a card at box, lifted by lift, as little as
// it takes for the strip's window to show the whole of it.
func (s *strip) keepIn(box ui.Rect, dx, dy, lift float32) (float32, float32) {
	gx, gy := box.W*(lift-1), box.H*(lift-1)/2
	clamp := func(v, lo, hi float32) float32 {
		if lo > hi {
			return v
		}
		return min(max(v, lo), hi)
	}
	return clamp(dx, gx-box.X, s.d.viewW-box.W-gx-box.X), clamp(dy, gy-box.Y, s.d.viewH-box.H-gy-box.Y)
}

// holds reports whether the strip's window shows the whole of a card at
// box, moved by ox and oy and lifted by lift, with room for the lift
// either way.
func (s *strip) holds(box ui.Rect, ox, oy, lift float32) bool {
	gx, gy := box.W*(lift-1), box.H*(lift-1)/2
	x, y := box.X+ox, box.Y+oy
	return x-gx >= 0 && x+box.W+gx <= s.d.viewW && y-gy >= 0 && y+box.H+gy <= s.d.viewH
}

// requestMove moves the apps of a card into the strip of display to, at
// slot at, once the frame is built; its windows were let go at drop, on
// the screen. Tests set App.act to see the request instead.
func (a *App) requestMove(to *display, s *set, at int, drop winapi.Rect) {
	if a.act != nil {
		a.act(actMove, to, s)
		return
	}
	post(func() { a.moveSet(to, s, at, drop) })
}

// requestReorder moves a card of display d's strip to slot at, once the
// frame is built. Tests set App.act to see the request instead.
func (a *App) requestReorder(d *display, s *set, at int) {
	if a.act != nil {
		a.act(actReorder, d, s)
		return
	}
	post(func() { a.reorder(d, s, at) })
}

// reorder moves a card of display d's strip to slot at, where the strip
// already shows it.
func (a *App) reorder(d *display, s *set, at int) {
	defer a.cardLanded()
	debugf("reorder %q to slot %d on %s", s.title(), at, d.mon.Device)
	d.model.Move(s.key, at)
	a.arrange(d, a.groupsOf(d), time.Now())
	a.redraw()
}

// moveSet moves the apps of a card from the strip of their display into
// that of display to, at slot at, without putting them on any stage: their
// windows move to that display, where they show once the card is picked
// there, as if the user had moved them with Win+Shift+Arrow. Each keeps
// its place relative to the work area. An app already on the stage there
// takes its windows in. The card comes in from drop, where it was let go.
func (a *App) moveSet(to *display, s *set, at int, drop winapi.Rect) {
	defer a.cardLanded()
	if !a.ready || a.quitting || !a.managed(to) {
		return
	}
	a.windows = a.cat.Windows()
	from := a.displayOf(s.front())
	if from == nil || from == to || from.landingOf(s) {
		return
	}
	// Apps on their way onto the stage stay there.
	stay := a.heading(from)
	apps := slices.DeleteFunc(s.names(), func(app string) bool { return slices.Contains(stay, app) })
	if len(apps) == 0 {
		return
	}
	debugf("move %v from %s to %s, slot %d", apps, from.mon.Device, to.mon.Device, at)
	src, dst := from.mon.Work, to.mon.Work
	there := a.heading(to)
	var show []winapi.HWND
	for _, w := range a.windows {
		if !slices.Contains(apps, w.App) || a.displayOf(w) != from {
			continue
		}
		frame := a.home(w)
		f := winapi.FitInto(across(frame, src, dst), to.mon.Handle)
		dx, dy := f.Left-frame.Left, f.Top-frame.Top
		if r, ok := a.park.Home(w.HWND); ok {
			a.park.Rehome(w.HWND, r.Offset(dx, dy))
			if slices.Contains(there, w.App) {
				show = append(show, w.HWND)
			}
			continue
		}
		winapi.SetWindowPos(w.HWND, 0, w.Rect.Left+dx, w.Rect.Top+dy, 0, 0,
			winapi.SwpNoSize|winapi.SwpNoZOrder|winapi.SwpNoActivate)
	}
	a.park.Unpark(show...)
	a.windows = a.cat.Windows()
	now := time.Now()
	to.model.Insert(apps, at)
	a.arrange(from, a.groupsOf(from), now)
	a.arrange(to, a.groupsOf(to), now)
	to.card(apps[0]).arrive = drop
	a.redraw()
	a.refreshSoon()
}

// across is where rectangle r, in work area src, goes in work area dst:
// the same share of the way across and down.
func across(r, src, dst winapi.Rect) winapi.Rect {
	share := func(v, from, span, to, toSpan int32) int32 {
		if span <= 0 {
			return to
		}
		return to + int32(int64(v-from)*int64(toSpan)/int64(span))
	}
	x := share(r.Left, src.Left, src.W()-r.W(), dst.Left, dst.W()-r.W())
	y := share(r.Top, src.Top, src.H()-r.H(), dst.Top, dst.H()-r.H())
	return r.Offset(x-r.Left, y-r.Top)
}
