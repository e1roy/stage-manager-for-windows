package app

import (
	"slices"
	"testing"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo/ui"
)

func TestLineupNeedsTwoWindows(t *testing.T) {
	for _, n := range []int{0, 1} {
		if l := layoutLineup(n, 200, 1000, false); l.box.W != 0 || l.extent != 0 || len(l.tiles) != 0 {
			t.Fatalf("a stage of %d windows has a list %+v", n, l)
		}
	}
}

func TestLineupFits(t *testing.T) {
	for _, height := range []float32{600, 900, 1400} {
		for n := 2; n <= 30; n++ {
			l := layoutLineup(n, 200, height, false)
			if len(l.tiles) == 0 || len(l.tiles) != len(l.thumbs) {
				t.Fatalf("%d windows in %v: tiles %v thumbs %v", n, height, l.tiles, l.thumbs)
			}
			if got := len(l.tiles); got < n && l.more.W == 0 || got == n && l.more.W != 0 {
				t.Fatalf("%d windows in %v: %d tiles, more %v", n, height, got, l.more)
			}
			// It keeps to its share of the strip, unless that leaves no
			// room for even one window.
			if bottom := l.box.Y + l.box.H; bottom > height*lineupShare+lineupTop+1 && len(l.tiles) > 1 {
				t.Fatalf("%d windows in %v reach %v", n, height, bottom)
			}
			for i, r := range l.tiles {
				th := l.thumbs[i]
				if th.H < lineupThumbMin-0.01 || th.H > lineupThumbMax+0.01 {
					t.Fatalf("%d windows in %v: a picture %v tall", n, height, th.H)
				}
				if th.X < r.X || th.Y < r.Y || th.X+th.W > r.X+r.W || th.Y+th.H > r.Y+r.H {
					t.Fatalf("picture %v out of its tile %v", th, r)
				}
				if i > 0 && r.Y < l.tiles[i-1].Y+l.tiles[i-1].H {
					t.Fatalf("tiles overlap: %v %v", l.tiles[i-1], r)
				}
			}
		}
	}
}

func TestCardsKeepBelowLineup(t *testing.T) {
	line := layoutLineup(3, 200, 1000, false)
	for _, n := range []int{1, 3, 12} {
		l := layoutStrip(n, 200, 1000, 0, 20, defPanel, line.extent, false)
		if l.cards[0].Y < line.box.Y+line.box.H {
			t.Fatalf("%d cards start at %v, in the list ending at %v", n, l.cards[0].Y, line.box.Y+line.box.H)
		}
		// Too many to fit scroll, from below the list.
		last := l.cards[n-1]
		if l.maxScroll(1000) == 0 && last.Y+last.H > 1000-defPanel.h-footerInset {
			t.Fatalf("%d cards reach the panel: %v", n, last)
		}
	}
	// With no list, a strip is laid out as it was before there was one.
	if a, b := layoutStrip(3, 200, 1000, 0, 20, defPanel, 0, false), layoutStrip(3, 200, 1000, 0, 20, defPanel, stripMargin, false); !slices.Equal(a.cards, b.cards) {
		t.Fatalf("cards moved for a list that is not there: %v %v", a.cards, b.cards)
	}
}

func TestLineupMirrors(t *testing.T) {
	l, r := layoutLineup(3, 220, 1000, false), layoutLineup(3, 220, 1000, true)
	if r.box != mirror(l.box, ui.Rect{W: 220}) || r.tiles[1] != mirror(l.tiles[1], ui.Rect{W: 220}) {
		t.Fatalf("left %v right %v", l.box, r.box)
	}
}

func TestFitIn(t *testing.T) {
	area := ui.Rect{X: 10, Y: 10, W: 160, H: 60}
	if f := fitIn(area, 16.0/9); f.H != 60 || f.W > 160 || f.X <= 10 {
		t.Fatalf("wide %v", f)
	}
	if f := fitIn(area, 4); f.W != 160 || f.H != 40 || f.Y != 20 {
		t.Fatalf("very wide %v", f)
	}
}

// testLineup builds a strip whose display has three windows of two apps on
// its stage.
func testLineup(t *testing.T) (*strip, *[]request, *ui.Tester) {
	t.Helper()
	s, got, _ := testStrip(t)
	win := func(h int, app string, z int) catalog.Window {
		return catalog.Window{HWND: winapi.HWND(h), Title: app + " window", App: app, Z: z, Frame: winapi.Rect{Right: 1600, Bottom: 1000}}
	}
	s.d.onStage = []*group{
		{app: "code", name: "Code", windows: []catalog.Window{win(10, "code", 0), win(11, "code", 2)}},
		{app: "chrome", name: "Chrome", windows: []catalog.Window{win(20, "chrome", 1)}},
	}
	tt := ui.NewTester(s.view, 200, 900)
	tt.Frame()
	return s, got, tt
}

func (s *strip) testLineupLayout() lineupLayout {
	return layoutLineup(len(s.line.order), 200, 900, false)
}

func TestLineupListsStage(t *testing.T) {
	s, _, _ := testLineup(t)
	want := []winapi.HWND{10, 11, 20}
	if !slices.Equal(s.line.order, want) {
		t.Fatalf("order %v", s.line.order)
	}
	if s.lineupHit.W == 0 {
		t.Fatal("the list takes no pointer")
	}
	if line := s.testLineupLayout(); s.d.layout.cards[0].Y < line.extent-0.5 {
		t.Fatalf("a card at %v, in the list down to %v", s.d.layout.cards[0].Y, line.extent)
	}
}

func TestLineupOrderStaysAsZChanges(t *testing.T) {
	s, _, tt := testLineup(t)
	// The second window of Code comes in front, as a click on it does.
	code := s.d.onStage[0]
	code.windows[0], code.windows[1] = code.windows[1], code.windows[0]
	code.windows[0].Z, code.windows[1].Z = 0, 2
	tt.Frame()
	if want := []winapi.HWND{10, 11, 20}; !slices.Equal(s.line.order, want) {
		t.Fatalf("order %v after the z-order changed", s.line.order)
	}
	// A new window of Code goes after its others, not to the end.
	code.windows = append(code.windows, catalog.Window{HWND: 12, App: "code", Z: 3})
	tt.Frame()
	if want := []winapi.HWND{10, 11, 12, 20}; !slices.Equal(s.line.order, want) {
		t.Fatalf("order %v after a window opened", s.line.order)
	}
}

func TestLineupClickBringsForward(t *testing.T) {
	s, got, tt := testLineup(t)
	tile := s.testLineupLayout().tiles[2]
	tt.ClickAt(center(tile))
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != (request{actFront, "chrome"}) {
		t.Fatalf("requests %v", *got)
	}
}

func TestLineupMenu(t *testing.T) {
	s, got, tt := testLineup(t)
	tile := s.testLineupLayout().tiles[1]
	tt.RightClickAt(center(tile))
	tt.Frame()
	tr := s.a.tr()
	if m := tt.Menu(); !slices.Contains(m, tr.BringToFront) || !slices.Contains(m, tr.SendToStrip) || !slices.Contains(m, tr.CloseWindow) {
		t.Fatalf("menu %v", m)
	}
	if err := tt.ChooseMenuItem(tr.SendToStrip); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != (request{actUnstage, "code"}) {
		t.Fatalf("requests %v", *got)
	}
}

func TestLineupCloseButton(t *testing.T) {
	s, got, tt := testLineup(t)
	tile := s.testLineupLayout().tiles[0]
	tt.Move(center(tile))
	tt.Frame()
	tt.Frame()
	// The button shows at the caption's end while the pointer is on the tile.
	x, y := tile.X+tile.W-lineupInset-1-8, tile.Y+lineupCaption/2
	tt.Move(x, y)
	tt.Frame()
	tt.ClickAt(x, y)
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != (request{actClose, "code"}) {
		t.Fatalf("requests %v", *got)
	}
}

func TestLineupOff(t *testing.T) {
	s, _, tt := testLineup(t)
	s.a.settings.StageList = false
	tt.Frame()
	if s.lineupHit.W != 0 || len(s.line.order) != 0 {
		t.Fatalf("a list turned off shows at %v with %v", s.lineupHit, s.line.order)
	}
	if want := layoutStrip(2, 200, 900, 0, 20, defPanel, 0, false); !slices.Equal(s.d.layout.cards, want.cards) {
		t.Fatalf("cards %v, want %v", s.d.layout.cards, want.cards)
	}
}

// However long a title, the close button keeps to the tile's top corner,
// active or not.
func TestLineupCloseButtonLongTitle(t *testing.T) {
	for _, i := range []int{0, 1} {
		s, got, tt := testLineup(t)
		for _, g := range s.d.onStage {
			for j := range g.windows {
				g.windows[j].Title = "NewKing - jvm.g1_10001.options - Visual Studio Code [SSH: devcloud]"
			}
		}
		s.a.fg.prev = s.d.onStage[0].windows[0].HWND
		tt.Frame()
		tile := s.testLineupLayout().tiles[i]
		tt.Move(center(tile))
		tt.Frame()
		tt.Frame()
		x, y := tile.X+tile.W-lineupInset-1-8, tile.Y+lineupCaption/2
		tt.Move(x, y)
		tt.Frame()
		tt.ClickAt(x, y)
		tt.Frame()
		if len(*got) != 1 || (*got)[0] != (request{actClose, "code"}) {
			t.Fatalf("tile %d: requests %v", i, *got)
		}
	}
}

func TestLineupHidesInOverview(t *testing.T) {
	s, _, tt := testLineup(t)
	s.d.inOverview = true
	tt.Frame()
	if s.lineupHit.W != 0 {
		t.Fatalf("the list shows at %v under the overview", s.lineupHit)
	}
}
