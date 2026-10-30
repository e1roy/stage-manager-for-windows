package app

import (
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/i18n"
	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo/ui"
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
	// ox is how far across the card showed moved in the last frame, and
	// follow how it followed the pointer then: not at all, kept in the
	// strip, or out of it.
	ox     float32
	follow follow
	// tilt and lift are how the card's windows showed in the last frame,
	// for windows flying out of it to start from.
	tilt, lift float32
	// arrive is where, on the screen, a card dragged in from another
	// display's strip was let go, for it to settle into its slot from
	// there rather than come in from the edge.
	arrive winapi.Rect
}

type follow int8

const (
	followNone follow = iota
	followIn
	followOut
)

// catchUp is the spring that takes up the gap between a dragged card and
// the pointer as the card leaves its strip, which held it, or comes back:
// quick, for the card is under the pointer again before it goes far. The
// gap starts from rest: what velocity Jump would make of it, jumping, is
// none the card had.
var catchUp = ui.Animation{Spring: ui.SpringOptions{Duration: 200 * time.Millisecond, Velocity: 1e-6}}

func (d *display) card(key string) *cardState {
	st := d.cards[key]
	if st == nil {
		st = &cardState{}
		d.cards[key] = st
	}
	return st
}

// landingOf reports whether windows are flying into a card, or are out in
// the overview: either way it holds still and shows nothing till they land.
func (d *display) landingOf(s *set) bool {
	if d.inOverview {
		return true
	}
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
	// Cards slide to their places and come in from the display's edge,
	// here the left one. They leave at once: a card that goes is either flying to the stage, where its
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
	if debugOn {
		t0 := time.Now()
		s.frames.frame(c.Now(), s.motion())
		s.frames.burst("strip " + d.mon.Device)
		defer func() { s.frames.built(time.Since(t0)) }()
	}
	right := s.right()
	gap, p := float32(a.settings.Spacing), a.panel()
	l := layoutStrip(len(d.sets), w, h, d.scroll, gap, p, right)
	if d.scroll > l.maxScroll(h) {
		d.scroll = l.maxScroll(h)
		l = layoutStrip(len(d.sets), w, h, d.scroll, gap, p, right)
	}
	d.layout = l
	slots, n, order := s.room()
	shown := l
	if n != len(d.sets) {
		shown = layoutStrip(n, w, h, d.scroll, gap, p, right)
	}

	root := ui.Box(c).Absolute().Left(0).Top(0).Size(w, h)
	out := float32(0)
	if !s.shown {
		// A floating strip slides out over its display's edge.
		out = -(w + 32) * turnSign(right)
	}
	x := root.AnimateTo("reveal", out, ui.Snappy)
	// Past where it goes, a strip sliding out is out of sight: it stops
	// there rather than bounce back out of sight for a second more, and
	// its window hides once a frame finds it stopped (strip.poll). The
	// frame that stops it has already asked for the next.
	stopped := false
	if !s.shown && x != out && x*out >= out*out {
		x, stopped = root.Jump("reveal", out), true
	}
	root.Translate(x, 0)
	s.away = !s.shown && !stopped && root.Settled("reveal")
	root.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputScroll {
			return false
		}
		d.scroll = min(max(d.scroll+ev.DY, 0), d.layout.maxScroll(d.viewH))
		return true
	})
	hits := s.hits[:0]
	footer := ui.Rect{}
	root.Children(func() {
		if n == 0 && a.ready {
			emptyHint(c, a.tr(), w, h)
		}
		for _, i := range order {
			if b := s.card(c, d.sets[i], shown.cards[slots[i]]); b.W > 0 {
				hits = append(hits, b)
			}
		}
		if a.ready && !d.inOverview {
			footer = s.footer(c, w, h)
		}
	})
	s.hits, s.footerHit = hits, footer
}

// footer draws the panel at the bottom of the strip that opens the
// overview. It comes in as the pointer nears it (strip.approach), or for a
// moment as the settings size it, so the strip looks as it does on macOS
// until it is wanted. It returns where it takes the pointer, for hit
// testing.
func (s *strip) footer(c *ui.Context, w, h float32) ui.Rect {
	a, d := s.a, s.d
	p := a.panel()
	band := footerBand(w, h, p, s.right())
	r := footerButton(w, h, p, s.right())
	el := ui.Box(c.Key("overview")).Absolute().Left(band.X).Top(band.Y).Size(band.W, band.H).Cursor(ui.CursorPointer)
	hovered, pressed := el.Hovered(), el.Pressed()
	// A click lights the panel up and lets it go with a little pop, so it
	// is seen to take the click in the moment before the overview opens.
	var flash float32
	if el.Clicked() {
		debugf("overview panel clicked on %s", d.mon.Device)
		a.requestOverview(d)
		flash = el.Jump("flash", 1)
		c.AnimationFrame()
	} else {
		flash = el.AnimateTo("flash", 0, panelFlash)
	}
	near := s.near
	if hovered || pressed || time.Now().Before(a.panelUntil) {
		near = 1
	}
	op := max(el.AnimateTo("shown", near, fadeQuick), flash)
	hot := el.AnimateTo("hot", b2f(hovered), fadeQuick)
	down := el.AnimateTo("down", b2f(pressed), panelPress)
	press := el.AnimateTo("press", 1-0.08*b2f(pressed), ui.Snappy)
	lit := max(down, flash)
	// It grows into place as it comes in.
	scale := (0.9 + 0.1*op) * press * (1 + 0.04*flash)
	accent, outlined := a.accent()
	bg := pillColor.Mix(panelHot, hot).Mix(accent, lit)
	edge := ui.RGBA(255, 255, 255, 0.16).Mix(ui.RGBA(255, 255, 255, 0.34), hot).Mix(ui.RGBA(150, 205, 255, 0.95), lit)
	if outlined {
		// Under the pointer the border takes the accent, as the corner
		// button's does; otherwise the panel keeps the faint edge it
		// rests with.
		edge = edge.Mix(accent.Alpha(max(hot, lit)), max(hot, lit))
	}
	el.Children(func() {
		// Its corners round as a window's do on Windows 11, less on a
		// thin panel.
		pane := ui.Row(c).Absolute().Left(r.X-band.X).Top(r.Y-band.Y).Size(r.W, r.H).Center().
			Radius(min(8, r.H/4)).Background(bg).Border(1, edge).
			Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.28)).Scale(scale, scale).Opacity(op)
		pane.Children(func() {
			if g := min(16, r.H*0.42, r.W*0.6); g >= 8 {
				overviewGlyph(c, g, white)
			}
		})
	})
	if !s.shown {
		return ui.Rect{}
	}
	return band
}

var (
	// panelHot is the panel under the pointer, a little lighter; pressed,
	// it takes the blue of the overview's ring.
	panelHot = ui.RGBA(44, 44, 52, 0.84)
	// The press shows at once; the flash of a click fades as the overview
	// opens.
	panelPress = ui.Animation{Duration: 70 * time.Millisecond}
	panelFlash = ui.Animation{Duration: 420 * time.Millisecond}
)

func b2f(v bool) float32 {
	if v {
		return 1
	}
	return 0
}

// panel is the size of the overview's panel the settings give.
func (a *App) panel() panel {
	return panel{share: float32(a.settings.PanelWidth), h: float32(a.settings.PanelHeight)}
}

// panelPeek is how long the strips show the overview's panel after the
// settings size it, though the pointer is not there.
const panelPeek = 1500 * time.Millisecond

// showPanel shows the overview's panel on every strip for a moment, as the
// settings size it, and puts it away after.
func (a *App) showPanel() {
	a.panelUntil = time.Now().Add(panelPeek)
	a.redraw()
	after(panelPeek+20*time.Millisecond, a.redraw)
}

// overviewGlyph draws the mark of the overview: four windows side by side.
func overviewGlyph(c *ui.Context, size float32, color ui.Color) {
	h, g := size*0.78, size*0.14
	cw, ch := (size-g)/2, (h-g)/2
	ui.Box(c).Size(size, h).Children(func() {
		for i := 0; i < 4; i++ {
			x, y := float32(i%2)*(cw+g), float32(i/2)*(ch+g)
			ui.Box(c.Key(i)).Absolute().Left(x).Top(y).Size(cw, ch).Radius(1.6).Background(color)
		}
	})
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
	right := s.right()
	sign := turnSign(right)
	motion := cardMotion
	if !st.arrive.Empty() {
		// It comes in where its windows were let go, at their size.
		// Enter motions move and scale about the card's middle.
		r := st.arrive
		st.arrive = winapi.Rect{}
		x := float32(float64(r.Left-s.rect.Left)/s.scale) + float32(float64(r.W())/s.scale)/2
		y := float32(float64(r.Top-s.rect.Top)/s.scale) + float32(float64(r.H())/s.scale)/2
		motion.Enter = &ui.Motion{
			X: x - (box.X + box.W/2), Y: y - (box.Y + box.H/2),
			Opacity: 1, Scale: float32(float64(r.W())/s.scale) / box.W,
		}
	} else if landing {
		motion.Enter = nil
	} else if right {
		enter := *motion.Enter
		enter.X = -enter.X
		motion.Enter = &enter
	}
	el := ui.Box(c.Key(g.key)).Absolute().Left(box.X).Top(box.Y).Size(box.W, box.H).
		Cursor(ui.CursorPointer).Transition(motion)

	// A card dragged goes where it is let go (carddrag.go): back, up or
	// down the strip, onto the stage or into another display's strip.
	// Dragged tells whether the card is held, wherever the pointer is;
	// Pressed only while it is over the card.
	dx, dy, held := el.Dragged()
	if held {
		s.drag(g, st, dx, dy)
	} else if st.dragging || st.dx != 0 || st.dy != 0 {
		s.letGo(g, st, box)
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
	// one turns them back. While a card is dragged, the others take no
	// notice of the pointer passing over them as they make room: they
	// would lift, and capture their windows at full size for a switch that
	// will not come, holding the main thread up mid-drag.
	hovered := (st.dragging || el.Hovered() && a.cardDrag == nil) && !landing
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
	// Moved up or down the strip, the card stays in it, whatever way the
	// pointer strays across: the strip is hardly wider than the card. Once
	// it heads for the stage or another display, it follows the pointer
	// out. Either way it moves with the pointer, frame for frame: a spring
	// chasing it would trail behind as long as it moves. Only the gap the
	// strip's edge made between them, as the card leaves or comes back, a
	// spring takes up.
	sorting := st.dragging && a.cardDrag.sorts()
	if st.dragging {
		tx, ty := st.dx, st.dy
		mode := followOut
		if sorting {
			tx, ty = s.keepIn(box, tx, ty, lift)
			mode = followIn
		}
		if mode != st.follow {
			st.follow = mode
			el.Jump("gap", st.ox-tx)
		}
		ox, oy = el.Jump("dx", tx+el.AnimateTo("gap", 0, catchUp)), el.Jump("dy", ty)
	} else {
		st.follow = followNone
		ox, oy = el.AnimateTo("dx", 0, ui.Smooth), el.AnimateTo("dy", 0, ui.Smooth)
	}
	st.ox = ox
	// The flight covers the card as it lands, so the card shows at once
	// underneath; fading it in once the flight is gone would blink. The
	// icons come in on the flight layer, above the flights.
	shown, iconOp := float32(1), float32(1)
	if landing {
		shown, iconOp = 0, 0
	}
	op := float32(1)
	if st.dragging && a.flights != nil {
		if sorting && s.holds(box, ox, oy, lift) {
			// Moved up or down the strip, the card shows here, in the same
			// frames as the cards making way for it: on the flight layer,
			// a window of its own drawn at its own times, it would lag
			// behind them and the pointer.
			if st.poses != nil {
				st.poses = nil
				a.flights.endDrag(d, 0)
			}
		} else if st.poses = s.dragPoses(g, box, ox, oy, lift, tilt); len(st.poses) > 0 {
			// Out of it, toward the stage or another display, it shows
			// on the flight layer, which reaches beyond the strip; here
			// it leaves its place empty.
			op = 0
		}
	}
	if s.leaving(g) {
		ox, oy, op = el.Jump("dx", 0), el.Jump("dy", 0), 0
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

	// The card scales and turns about its outer edge, the one by the
	// display's edge.
	local := ui.Rect{W: box.W, H: box.H}
	stack := g.stack(local, right)
	originX := (1 - sign) / 2
	el.Children(func() {
		windows := ui.Box(c).Absolute().Left(0).Top(0).Size(box.W, box.H).
			TransformOrigin(originX, 0.5).Scale(lift, lift).RotateY(tilt * sign).Perspective(cardPerspective).Opacity(shown)
		windows.Children(func() {
			for i := len(stack) - 1; i >= 0; i-- {
				s.windowStill(c, g, i, stack[i])
			}
		})
		// The icons of a set's apps overlap a little, the first on top.
		row := iconRow(stack[0], local, len(g.apps), float32(a.settings.IconScale), right)
		ir := row[len(row)-1]
		overlay := ui.Box(c).Absolute().Left(0).Top(0).Size(box.W, box.H).TransformOrigin(originX, 0.5).Scale(lift, lift).Opacity(iconOp)
		overlay.Children(func() {
			for i := len(row) - 1; i >= 0; i-- {
				if icon := a.media.icons[g.apps[i].app]; icon != nil {
					r := row[i]
					ui.Image(c.Key(g.apps[i].app), icon).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H)
				}
			}
			// The name follows the icons toward the stage.
			label := ui.Text(c, g.title()).FontSize(12).Bold().TextColor(white).MaxLines(1).
				Absolute().Top(ir.Y+ir.H-24).Background(pillColor).Padding(3, 9).Radius(11)
			if right {
				label.Right(box.W - ir.X + 4).MaxWidth(max(ir.X, 60))
			} else {
				end := ir.X + ir.W
				label.Left(end + 4).MaxWidth(max(box.W-end, 60))
			}
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

// motion says what moves in the strip, for the debug log.
func (s *strip) motion() string {
	c := s.a.cardDrag
	switch {
	case s.dragging:
		return "card dragged"
	case c != nil && c.done && c.to == s.d:
		return "card let go"
	case c != nil && c.to == s.d:
		return "card dragged in"
	case s.d.inOverview:
		return "overview"
	case s.a.busy:
		return "switch"
	}
	return "cards"
}

// dragPoses puts the stills of a dragged card's windows on the flight
// layer where the card shows, offset by the drag, and returns where.
func (s *strip) dragPoses(g *set, box ui.Rect, ox, oy, lift, tilt float32) map[winapi.HWND]pose {
	right := s.right()
	card := ui.Rect{X: box.X + ox, Y: box.Y + oy, W: box.W, H: box.H}
	stack := g.stack(card, right)
	poses := map[winapi.HWND]pose{}
	var list []flight
	for i := len(stack) - 1; i >= 0; i-- {
		w := g.windows[i]
		st := s.a.media.still(w.HWND)
		if st == nil {
			continue
		}
		x, y, pw, ph := s.toScreen(lifted(card, stack[i], lift, right))
		p := pose{x: x, y: y, w: pw, h: ph, tilt: tilt * turnSign(right)}
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
