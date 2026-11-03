package app

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo/ui"
)

// lineup is the list of the windows on a display's stage at the top of its
// strip (settings.StageList). The strip shows what is off the stage; once
// the stage holds several windows, one in front hides the others, and the
// list keeps them in sight: a small live picture of each, the active one
// ringed, to bring forward with a click.
//
// The pictures are DWM thumbnails of the windows, drawn by DWM over the
// strip's own drawing: live, as the windows change, and costing the apps
// nothing. A still would ask each app to draw itself (PrintWindow), which
// some apps flash at, and be out of date at once.
type lineup struct {
	// order is the windows as listed, kept from frame to frame so that a
	// click, which changes the z-order, does not shuffle the list under
	// the pointer.
	order []winapi.HWND
	// known are the windows already listed, which new ones fade in among.
	known  map[winapi.HWND]bool
	thumbs map[winapi.HWND]*lineThumb
	// failed are windows DWM would not show, which keep the still or icon
	// the strip draws under where the picture would be.
	failed map[winapi.HWND]bool
	shown  bool // the list showed in the last frame
	// tints are the colors of the tiles of windows that show one
	// (tint.go), from their pictures as the screen shows them; sampling
	// is set while those are read, and wake is set to look again.
	tints    map[winapi.HWND]lineTint
	sampling bool
	wake     *time.Timer
}

// lineTint is the background and the edge of the tile of a window, of the
// color the window mostly shows.
type lineTint struct{ bg, edge ui.Color }

// A tile's color is read from its picture once DWM has drawn the picture
// in its place, and again now and then, as windows change what they show.
const (
	lineupSettle   = 250 * time.Millisecond
	lineupResample = 3 * time.Second
	tintFade       = 400 * time.Millisecond
)

// lineThumb is DWM's picture of a listed window, where it last put it.
type lineThumb struct {
	t         winapi.Thumbnail
	dest, src winapi.Rect
	op        float64
	visible   bool
	next      time.Time // when to read its colors again
}

// thumbAt is where a frame wants a window's picture: the area, in DIPs of
// the strip, it is fitted in, and how opaque it is.
type thumbAt struct {
	area ui.Rect
	op   float32
}

// stageWindows returns the windows on a display's stage, as its list shows
// them: by app, in the order the apps came on the stage, each app's
// windows in the order they were first listed and new ones after, front
// first. It returns none with the list turned off.
func (s *strip) stageWindows() []catalog.Window {
	if !s.a.settings.StageList || !s.a.ready {
		s.line.order = s.line.order[:0]
		return nil
	}
	pos := make(map[winapi.HWND]int, len(s.line.order))
	for i, h := range s.line.order {
		pos[h] = i
	}
	var out []catalog.Window
	for _, g := range s.d.onStage {
		ws := slices.Clone(g.windows)
		slices.SortStableFunc(ws, func(x, y catalog.Window) int {
			px, okx := pos[x.HWND]
			py, oky := pos[y.HWND]
			switch {
			case okx && oky:
				return px - py
			case okx:
				return -1
			case oky:
				return 1
			}
			return frontFirst(x, y)
		})
		out = append(out, ws...)
	}
	s.line.order = s.line.order[:0]
	for _, w := range out {
		s.line.order = append(s.line.order, w.HWND)
	}
	return out
}

// lineupFor lays out the list of a display's stage in a strip of the given
// size, as stageWindows lists it.
func (s *strip) lineupFor(width, height float32) lineupLayout {
	return layoutLineup(len(s.stageWindows()), width, height, s.right())
}

// activeOf is the window of the list to ring: the one the user is in, or,
// while they are in none of them, as in the taskbar, the front-most.
func (s *strip) activeOf(ws []catalog.Window) winapi.HWND {
	if h := s.a.fg.prev; slices.ContainsFunc(ws, func(w catalog.Window) bool { return w.HWND == h }) {
		return h
	}
	front := ws[0]
	for _, w := range ws[1:] {
		if w.Z < front.Z {
			front = w
		}
	}
	return front.HWND
}

// A tile is dark and see-through, as the strip's pills are; lighter under
// the pointer, and tinted with the accent while its window is the active one.
var (
	lineupTile    = ui.RGBA(18, 18, 22, 0.66)
	lineupTileHot = ui.RGBA(44, 44, 52, 0.8)
	lineupTileOn  = ui.RGBA(30, 30, 38, 0.82)
	lineupEdge    = ui.RGBA(255, 255, 255, 0.12)
	lineupMuted   = ui.RGBA(255, 255, 255, 0.62)
)

// lineupView draws the list of the windows on the stage, laid out in l,
// with dx the strip's slide in or out. It returns where it takes the
// pointer, empty where it does not show.
func (s *strip) lineupView(c *ui.Context, l lineupLayout, ws []catalog.Window, dx float32) ui.Rect {
	a, d := s.a, s.d
	want := map[winapi.HWND]thumbAt{}
	defer func() { s.placeThumbs(want, dx) }()
	if l.box.W <= 0 || d.inOverview {
		s.line.shown = false
		return ui.Rect{}
	}
	if !s.line.shown {
		// The list comes in as the stage gets its second window, every
		// tile fading in, rather than being there all at once.
		s.line.known = nil
	}
	s.line.shown = true
	accent, _ := a.accent()
	active := s.activeOf(ws)
	names := map[string]string{}
	for _, g := range d.onStage {
		names[g.app] = g.name
	}
	for i, t := range l.tiles {
		w := ws[i]
		op := s.lineTile(c, w, names[w.App], i, len(ws), t, l.thumbs[i], w.HWND == active, accent)
		want[w.HWND] = thumbAt{area: l.thumbs[i], op: op}
	}
	if l.more.W > 0 {
		s.lineMore(c, l.more, len(ws)-len(l.tiles))
	}
	listed := map[winapi.HWND]bool{}
	for _, w := range ws {
		listed[w.HWND] = true
	}
	s.line.known = listed
	if !s.shown {
		return ui.Rect{}
	}
	return l.box
}

// lineTile draws the tile of the i-th of n windows of the list at r, its
// picture to show at thumb, and handles its input. It returns how far it
// has faded in, which its picture follows.
func (s *strip) lineTile(c *ui.Context, w catalog.Window, name string, i, n int, r, thumb ui.Rect, active bool, accent ui.Color) float32 {
	a, d := s.a, s.d
	tr := a.tr()
	el := ui.Box(c.Key(w.HWND)).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Radius(8).Cursor(ui.CursorPointer)
	if !s.line.known[w.HWND] {
		el.Jump("op", 0)
	}
	op := el.AnimateTo("op", 1, fadeQuick)
	hovered := el.Hovered()
	hot := el.AnimateTo("hot", b2f(hovered), fadeQuick)
	on := el.AnimateTo("on", b2f(active), fadeQuick)
	// The tile takes its window's color once it is known, fading into it;
	// a window of none, or of none yet, keeps the plain one. The active
	// tile's edge is the accent either way.
	base, rim := lineupTile.Mix(lineupTileOn.Mix(accent, 0.3), on), lineupEdge
	if tint, ok := s.line.tints[w.HWND]; ok {
		base, rim = tint.bg, tint.edge
	}
	base, rim = fadeColor(el, "bg", base), fadeColor(el, "edge", rim)
	bg := base.Mix(ui.RGBA(255, 255, 255, float32(base.A)/255), 0.12*hot)
	edge := rim.Mix(accent, on)
	el.Background(bg).Border(1+on, edge).Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.3)).Opacity(op)
	title := w.Title
	if title == "" {
		title = name
	}
	// No tooltip: MyGo draws those in the window, where DWM's pictures
	// would cover them. The caption shows the title, and assistive
	// technology reads it whole.
	el.Description(title)

	// The menu and the close button act on this window, which a card's
	// request takes as a set of its own.
	one := func() *set {
		return newSet([]*group{{app: w.App, windows: []catalog.Window{w}}})
	}
	if el.Clicked() {
		debugf("lineup: %x %q clicked", w.HWND, title)
		a.request(actFront, d, one())
	}
	el.ContextMenu(func(m *ui.Menu) {
		if m.Item(tr.BringToFront).Chosen() {
			a.request(actFront, d, one())
		}
		if m.Item(tr.SendToStrip).Chosen() {
			a.request(actUnstage, d, one())
		}
		m.Separator()
		if m.Item(tr.CloseWindow).Chosen() {
			a.request(actClose, d, one())
		}
	})

	el.Children(func() {
		// The caption: the window's place in the list, as 1/2, its app's
		// icon and its title, and while the pointer is on the tile, a
		// button that closes the window. The button keeps to the caption's
		// end, out of the row: a title, which does not shrink below its
		// text, would push it off the tile.
		place := fmt.Sprintf("%d/%d", i+1, n)
		placeW := float32(8 + 6*len(place))
		row := r.W - 2*lineupInset - 2
		titleW := row - placeW - 5 - 14 - 5
		if hovered {
			titleW -= lineupCloseSize + 5
		}
		ui.Row(c).Absolute().Left(lineupInset+1).Top(0).Size(row, lineupCaption).
			AlignItems(ui.Center).Gap(5).Children(func() {
			ui.Row(c).Size(placeW, 14).Shrink(0).Radius(4).Center().
				Background(ui.RGBA(255, 255, 255, 0.14).Mix(accent, on)).Children(func() {
				ui.Text(c, place).FontSize(10).Bold().TextColor(white).MaxLines(1)
			})
			if icon := a.media.icons[w.App]; icon != nil {
				ui.Image(c, icon).Size(14, 14).Shrink(0)
			}
			text := lineupMuted.Mix(white, max(on, hot))
			t := ui.Text(c, title).FontSize(11).TextColor(text).MaxLines(1).MaxWidth(max(titleW, 0)).Shrink(1)
			if active {
				t.Bold()
			}
		})
		if hovered {
			b := s.lineClose(c, tr.CloseWindow, func() { a.request(actClose, d, one()) })
			b.Absolute().Left(r.W - lineupInset - 1 - lineupCloseSize).Top((lineupCaption - lineupCloseSize) / 2)
		}
		// Under the picture, which covers it, the still the window last
		// left, or its icon: what shows where DWM would not show it, and
		// in the headless tests.
		if s.line.thumbs[w.HWND] == nil {
			pic := thumb
			pic.X -= r.X
			pic.Y -= r.Y
			if st := a.media.still(w.HWND); st != nil {
				f := fitIn(pic, w.Aspect())
				ui.Image(c, st.card).Absolute().Left(f.X).Top(f.Y).Size(f.W, f.H).Fit(ui.FillBox).Radius(3)
			} else if icon := a.media.icons[w.App]; icon != nil {
				sz := min(pic.H*0.6, 28)
				ui.Image(c, icon).Absolute().Left(pic.X+(pic.W-sz)/2).Top(pic.Y+(pic.H-sz)/2).Size(sz, sz).Opacity(0.8)
			}
		}
	})
	return op
}

// lineupCloseSize is the size of the button that closes a window of the
// list.
const lineupCloseSize = 16

// lineClose draws the button that closes a window of the list, for the
// caller to place.
func (s *strip) lineClose(c *ui.Context, tip string, close func()) ui.Element {
	b := ui.Box(c.Key("close")).Size(lineupCloseSize, lineupCloseSize).Radius(lineupCloseSize / 2).Cursor(ui.CursorPointer).Description(tip)
	hot := b.AnimateTo("hot", b2f(b.Hovered()), fadeQuick)
	b.Background(ui.RGBA(255, 255, 255, 0.12).Mix(ui.RGBA(232, 17, 35, 0.95), hot))
	if b.Clicked() {
		close()
	}
	b.Draw(func(p *ui.Painter, r ui.Rect) {
		k := r.W * 0.3
		x0, y0, x1, y1 := r.X+k, r.Y+k, r.X+r.W-k, r.Y+r.H-k
		p.StrokePath(new(ui.Path).MoveTo(x0, y0).LineTo(x1, y1).MoveTo(x1, y0).LineTo(x0, y1), 1.4, white)
	})
	return b
}

// lineMore draws the line that counts the windows the list has no room
// for; a click spreads out every window of the display.
func (s *strip) lineMore(c *ui.Context, r ui.Rect, n int) {
	el := ui.Row(c.Key("more")).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Radius(8).
		AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
	hot := el.AnimateTo("hot", b2f(el.Hovered()), fadeQuick)
	el.Background(lineupTile.Mix(lineupTileHot, hot)).Border(1, lineupEdge)
	if el.Clicked() {
		s.a.requestOverview(s.d)
	}
	el.Children(func() {
		ui.Text(c, fmt.Sprintf(s.a.tr().StageListMore, n)).FontSize(11).TextColor(lineupMuted.Mix(white, hot)).MaxLines(1)
	})
}

// placeThumbs puts DWM's pictures of the listed windows where the frame
// wants them, dx DIPs across as the strip slides, and takes away those of
// windows no longer listed. A picture keeps to its window's shape, fitted
// in its area. DWM takes the changes in with the next frame it composes.
func (s *strip) placeThumbs(want map[winapi.HWND]thumbAt, dx float32) {
	if s.hwnd == 0 {
		return
	}
	if s.line.thumbs == nil {
		s.line.thumbs = map[winapi.HWND]*lineThumb{}
		s.line.failed = map[winapi.HWND]bool{}
	}
	for h, th := range s.line.thumbs {
		if _, ok := want[h]; ok {
			continue
		}
		if !slices.Contains(s.line.order, h) {
			th.t.Unregister()
			delete(s.line.thumbs, h)
			delete(s.line.tints, h)
		} else if th.visible {
			th.t.Hide()
			th.visible = false
		}
	}
	for h := range s.line.failed {
		if !slices.Contains(s.line.order, h) {
			delete(s.line.failed, h)
		}
	}
	px := func(v float32) int32 { return int32(math.Round(float64(v) * s.scale)) }
	for h, at := range want {
		th := s.line.thumbs[h]
		if th == nil {
			if s.line.failed[h] {
				continue
			}
			t, ok := winapi.RegisterThumbnail(s.hwnd, h)
			if !ok {
				debugf("lineup: no thumbnail of %x", h)
				s.line.failed[h] = true
				continue
			}
			th = &lineThumb{t: t}
			s.line.thumbs[h] = th
			// The still or icon under it goes in the next frame.
			s.win.Invalidate()
		}
		// The window's visible frame, without the borders it resizes by,
		// which are see-through.
		frame, win := winapi.FrameBounds(h), winapi.WindowRect(h)
		if frame.Empty() {
			frame = win
		}
		src := winapi.Rect{Left: frame.Left - win.Left, Top: frame.Top - win.Top, Right: frame.Right - win.Left, Bottom: frame.Bottom - win.Top}
		f := fitIn(at.area, float64(frame.W())/float64(max(frame.H(), 1)))
		dest := winapi.Rect{Left: px(f.X + dx), Top: px(f.Y), Right: px(f.X + dx + f.W), Bottom: px(f.Y + f.H)}
		op := float64(at.op)
		if th.visible && dest == th.dest && src == th.src && math.Abs(op-th.op) < 0.004 {
			continue
		}
		if dest != th.dest || src != th.src {
			// Moved or resized, it is read again once DWM has drawn it.
			th.next = time.Now().Add(lineupSettle)
		}
		th.t.Show(dest, src, op)
		th.dest, th.src, th.op, th.visible = dest, src, op, op > 0
	}
	s.sampleTints()
}

// sampleTints reads the colors of the windows of the list from their
// pictures, as the screen shows them in the strip, which is above other
// windows: what DWM drew, and nothing asked of the apps. It reads those
// due, on a goroutine, as a screen copy may wait for DWM's next frame, and
// has the strip draw again if a color changed. Not while windows fly, nor
// while the strip is away: something else would be where the pictures
// are.
func (s *strip) sampleTints() {
	if s.line.sampling || !s.visible || !s.shown || s.a.busy || s.d.inOverview {
		return
	}
	now := time.Now()
	type job struct {
		h winapi.HWND
		r winapi.Rect
	}
	var jobs []job
	var soonest time.Time
	for h, th := range s.line.thumbs {
		if !th.visible || th.op < 0.99 {
			continue
		}
		if now.Before(th.next) {
			if soonest.IsZero() || th.next.Before(soonest) {
				soonest = th.next
			}
			continue
		}
		jobs = append(jobs, job{h, th.dest.Offset(s.rect.Left, s.rect.Top)})
	}
	if !soonest.IsZero() && s.line.wake == nil {
		s.line.wake = after(soonest.Sub(now)+10*time.Millisecond, func() {
			s.line.wake = nil
			if s.win != nil {
				s.win.Invalidate()
			}
		})
	}
	if len(jobs) == 0 {
		return
	}
	s.line.sampling = true
	go func() {
		type found struct {
			h       winapi.HWND
			r, g, b float64
			ok      bool
		}
		var out []found
		for _, j := range jobs {
			img := winapi.CaptureScreen(j.r)
			if blank(img) {
				// The screen is locked, or the secure desktop is up: the
				// copy shows nothing of the window, which keeps its color.
				continue
			}
			r, g, b, ok := dominantColor(img)
			out = append(out, found{j.h, r, g, b, ok})
		}
		post(func() {
			s.line.sampling = false
			if s.line.tints == nil {
				s.line.tints = map[winapi.HWND]lineTint{}
			}
			changed := false
			for _, j := range jobs {
				if th := s.line.thumbs[j.h]; th != nil {
					th.next = time.Now().Add(lineupResample)
				}
			}
			for _, f := range out {
				if s.line.thumbs[f.h] == nil {
					continue
				}
				old, had := s.line.tints[f.h]
				if !f.ok {
					if had {
						delete(s.line.tints, f.h)
						changed = true
					}
					continue
				}
				var t lineTint
				t.bg, t.edge = tileTint(f.r, f.g, f.b)
				if !had || t != old {
					s.line.tints[f.h] = t
					changed = true
				}
			}
			if changed {
				debugf("lineup %s: tints %d", s.d.mon.Device, len(s.line.tints))
			}
			// Again, for those due, or the wake for the next.
			s.win.Invalidate()
		})
	}()
}

// fadeColor returns a color that goes to c, under key, as a tile's color
// changes.
func fadeColor(el ui.Element, key string, c ui.Color) ui.Color {
	a := ui.Animation{Duration: tintFade}
	r := el.AnimateTo(key+"r", float32(c.R), a)
	g := el.AnimateTo(key+"g", float32(c.G), a)
	b := el.AnimateTo(key+"b", float32(c.B), a)
	al := el.AnimateTo(key+"a", float32(c.A), a)
	u8 := func(v float32) uint8 { return uint8(min(max(v+0.5, 0), 255)) }
	return ui.RGBA(u8(r), u8(g), u8(b), float32(u8(al))/255)
}

// dropThumbs lets go of every picture of the list, as the strip closes.
func (s *strip) dropThumbs() {
	if s.line.wake != nil {
		s.line.wake.Stop()
		s.line.wake = nil
	}
	for h, th := range s.line.thumbs {
		th.t.Unregister()
		delete(s.line.thumbs, h)
	}
}

// overLineup reports whether a screen point is over the list of the
// stage's windows.
func (s *strip) overLineup(p winapi.Point) bool {
	r := s.lineupHit
	x := float32(float64(p.X-s.rect.Left) / s.scale)
	y := float32(float64(p.Y-s.rect.Top) / s.scale)
	return r.W > 0 && x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// bringForward brings a window of the stage in front of the others there.
func (a *App) bringForward(w catalog.Window) {
	winapi.Activate(w.HWND)
	a.redraw()
}

// unstage sends the app of a window of the stage to the strip, leaving the
// rest of the stage where it is.
func (a *App) unstage(d *display, s *set) {
	next := slices.DeleteFunc(a.heading(d), func(app string) bool { return app == s.key })
	a.switchStage(d, next, 0, len(next) > 0, false)
}
