package app

import (
	"slices"
	"testing"

	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/egoist/mygo/ui"
)

// defPanel is the overview's panel as the settings first size it.
var defPanel = panel{share: float32(settings.Default().PanelWidth), h: float32(settings.Default().PanelHeight)}

func TestStripFitsAndCenters(t *testing.T) {
	l := layoutStrip(3, 200, 1000, 0, 20, defPanel, false)
	if l.scale != 1 || len(l.cards) != 3 {
		t.Fatalf("scale %v cards %d", l.scale, len(l.cards))
	}
	top, bottom := l.cards[0].Y, l.cards[2].Y+l.cards[2].H
	if d := (top - 0) - (1000 - bottom); d > 0.5 || d < -0.5 {
		t.Fatalf("not centered: top %v bottom %v", top, bottom)
	}
}

func TestStripShrinksThenScrolls(t *testing.T) {
	l := layoutStrip(8, 200, 900, 0, 20, defPanel, false)
	if l.scale >= 1 || l.scale < minCardScale {
		t.Fatalf("scale %v", l.scale)
	}
	last := l.cards[7]
	if last.Y+last.H > 900-stripMargin+0.5 {
		t.Fatalf("last card ends at %v", last.Y+last.H)
	}
	many := layoutStrip(40, 200, 900, 0, 20, defPanel, false)
	if many.scale != minCardScale || many.maxScroll(900) <= 0 {
		t.Fatalf("scale %v max scroll %v", many.scale, many.maxScroll(900))
	}
	scrolled := layoutStrip(40, 200, 900, 100, 20, defPanel, false)
	if scrolled.cards[0].Y != many.cards[0].Y-100 {
		t.Fatalf("scroll moved the first card from %v to %v", many.cards[0].Y, scrolled.cards[0].Y)
	}
}

func TestStackStaysInCard(t *testing.T) {
	card := ui.Rect{X: 18, Y: 100, W: 160, H: 105}
	for _, aspects := range [][]float64{{16.0 / 9}, {0.6}, {3.5, 1.2, 1.6}, {1.6, 1.6, 1.6, 1.6}} {
		rs := stackWindows(card, aspects, false)
		if len(rs) != min(len(aspects), maxStacked) {
			t.Fatalf("%d windows for %v", len(rs), aspects)
		}
		for i, r := range rs {
			if r.X < card.X-0.01 || r.Y < card.Y-0.01 || r.X+r.W > card.X+card.W+0.01 || r.Y+r.H > card.Y+card.H+0.01 {
				t.Fatalf("window %d %+v leaves card %+v for %v", i, r, card, aspects)
			}
		}
		if len(rs) > 1 && !(rs[1].Y < rs[0].Y && rs[1].X > rs[0].X) {
			t.Fatalf("window behind does not peek out: %+v", rs)
		}
	}
}

func TestTurnedCard(t *testing.T) {
	if rx, rh := turnedCard(40, 30, 0, 120); rx != 40 || rh != 15 {
		t.Fatalf("a flat card projects to %v, %v", rx, rh)
	}
	rx, rh := turnedCard(40, 30, 30, 120)
	if !(rx < 40*0.87 && rh < 15) {
		t.Fatalf("a turned card projects to %v, %v: its far edge should come nearer and shrink", rx, rh)
	}
}

// The cards keep clear of the band at the bottom where the overview's
// panel shows, however many there are, however far they scroll and
// however tall the panel is; a narrow panel is centered under the cards.
func TestCardsKeepClearOfFooter(t *testing.T) {
	const height = 900
	for _, p := range []panel{defPanel, {share: 0.3, h: 12}, {share: 1, h: 80}} {
		band := footerBand(200, height, p, false)
		button := footerButton(200, height, p, false)
		if button.X < band.X || button.Y < band.Y || button.X+button.W > band.X+band.W || button.Y+button.H > band.Y+band.H {
			t.Fatalf("panel %+v leaves its band %+v", button, band)
		}
		if band.Y+band.H != height {
			t.Fatalf("band %+v stops short of the strip's bottom %v", band, height)
		}
		for _, n := range []int{1, 5, 8, 40} {
			l := layoutStrip(n, 200, height, 0, 20, p, false)
			at := layoutStrip(n, 200, height, l.maxScroll(height), 20, p, false)
			last := at.cards[n-1]
			if last.Y+last.H > band.Y {
				t.Fatalf("%v, %d cards: the last ends at %v, in the footer from %v", p, n, last.Y+last.H, band.Y)
			}
		}
		cards := layoutStrip(1, 200, height, 0, 20, p, false).cards[0]
		if d := (button.X + button.W/2) - (cards.X + cards.W/2); d > 0.01 || d < -0.01 {
			t.Fatalf("panel %+v is not centered under the cards %+v", button, cards)
		}
		if r := footerButton(200, height, p, true); r.X+r.W > 200-stripPadLeft+0.01 || r.X < button.X {
			t.Fatalf("the panel of a strip on the right %+v does not mirror %+v", r, button)
		}
	}
	narrow := footerButton(200, height, panel{share: 0.3, h: 38}, false)
	if want := (200 - stripPadLeft - stripPadRight) * float32(0.3); narrow.W != want {
		t.Fatalf("a panel of 30%% is %v wide, want %v", narrow.W, want)
	}
}

// The overview spreads windows out without overlapping, inside its area,
// as they are shaped, never larger than they are, and in rows that follow
// where they were.
func TestOverviewSpreadsWindows(t *testing.T) {
	area := ui.Rect{X: 40, Y: 40, W: 1800, H: 960}
	items := []overviewItem{
		{w: 1600, h: 1000, cx: 960, cy: 540}, // maximized on the stage
		{w: 1200, h: 800, cx: 100, cy: 200},  // in the strip's top card
		{w: 600, h: 400, cx: 100, cy: 520},   // a small window, in the middle card
		{w: 900, h: 1100, cx: 100, cy: 840},  // a tall one, in the bottom card
		{w: 1400, h: 900, cx: 120, cy: 860},
	}
	rs := layoutOverview(area, items, 36, 60)
	if len(rs) != len(items) {
		t.Fatalf("%d places for %d windows", len(rs), len(items))
	}
	for i, r := range rs {
		it := items[i]
		if r.X < area.X-0.01 || r.Y < area.Y-0.01 || r.X+r.W > area.X+area.W+0.01 || r.Y+r.H > area.Y+area.H+0.01 {
			t.Fatalf("window %d at %+v leaves %+v", i, r, area)
		}
		if r.W > it.w+0.01 || r.H > it.h+0.01 {
			t.Fatalf("window %d of %vx%v shows larger, %vx%v", i, it.w, it.h, r.W, r.H)
		}
		if d := r.W/r.H - it.w/it.h; d > 0.01 || d < -0.01 {
			t.Fatalf("window %d of %vx%v shows %vx%v", i, it.w, it.h, r.W, r.H)
		}
		for j, s := range rs[:i] {
			if r.X < s.X+s.W && s.X < r.X+r.W && r.Y < s.Y+s.H && s.Y < r.Y+r.H {
				t.Fatalf("windows %d %+v and %d %+v overlap", i, r, j, s)
			}
		}
	}
	// The top card's window goes above the bottom card's.
	if !(rs[1].Y < rs[3].Y) {
		t.Fatalf("the window from the top %+v is not above the one from the bottom %+v", rs[1], rs[3])
	}
	// One small window shows at its own size, in the middle.
	one := layoutOverview(area, []overviewItem{{w: 600, h: 400}}, 36, 60)[0]
	if one.W != 600 || one.H != 400 || one.X+one.W/2 != area.X+area.W/2 || one.Y+one.H/2 != area.Y+area.H/2 {
		t.Fatalf("a lone small window shows at %+v", one)
	}
	if got := layoutOverview(area, nil, 36, 60); len(got) != 0 {
		t.Fatalf("no windows placed at %v", got)
	}
}

// The panel comes in as the pointer nears it, in full before the pointer
// is on it, and not at all from afar.
func TestPanelComesInAsPointerNears(t *testing.T) {
	r := ui.Rect{X: 20, Y: 800, W: 160, H: 38}
	if d := rectDist(r, 100, 810); d != 0 {
		t.Fatalf("a point on the panel is %v from it", d)
	}
	if d := rectDist(r, 100, 760); d != 40 {
		t.Fatalf("a point 40 above the panel is %v from it", d)
	}
	if d := rectDist(r, 0, 800-15); d != 25 {
		t.Fatalf("a point 20 left and 15 above the panel is %v from it", d)
	}
	if k := panelNearness(panelFull); k != 1 {
		t.Fatalf("at %v the panel shows %v, not in full", panelFull, k)
	}
	if k := panelNearness(panelReach); k != 0 {
		t.Fatalf("at %v the panel shows %v, not yet at all", panelReach, k)
	}
	last := float32(1)
	for d := float32(0); d <= panelReach+20; d += 5 {
		k := panelNearness(d)
		if k > last {
			t.Fatalf("the panel shows more, %v, farther away at %v", k, d)
		}
		last = k
	}
}

func TestStripGap(t *testing.T) {
	near, far := layoutStrip(3, 200, 1000, 0, 0, defPanel, false), layoutStrip(3, 200, 1000, 0, 60, defPanel, false)
	if d := far.extent - near.extent; d < 59.5 || d > 60.5 {
		t.Fatalf("a gap of 60 more set cards %v further apart", d)
	}
}

// A strip on the right is the one on the left in a mirror: cards keep to
// the display's edge, the windows behind peek out toward the stage, and
// the icons sit on the outer corner.
func TestRightMirrorsLeft(t *testing.T) {
	const width = 200
	near := func(a, b float32) bool { return a-b < 0.01 && b-a < 0.01 }
	l, r := layoutStrip(3, width, 1000, 0, 20, defPanel, false), layoutStrip(3, width, 1000, 0, 20, defPanel, true)
	for i := range l.cards {
		lc, rc := l.cards[i], r.cards[i]
		if !near(rc.X, width-lc.X-lc.W) || rc.Y != lc.Y || rc.W != lc.W {
			t.Fatalf("card %d on the right %+v does not mirror %+v", i, rc, lc)
		}
	}
	card := r.cards[0]
	aspects := []float64{1.6, 1.6, 1.6}
	ls, rs := stackWindows(l.cards[0], aspects, false), stackWindows(card, aspects, true)
	for i := range ls {
		if !near(rs[i].X+rs[i].W, width-ls[i].X) || rs[i].Y != ls[i].Y {
			t.Fatalf("window %d on the right %+v does not mirror %+v", i, rs[i], ls[i])
		}
	}
	if !(rs[1].X+rs[1].W < rs[0].X+rs[0].W) {
		t.Fatalf("window behind does not peek out toward the stage: %+v", rs)
	}
	icons := iconRow(rs[0], card, 2, 1, true)
	if !(icons[0].X+icons[0].W > rs[0].X+rs[0].W && icons[1].X < icons[0].X) {
		t.Fatalf("icons %+v are not over the outer corner of %+v, in a row toward the stage", icons, rs[0])
	}
	// Lifted, a card grows from its outer edge, which stays put.
	if up := lifted(card, card, 1.1, true); !near(up.X+up.W, card.X+card.W) {
		t.Fatalf("a lifted card %+v leaves its outer edge %v", up, card.X+card.W)
	}
}

func TestSlotAt(t *testing.T) {
	l := layoutStrip(3, 200, 1000, 0, 20, defPanel, false)
	c := l.cards
	for _, tc := range []struct {
		y    float32
		want int
	}{
		{0, 0},
		{c[0].Y + c[0].H/2 - 1, 0},
		{c[0].Y + c[0].H/2 + 1, 1},
		{c[1].Y + c[1].H, 2},
		{1000, 3},
	} {
		if got := l.slotAt(tc.y); got != tc.want {
			t.Errorf("slotAt(%v) = %d, want %d", tc.y, got, tc.want)
		}
	}
}

func TestMakeRoom(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to int
		slots    []int
		total    int
	}{
		{"none", -1, -1, []int{0, 1, 2, 3}, 4},
		// Card 0 of four on its way to slot 2: 1 and 2 close up, 3 stays.
		{"down", 0, 2, []int{2, 0, 1, 3}, 4},
		{"up", 3, 1, []int{0, 2, 3, 1}, 4},
		{"in place", 1, 1, []int{0, 1, 2, 3}, 4},
		// A card from another strip coming in at slot 1.
		{"in", -1, 1, []int{0, 2, 3, 4}, 5},
		{"in last", -1, 4, []int{0, 1, 2, 3}, 5},
	} {
		slots, total := makeRoom(4, tc.from, tc.to)
		if !slices.Equal(slots, tc.slots) || total != tc.total {
			t.Errorf("%s: slots %v of %d, want %v of %d", tc.name, slots, total, tc.slots, tc.total)
		}
	}
	l := layoutStrip(3, 200, 1000, 0, 20, defPanel, false)
	c := l.cards
	if got := l.nearestSlot(c[2].Y + c[2].H/2 + 30); got != 2 {
		t.Fatalf("nearest slot %d", got)
	}
}
