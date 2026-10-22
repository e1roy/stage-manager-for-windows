package app

import (
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/renkai9418/stage-bar/internal/settings"
	"github.com/renkai9418/stage-bar/internal/winapi"
)

const (
	edgeWidth    = 2 // pixels at a display's left edge that reveal a floating strip
	autoHide     = 700 * time.Millisecond
	slideOutTime = 450 * time.Millisecond
)

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
	scale   float64     // the display's scale factor
	visible bool        // the window shows
	shown   bool        // the strip is in (a floating strip slides out)

	clickThrough bool
	lastInside   time.Time
	holdUntil    time.Time
	hiddenAt     time.Time
	dragging     bool // a card is dragged
	dismissed    bool // hidden by the shortcut: only the edge brings it back
	holding      bool // the button went down on a card and is still down
	fullScreen   bool // a full-screen app is on the display
	lastBar      time.Time
	wasInside    bool
	edgeSince    time.Time // when the pointer came to the left edge
	hits         []ui.Rect // the cards, in DIPs of the window, as last drawn
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
		appBarMessage = winapi.RegisterWindowMessage("StageBar.AppBar")
	}
	s.bar = winapi.AppBar{Wnd: s.hwnd, Callback: appBarMessage}
	winapi.Subclass(s.hwnd, s.message)
	s.shown = a.settings.Mode == settings.Fixed
	return s
}

// place puts the window where the mode wants it: in a band of the work
// area it reserves, or over the left edge of the work area.
func (s *strip) place() {
	mon := winapi.MonitorInfo(s.d.mon.Handle)
	s.scale = mon.Scale()
	width := int32(math.Round(s.a.settings.Width * s.scale))
	var r winapi.Rect
	if s.a.settings.Mode == settings.Fixed {
		s.lastBar = time.Now()
		r = s.bar.Reserve(mon.Monitor, width)
		s.shown = true
	} else {
		if s.bar.Registered() {
			s.bar.Remove()
			mon = winapi.MonitorInfo(s.d.mon.Handle)
		}
		r = winapi.Rect{Left: mon.Work.Left, Top: mon.Work.Top, Right: mon.Work.Left + width, Bottom: mon.Work.Bottom}
	}
	s.rect = r
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

// poll follows the pointer: a floating strip comes out at the display's
// left edge and goes once the pointer has left it, and clicks go through
// wherever no card is.
func (s *strip) poll(cur winapi.Point, now time.Time) {
	inside := s.visible && s.rect.Contains(cur) && s.overCard(cur)
	if inside && !s.wasInside {
		// The pointer reached the cards: a switch may follow, so the
		// windows on the stage get fresh stills to fly with now.
		s.a.freshenStage(s.d)
	}
	s.wasInside = inside
	if s.a.settings.Mode == settings.Floating {
		m := s.d.mon.Monitor
		if cur.X >= m.Left && cur.X < m.Left+edgeWidth && cur.Y >= m.Top && cur.Y < m.Bottom {
			// Where another display lies to the left, the pointer crosses
			// this edge on its way there: it has to rest a moment.
			if s.edgeSince.IsZero() {
				s.edgeSince = now
				debugf("strip %s: pointer at the edge, hard=%v", s.d.mon.Device, s.hardEdge(cur))
			}
			if s.hardEdge(cur) || now.Sub(s.edgeSince) > 250*time.Millisecond {
				s.lastInside = now
				s.dismissed = false
			}
		} else {
			s.edgeSince = time.Time{}
			if inside {
				s.lastInside = now
			}
		}
		want := now.Sub(s.lastInside) < autoHide || now.Before(s.holdUntil) || s.dragging ||
			(!s.dismissed && !s.d.covered && len(s.d.groups) > 0 && s.a.ready)
		s.setShown(want, now)
		if !s.shown && s.visible && now.Sub(s.hiddenAt) > slideOutTime {
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
	s.setClickThrough(!(inside || s.dragging || s.holding))
}

func (s *strip) setShown(v bool, now time.Time) {
	if v == s.shown {
		return
	}
	s.shown = v
	debugf("strip %s shown=%v groups=%d covered=%v", s.d.mon.Device, v, len(s.d.groups), s.d.covered)
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

// toggle shows or hides a floating strip, from the shortcut.
func (s *strip) toggle() {
	now := time.Now()
	if s.a.settings.Mode == settings.Fixed {
		return
	}
	if s.shown {
		s.lastInside, s.holdUntil = time.Time{}, time.Time{}
		s.dismissed = true
		s.setShown(false, now)
		return
	}
	s.dismissed = false
	s.holdOpen(3 * time.Second)
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
// dragged; the flight layer shows the card beyond the strip's edge.
func (s *strip) setDragging(v bool) { s.dragging = v }

// hardEdge reports whether no display lies left of the point, so the
// pointer stops at the edge.
func (s *strip) hardEdge(p winapi.Point) bool {
	return !winapi.OnAnyMonitor(winapi.Rect{Left: p.X - 2, Top: p.Y, Right: p.X - 1, Bottom: p.Y + 1})
}

// overCard reports whether a screen point is over a card.
func (s *strip) overCard(p winapi.Point) bool {
	x := float32(float64(p.X-s.rect.Left) / s.scale)
	y := float32(float64(p.Y-s.rect.Top) / s.scale)
	const slop = 6
	for _, r := range s.hits {
		if x >= r.X-slop && x < r.X+r.W+slop && y >= r.Y-slop && y < r.Y+r.H+slop {
			return true
		}
	}
	return false
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
