package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
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
	a.act = func(act action, _ *display, s *set) { got = append(got, request{act, s.key}) }
	d := &display{cards: map[string]*cardState{}, landing: map[string]bool{}}
	d.mon.Work = winapi.Rect{Right: 2000, Bottom: 900}
	for i, app := range []string{"mail", "notes"} {
		w := catalog.Window{HWND: winapi.HWND(i + 1), Title: app, App: app, Frame: winapi.Rect{Right: 1600, Bottom: 1000}}
		d.sets = append(d.sets, newSet([]*group{{app: app, name: app, windows: []catalog.Window{w}, z: i}}))
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

func TestShiftClickJoinsStage(t *testing.T) {
	s, got, tt := testStrip(t)
	s.d.model.Init("code", time.Now())
	x, y := center(s.d.layout.cards[1])
	tt.ClickAtWith(ui.Shift, x, y)
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != (request{actJoin, "notes"}) {
		t.Fatalf("requests %v", *got)
	}
}

func TestSetCardSplits(t *testing.T) {
	s, got, tt := testStrip(t)
	s.d.sets = []*set{newSet([]*group{s.d.sets[0].apps[0], s.d.sets[1].apps[0]})}
	tt.Frame()
	// The card slides to its new place first.
	tt.Advance(time.Second)
	tt.Frame()
	if len(s.d.layout.cards) != 1 {
		t.Fatalf("%d cards for one set", len(s.d.layout.cards))
	}
	x, y := center(s.d.layout.cards[0])
	tt.RightClickAt(x, y)
	tt.Frame()
	if err := tt.ChooseMenuItem("拆开组合"); err != nil {
		t.Fatalf("%v; menu %v", err, tt.Menu())
	}
	tt.Frame()
	if len(*got) != 1 || (*got)[0] != (request{actSplit, "mail"}) {
		t.Fatalf("requests %v", *got)
	}
}
