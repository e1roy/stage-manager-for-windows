package app

import (
	"math"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// hotCorner is the height, in DIPs, of the bottom-left corner of a display
// a click on which opens its overview, as a hot corner of macOS does on
// arrival: a Windows 11 taskbar's height, which a centered taskbar leaves
// empty. The pointer runs into a display's corner and stops there, so it
// takes no aim. A taskbar button's width is the user's (CornerWidth); a
// square one is the corner as it first was.
const hotCorner = 48

// cornerWidth is how wide, in DIPs, a display's corner button is: the
// user's, or a square as tall as a taskbar where the settings say nothing.
func (a *App) cornerWidth() float64 {
	if w := a.settings.CornerWidth; w > 0 {
		return w
	}
	return hotCorner
}

// The corner's mark begins to show with the pointer cornerReach DIPs from
// the corner and is in full cornerFull from it, as the panel at the bottom
// of the strip does. It reaches less far, so that it stays out of the way
// of that panel, just above it.
const (
	cornerReach = 40
	cornerFull  = 6
	// cornerFaint is how much the mark shows on the pointer's approach;
	// in the corner it shows in full, on a button's hover.
	cornerFaint = 0.6
	// cornerLinger keeps the window a moment after the pointer leaves, for
	// the mark to fade.
	cornerLinger = 250 * time.Millisecond
)

// cornerRect is where the corner button of a display of the given bounds
// and scale shows, in pixels: width DIPs wide and a taskbar's height tall.
func cornerRect(m winapi.Rect, scale, width float64) winapi.Rect {
	w := int32(math.Round(width * scale))
	h := int32(math.Round(hotCorner * scale))
	return winapi.Rect{Left: m.Left, Top: m.Bottom - h, Right: m.Left + w, Bottom: m.Bottom}
}

// cornerNearness is how far the corner's mark shows, 0 to 1, with the
// pointer at p: only from the corner's own display.
func cornerNearness(m winapi.Rect, scale, width float64, p winapi.Point) float32 {
	return boundsNearness(m, cornerRect(m, scale, width), scale, p)
}

// boundsNearness is how far the mark of a corner button at r shows, 0 to
// 1, with the pointer at p: only from the button's own display m.
func boundsNearness(m, r winapi.Rect, scale float64, p winapi.Point) float32 {
	if !m.Contains(p) {
		return 0
	}
	// In DIPs from the button's top-left corner.
	x := float32(float64(p.X-r.Left) / scale)
	y := float32(float64(p.Y-r.Top) / scale)
	b := ui.Rect{W: float32(float64(r.W()) / scale), H: float32(float64(r.H()) / scale)}
	return nearness(rectDist(b, x, y), cornerReach, cornerFull)
}

// The corner button joins the strip's panel above it into one button, as
// a strip on the left of its display brings its panel down to the
// taskbar, right above the corner: the two are lit, pressed and shown
// together, and drawn as one, flush with the display's edges, which the
// pointer runs into as it does into the corner. jointRadius rounds its
// corners, as a window's are on Windows 11.
const jointRadius = 8

// cornerFreeFor is how long the poll trusts what it last read of the
// taskbar's settings.
const cornerFreeFor = time.Second

// taskbarCornerFree is winapi.TaskbarCornerFree, read once a second at
// most: the poll asks for every display every 16 ms.
func (a *App) taskbarCornerFree(now time.Time) bool {
	if now.Sub(a.cornerChecked) > cornerFreeFor {
		a.cornerFree, a.cornerChecked = winapi.TaskbarCornerFree(), now
	}
	return a.cornerFree
}

// joinable reports whether the corner button of d joins its strip's
// panel: the strip shows on the display's left, ending at the top of a
// taskbar at the bottom that leaves its corner empty.
func (a *App) joinable(d *display, now time.Time) bool {
	s := d.strip
	if !a.ready || a.quitting || s == nil || !s.visible || !s.shown || s.right() {
		return false
	}
	m := d.mon.Monitor
	below := m.Bottom - s.rect.Bottom
	if s.rect.Left != m.Left || below <= 0 || float64(below) > 2*hotCorner*s.scale {
		return false
	}
	return a.taskbarCornerFree(now)
}

// setJoined joins the strip's panel to the corner button below it, or
// parts them.
func (s *strip) setJoined(v bool) {
	if v == s.joined {
		return
	}
	s.joined = v
	debugf("strip %s joined to its corner=%v", s.d.mon.Device, v)
	s.win.Invalidate()
}

// cornerHeight is how tall the corner button below a joined strip is, in
// DIPs: from the strip's bottom to the display's.
func (s *strip) cornerHeight() float32 {
	return float32(float64(s.d.mon.Monitor.Bottom-s.rect.Bottom) / s.scale)
}

// jointWidth is how wide a joined button is, in DIPs: as the panel last
// drew, or will once the strip is as wide as the settings say.
func (s *strip) jointWidth() float32 {
	w := s.d.viewW
	if w <= 0 {
		w = float32(s.a.settings.Width)
	}
	return jointButton(w, 0, s.a.panel()).W
}

// joint is the corner button joined to the strip's panel, while it shows.
func (s *strip) joint() *cornerHint {
	if h := s.a.hint; s.joined && h != nil && h.visible && h.d == s.d {
		return h
	}
	return nil
}

// footState records the pointer over the panel and the button down on
// it, and has the corner button joined to it draw them too.
func (s *strip) footState(hover, press bool) {
	if hover == s.footHover && press == s.footPress {
		return
	}
	s.footHover, s.footPress = hover, press
	if h := s.joint(); h != nil {
		h.redraw()
	}
}

// redraw draws the strip again, the panel joined to the corner button
// as the button changes: the window draws only as it is asked to. The
// headless tests draw it without one.
func (s *strip) redraw() {
	if s.win != nil {
		s.win.Invalidate()
	}
}

// redraw draws the corner button again, as the panel joined to it
// changes.
func (h *cornerHint) redraw() {
	if h.win != nil {
		h.win.Invalidate()
	}
}

// joint is the strip whose panel the corner button joins, while it does.
func (h *cornerHint) joint() *strip {
	if d := h.d; d != nil && d.strip != nil && d.strip.joined {
		return d.strip
	}
	return nil
}

// cornerBounds is where the corner button of d shows, in pixels: under a
// joined strip's panel and as wide, from the taskbar's top down; or as
// the settings size it.
func (a *App) cornerBounds(d *display) winapi.Rect {
	m := d.mon.Monitor
	if s := d.strip; s != nil && s.joined {
		// Wide enough for the panel's edge, which need not fall on a
		// pixel: the button draws it where the panel does.
		w := int32(math.Ceil(float64(s.jointWidth()) * s.scale))
		return winapi.Rect{Left: m.Left, Top: s.rect.Bottom, Right: m.Left + w, Bottom: m.Bottom}
	}
	return cornerRect(m, d.mon.Scale(), a.cornerWidth())
}

// flash is how far the overview's button of d still flashes after a
// click, 1 to 0, fading as the overview opens.
func (d *display) flash(now time.Time) float32 {
	t := now.Sub(d.flashAt)
	if d.flashAt.IsZero() || t < 0 || t >= panelFlash.Duration {
		return 0
	}
	k := 1 - float32(t)/float32(panelFlash.Duration)
	return k * k * k
}

// panelEdge is the border of the overview's panel, and of the corner
// button joined to it: faint at rest, lighter under the pointer, and
// blue, or the accent, pressed.
func panelEdge(hot, lit float32, accent ui.Color, outlined bool) ui.Color {
	edge := ui.RGBA(255, 255, 255, 0.16).Mix(ui.RGBA(255, 255, 255, 0.34), hot).Mix(ui.RGBA(150, 205, 255, 0.95), lit)
	if outlined {
		edge = edge.Mix(accent.Alpha(max(hot, lit)), max(hot, lit))
	}
	return edge
}

// cornerHint is the window over the hot corner of a display: it shows a
// mark there as the pointer nears, lights up as a taskbar button does
// under it, and takes the click, which the empty taskbar would swallow
// without a sign. It never takes the keyboard, so the window the user is
// in stays in front. One serves every display, as the pointer is near one
// corner at a time.
type cornerHint struct {
	a       *App
	win     *mygo.Window
	hwnd    winapi.HWND
	d       *display // the display whose corner it is over
	visible bool
	near    float32 // how near the pointer is, 0 to 1
	light   bool    // the taskbar is light, as the theme of Windows makes it
	// away is since when the pointer is away, while it shows, and peek is
	// till when the settings show it though the pointer is not near.
	away, peek time.Time
	// pressing is whether the button is down on it, and open whether the
	// overview showed as it went down: then the click closes it.
	pressing, open bool
	// hover is whether the pointer is over it, as the view last found;
	// rect is where it shows.
	hover bool
	rect  winapi.Rect
}

func newCornerHint(a *App) *cornerHint {
	h := &cornerHint{a: a}
	h.win = mygo.NewFlyout(mygo.FlyoutOptions{Tray: a.tray, Hidden: true, Width: int(a.cornerWidth()), Height: hotCorner, Content: ui.View(h.view)})
	h.hwnd = winapi.HWND(h.win.NativeHandle())
	h.win.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
		}
	})
	winapi.SetLayered(h.hwnd)
	winapi.SetTransitionsDisabled(h.hwnd, true)
	return h
}

// watchCorner shows the corner's mark as the pointer nears a hot corner,
// and puts it away once the pointer leaves. Main thread, from the poll.
func (a *App) watchCorner(cur winapi.Point, now time.Time) {
	d, near := a.nearCorner(cur)
	h := a.hint
	if h == nil {
		if d == nil {
			return
		}
		h = newCornerHint(a)
		a.hint = h
	}
	h.follow(d, near, now)
}

// nearCorner returns the display whose hot corner the pointer is near, and
// how near, if the taskbar is what shows there and leaves it empty: not a
// window over it, an app in full screen or the Start button.
func (a *App) nearCorner(cur winapi.Point) (*display, float32) {
	if !a.ready || a.quitting {
		return nil, 0
	}
	for _, d := range a.displays {
		if d.strip == nil {
			continue
		}
		m := d.mon.Monitor
		near := boundsNearness(m, a.cornerBounds(d), d.mon.Scale(), cur)
		if d.strip.joined && !d.inOverview {
			// It comes and goes with the panel it joins. While the
			// overview shows, the panel does not, and the button is
			// there to close it, on its own.
			near = d.strip.near
		}
		if near == 0 {
			continue
		}
		under := winapi.Root(winapi.WindowFromPoint(winapi.Point{X: m.Left, Y: m.Bottom - 1}))
		mine := a.hint != nil && under == a.hint.hwnd
		if !mine && !winapi.IsTaskbar(under) || !winapi.TaskbarCornerFree() {
			return nil, 0
		}
		return d, near
	}
	return nil, 0
}

// showCorner shows a display's corner button for a moment, as the settings
// size it, as showPanel does for the strip's panel: it stays hidden till
// the pointer comes near, so the size would not be seen otherwise.
func (a *App) showCorner() {
	if len(a.displays) == 0 || !winapi.TaskbarCornerFree() {
		return
	}
	d := a.displayByHandle(winapi.MonitorFromPoint(winapi.Cursor()))
	if d == nil || d.strip == nil {
		d = a.displays[0]
	}
	h := a.hint
	if h == nil {
		h = newCornerHint(a)
		a.hint = h
	}
	h.peek, h.near = time.Now().Add(panelPeek), 1
	h.show(d)
}

// follow moves the hint to the corner of d, or lets it fade and go when d
// is nil, and draws it as near as the pointer is.
func (h *cornerHint) follow(d *display, near float32, now time.Time) {
	// A peek of the settings keeps it where it left it, in full, till it
	// ends.
	if d == nil && h.d != nil && now.Before(h.peek) {
		d, near = h.d, 1
	}
	switch {
	case d != nil:
		h.away = time.Time{}
		// It follows a joined panel's width as the settings change it.
		if !h.visible || d != h.d || h.a.cornerBounds(d) != h.rect {
			h.show(d)
		}
	case h.visible && !h.pressing:
		if h.away.IsZero() {
			h.away = now
		} else if now.Sub(h.away) > cornerLinger {
			h.hide()
			return
		}
	}
	// As the strip's panel: small steps wait for the next, the ends do not.
	if near == h.near || near != 0 && near != 1 && math.Abs(float64(near-h.near)) < 0.03 {
		return
	}
	h.near = near
	if h.visible {
		h.win.Invalidate()
	}
}

// show puts the hint over the corner of d, above the taskbar.
func (h *cornerHint) show(d *display) {
	h.d = d
	h.light = winapi.TaskbarLight()
	r := h.a.cornerBounds(d)
	h.rect = r
	place := func() {
		winapi.SetWindowPos(h.hwnd, winapi.HwndTopmost, r.Left, r.Top, r.W(), r.H(), winapi.SwpNoActivate)
	}
	// Placed again once shown: moving onto a display of another scale,
	// the window is resized for it.
	place()
	if !h.visible {
		h.win.ShowInactive()
		h.visible = true
	}
	place()
	h.win.Invalidate()
}

func (h *cornerHint) hide() {
	h.win.Hide()
	h.visible, h.d, h.near, h.away = false, nil, 0, time.Time{}
	h.hover, h.pressing = false, false
}

// click opens the overview of the corner's display, or closes the
// overview if it showed as the button went down.
func (h *cornerHint) click() {
	d := h.d
	if d == nil {
		return
	}
	debugf("hot corner clicked on %s, overview open=%v", d.mon.Device, h.open)
	if !h.open {
		h.a.requestOverview(d)
		return
	}
	// The press outside the overview closes it already, as it does a
	// menu; this is in case it did not. Whatever closes one closes them
	// all, as Task View does.
	if o := d.ov; o != nil && o.d == d {
		post(func() { h.a.closeOverviews(nil) })
	}
}

// view draws the mark of the overview in the corner, as the taskbar draws
// its buttons: a faint mark on the pointer's approach; under it, a tile of
// the taskbar's hover, outlined in the blue of the overview so that the
// pointer is seen to be over it; pressed, the blue fills the tile, which
// lets go with the flash and pop of the strip's panel. Joined to the
// strip's panel, it draws the bottom of the one button (jointView).
func (h *cornerHint) view(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	w, ht := c.Size()
	el := ui.Box(c.Key("corner")).Absolute().Left(0).Top(0).Size(w, ht)
	hovered, pressed := el.Hovered(), el.Pressed()
	if pressed && !h.pressing {
		h.open = h.d != nil && h.d.ov != nil && h.d.ov.phase != ovHidden
	}
	s := h.joint()
	if s != nil && (hovered != h.hover || pressed != h.pressing) {
		s.redraw()
	}
	h.hover, h.pressing = hovered, pressed
	now := time.Now()
	var flash float32
	if d := h.d; d != nil {
		if el.Clicked() {
			h.click()
			d.flashAt = now
			if s != nil {
				s.redraw()
			}
		}
		flash = d.flash(now)
	}
	if flash > 0 {
		c.AnimationFrame()
	}
	if s != nil {
		h.jointView(c, el, s, w, ht, flash)
		return
	}
	hot := el.AnimateTo("hot", b2f(hovered || pressed), fadeQuick)
	// A peek of the settings shows the whole mark, so that its size is
	// seen, as the strip shows its panel.
	shown := el.AnimateTo("shown", max(cornerFaint*h.near, b2f(hovered || pressed || now.Before(h.peek))), fadeQuick)
	down := el.AnimateTo("down", b2f(pressed), panelPress)
	press := el.AnimateTo("press", 1-0.08*b2f(pressed), ui.Snappy)
	lit := max(down, flash)
	scale := press * (1 + 0.04*flash)
	tileBg, mark := ui.RGBA(255, 255, 255, 0.08), ui.RGB(255, 255, 255)
	if h.light {
		tileBg, mark = ui.RGBA(0, 0, 0, 0.06), ui.RGB(28, 28, 30)
	}
	accent, outlined := h.a.accent()
	const inset = 4
	el.Children(func() {
		tile := ui.Row(c).Absolute().Left(inset).Top(inset).Size(w-2*inset, ht-2*inset).Center().Radius(4).
			Background(tileBg.Alpha(hot).Mix(accent, lit)).Scale(scale, scale)
		if outlined {
			tile.Border(1, accent.Alpha(max(hot, lit)))
		}
		tile.Children(func() {
			ui.Box(c).Opacity(max(shown, lit)).Children(func() {
				overviewGlyph(c, 16, mark.Mix(white, lit))
			})
		})
	})
}

// jointView draws the corner button as the bottom of one button with the
// strip's panel above it: the panel's colors, over the taskbar whatever
// its theme, lit as either half is under the pointer or pressed, and
// shown as the panel is. It holds still on a press, as the panel does
// joined: the two would part were either to shrink. While the overview
// shows, the panel is gone, and it is a button on its own.
func (h *cornerHint) jointView(c *ui.Context, el ui.Element, s *strip, w, ht, flash float32) {
	a, d := h.a, h.d
	hovered := h.hover || s.footHover
	pressed := h.pressing || s.footPress
	alone := d.inOverview
	now := time.Now()
	shown := el.AnimateTo("shown", max(h.near, b2f(hovered || pressed || now.Before(h.peek) || now.Before(a.panelUntil))), fadeQuick)
	hot := el.AnimateTo("hot", b2f(hovered), fadeQuick)
	down := el.AnimateTo("down", b2f(pressed), panelPress)
	lit := max(down, flash)
	op := max(shown, flash)
	accent, outlined := a.accent()
	bg := panelFill(float32(a.settings.PanelOpacity), hot).Mix(accent, lit)
	edge := panelEdge(hot, lit, accent, outlined)
	p := a.panel()
	bw := s.jointWidth()
	// The mark as the panel draws it, sized and placed for the two.
	g := min(18, (p.h+ht)*0.42, bw*0.6)
	gh := g * 0.78
	y := jointGlyph(p.h, ht, gh) - p.h
	if alone {
		g = min(18, ht*0.42, bw*0.6)
		gh = g * 0.78
		y = (ht - gh) / 2
	}
	radii := [4]float32{0, 0, jointRadius, jointRadius}
	if alone {
		radii = [4]float32{jointRadius, jointRadius, jointRadius, jointRadius}
	}
	el.Children(func() {
		// The bottom of the glass of the whole button; alone, it shows
		// what it can of it.
		if b := s.glass.bmp; b != nil {
			ui.Box(c).Absolute().Left(0).Top(0).Size(bw, ht).Radius(radii[:]...).Clip().Opacity(op).Children(func() {
				top := p.h
				if alone {
					top = 0
				}
				ui.Image(c, b).Absolute().Left(0).Top(-top).Size(bw, top+ht).Fit(ui.FillBox)
			})
		}
		pane := ui.Box(c).Absolute().Left(0).Top(0).Size(bw, ht).Background(bg).BorderColor(edge).Opacity(op).Radius(radii[:]...)
		if alone {
			pane.BorderWidth(1)
		} else {
			// The bottom of the one outline: none toward the panel.
			pane.BorderWidth(0, 1, 1, 1)
		}
		if y >= 0 && g >= 8 {
			ui.Box(c).Absolute().Left((bw - g) / 2).Top(y).Opacity(op).Children(func() {
				overviewGlyph(c, g, white)
			})
		}
	})
}
