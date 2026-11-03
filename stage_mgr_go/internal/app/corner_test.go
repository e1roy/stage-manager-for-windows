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
	d.ov = &overview{phase: ovOpen}
	tt.ClickAt(24, 24)
	tt.Frame()
	if len(got) != 1 {
		t.Fatalf("a click while the overview shows asked for %v", got)
	}
}

// One overview per display: anyOverview finds the one that shows, and
// overviewWindow tells their windows from others', which the ones a flyout
// is dismissed by are.
func TestOverviewWindows(t *testing.T) {
	one := &overview{phase: ovOpen, hwnd: 0x11}
	hidden := &overview{phase: ovHidden, hwnd: 0x22}
	a := &App{displays: []*display{{ov: one}, {ov: hidden}, {}}}
	if !a.anyOverview() {
		t.Fatal("an overview shows and anyOverview says none does")
	}
	if !a.overviewWindow(0x11) {
		t.Fatal("the window of the overview showing is not recognized")
	}
	if !a.overviewWindow(0x22) {
		t.Fatal("the window of an overview made but hidden is not recognized")
	}
	if a.overviewWindow(0) || a.overviewWindow(0x33) {
		t.Fatal("a window that is no overview's is taken for one")
	}
	one.phase = ovHidden
	if a.anyOverview() {
		t.Fatal("no overview shows and anyOverview says one does")
	}
}

// Joined, the strip's panel comes down to the taskbar and in to the
// display's left edge, as wide as on its own from there, and the corner
// button under it is as wide, from the taskbar's top to the bottom.
func TestJointButton(t *testing.T) {
	p := panel{share: 1, h: 38}
	alone := footerButton(220, 1000, p, false)
	r := jointButton(220, 1000, p)
	if r.X != 0 || r.Y+r.H != 1000 || r.H != p.h || r.W != alone.X+alone.W {
		t.Fatalf("the joined panel is %v, on its own %v", r, alone)
	}
	if b := jointBand(220, 1000, p); b.X != 0 || b.Y+b.H != 1000 || b.Y != r.Y-footerInset || b.W > 220 {
		t.Fatalf("the joined panel takes the pointer in %v", b)
	}
	a := &App{settings: settings.Default()}
	m := winapi.Rect{Left: 1920, Right: 1920 + 2560, Bottom: 1440}
	d := &display{}
	d.mon.Monitor = m
	s := &strip{a: a, d: d, joined: true, scale: 1.5, rect: winapi.Rect{Left: 1920, Right: 1920 + 330, Bottom: 1440 - 72}}
	d.strip = s
	c := a.cornerBounds(d)
	if c.Left != m.Left || c.Top != s.rect.Bottom || c.Bottom != m.Bottom || float64(c.W()) < float64(s.jointWidth())*1.5 {
		t.Fatalf("the joined corner is %+v under %+v, %v DIPs wide", c, s.rect, s.jointWidth())
	}
	if h := s.cornerHeight(); h != 48 {
		t.Fatalf("the joined corner is %v DIPs tall", h)
	}
	s.joined = false
	if c := a.cornerBounds(d); c != cornerRect(m, d.mon.Scale(), a.cornerWidth()) {
		t.Fatalf("parted, the corner is %+v", c)
	}
}

// The mark of a joined button is centered on it, but never across the
// seam between its two windows.
func TestJointGlyph(t *testing.T) {
	for _, c := range []struct{ top, below float32 }{{38, 48}, {12, 48}, {80, 48}, {38, 30}, {48, 48}} {
		const gh = 14
		y := jointGlyph(c.top, c.below, gh)
		if y < c.top && y+gh > c.top {
			t.Fatalf("%v over %v: the mark at %v crosses the seam", c.top, c.below, y)
		}
		if y < 0 || y+gh > c.top+c.below {
			t.Fatalf("%v over %v: the mark at %v is out of the button", c.top, c.below, y)
		}
		if mid := (c.top + c.below) / 2; y+gh/2 < mid-gh || y+gh/2 > mid+gh {
			t.Fatalf("%v over %v: the mark at %v is far from the middle %v", c.top, c.below, y, mid)
		}
	}
}

// A joined panel takes the pointer down to the strip's bottom, and lights
// up with the corner button under it.
func TestJoinedFooter(t *testing.T) {
	s, _, tt := testStrip(t)
	s.d.mon.Monitor = winapi.Rect{Right: 2000, Bottom: 948}
	s.joined = true
	tt.Frame()
	if b := jointBand(200, 900, s.a.panel()); s.footerHit != b {
		t.Fatalf("the joined panel takes the pointer in %v, not %v", s.footerHit, b)
	}
	if !s.over(winapi.Point{X: 1, Y: 899}) {
		t.Fatal("the strip lets the pointer through in its bottom-left corner")
	}
	tt.Move(4, 896)
	tt.Frame()
	if !s.footHover {
		t.Fatal("the corner button does not hear of the pointer over the panel")
	}
	s.d.inOverview = true
	tt.Frame()
	if s.footHover || s.footerHit.W != 0 {
		t.Fatalf("the panel is still lit %v, in %v, while the overview shows", s.footHover, s.footerHit)
	}
}
