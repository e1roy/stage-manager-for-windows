package app

import (
	"math"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	edgeWidth = 2 // pixels at a display's edge that reveal a floating strip
	autoHide  = 700 * time.Millisecond
	// slideTime is how long a floating strip takes to slide in or out at
	// the speed the settings start with: ui.Snappy's. The window hides a
	// little before the spring settles, once the strip is past the edge.
	slideTime    = 500 * time.Millisecond
	slideOutTime = 450 * time.Millisecond
)

// slide is the spring a floating strip slides in and out with, at the
// speed the settings give it.
func (a *App) slide() ui.Animation {
	m := ui.Snappy
	m.Spring.Duration = time.Duration(float64(slideTime) / a.settings.SlideSpeed)
	return m
}

// slideOut is how long after a floating strip starts out of sight its
// window hides.
func (a *App) slideOut() time.Duration {
	return time.Duration(float64(slideOutTime) / a.settings.SlideSpeed)
}

// strip is the window of one display's strip: transparent, above other
// windows, never activated, and letting clicks through wherever no card is.
// MyGo draws it; its place on the screen is set in physical pixels here.
type strip struct {
	a    *App
	d    *display
	win  *mygo.Window
	hwnd winapi.HWND
	bar  winapi.AppBar

	rect    winapi.Rect // where the window is
	placed  bool        // the side rect is on: true on the right
	scale   float64     // the display's scale factor
	visible bool        // the window shows
	shown   bool        // the strip is in (a floating strip slides out)

	clickThrough bool
	lastInside   time.Time
	holdUntil    time.Time
	hiddenAt     time.Time
	dragging     bool // a card is dragged
	holding      bool // the button went down on a card and is still down
	fullScreen   bool // a full-screen app is on the display
	lastBar      time.Time
	wasInside    bool
	edgeSince    time.Time // when the pointer came to the strip's edge
	hits         []ui.Rect // the cards, in DIPs of the window, as last drawn
	footerHit    ui.Rect   // the band the overview's button shows in, likewise
	// near is how far the overview's panel shows as the pointer comes
	// near it, 0 to 1. The poll measures it everywhere on the display,
	// where the strip lets the pointer through and so hears nothing of it.
	near float32
	// frames measures the strip's frames while its cards move, in debug
	// runs.
	frames frameStats
}

var appBarMessage uint32

func newStrip(a *App, d *display) *strip {
	s := &strip{a: a, d: d}
	s.win = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:    a.tray,
		Hidden:  true,
		Width:   int(a.settings.Width),
		Height:  400,
		Content: ui.View(s.view),
	})
	s.hwnd = winapi.HWND(s.win.NativeHandle())
	s.win.OnClose(func(e *mygo.CloseEvent) {
		// Escape closes flyouts; the strip stays.
		if !a.quitting {
			e.PreventDefault()
		}
	})
	// Layered from the start, so that letting clicks through later only
	// takes WS_EX_TRANSPARENT; the strip slides itself, without DWM's fades.
	winapi.SetLayered(s.hwnd)
	winapi.SetTransitionsDisabled(s.hwnd, true)
	if appBarMessage == 0 {
		appBarMessage = winapi.RegisterWindowMessage("Stage Manager.AppBar")
	}
	s.bar = winapi.AppBar{Wnd: s.hwnd, Callback: appBarMessage}
	winapi.Subclass(s.hwnd, s.message)
	s.shown = a.settings.Mode == settings.Fixed
	return s
}

// right reports whether the strip keeps to its display's right edge.
func (s *strip) right() bool { return s.a.right(s.d) }

// place puts the window where the mode and the side want it: in a band of
// the work area it reserves, or over that edge of the work area.
func (s *strip) place() {
	right := s.right()
	if s.bar.Registered() && right != s.placed {
		// The band on the other edge goes back first, so that the shell
		// does not keep both out of the work area.
		s.bar.Remove()
	}
	mon := winapi.MonitorInfo(s.d.mon.Handle)
	s.scale = mon.Scale()
	width := int32(math.Round(s.a.settings.Width * s.scale))
	var r winapi.Rect
	if s.a.settings.Mode == settings.Fixed {
		s.lastBar = time.Now()
		r = s.bar.Reserve(mon.Monitor, width, right)
		s.shown = true
	} else {
		if s.bar.Registered() {
			s.bar.Remove()
			mon = winapi.MonitorInfo(s.d.mon.Handle)
		}
		r = winapi.Rect{Left: mon.Work.Left, Top: mon.Work.Top, Right: mon.Work.Left + width, Bottom: mon.Work.Bottom}
		if right {
			r.Left, r.Right = mon.Work.Right-width, mon.Work.Right
		}
	}
	s.rect, s.placed = r, right
	debugf("strip %s placed %+v scale=%.2f shown=%v", s.d.mon.Device, r, s.scale, s.shown)
	if s.shown && !s.visible {
		s.win.ShowInactive()
		s.visible = true
	}
	s.applyBounds()
}

// applyBounds moves the window to s.rect.
func (s *strip) applyBounds() {
	r := s.rect
	z := winapi.HwndTopmost
	if s.fullScreen {
		z = winapi.HwndNoTopmost
	}
	winapi.SetWindowPos(s.hwnd, z, r.Left, r.Top, r.W(), r.H(), winapi.SwpNoActivate)
}

func (s *strip) relayout() { s.place() }

// message handles the shell's app bar notices.
func (s *strip) message(msg uint32, wParam, lParam uintptr) (uintptr, bool) {
	const wmEndSession = 0x0016
	if msg == wmEndSession && wParam != 0 {
		// Signing out or shutting down: windows go back before the
		// session ends, as they would on quitting.
		s.a.shutdown()
		return 0, false
	}
	if msg != appBarMessage || appBarMessage == 0 {
		return 0, false
	}
	switch wParam {
	case winapi.AbnPosChanged:
		// Only a real change of the taskbar or another bar needs a new
		// reservation; reserving on every notice makes the shell move the
		// taskbar around again.
		if s.a.settings.Mode == settings.Fixed && time.Since(s.lastBar) > 500*time.Millisecond {
			s.lastBar = time.Now()
			post(func() {
				if !s.a.quitting && s.d.strip == s {
					s.place()
				}
			})
		}
	case winapi.AbnFullScreenApp:
		// App bars step behind videos and games in full screen.
		s.fullScreen = lParam != 0
		post(s.applyBounds)
	}
	return 0, true
}

// poll follows the pointer: a floating strip comes out at its display's
// edge and goes once the pointer has left it, and clicks go through
// wherever no card is. While the overview shows the display's windows, the
// strip under it takes nothing.
func (s *strip) poll(cur winapi.Point, now time.Time) {
	inside := s.visible && !s.d.inOverview && s.rect.Contains(cur) && s.over(cur)
	if inside && !s.wasInside && s.a.cardDrag == nil && !s.a.busy {
		// The pointer reached the cards: a switch may follow, so the
		// windows on the stage get fresh stills to fly with now. Not while
		// a card is dragged over them: no switch follows that. Nor while
		// windows fly: capturing a window at full size has DWM draw it
		// whole, which holds up the frames of the flights; the switch that
		// follows takes what it lacks.
		s.a.freshenStage(s.d)
	}
	s.wasInside = inside
	if s.a.settings.Mode == settings.Floating {
		m := s.d.mon.Monitor
		x0 := m.Left
		if s.placed {
			x0 = m.Right - edgeWidth
		}
		if cur.X >= x0 && cur.X < x0+edgeWidth && cur.Y >= m.Top && cur.Y < m.Bottom {
			// Where another display lies beyond the edge, the pointer
			// crosses it on its way there: it has to rest a moment.
			if s.edgeSince.IsZero() {
				s.edgeSince = now
				debugf("strip %s: pointer at the edge, hard=%v", s.d.mon.Device, s.hardEdge(cur))
			}
			if s.hardEdge(cur) || now.Sub(s.edgeSince) > 250*time.Millisecond {
				s.lastInside = now
			}
		} else {
			s.edgeSince = time.Time{}
			// A window dragged over a strip that is out keeps it out,
			// for the window to be dropped in.
			if inside || s.shown && s.a.drag != nil && s.rect.Contains(cur) {
				s.lastInside = now
			}
		}
		// A card dragged over it from another display's strip brings it
		// out, edge or not.
		if c := s.a.cardDrag; c != nil && c.aim == aimMove && c.to == s.d {
			s.lastInside = now
		}
		want := now.Sub(s.lastInside) < autoHide || now.Before(s.holdUntil) || s.dragging ||
			(!s.d.covered && len(s.d.sets) > 0 && s.a.ready)
		s.setShown(want, now)
		if !s.shown && s.visible && now.Sub(s.hiddenAt) > s.a.slideOut() {
			s.win.Hide()
			s.visible = false
		}
	}
	// A press that began on a card keeps the window taking the pointer
	// until it goes up, wherever the pointer goes meanwhile: otherwise the
	// moves of a drag would go to the windows below.
	if !winapi.LeftButtonDown() {
		s.holding = false
	} else if inside {
		s.holding = true
	}
	// While it shows, the window takes the pointer over its whole band, not
	// only over a card: this poll runs every 16 ms, and a press that comes
	// in the tick the pointer reaches the strip would otherwise go to the
	// window below, leaving the card unpressed and so impossible to drag.
	// While the overview shows, it takes nothing, as the cards it shows do
	// not: the overview's window is what the pointer is on.
	band := s.visible && s.shown && !s.d.inOverview && s.rect.Contains(cur)
	s.setClickThrough(!(band || inside || s.dragging || s.holding))
	s.approach(cur)
}

// approach brings the overview's panel in as the pointer nears it, from
// the stage or the cards, before it reaches the band that takes clicks.
// The taskbar is left out: the pointer passes the strip's bottom corner on
// its way to Start. A fixed strip has reserved its band out of the work
// area, so its own rectangle counts besides.
func (s *strip) approach(cur winapi.Point) {
	d := s.d
	near := float32(0)
	on := d.mon.Work.Contains(cur) || s.rect.Contains(cur)
	if s.visible && s.shown && s.a.ready && !d.inOverview && d.viewW > 0 && on {
		x := float32(float64(cur.X-s.rect.Left) / s.scale)
		y := float32(float64(cur.Y-s.rect.Top) / s.scale)
		near = panelNearness(rectDist(footerButton(d.viewW, d.viewH, s.a.panel(), s.right()), x, y))
	}
	// Small steps on the way are left to the next; the ends are not, so
	// that it shows in full and goes away entirely.
	if near == s.near || near != 0 && near != 1 && math.Abs(float64(near-s.near)) < 0.03 {
		return
	}
	s.near = near
	s.win.Invalidate()
}

func (s *strip) setShown(v bool, now time.Time) {
	if v == s.shown {
		return
	}
	s.shown = v
	debugf("strip %s shown=%v groups=%d covered=%v", s.d.mon.Device, v, len(s.d.sets), s.d.covered)
	if v {
		if !s.visible {
			s.win.ShowInactive()
			s.visible = true
			s.applyBounds()
		}
	} else {
		s.hiddenAt = now
	}
	s.win.Invalidate()
}

func (s *strip) holdOpen(d time.Duration) {
	now := time.Now()
	s.holdUntil = now.Add(d)
	s.lastInside = now
	s.setShown(true, now)
}

func (s *strip) setClickThrough(v bool) {
	if v == s.clickThrough {
		return
	}
	s.clickThrough = v
	debugf("strip %s clickThrough=%v hits=%v", s.d.mon.Device, v, s.hits)
	if v {
		winapi.SetExStyleBits(s.hwnd, winapi.WsExTransparent, 0)
	} else {
		winapi.SetExStyleBits(s.hwnd, 0, winapi.WsExTransparent)
	}
}

// setDragging keeps the strip out and taking the pointer while a card is
// dragged.
func (s *strip) setDragging(v bool) { s.dragging = v }

// hardEdge reports whether no display lies beyond the strip's edge next
// to the point, so the pointer stops there.
func (s *strip) hardEdge(p winapi.Point) bool {
	x := p.X - 2
	if s.placed {
		x = p.X + 2
	}
	return !winapi.OnAnyMonitor(winapi.Rect{Left: x, Top: p.Y, Right: x + 1, Bottom: p.Y + 1})
}

// over reports whether a screen point is over a card, or the band of the
// overview's button: the strip takes the pointer there.
func (s *strip) over(p winapi.Point) bool {
	x := float32(float64(p.X-s.rect.Left) / s.scale)
	y := float32(float64(p.Y-s.rect.Top) / s.scale)
	const slop = 6
	for _, r := range s.hits {
		if x >= r.X-slop && x < r.X+r.W+slop && y >= r.Y-slop && y < r.Y+r.H+slop {
			return true
		}
	}
	f := s.footerHit
	return f.W > 0 && x >= f.X && x < f.X+f.W && y >= f.Y && y < f.Y+f.H
}

// cardBand is the band of the screen the cards take, in pixels: a window
// over it covers the strip, one lower on the screen does not.
func (s *strip) cardBand() winapi.Rect {
	cards := s.d.layout.cards
	if len(cards) == 0 {
		return winapi.Rect{}
	}
	top := cards[0].Y
	bottom := cards[len(cards)-1].Y + cards[len(cards)-1].H
	return winapi.Rect{
		Left:   s.rect.Left,
		Top:    s.rect.Top + int32(float64(top)*s.scale),
		Right:  s.rect.Right,
		Bottom: s.rect.Top + int32(float64(bottom)*s.scale),
	}
}

// toScreen converts a rectangle in DIPs of the strip window to pixels.
func (s *strip) toScreen(r ui.Rect) (x, y, w, h float64) {
	return float64(s.rect.Left) + float64(r.X)*s.scale, float64(s.rect.Top) + float64(r.Y)*s.scale,
		float64(r.W) * s.scale, float64(r.H) * s.scale
}

func (s *strip) close() {
	s.bar.Remove()
	winapi.Unsubclass(s.hwnd)
	s.win.Destroy()
}
