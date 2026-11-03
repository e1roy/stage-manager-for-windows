package app

import (
	"slices"
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
	// over is whether the pointer was on the card in the last frame, and
	// shook when the stage's card was last shaken (shakeGap).
	over  bool
	shook time.Time
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
	// ghostFade is how long the card of the apps on the stage takes to become
	// a ghost, or back to a card: as long as the windows take to leave it.
	ghostFade = ui.Animation{Duration: 320 * time.Millisecond}
	// shakeSize is how far, in DIPs, the stage's card is knocked aside when
	// clicked: the strip has the room, its cards keeping stripPadLeft and
	// stripPadRight from its edges. shakeSpring lets it swing back and forth
	// about its place, about 140 ms from side to side, as a shake does: a
	// spring that bounces a lot, which stills in half a second.
	shakeSize = float32(9)
	// shakeGap is the time before the pointer coming on the stage's card
	// shakes it again: the shake takes under a second, and has the card back
	// under the pointer, which is then no news.
	shakeGap    = 800 * time.Millisecond
	shakeSpring = ui.Animation{Spring: ui.SpringOptions{Duration: 140 * time.Millisecond, Bounce: 0.8}}
	// The badge on the card of the stage is a small green button of glass, as
	// the checks of macOS and Windows settings are: lit from the upper left,
	// with a gloss on its top, a shadow it casts on the card, and a white check
	// standing on it. It is kept light, in minty greens that end at the system
	// green rather than in the dark ones that give a button weight: on a card
	// that is a ghost, a heavy one pulls the eye from the card, and the check is
	// all it has to say.
	badgeLight  = ui.RGB(178, 248, 196)
	badgeMid    = ui.RGB(98, 222, 132)
	badgeDeep   = ui.RGB(52, 199, 89)
	badgeEdge   = ui.RGBA(30, 150, 66, 0.35)
	badgeCast   = ui.RGBA(0, 60, 20, 0.3)
	badgeGloss  = ui.RGBA(255, 255, 255, 0.55)
	badgeCheck  = ui.RGB(255, 255, 255)
	badgeEmboss = ui.RGBA(16, 120, 50, 0.35)
	// badgeTurn is how much of the card's turn the badge takes, as a share,
	// and badgePerspective how far away, in DIPs, it is seen from: the
	// closer, the more the side turned from the eye shrinks.
	badgeTurn        = float32(0.5)
	badgePerspective = float32(160)
)

// checkIcon is the mark in the badge: a bold check, in the text color.
var checkIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3.8" stroke-linecap="round" stroke-linejoin="round"><path d="M6 12.6l4.4 4.4 7.8-9"/></svg>`))

// stageBadge draws the badge of the card of the stage in r, grown to its size
// by pop, from 0 to 1, and turned about its own middle by turn degrees. It
// turns itself, rather than ride in the layer of the card's windows and be
// turned back there: MyGo does not undo a turn inside a turned layer, it only
// squeezes by another. It is built of what MyGo has, which is linear
// gradients, inset shadows and the shadow below: a disc that goes from light
// at its upper left to green at its lower right, a light rim inside its top
// edge and a faint dark one inside its bottom, the gloss of a pill of white
// fading downward across its top half, and the check, white, over a copy of
// itself in green a little below, which makes it stand off the disc.
func stageBadge(c *ui.Context, r ui.Rect, pop, turn float32) {
	d := r.W
	disc := ui.Box(c.Key("badge")).Absolute().Left(r.X).Top(r.Y).Size(d, d).Radius(d/2).Scale(pop, pop).RotateY(turn).Perspective(badgePerspective).
		LinearGradient(ui.LinearGradient{Angle: 155, Stops: []ui.GradientStop{{Color: badgeLight, At: 0}, {Color: badgeMid, At: 0.45}, {Color: badgeDeep, At: 1}}}).
		Border(max(d*0.03, 0.5), badgeEdge).
		Shadow(0, d*0.08, d*0.22, 0, badgeCast).
		InsetShadow(0, d*0.05, d*0.07, 0, ui.RGBA(255, 255, 255, 0.5)).
		InsetShadow(0, -d*0.07, d*0.12, 0, ui.RGBA(20, 140, 60, 0.22)).
		PassThrough()
	disc.Children(func() {
		ui.Box(c.Key("gloss")).Absolute().Left(d*0.13).Top(d*0.05).Size(d*0.74, d*0.44).Radius(d*0.22).
			Gradient(badgeGloss, ui.RGBA(255, 255, 255, 0), 180).PassThrough()
		// The check sits a little above the middle, where the eye puts it on a
		// lit disc, and its copy a little below it.
		mark := d * 0.6
		x, y := (d-mark)/2, (d-mark)/2+d*0.02
		ui.Icon(c.Key("emboss"), checkIcon).Absolute().Left(x).Top(y + d*0.05).FontSize(mark).TextColor(badgeEmboss).PassThrough()
		ui.Icon(c.Key("check"), checkIcon).Absolute().Left(x).Top(y).FontSize(mark).TextColor(badgeCheck).PassThrough()
	})
}

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
	gap, p, at := float32(a.settings.Spacing), a.panel(), float32(a.settings.CardsAt)
	// The cards keep below the list of the stage's windows.
	staged := s.stageWindows()
	line := layoutLineup(len(staged), w, h, right)
	l := layoutStrip(len(d.sets), w, h, d.scroll, gap, p, line.extent, at, right)
	if d.scroll > l.maxScroll(h) {
		d.scroll = l.maxScroll(h)
		l = layoutStrip(len(d.sets), w, h, d.scroll, gap, p, line.extent, at, right)
	}
	d.layout = l
	slots, n, order := s.room()
	shown := l
	if n != len(d.sets) {
		shown = layoutStrip(n, w, h, d.scroll, gap, p, line.extent, at, right)
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
	footer, list := ui.Rect{}, ui.Rect{}
	s.previous = s.previousStage()
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
		} else {
			s.footState(false, false)
		}
		// Over the cards, should they scroll up to it. DWM draws the
		// pictures over the window, so they slide with it by hand.
		list = s.lineupView(c, line, staged, x)
	})
	s.hits, s.footerHit, s.lineupHit = hits, footer, list
}

// footer draws the panel at the bottom of the strip that opens the
// overview. It comes in as the pointer nears it (strip.approach), or for a
// moment as the settings size it, so the strip looks as it does on macOS
// until it is wanted. It returns where it takes the pointer, for hit
// testing. Joined to the corner button below it, it draws the top of the
// one button the two make.
func (s *strip) footer(c *ui.Context, w, h float32) ui.Rect {
	a, d := s.a, s.d
	p := a.panel()
	joined := s.joined
	band := footerBand(w, h, p, s.right())
	r := footerButton(w, h, p, s.right())
	if joined {
		band, r = jointBand(w, h, p), jointButton(w, h, p)
	}
	el := ui.Box(c.Key("overview")).Absolute().Left(band.X).Top(band.Y).Size(band.W, band.H).Cursor(ui.CursorPointer)
	hovered, pressed := el.Hovered(), el.Pressed()
	s.footState(hovered, pressed)
	now := time.Now()
	peek := now.Before(a.panelUntil)
	// The corner button joined to it lights it up, presses it and shows
	// it as it does itself.
	if hint := s.joint(); hint != nil {
		hovered, pressed = hovered || hint.hover, pressed || hint.pressing
		peek = peek || now.Before(hint.peek)
	}
	// A click lights the panel up and lets it go with a little pop, so it
	// is seen to take the click in the moment before the overview opens.
	if el.Clicked() {
		debugf("overview panel clicked on %s", d.mon.Device)
		a.requestOverview(d)
		d.flashAt = now
		if hint := s.joint(); hint != nil {
			hint.redraw()
		}
	}
	flash := d.flash(now)
	if flash > 0 {
		c.AnimationFrame()
	}
	near := s.near
	if hovered || pressed || peek {
		near = 1
	}
	op := max(el.AnimateTo("shown", near, fadeQuick), flash)
	// The glass is taken as the panel comes in, while nothing of it
	// shows yet to be taken with what is behind it.
	if near > 0 && !s.frosted {
		s.frosted = true
		s.frost(s.buttonScreen(r))
	} else if near == 0 && op == 0 {
		s.frosted = false
	}
	hot := el.AnimateTo("hot", b2f(hovered), fadeQuick)
	down := el.AnimateTo("down", b2f(pressed), panelPress)
	press := el.AnimateTo("press", 1-0.08*b2f(pressed), ui.Snappy)
	lit := max(down, flash)
	accent, outlined := a.accent()
	// Under the pointer the border takes the accent, as the corner
	// button's does; otherwise the panel keeps the faint edge it rests
	// with.
	edge := panelEdge(hot, lit, accent, outlined)
	if joined {
		s.jointFooter(c, el, band, r, op, hot, lit, accent, edge)
	} else {
		// It grows into place as it comes in.
		scale := (0.9 + 0.1*op) * press * (1 + 0.04*flash)
		bg := panelFill(float32(a.settings.PanelOpacity), hot).Mix(accent, lit)
		// Its corners round as a window's do on Windows 11, less on a
		// thin panel.
		radius := min(8, r.H/4)
		el.Children(func() {
			if b := s.glass.bmp; b != nil {
				ui.Image(c, b).Absolute().Left(r.X-band.X).Top(r.Y-band.Y).Size(r.W, r.H).Fit(ui.FillBox).
					Radius(radius).Scale(scale, scale).Opacity(op)
			}
			pane := ui.Row(c).Absolute().Left(r.X-band.X).Top(r.Y-band.Y).Size(r.W, r.H).Center().
				Radius(radius).Background(bg).Border(1, edge).
				Shadow(0, 4, 14, 0, ui.RGBA(0, 0, 0, 0.28)).Scale(scale, scale).Opacity(op)
			pane.Children(func() {
				if g := min(16, r.H*0.42, r.W*0.6); g >= 8 {
					overviewGlyph(c, g, white)
				}
			})
		})
	}
	if !s.shown {
		return ui.Rect{}
	}
	return band
}

// jointFooter draws the panel as the top of one button with the corner
// button below it (corner.go): flush with the display's left edge and the
// taskbar, outlined and rounded all round with it, as one button, but not
// between the two, and the overview's mark centered on the two. Neither
// grows nor shrinks, which would part them.
func (s *strip) jointFooter(c *ui.Context, el ui.Element, band, r ui.Rect, op, hot, lit float32, accent, edge ui.Color) {
	bg := panelFill(float32(s.a.settings.PanelOpacity), hot).Mix(accent, lit)
	below := s.cornerHeight()
	g := min(18, (r.H+below)*0.42, r.W*0.6)
	gh := g * 0.78
	y := jointGlyph(r.H, below, gh)
	x0, y0 := r.X-band.X, r.Y-band.Y
	el.Children(func() {
		// The glass is the whole button's: the panel shows its top.
		if b := s.glass.bmp; b != nil {
			ui.Box(c).Absolute().Left(x0).Top(y0).Size(r.W, r.H).Radius(jointRadius, jointRadius, 0, 0).Clip().Opacity(op).Children(func() {
				ui.Image(c, b).Absolute().Left(0).Top(0).Size(r.W, r.H+below).Fit(ui.FillBox)
			})
		}
		ui.Box(c).Absolute().Left(x0).Top(y0).Size(r.W, r.H).
			Radius(jointRadius, jointRadius, 0, 0).Background(bg).BorderWidth(1, 1, 0, 1).BorderColor(edge).Opacity(op)
		if y+gh <= r.H && g >= 8 {
			ui.Box(c).Absolute().Left(r.X - band.X + (r.W-g)/2).Top(r.Y - band.Y + y).Opacity(op).Children(func() {
				overviewGlyph(c, g, white)
			})
		}
	})
}

var (
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
	// The stage's own card, in a strip that lists every stage, is a ghost
	// holding the stage's slot: nothing comes of a click on it.
	cursor := ui.CursorPointer
	if g.staged {
		cursor = ui.CursorDefault
	}
	el := ui.Box(c.Key(g.key)).Absolute().Left(box.X).Top(box.Y).Size(box.W, box.H).
		Cursor(cursor).Transition(motion)

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
		switch {
		case g.staged:
			// Nothing to switch to, and a click that does nothing looks like
			// one that was missed: the card shakes its head, as the login
			// box of macOS does for a wrong password.
			el.Jump("shake", shakeSize)
			st.shook = c.Now()
		case el.ClickModifiers()&ui.Shift != 0 && !d.model.Empty():
			// Shift-click adds the card to the stage, as on macOS.
			a.request(actJoin, d, g)
		default:
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
	// The pointer coming onto the stage's card shakes it too, as a click
	// does: it has nothing to bring, and it says so before the click that
	// would ask. The shake moves the card from under the pointer and back, so
	// that it comes on it again: not within shakeGap of the last, or it would
	// shake on forever at the card's edge.
	over := hovered && !st.dragging
	if g.staged && over && !st.over && c.Now().Sub(st.shook) > shakeGap {
		el.Jump("shake", shakeSize)
		st.shook = c.Now()
	}
	st.over = over
	// No switch follows a click on the stage's own card, so the pointer over
	// it neither turns nor lifts it, nor takes the pictures of its windows,
	// which the user is in; held and moved, it is a card like the others.
	live := hovered && (!g.staged || st.dragging)
	if live && !st.dragging {
		// A switch may follow: take the pictures its flights need now, at
		// the size the card cannot show.
		a.media.preview(g.windows)
	}
	tilt, hoverTilt := cardTilts(a.settings.Tilt)
	if live {
		tilt = hoverTilt
	}
	tilt = el.AnimateTo("tilt", tilt, ui.Snappy)
	lift := float32(1)
	switch {
	case held && !st.moved && !g.staged:
		lift = 0.96
	case st.dragging:
		lift = 1.08
	case live:
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
	// The shake runs along the strip's width, to and fro about where the card is.
	el.Translate(ox+el.AnimateTo("shake", 0, shakeSpring), oy).Opacity(op)

	el.ContextMenu(func(m *ui.Menu) {
		tr := a.tr()
		if !g.staged {
			if m.Item(tr.PickStage).Chosen() {
				a.request(actPick, d, g)
			}
			if !d.model.Empty() && m.Item(tr.JoinStage).Chosen() {
				a.request(actJoin, d, g)
			}
			if len(g.apps) > 1 && m.Item(tr.SplitSet).Chosen() {
				a.request(actSplit, d, g)
			}
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
	// The stage's own card fades to a ghost as its apps come on the stage,
	// and back as they leave it; the icons, which tell the app, stay more
	// plainly there than its windows.
	ghost := el.AnimateTo("ghost", b2f(g.staged), ghostFade)
	fade := 1 - ghost*(1-float32(a.settings.StagedOpacity))
	gray := ghost * float32(a.settings.StagedGray)
	iconOp *= 1 - ghost*(1-float32(a.settings.StagedOpacity))/2
	el.Children(func() {
		// The windows fade as one picture: an element's opacity is that of
		// each shape it draws, so the stacked windows of a ghost would show
		// through each other. A cached layer is drawn as one image, at the
		// opacity of what holds it.
		shell := ui.Box(c).Absolute().Left(0).Top(0).Size(box.W, box.H).Opacity(shown * fade)
		shell.Children(func() {
			windows := ui.Box(c).Absolute().Left(0).Top(0).Size(box.W, box.H).
				TransformOrigin(originX, 0.5).Scale(lift, lift).RotateY(tilt * sign).Perspective(cardPerspective)
			if ghost > 0 {
				windows.Cache()
			}
			windows.Children(func() {
				s.previousMark(c, g, stack)
				for i := len(stack) - 1; i >= 0; i-- {
					s.windowStill(c, g, i, stack[i], gray)
				}
				// While the pointer is on the card, a button on the top right
				// corner of its front window, half out of it, closes the card's
				// windows, every one of a set of several, as its menu's last
				// item does: out there, it hides little of the window. It is
				// drawn with the windows, so it turns and lifts as they do.
				closing := el.AnimateTo("close", b2f(live && !st.dragging), fadeQuick)
				if closing > 0 {
					f := stack[0]
					label := a.tr().CloseWindow
					if len(g.windows) > 1 {
						label = a.tr().CloseWindows
					}
					b := closeButton(c, cardCloseSize, cardCloseRest, label, func() { a.request(actClose, d, g) })
					b.Absolute().Left(f.X+f.W-cardCloseSize/2).Top(f.Y-cardCloseSize/2).
						Shadow(0, 1, 4, 0, ui.RGBA(0, 0, 0, 0.3)).Opacity(closing)
				}
			})
		})
		// The icons of a set's apps overlap a little, the first on top.
		row := iconRow(stack[0], local, len(g.apps), float32(a.settings.IconScale), right)
		ir := row[len(row)-1]
		// The overlay spans the card, above the windows: it lets the
		// pointer through to them, or the button that closes them would
		// not know it is under the pointer, nor take its click.
		// The stage's card wears a green check, which is what makes the ghost
		// read as the one that is open, and is not faded with its windows. It
		// sits where the card's corner shows once the card is turned
		// (turnedPoint), and lifts with the card. A disc turned as far as the
		// card is squeezes to an oval that looks nothing like a button, and
		// one that lies flat beside a card at an angle looks pasted on: it
		// turns about itself by a share of the card's turn (badgeTurn), which
		// is not what the geometry says, and looks right. It pops in as the
		// ghost comes. Built for every card, so that the ones after it keep
		// their place in the tree as the stage comes and goes.
		badge := ui.Box(c).Absolute().Left(0).Top(0).Size(box.W, box.H).
			TransformOrigin(originX, 0.5).Scale(lift, lift).Opacity(shown * ghost).PassThrough()
		badge.Children(func() {
			b := badgeRect(stack, local, float32(a.settings.IconScale), right, ui.Rect{X: -box.X, W: d.viewW})
			x, y := turnedPoint(b.X+b.W/2, b.Y+b.H/2, box.W, box.H, tilt, cardPerspective, right)
			stageBadge(c, ui.Rect{X: x - b.W/2, Y: y - b.H/2, W: b.W, H: b.H}, 0.5+0.5*ghost, badgeTurn*tilt*sign)
		})
		overlay := ui.Box(c).Absolute().Left(0).Top(0).Size(box.W, box.H).TransformOrigin(originX, 0.5).Scale(lift, lift).Opacity(iconOp).PassThrough()
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

// The button that closes a card's windows: grey and see-through, so that
// it shows over a window of any color without hiding much of it.
const cardCloseSize = 20

var cardCloseRest = ui.RGBA(60, 60, 66, 0.6)

// previousStage returns the first app of the set the stage last left, as
// its card is marked (settings.MarkPrevious), or "" for none. Any way off
// the stage counts, a click on a card, Alt+Tab, the taskbar or the
// wallpaper: whichever way the user went off on a detour, the mark shows
// the way back.
func (s *strip) previousStage() string {
	if !s.a.settings.MarkPrevious {
		return ""
	}
	if prev := s.d.model.Previous(); len(prev) > 0 {
		return prev[0]
	}
	return ""
}

// previousMark draws a dashed outline around a card's windows if its set
// is the stage last left, fading in and out as the mark moves to another
// card. It is drawn with the windows, so it turns and lifts as they do,
// and shows no sooner than they land.
func (s *strip) previousMark(c *ui.Context, g *set, stack []ui.Rect) {
	marked := s.previous != "" && slices.ContainsFunc(g.apps, func(app *group) bool { return app.app == s.previous })
	r, radius := markRect(stack)
	color := s.outline(markColor)
	el := ui.Box(c.Key("previous")).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).
		Radius(radius).Border(markWidth, color).BorderStyle(ui.BorderDashed)
	el.Opacity(el.AnimateTo("shown", b2f(marked), markFade))
}

// outline is the color of a thin line drawn on the strip: the accent the
// panel and the corner take, or fallback when the user turned the accent off.
func (s *strip) outline(fallback ui.Color) ui.Color {
	accent, on := s.a.accent()
	if !on {
		return fallback
	}
	// A saturated accent such as purple or blue is dark to the eye on the
	// strip's black, so a thin line of it nearly vanishes: lighten it.
	return accent.Mix(ui.RGB(255, 255, 255), markLighten)
}

var (
	// markColor is the dashed outline of the stage last left when the user
	// has turned the accent off: white, as the cards' labels are. Otherwise
	// the outline takes the accent, like the panel and the corner do.
	markColor = ui.RGBA(255, 255, 255, 0.85)
	// markWidth is the outline's width; MyGo sizes a dash at about three
	// times it, so a thicker line also has longer dashes.
	markWidth = float32(2)
	// markLighten is how far the accent is mixed toward white for the outline.
	markLighten = float32(0.4)
	markFade    = ui.Animation{Duration: 240 * time.Millisecond}
)

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

// grayOver keys the gray copy of a window's still, drawn over it.
type grayOver struct{ h winapi.HWND }

// windowStill draws the i-th window of a card: its still, or while it has
// none, a frosted pane with its app's icon. Gray, from 0 to 1, is how far
// the still goes gray: the built-in grayscale is all or nothing, so a gray
// copy of it is drawn over it, that much opaque.
func (s *strip) windowStill(c *ui.Context, g *set, i int, r ui.Rect, gray float32) {
	w := g.windows[i]
	radius := max(r.H*0.045, 3)
	if st := s.a.media.still(w.HWND); st != nil {
		ui.Image(c.Key(w.HWND), st.card).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).
			Fit(ui.FillBox).Radius(radius).Shadow(0, r.H*0.05, r.H*0.16, 0, shadowSoft)
		if gray > 0.005 {
			ui.Image(c.Key(grayOver{w.HWND}), st.card).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).
				Fit(ui.FillBox).Radius(radius).Grayscale().Opacity(gray)
		}
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
