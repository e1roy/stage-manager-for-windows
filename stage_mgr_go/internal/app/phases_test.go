package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"

	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

func TestTripCourse(t *testing.T) {
	// A trip drawn as it should be, and one turned back mid-way.
	for _, course := range [][]tripPhase{
		{tripHolding, tripFlying, tripArrived, tripLanded, tripDone},
		{tripHolding, tripFlying, tripStopped},
		{tripHolding, tripFlying, tripArrived, tripStopped},
		// Timed out before the layer drew: it takes off and lands at once.
		{tripFlying, tripLanded, tripDone},
	} {
		tr := &trip{}
		for _, p := range course {
			if from := tr.phase; !tr.to(p) {
				t.Fatalf("course %v: no way from %v to %v", course, from, p)
			}
		}
	}
}

func TestTripRefusesWhatItsCourseDoesNot(t *testing.T) {
	for _, c := range []struct {
		from, to tripPhase
		why      string
	}{
		{tripPending, tripStopped, "nothing to stop before take-off: the real windows are still there"},
		{tripHolding, tripStopped, "nothing to stop before take-off: the real windows are still there"},
		{tripLanded, tripStopped, "landed, the real windows are home again"},
		{tripHolding, tripLanded, "it lands only once it took off"},
		{tripFlying, tripFlying, "it takes off once"},
		{tripArrived, tripArrived, "land is posted once"},
		{tripLanded, tripLanded, "it lands once"},
		{tripFlying, tripDone, "it is done only once landed"},
		{tripStopped, tripLanded, "a stopped trip's windows fly on with the switch after"},
		{tripStopped, tripDone, "a stopped trip is over: done is the switch after's"},
		{tripDone, tripStopped, "done is the end"},
	} {
		tr := &trip{phase: c.from}
		if tr.to(c.to) || tr.phase != c.from {
			t.Errorf("%v to %v: %s", c.from, c.to, c.why)
		}
	}
}

func TestTripProgressFollowsItsPhase(t *testing.T) {
	tr := &trip{phase: tripHolding, span: 1}
	if p := tr.progress(tr.start.Add(5)); p != 0 {
		t.Fatalf("holding, it is at %v, want 0", p)
	}
	tr.phase, tr.stoppedAt = tripStopped, 0.4
	if p := tr.progress(tr.start.Add(5)); p != 0.4 {
		t.Fatalf("stopped, it is at %v, want where it stopped", p)
	}
	if tripPending.flown() || tripHolding.flown() || !tripStopped.flown() || !tripDone.flown() {
		t.Fatal("flown: from take-off on")
	}
	if tripArrived.down() || !tripLanded.down() || tripStopped.down() {
		t.Fatal("down: only once landed")
	}
}

func TestOverviewCourse(t *testing.T) {
	for _, course := range [][]overviewPhase{
		{ovShooting, ovShowing, ovHolding, ovOpening, ovOpen, ovClosing, ovLanding, ovLanded, ovHidden},
		// Closed as it was still opening.
		{ovShooting, ovShowing, ovHolding, ovOpening, ovClosing, ovLanding, ovLanded, ovHidden},
		// Gone before any window left: it only hides.
		{ovShooting, ovHidden},
		{ovShooting, ovShowing, ovHidden},
		{ovShooting, ovShowing, ovHolding, ovHidden},
	} {
		o := &overview{}
		for _, p := range course {
			if from := o.phase; !o.to(p) {
				t.Fatalf("course %v: no way from %v to %v", course, from, p)
			}
		}
	}
}

func TestOverviewRefusesWhatItsCourseDoesNot(t *testing.T) {
	for _, c := range []struct {
		from, to overviewPhase
		why      string
	}{
		{ovHidden, ovOpening, "it takes stills and shows before it opens"},
		{ovShowing, ovOpening, "the real windows go only once its first frame is drawn"},
		{ovHolding, ovClosing, "nothing to fly back before it opened: it hides"},
		{ovOpening, ovHidden, "once open, the windows come back the way they went"},
		{ovOpen, ovHidden, "once open, the windows come back the way they went"},
		{ovClosing, ovHidden, "the windows land before it hides"},
		{ovLanding, ovHidden, "the windows land before it hides"},
		{ovLanded, ovLanded, "it lands once"},
		{ovOpen, ovOpening, "it opens once"},
	} {
		o := &overview{phase: c.from}
		if o.to(c.to) || o.phase != c.from {
			t.Errorf("%v to %v: %s", c.from, c.to, c.why)
		}
	}
}

func TestOverviewPhaseQuestions(t *testing.T) {
	for p := ovHidden; p <= ovLanded; p++ {
		live := p == ovOpening || p == ovOpen
		if p.live() != live {
			t.Errorf("%v: live %v", p, p.live())
		}
		if p.live() && !p.opened() {
			t.Errorf("%v takes the pointer before its windows left", p)
		}
		if p.closing() && !p.opened() {
			t.Errorf("%v closes before it opened", p)
		}
		if p.String() == "" {
			t.Errorf("phase %d has no name", p)
		}
	}
}

func TestHoldLetsGoOnlyForItsHolder(t *testing.T) {
	a := &App{}
	r := &switchRequest{}
	a.take(holdSwitch, r)
	if !a.busy() || a.running != r {
		t.Fatal("a switch holds the stages")
	}
	// The overviews never held them: the switch goes on.
	a.letGo(holdOverview)
	if a.hold != holdSwitch || a.running != r {
		t.Fatalf("let go by another: %v", a.hold)
	}
	a.letGo(holdSwitch)
	if a.busy() || a.running != nil {
		t.Fatalf("let go by the switch: %v, running %v", a.hold, a.running)
	}
	a.take(holdOverview, nil)
	if !a.busy() || a.running != nil {
		t.Fatal("the overviews hold the stages, with no switch running")
	}
}

func TestMinimizeSteps(t *testing.T) {
	m := &minimizeTarget{}
	if m.step != minimizeIconic {
		t.Fatal("a minimize starts before the window is iconic")
	}
	m.observe(true)
	if m.step != minimizeRestoring {
		t.Fatalf("iconic, it is restored: %v", m.step)
	}
}

// TestRequestInheritsHomes: a switch that takes over keeps where the
// windows dragged into the strip go back to, without overriding its own;
// one dropped takes them along, and nothing on the App keeps them.
func TestRequestInheritsHomes(t *testing.T) {
	old := &switchRequest{homes: map[winapi.HWND]winapi.Rect{1: {Left: 10}, 2: {Left: 20}}}
	r := &switchRequest{homes: map[winapi.HWND]winapi.Rect{2: {Left: 99}}}
	r.inherit(old)
	r.inherit(nil)
	if r.homes[1].Left != 10 || r.homes[2].Left != 99 {
		t.Fatalf("homes %v", r.homes)
	}
	empty := &switchRequest{}
	empty.inherit(&switchRequest{})
	if empty.homes != nil {
		t.Fatal("inheriting nothing made a map")
	}
}

// TestEveryActionDispatched: every action is either carried out by
// request's switch or has its own entry point, so that one added silently
// does nothing. Read from the source: calling request with App.act set
// only proves the pass-through, which holds whatever the switch does.
func TestEveryActionDispatched(t *testing.T) {
	src, err := os.ReadFile("switch.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "switch.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	// The actions request's switch carries out, from its cases.
	cases := map[action]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "request" || fn.Body == nil {
			return true
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			cl, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cl.List {
				if id, ok := e.(*ast.Ident); ok {
					cases[actionByName[id.Name]] = true
				}
			}
			return true
		})
		return false
	})
	// Those with an entry point of their own instead.
	own := map[action]bool{actOverview: true, actMove: true, actReorder: true}
	for act := action(0); act < actCount; act++ {
		if own[act] {
			continue
		}
		if !cases[act] {
			t.Errorf("%v is dispatched by neither request's switch nor an entry point of its own", act)
		}
	}
}

// actionByName maps the source's spelling of every action to the action.
// Kept by hand beside the constants: a new one missing here fails the
// test as one dispatched by nothing.
var actionByName = map[string]action{
	"actPick": actPick, "actJoin": actJoin, "actSplit": actSplit,
	"actClose": actClose, "actFront": actFront, "actUnstage": actUnstage,
	"actKeepTogether": actKeepTogether, "actOverview": actOverview, "actMove": actMove,
	"actReorder": actReorder,
}
