package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/renkai9418/stage-bar/internal/catalog"
	"github.com/renkai9418/stage-bar/internal/settings"
	"github.com/renkai9418/stage-bar/internal/winapi"
)

type request struct {
	act action
	app string
}

// testStrip builds a strip of two apps whose cards report what they ask
// for, without any window or the system behind them.
func testStrip(t *testing.T) (*strip, *[]request, *ui.Tester) {
	t.Helper()
	var got []request
	a := &App{settings: settings.Default(), media: newMedia(), ready: true}
	a.act = func(act action, _ *display, g *group) { got = append(got, request{act, g.app}) }
	d := &display{cards: map[string]*cardState{}, landing: map[string]bool{}}
	d.mon.Work = winapi.Rect{Right: 2000, Bottom: 900}
	for i, app := range []string{"mail", "notes"} {
		w := catalog.Window{HWND: winapi.HWND(i + 1), Title: app, App: app, Frame: winapi.Rect{Right: 1600, Bottom: 1000}}
		d.groups = append(d.groups, &group{app: app, name: app, windows: []catalog.Window{w}})
	}
	s := &strip{a: a, d: d, shown: true, scale: 1, rect: winapi.Rect{Right: 200, Bottom: 900}}
	d.strip = s
	tt := ui.NewTester(s.view, 200, 900)
	tt.Frame()
	return s, &got, tt
}

func center(r ui.Rect) (float32, float32) { return r.X + r.W/2, r.Y + r.H/2 }

func TestClickPicksCard(t *testing.T) {
	s, got, tt := testStrip(t)
	x, y := center(s.d.layout.cards[1])
	tt.ClickAt(x, y)
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != (request{actPick, "notes"}) {
		t.Fatalf("requests %v", *got)
	}
	if s.dragging {
		t.Fatal("a click left the strip dragging")
	}
}

func TestDragOutJoinsStage(t *testing.T) {
	s, got, tt := testStrip(t)
	x, y := center(s.d.layout.cards[0])
	tt.Move(x, y)
	tt.Frame()
	tt.Press(x, y)
	tt.Frame()
	for i := 1; i <= 10; i++ {
		tt.Move(x+float32(i)*25, y)
		tt.Frame()
	}
	if !s.dragging {
		t.Fatal("not dragging after the pointer moved")
	}
	tt.Release(x+250, y)
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != (request{actJoin, "mail"}) {
		t.Fatalf("requests %v", *got)
	}
	if s.dragging {
		t.Fatal("still dragging after release")
	}
}

func TestShortDragReturnsWithoutClick(t *testing.T) {
	s, got, tt := testStrip(t)
	x, y := center(s.d.layout.cards[0])
	tt.Move(x, y)
	tt.Frame()
	tt.Press(x, y)
	tt.Frame()
	for i := 1; i <= 4; i++ {
		tt.Move(x, y+float32(i)*10)
		tt.Frame()
	}
	tt.Release(x, y+40)
	tt.Frame()
	tt.Advance(time.Second)
	if len(*got) != 0 {
		t.Fatalf("a drag that went nowhere asked for %v", *got)
	}
}

func TestContextMenu(t *testing.T) {
	s, got, tt := testStrip(t)
	x, y := center(s.d.layout.cards[0])
	tt.RightClickAt(x, y)
	tt.Frame()
	if m := tt.Menu(); len(m) == 0 || m[0] != "切换到舞台" {
		t.Fatalf("menu %v", m)
	}
	if err := tt.ChooseMenuItem("关闭窗口"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != (request{actClose, "mail"}) {
		t.Fatalf("requests %v", *got)
	}
}

func TestHiddenStripHasNoHits(t *testing.T) {
	s, _, tt := testStrip(t)
	if len(s.hits) != 2 {
		t.Fatalf("hits %v", s.hits)
	}
	s.shown = false
	tt.Frame()
	if len(s.hits) != 0 {
		t.Fatalf("a hidden strip keeps hits %v", s.hits)
	}
}
