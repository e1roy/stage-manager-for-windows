package app

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/e1roy/stage_manager_for_windows/internal/i18n"
	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

// cardState is what a card remembers between frames.
type cardState struct {
	dx, dy   float32 // how far it is dragged
	dragging bool
	moved    bool // the press became a drag, so it is no click
	// poses are where the card's windows show on the flight layer while
	// it is dragged, and returning that it is on its way back.
	poses     map[winapi.HWND]pose
	returning bool
	// tilt and lift are how the card's windows showed in the last frame,
	// for windows flying out of it to start from.
	tilt, lift float32
}

func (d *display) card(key string) *cardState {
	st := d.cards[key]
	if st == nil {
		st = &cardState{}
		d.cards[key] = st
	}
	return st
}

// landingOf reports whether windows are flying into a card.
func (d *display) landingOf(s *set) bool {
	for _, g := range s.apps {
		if d.landing[g.app] {
			return true
		}
	}
	return false
}

var (
	white      = ui.RGB(255, 255, 255)
	pillColor  = ui.RGBA(18, 18, 22, 0.72)
	shadowSoft = ui.RGBA(0, 0, 0, 0.34)
	// Cards slide to their places and come in from the left. They leave
	// at once: a card that goes is either flying to the stage, where its
	// flight takes over, or an app that closed.
	cardMotion = ui.ElementTransition{
		Animation: &ui.Snappy,
		Enter:     &ui.Motion{X: -36, Scale: 0.86},
	}
	fadeQuick = ui.Animation{Duration: 160 * time.Millisecond}
)

// view draws a strip: the cards of the apps off the stage, over nothing.
func (s *strip) view(c *ui.Context) {
	a, d := s.a, s.d
	c.Root().Background(ui.Transparent)
	w, h := c.Size()
	d.viewW, d.viewH = w, h
	l := layoutStrip(len(d.sets), w, h, d.scroll, float32(a.settings.Spacing))
	if d.scroll > l.maxScroll(h) {
		d.scroll = l.maxScroll(h)
		l = layoutStrip(len(d.sets), w, h, d.scroll, float32(a.settings.Spacing))
	}
	d.layout = l

	root := ui.Box(c).Absolute().Left(0).Top(0).Size(w, h)
	out := float32(0)
	if !s.shown {
		out = -(w + 32)
	}
	root.Translate(root.AnimateTo("reveal", out, ui.Snappy), 0)
	root.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputScroll {
			return false
		}
		d.scroll = min(max(d.scroll+ev.DY, 0), d.layout.maxScroll(d.viewH))
		return true
	})
	hits := s.hits[:0]
	root.Children(func() {
		if len(d.sets) == 0 && a.ready {
			emptyHint(c, a.tr(), w, h)
		}
		for i, set := range d.sets {
			if b := s.card(c, set, l.cards[i]); b.W > 0 {
				hits = append(hits, b)
			}
		}
	})
	s.hits = hits
}

func emptyHint(c *ui.Context, tr *i18n.Strings, w, h float32) {
	ui.Column(c).Absolute().Left(0).Top(h/2-30).Size(w, 60).Center().Gap(4).Children(func() {
		ui.Text(c, tr.EmptyStrip).FontSize(13).TextColor(ui.RGBA(255, 255, 255, 0.78)).Bold()
		ui.Text(c, tr.EmptyStripHint).FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.55))
	})
}

// card draws the card of a set of apps and handles its input. It returns
// where the card showed, in DIPs of the window, for hit testing.
func (s *strip) card(c *ui.Context, g *set, box ui.Rect) ui.Rect {
	a, d := s.a, s.d
	st := d.card(g.key)
	// A card that windows are flying into shows nothing until they land.
	// It enters where it lands rather than sliding in, so that it is in
	// place when they get there.
	landing := d.landingOf(g)
	motion := cardMotion
	if landing {
		motion.Enter = nil
	}
	el := ui.Box(c.Key(g.key)).Absolute().Left(box.X).Top(box.Y).Size(box.W, box.H).
		Cursor(ui.CursorPointer).Transition(motion)

	// Dragging a card out onto the desktop puts its app on the stage too.
	// Dragged tells whether the card is held, wherever the pointer is;
	// Pressed only while it is over the card.
	dx, dy, held := el.Dragged()
	if held {
		st.dx += dx
		st.dy += dy
		if !st.dragging && st.dx*st.dx+st.dy*st.dy > 64 {
			st.dragging, st.moved = true, true
			s.setDragging(true)
		}
	} else if st.dragging || st.dx != 0 || st.dy != 0 {
		if st.dragging && st.dx > float32(a.settings.Width)*0.75 {
			// The windows stay on the layer where they were let go until
			// the switch flies them on.
			d.dropPoses = st.poses
			a.request(actJoin, d, g)
			a.flights.endDrag(d, 800*time.Millisecond)
		} else if st.dragging {
			a.flights.endDrag(d, 0)
			st.returning = true
		}
		st.dragging = false
		st.dx, st.dy = 0, 0
		st.poses = nil
		s.setDragging(false)
	}
	if el.Clicked() && !st.moved {
		debugf("card %q clicked", g.title())
		// Shift-click adds the card to the stage, as on macOS.
		if el.ClickModifiers()&ui.Shift != 0 && !d.model.Empty() {
			a.request(actJoin, d, g)
		} else {
			a.request(actPick, d, g)
		}
	}
	if !held {
		st.moved = false
	}

	// A card being landed in holds still for its flights, but takes clicks:
	// one turns them back.
	hovered := (el.Hovered() || st.dragging) && !landing
	if hovered && !st.dragging {
		// A switch may follow: take the pictures its flights need now, at
		// the size the card cannot show.
		a.media.preview(g.windows)
	}
	tilt, hoverTilt := cardTilts(a.settings.Tilt)
	if hovered {
		tilt = hoverTilt
	}
	tilt = el.AnimateTo("tilt", tilt, ui.Snappy)
	lift := float32(1)
	switch {
	case held && !st.moved:
		lift = 0.96
	case st.dragging:
		lift = 1.08
	case hovered:
		lift = 1.045
	}
	lift = el.AnimateTo("lift", lift, ui.Snappy)
	st.tilt, st.lift = tilt, lift
	var ox, oy float32
	if st.dragging {
		ox, oy = el.Jump("dx", st.dx), el.Jump("dy", st.dy)
	} else {
		ox, oy = el.AnimateTo("dx", 0, ui.Smooth), el.AnimateTo("dy", 0, ui.Smooth)
	}
	// The flight covers the card as it lands, so the card shows at once
	// underneath; fading it in once the flight is gone would blink. The
	// icons come in on the flight layer, above the flights.
	shown, iconOp := float32(1), float32(1)
	if landing {
		shown, iconOp = 0, 0
	}
	op := float32(1)
	if st.dragging && a.flights != nil {
		// Dragged, the card shows on the flight layer, which reaches
		// beyond the strip; here it leaves its place empty.
		if st.poses = s.dragPoses(g, box, ox, oy, lift, tilt); len(st.poses) > 0 {
			op = 0
		}
	}
	if st.returning && el.Settled("dx") && el.Settled("dy") {
		st.returning = false
	}
	el.Translate(ox, oy).Opacity(op)

	el.ContextMenu(func(m *ui.Menu) {
		tr := a.tr()
		if m.Item(tr.PickStage).Chosen() {
			a.request(actPick, d, g)
		}
		if !d.model.Empty() && m.Item(tr.JoinStage).Chosen() {
			a.request(actJoin, d, g)
		}
		if len(g.apps) > 1 && m.Item(tr.SplitSet).Chosen() {
			a.request(actSplit, d, g)
		}
		// Companions are picked here, from the apps at hand, rather than
		// typed into the settings by the name of their executable.
		companion := func(m *ui.Menu, label string, app *group) {
			if exe := app.front().Exe; exe != "" && m.Item(label).Checked(a.settings.Companion(exe)).Chosen() {
				a.request(actCompanion, d, newSet([]*group{app}))
			}
		}
		if len(g.apps) == 1 {
			companion(m, tr.CompanionLabel, g.apps[0])
		} else {
			m.Submenu(tr.CompanionLabel, func(m *ui.Menu) {
				for _, app := range g.apps {
					companion(m, app.name, app)
				}
			})
		}
		m.Separator()
		label := tr.CloseWindow
		if len(g.windows) > 1 {
			label = tr.CloseWindows
		}
		if m.Item(label).Chosen() {
			a.request(actClose, d, g)
		}
	})

	local := ui.Rect{W: box.W, H: box.H}
	stack := g.stack(local)
	el.Children(func() {
		windows := ui.Box(c).Absolute().Left(0).Top(0).Size(box.W, box.H).
			TransformOrigin(0, 0.5).Scale(lift, lift).RotateY(tilt).Perspective(cardPerspective).Opacity(shown)
		windows.Children(func() {
			for i := len(stack) - 1; i >= 0; i-- {
				s.windowStill(c, g, i, stack[i])
			}
		})
		// The icons of a set's apps overlap a little, the first on top.
		row := iconRow(stack[0], local, len(g.apps), float32(a.settings.IconScale))
		ir := row[len(row)-1]
		overlay := ui.Box(c).Absolute().Left(0).Top(0).Size(box.W, box.H).TransformOrigin(0, 0.5).Scale(lift, lift).Opacity(iconOp)
		overlay.Children(func() {
			for i := len(row) - 1; i >= 0; i-- {
				if icon := a.media.icons[g.apps[i].app]; icon != nil {
					r := row[i]
					ui.Image(c.Key(g.apps[i].app), icon).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H)
				}
			}
			right := ir.X + ir.W
			label := ui.Text(c, g.title()).FontSize(12).Bold().TextColor(white).MaxLines(1).
				Absolute().Left(right+4).Top(ir.Y+ir.H-24).MaxWidth(max(box.W-right, 60)).
				Background(pillColor).Padding(3, 9).Radius(11)
			op := float32(0)
			if hovered && !st.dragging {
				op = 1
			}
			label.Opacity(label.AnimateTo("label", op, fadeQuick))
		})
	})
	if !s.shown {
		return ui.Rect{}
	}
	return el.Bounds()
}

// dragPoses puts the stills of a dragged card's windows on the flight
// layer where the card shows, offset by the drag, and returns where.
func (s *strip) dragPoses(g *set, box ui.Rect, ox, oy, lift, tilt float32) map[winapi.HWND]pose {
	card := ui.Rect{X: box.X + ox, Y: box.Y + oy, W: box.W, H: box.H}
	stack := g.stack(card)
	poses := map[winapi.HWND]pose{}
	var list []flight
	for i := len(stack) - 1; i >= 0; i-- {
		w := g.windows[i]
		st := s.a.media.still(w.HWND)
		if st == nil {
			continue
		}
		x, y, pw, ph := s.toScreen(lifted(card, stack[i], lift))
		p := pose{x: x, y: y, w: pw, h: ph, tilt: tilt}
		poses[w.HWND] = p
		list = append(list, flight{still: st.fly, from: p})
	}
	if len(list) > 0 {
		s.a.flights.dragged(s.d, list)
	}
	return poses
}

// windowStill draws the i-th window of a card: its still, or while it has
// none, a frosted pane with its app's icon.
func (s *strip) windowStill(c *ui.Context, g *set, i int, r ui.Rect) {
	w := g.windows[i]
	radius := max(r.H*0.045, 3)
	if st := s.a.media.still(w.HWND); st != nil {
		ui.Image(c.Key(w.HWND), st.card).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).
			Fit(ui.FillBox).Radius(radius).Shadow(0, r.H*0.05, r.H*0.16, 0, shadowSoft)
		return
	}
	pane := ui.Column(c.Key(w.HWND)).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Radius(radius).
		Background(ui.RGBA(250, 250, 252, 0.2)).Border(1, ui.RGBA(255, 255, 255, 0.3)).
		Shadow(0, r.H*0.05, r.H*0.16, 0, shadowSoft).Center().Gap(4).Padding(6)
	pane.Children(func() {
		if icon := s.a.media.icons[w.App]; icon != nil && i < len(g.apps) {
			sz := min(r.H*0.38, 48)
			ui.Image(c, icon).Size(sz, sz).Opacity(0.85)
		}
		if i < len(g.apps) && r.H > 60 {
			ui.Text(c, w.Title).FontSize(10).TextColor(ui.RGBA(255, 255, 255, 0.8)).MaxLines(1).TextAlign(ui.Center)
		}
	})
}

// isFloating reports whether strips float over the desktop.
func (a *App) isFloating() bool { return a.settings.Mode == settings.Floating }
