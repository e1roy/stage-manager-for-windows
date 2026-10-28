package app

import (
	"testing"

	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo/ui"
)

// The hot corner is the bottom-left corner of its own display, a taskbar's
// height tall and as wide as the settings say, at the display's scale.
func TestHotCorner(t *testing.T) {
	second := winapi.Rect{Left: 1920, Top: 0, Right: 1920 + 2560, Bottom: 1440}
	if r := cornerRect(second, 1.5, 96); r != (winapi.Rect{Left: 1920, Top: 1440 - 72, Right: 1920 + 144, Bottom: 1440}) {
		t.Fatalf("the corner at 150%% is %+v", r)
	}
	if r := cornerRect(second, 1, hotCorner); r.W() != hotCorner || r.H() != hotCorner {
		t.Fatalf("the corner at 100%% is %+v", r)
	}
}

// A wider corner button reaches farther to the right, and its mark shows in
// full along its whole width.
func TestCornerWidth(t *testing.T) {
	first := winapi.Rect{Right: 1920, Bottom: 1080}
	at := func(width float64) float32 { return cornerNearness(first, 1, width, winapi.Point{X: 100, Y: 1079}) }
	if k := at(48); k != 0 {
		t.Fatalf("beyond a narrow corner the mark shows %v", k)
	}
	if k := at(200); k != 1 {
		t.Fatalf("over a wide corner the mark shows %v", k)
	}
}

// The mark shows in full in the corner, comes in as the pointer nears it
// on its own display, and shows nothing from afar or from the display
// beside it.
func TestCornerComesInAsPointerNears(t *testing.T) {
	second := winapi.Rect{Left: 1920, Top: 0, Right: 1920 + 2560, Bottom: 1440}
	near := func(x, y int32) float32 { return cornerNearness(second, 1.5, hotCorner, winapi.Point{X: x, Y: y}) }
	if k := near(1920, 1439); k != 1 {
		t.Fatalf("in the corner the mark shows %v", k)
	}
	if k := near(1920+71, 1440-72); k != 1 {
		t.Fatalf("at the corner's inner edge the mark shows %v", k)
	}
	if k := near(1919, 1439); k != 0 {
		t.Fatalf("from the display to the left the mark shows %v", k)
	}
	if k := near(1920+72+cornerReach*1.5, 1439); k != 0 {
		t.Fatalf("from %v DIPs away the mark shows %v", cornerReach, k)
	}
	last := float32(1)
	for x := int32(1920 + 72); x < 1920+72+cornerReach*2; x += 6 {
		k := near(x, 1439)
		if k > last {
			t.Fatalf("the mark shows more, %v, farther away at %v", k, x)
		}
		last = k
	}
	if k := near(1920+72+15, 1440-72-15); k <= 0 || k >= 1 {
		t.Fatalf("on the way in, above and to the right, the mark shows %v", k)
	}
}

// The accent is the blue of the overview's ring unless the settings pick a
// color, and outlines nothing once the user turns it off.
func TestAccent(t *testing.T) {
	a := &App{settings: settings.Default()}
	if c, on := a.accent(); !on || c != ringColor {
		t.Fatalf("the default accent is %v, outlined %v", c, on)
	}
	a.settings.Accent = "#30D158"
	if c, on := a.accent(); !on || c != ui.RGB(48, 209, 88) {
		t.Fatalf("the green accent is %v, outlined %v", c, on)
	}
	a.settings.Accent = settings.AccentNone
	if _, on := a.accent(); on {
		t.Fatal("no accent still outlines the buttons")
	}
}

// A click in the corner opens the overview of its display; one while the
// overview shows asks for no other.
func TestCornerClickOpensOverview(t *testing.T) {
	var got []action
	a := &App{settings: settings.Default(), media: newMedia(), ready: true}
	d := &display{}
	a.act = func(act action, at *display, _ *set) {
		if at != d {
			t.Errorf("asked for display %p, not %p", at, d)
		}
		got = append(got, act)
	}
	h := &cornerHint{a: a, d: d, visible: true, near: 1}
	tt := ui.NewTester(h.view, hotCorner, hotCorner)
	tt.Frame()
	tt.Move(24, 24)
	tt.Frame()
	tt.ClickAt(24, 24)
	tt.Frame()
	if len(got) != 1 || got[0] != actOverview {
		t.Fatalf("requests %v", got)
	}
	a.ov = &overview{phase: ovOpen}
	tt.ClickAt(24, 24)
	tt.Frame()
	if len(got) != 1 {
		t.Fatalf("a click while the overview shows asked for %v", got)
	}
}
