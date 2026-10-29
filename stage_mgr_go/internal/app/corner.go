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
	if !m.Contains(p) {
		return 0
	}
	// In DIPs right of the display's left edge and up from its bottom.
	x := float32(float64(p.X-m.Left) / scale)
	y := float32(float64(m.Bottom-p.Y) / scale)
	return nearness(rectDist(ui.Rect{W: float32(width), H: hotCorner}, x, y), cornerReach, cornerFull)
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
		near := cornerNearness(m, d.mon.Scale(), a.cornerWidth(), cur)
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
		if !h.visible || d != h.d {
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
	r := cornerRect(d.mon.Monitor, d.mon.Scale(), h.a.cornerWidth())
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
// lets go with the flash and pop of the strip's panel.
func (h *cornerHint) view(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	w, ht := c.Size()
	el := ui.Box(c.Key("corner")).Absolute().Left(0).Top(0).Size(w, ht)
	hovered, pressed := el.Hovered(), el.Pressed()
	if pressed && !h.pressing {
		h.open = h.d != nil && h.d.ov != nil && h.d.ov.phase != ovHidden
	}
	h.pressing = pressed
	var flash float32
	if el.Clicked() {
		h.click()
		flash = el.Jump("flash", 1)
		c.AnimationFrame()
	} else {
		flash = el.AnimateTo("flash", 0, panelFlash)
	}
	hot := el.AnimateTo("hot", b2f(hovered || pressed), fadeQuick)
	// A peek of the settings shows the whole mark, so that its size is
	// seen, as the strip shows its panel.
	shown := el.AnimateTo("shown", max(cornerFaint*h.near, b2f(hovered || pressed || time.Now().Before(h.peek))), fadeQuick)
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
