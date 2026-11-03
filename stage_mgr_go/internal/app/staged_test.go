package app

import (
	"image"
	"image/color"
	"slices"
	"testing"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo/ui"
)

// groupsOfApps makes a group of one window for each app, front first.
func groupsOfApps(apps ...string) []*group {
	var out []*group
	for i, app := range apps {
		w := catalog.Window{HWND: winapi.HWND(i + 1), Title: app, App: app, Z: i, Frame: winapi.Rect{Right: 1600, Bottom: 1000}}
		out = append(out, &group{app: app, name: app, windows: []catalog.Window{w}, z: i})
	}
	return out
}

// keys and staged tell the cards of a display, top first, and which of them
// is the stage's.
func keys(d *display) (out []string, staged []bool) {
	for _, s := range d.sets {
		out = append(out, s.key)
		staged = append(staged, s.staged)
	}
	return out, staged
}

// With every stage listed, the stage's own card is among the cards, marked,
// and picking another app moves no card.
func TestAllStagesListsTheStage(t *testing.T) {
	a := &App{settings: settings.Default(), media: newMedia()}
	a.settings.AllStages = true
	d := &display{cards: map[string]*cardState{}, landing: map[string]bool{}}
	groups := groupsOfApps("a", "b", "c", "d")
	now := time.Now()
	d.model.Init("a", now)
	a.arrange(d, groups, now)
	if got, staged := keys(d); !slices.Equal(got, []string{"a", "b", "c", "d"}) || !slices.Equal(staged, []bool{true, false, false, false}) {
		t.Fatalf("cards %v, staged %v", got, staged)
	}
	if d.waiting() != 3 || len(d.onStage) != 1 {
		t.Fatalf("%d cards waiting, %d apps on the stage", d.waiting(), len(d.onStage))
	}
	for _, app := range []string{"c", "b", "d", "a"} {
		d.model.Switch(d.model.SetOf(app), now)
		a.arrange(d, groups, now)
		got, staged := keys(d)
		want := make([]bool, len(got))
		want[slices.Index(got, app)] = true
		if !slices.Equal(got, []string{"a", "b", "c", "d"}) || !slices.Equal(staged, want) {
			t.Fatalf("after picking %s: cards %v, staged %v", app, got, staged)
		}
	}
	// Turned off, the stage's card goes, and the others stay where they are.
	d.model.Switch(d.model.SetOf("c"), now)
	a.settings.AllStages = false
	a.arrange(d, groups, now)
	if got, staged := keys(d); !slices.Equal(got, []string{"a", "b", "d"}) || slices.Contains(staged, true) {
		t.Fatalf("cards %v, staged %v", got, staged)
	}
	if d.waiting() != 3 {
		t.Fatalf("%d cards waiting", d.waiting())
	}
	a.settings.AllStages = true
	a.arrange(d, groups, now)
	if got, staged := keys(d); !slices.Equal(got, []string{"a", "b", "c", "d"}) || !staged[2] {
		t.Fatalf("cards %v, staged %v", got, staged)
	}
}

// A strip that scrolls brings the card of the apps that leave the stage into
// view, where it keeps their slot; one that does not list every stage leaves
// the scroll alone.
func TestRevealScrollsToTheLeavingCard(t *testing.T) {
	s, _, _ := testStrip(t)
	a, d := s.a, s.d
	var apps []string
	for i := 0; i < 30; i++ {
		apps = append(apps, string(rune('A'+i/10))+string(rune('0'+i%10)))
	}
	d.sets = nil
	for _, g := range groupsOfApps(apps...) {
		d.sets = append(d.sets, newSet([]*group{g}))
	}
	d.viewW, d.viewH = 200, 900
	a.reveal(d, apps[29])
	if d.scroll != 0 {
		t.Fatalf("scrolled to %v without every stage listed", d.scroll)
	}
	a.settings.AllStages = true
	a.reveal(d, apps[29])
	if d.scroll <= 0 {
		t.Fatalf("scroll %v for the last of 30 cards", d.scroll)
	}
	last := d.scroll
	a.reveal(d, apps[28])
	if d.scroll > last || last-d.scroll > d.layout.extent*2 {
		t.Fatalf("scrolled from %v to %v for the card above", last, d.scroll)
	}
	a.reveal(d, apps[0])
	if d.scroll != 0 {
		t.Fatalf("scroll %v for the first card", d.scroll)
	}
	a.reveal(d, "nothing")
	if d.scroll != 0 {
		t.Fatalf("scroll %v for no card", d.scroll)
	}
}

// The stage's own card is a ghost: a click on it asks for nothing, its menu
// has no stage to pick, and no button closes it under the pointer.
func TestStagedCardTakesNoClick(t *testing.T) {
	s, got, tt := testStrip(t)
	s.d.sets[0].staged = true
	tt.Frame()
	x, y := center(s.d.layout.cards[0])
	tt.ClickAt(x, y)
	tt.Frame()
	if len(*got) != 0 {
		t.Fatalf("a click on the stage's card asked for %v", *got)
	}
	tt.Move(x, y)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if _, ok := tt.Find(s.a.tr().CloseWindow); ok {
		t.Fatal("a close button on the stage's card")
	}
	tt.RightClickAt(x, y)
	tt.Frame()
	menu := tt.Menu()
	if slices.Contains(menu, s.a.tr().PickStage) || slices.Contains(menu, s.a.tr().JoinStage) || !slices.Contains(menu, s.a.tr().CloseWindow) {
		t.Fatalf("menu %v", menu)
	}
	if err := tt.ChooseMenuItem(s.a.tr().CloseWindow); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != (request{actClose, "mail"}) {
		t.Fatalf("requests %v", *got)
	}
	// The others are as they were.
	x, y = center(s.d.layout.cards[1])
	tt.ClickAt(x, y)
	tt.Frame()
	if len(*got) != 2 || (*got)[1] != (request{actPick, "notes"}) {
		t.Fatalf("requests %v", *got)
	}
}

// Dragged toward the stage, or into another display's strip, the stage's
// card stays in its strip.
func TestStagedCardOnlySorts(t *testing.T) {
	s, got, tt := testStrip(t)
	s.d.sets[0].staged = true
	tt.Frame()
	x, y := center(s.d.layout.cards[0])
	tt.Move(x, y)
	tt.Frame()
	tt.Press(x, y)
	tt.Frame()
	for i := 1; i <= 10; i++ {
		tt.Move(x+float32(i)*25, y)
		tt.Frame()
	}
	if c := s.a.cardDrag; c == nil || c.aim == aimJoin || c.aim == aimMove || c.out {
		t.Fatalf("dragged %+v", c)
	}
	tt.Release(x+250, y)
	tt.Frame()
	if len(*got) != 0 {
		t.Fatalf("requests %v", *got)
	}
}

// The stage's card shows through, and gray, as far as the settings say.
func TestStagedCardIsAGhost(t *testing.T) {
	s, _, tt := testStrip(t)
	s.d.sets[0].staged = true
	// Both windows have a still of one red.
	red := image.NewRGBA(image.Rect(0, 0, 160, 100))
	for i := 0; i < len(red.Pix); i += 4 {
		red.Pix[i], red.Pix[i+3] = 220, 255
	}
	bmp := ui.NewBitmap(red)
	s.a.media.stills[1] = &still{card: bmp, fly: bmp}
	s.a.media.stills[2] = &still{card: bmp, fly: bmp}
	look := func(i int) color.RGBA {
		tt.Frame()
		tt.Advance(2 * time.Second)
		tt.Frame()
		stack := s.d.sets[i].stack(s.d.layout.cards[i], false)
		x, y := center(stack[0])
		return tt.Image().RGBAAt(int(x), int(y))
	}
	s.a.settings.StagedOpacity, s.a.settings.StagedGray = 1, 0
	if c := look(0); c.R < 200 || c.G > 40 {
		t.Fatalf("ghost at full opacity, no gray is %v, not red", c)
	}
	s.a.settings.StagedGray = 1
	if c := look(0); c.R != c.G || c.G != c.B || c.R < 40 {
		t.Fatalf("ghost wholly gray is %v", c)
	}
	s.a.settings.StagedGray = 0
	s.a.settings.StagedOpacity = 0.4
	faint := look(0)
	if faint.A > 140 || faint.A < 80 {
		t.Fatalf("ghost at 40%% is %v", faint)
	}
	if c := look(1); c.A < 250 || c.R < 200 || c.G > 40 {
		t.Fatalf("the other card is %v, not red and opaque", c)
	}
	// A new still of its window, taken as the pointer comes to the strip,
	// shows in the ghost: it is drawn as one picture, kept till it changes.
	green := image.NewRGBA(red.Rect)
	for i := 0; i < len(green.Pix); i += 4 {
		green.Pix[i+1], green.Pix[i+3] = 220, 255
	}
	bmp.Update(green)
	if c := look(0); c.G < 60 || c.R > 20 {
		t.Fatalf("the ghost is %v after its window's still turned green", c)
	}
}

// isGreen tells a pixel of the badge's disc, light green or deep, from the red
// windows and the white of its check.
func isGreen(c color.RGBA) bool {
	return c.A > 200 && c.G >= 150 && int(c.G) > int(c.R)+30 && int(c.G) > int(c.B)+25
}

// stillRed gives the first two windows of a test strip a still of one red.
func stillRed(s *strip) {
	red := image.NewRGBA(image.Rect(0, 0, 160, 100))
	for i := 0; i < len(red.Pix); i += 4 {
		red.Pix[i], red.Pix[i+3] = 220, 255
	}
	bmp := ui.NewBitmap(red)
	s.a.media.stills[1] = &still{card: bmp, fly: bmp}
	s.a.media.stills[2] = &still{card: bmp, fly: bmp}
}

// A click on the stage's card, which has nowhere to go, shakes it about its
// place and leaves it there.
func TestStagedCardShakesWhenClicked(t *testing.T) {
	s, got, tt := testStrip(t)
	s.d.sets[0].staged = true
	stillRed(s)
	s.a.settings.StagedOpacity, s.a.settings.StagedGray = 1, 0
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	front := s.d.sets[0].stack(s.d.layout.cards[0], false)[0]
	y := int(front.Y + front.H/2)
	// Where the red begins along the middle of the window.
	edge := func() int {
		img := tt.Image()
		for x := 0; x < img.Bounds().Dx(); x++ {
			if c := img.RGBAAt(x, y); c.R > 150 && c.G < 60 && c.A > 200 {
				return x
			}
		}
		return -1
	}
	rest := edge()
	if rest < 0 {
		t.Fatal("no window on the stage's card")
	}
	x, cy := center(s.d.layout.cards[0])
	tt.ClickAt(x, cy)
	lo, hi := rest, rest
	for i := 0; i < 40; i++ {
		tt.Frame()
		e := edge()
		lo, hi = min(lo, e), max(hi, e)
		tt.Advance(16 * time.Millisecond)
	}
	if hi-lo < 6 {
		t.Fatalf("the card moved between %d and %d from %d", lo, hi, rest)
	}
	if lo >= rest || hi <= rest {
		t.Fatalf("the card shook to one side only: %d to %d about %d", lo, hi, rest)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if e := edge(); e != rest {
		t.Fatalf("the card came to rest at %d, not %d", e, rest)
	}
	if len(*got) != 0 {
		t.Fatalf("the shake asked for %v", *got)
	}
	// A card that waits does not shake: it goes to the stage.
	x, cy = center(s.d.layout.cards[1])
	tt.ClickAt(x, cy)
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != (request{actPick, "notes"}) {
		t.Fatalf("requests %v", *got)
	}
}

// The stage's card wears a green check at its inner bottom corner, where the
// cards that wait have none, and it goes when the stage leaves the card.
func TestStagedCardHasACheck(t *testing.T) {
	s, _, tt := testStrip(t)
	s.a.settings.Tilt = 0
	s.d.sets[0].staged = true
	stillRed(s)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	// How many pixels of the badge are green, and how many white, as the
	// check is.
	badge := func(i int) (green, white int) {
		card := s.d.layout.cards[i]
		local := ui.Rect{W: card.W, H: card.H}
		b := badgeRect(s.d.sets[i].stack(local, false), local, float32(s.a.settings.IconScale), false, ui.Rect{X: -card.X, W: s.d.viewW})
		img := tt.Image()
		for y := int(card.Y + b.Y); y < int(card.Y+b.Y+b.H); y++ {
			for x := int(card.X + b.X); x < int(card.X+b.X+b.W); x++ {
				switch c := img.RGBAAt(x, y); {
				case c.A < 200:
				case isGreen(c):
					green++
				case c.R >= 235 && c.G >= 235 && c.B >= 235:
					white++
				}
			}
		}
		return green, white
	}
	if green, white := badge(0); green < 120 || white < 12 {
		t.Fatalf("%d green and %d white pixels at the corner of the stage's card, want a green disc with a white check", green, white)
	}
	if green, white := badge(1); green != 0 || white != 0 {
		t.Fatalf("%d green and %d white pixels at the corner of a card that waits", green, white)
	}
	s.d.sets[0].staged = false
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if green, _ := badge(0); green != 0 {
		t.Fatalf("%d green pixels on a card that left the stage", green)
	}
}

// Turned with the card as the strip has it, the badge stays nearly round, and
// sits where the card's corner shows, on either side of the display.
func TestStagedBadgeStaysRoundOnTheCorner(t *testing.T) {
	for _, right := range []bool{false, true} {
		s, _, tt := testStrip(t)
		if right {
			s.a.settings.Sides = map[string]settings.Side{s.d.mon.Device: settings.Right}
		}
		s.d.sets[0].staged = true
		stillRed(s)
		tt.Frame()
		tt.Advance(2 * time.Second)
		tt.Frame()
		card := s.d.layout.cards[0]
		local := ui.Rect{W: card.W, H: card.H}
		b := badgeRect(s.d.sets[0].stack(local, right), local, float32(s.a.settings.IconScale), right, ui.Rect{X: -card.X, W: s.d.viewW})
		deg, _ := cardTilts(s.a.settings.Tilt)
		px, py := turnedPoint(b.X+b.W/2, b.Y+b.H/2, card.W, card.H, deg, cardPerspective, right)
		img := tt.Image()
		x0, y0, x1, y1 := 1<<30, 1<<30, -1, -1
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				if isGreen(img.RGBAAt(x, y)) {
					x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
				}
			}
		}
		if x1 < 0 {
			t.Fatalf("right=%v: no badge", right)
		}
		w, h := float32(x1-x0+1), float32(y1-y0+1)
		if w/h < 0.88 || w/h > 1.05 {
			t.Fatalf("right=%v: the badge is %v by %v, an oval", right, w, h)
		}
		cx, cy := float32(x0)+w/2-card.X, float32(y0)+h/2-card.Y
		if d := cx - px; d < -3 || d > 3 || cy-py < -3 || cy-py > 3 {
			t.Fatalf("right=%v: the badge is centered at %v, %v of its card, not at the corner %v, %v", right, cx, cy, px, py)
		}
	}
}
