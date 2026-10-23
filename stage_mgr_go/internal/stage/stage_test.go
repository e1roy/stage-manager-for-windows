package stage

import (
	"slices"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func equalSets(a, b [][]string) bool {
	return slices.EqualFunc(a, b, func(x, y []string) bool { return slices.Equal(x, y) })
}

func TestFirstStripFollowsZOrder(t *testing.T) {
	var d Display
	d.Init("code", t0)
	got := d.Strip([]string{"code", "chrome", "mail", "music"})
	if want := [][]string{{"chrome"}, {"mail"}, {"music"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
}

func TestSwitchTakesPickedSlot(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail", "music"})
	// Picking "mail" (second card) sends "code" into its slot.
	d.Switch([]string{"mail"}, t0)
	got := d.Strip([]string{"mail", "code", "chrome", "music"})
	if want := [][]string{{"chrome"}, {"code"}, {"music"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	if !d.OnStage("mail") || d.OnStage("code") {
		t.Fatalf("stage %v", d.Stage())
	}
}

func TestSwitchWithoutSlotEntersAtTop(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail"})
	// The wallpaper was clicked: nothing comes in.
	d.Switch(nil, t0)
	got := d.Strip([]string{"code", "chrome", "mail"})
	if want := [][]string{{"code"}, {"chrome"}, {"mail"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	if !d.Empty() {
		t.Fatalf("stage %v", d.Stage())
	}
}

func TestSwitchWithoutSlotGoesBack(t *testing.T) {
	var d Display
	d.Init("", t0)
	d.Strip([]string{"code", "chrome", "mail", "music"})
	d.Switch(d.SetOf("chrome"), t0)
	// The wallpaper was clicked: chrome goes back to its slot, not the top.
	d.Switch(nil, t0)
	got := d.Strip([]string{"code", "chrome", "mail", "music"})
	if want := [][]string{{"code"}, {"chrome"}, {"mail"}, {"music"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	// From the top card, it goes back to the top.
	d.Switch(d.SetOf("code"), t0)
	d.Switch(nil, t0)
	got = d.Strip([]string{"code", "chrome", "mail", "music"})
	if want := [][]string{{"code"}, {"chrome"}, {"mail"}, {"music"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
}

func TestSwitchWithoutSlotGoesBackBelowTheSameSet(t *testing.T) {
	var d Display
	d.Init("", t0)
	d.Strip([]string{"code", "chrome", "mail", "music"})
	d.Switch(d.SetOf("mail"), t0)
	// "music" takes the place of "mail", which takes its slot: "music" came
	// from below "chrome", where "mail" is now.
	d.Switch(d.SetOf("music"), t0)
	d.Switch(nil, t0)
	got := d.Strip([]string{"code", "chrome", "mail", "music"})
	if want := [][]string{{"code"}, {"chrome"}, {"music"}, {"mail"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	// Joined by another app, the stage still goes back where it came from.
	d.Switch(d.SetOf("chrome"), t0)
	d.Switch(append(d.Stage(), "mail"), t0)
	d.Switch(nil, t0)
	got = d.Strip([]string{"code", "chrome", "mail", "music"})
	if want := [][]string{{"code"}, {"chrome", "mail"}, {"music"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	// The app it came from below is gone: it enters at the top.
	d.Switch(d.SetOf("music"), t0)
	d.Strip([]string{"music", "mail"})
	d.Switch(nil, t0)
	got = d.Strip([]string{"music", "mail"})
	if want := [][]string{{"music"}, {"mail"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
}

func TestNewcomersEnterAtTopAndGoneLeave(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail"})
	got := d.Strip([]string{"notes", "code", "mail"})
	if want := [][]string{{"notes"}, {"mail"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
}

func TestJoinAndPrune(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail"})
	d.Join("mail", t0)
	if !d.OnStage("mail") || !d.OnStage("code") {
		t.Fatalf("stage %v", d.Stage())
	}
	// Within the settle time nothing is pruned.
	if gone := d.Prune(func(a string) bool { return a == "code" }, t0.Add(100*time.Millisecond)); gone != nil {
		t.Fatalf("pruned %v while settling", gone)
	}
	gone := d.Prune(func(a string) bool { return a == "code" }, t0.Add(2*time.Second))
	if !slices.Equal(gone, []string{"mail"}) || d.OnStage("mail") {
		t.Fatalf("pruned %v, stage %v", gone, d.Stage())
	}
	if got := d.Strip([]string{"code", "chrome", "mail"}); !slices.Equal(got[0], []string{"mail"}) {
		t.Fatalf("pruned app not at the top: %v", got)
	}
}

func TestStageLeavesAndReturnsAsOneSet(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail", "music"})
	d.Join("chrome", t0)
	if got := d.Strip([]string{"code", "chrome", "mail", "music"}); !equalSets(got, [][]string{{"mail"}, {"music"}}) {
		t.Fatalf("strip %v", got)
	}
	// Picking "music" sends code and chrome into its slot, together.
	d.Switch(d.SetOf("music"), t0)
	got := d.Strip([]string{"music", "code", "chrome", "mail"})
	if want := [][]string{{"mail"}, {"code", "chrome"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	// Coming back to either app brings the whole set.
	if set := d.SetOf("chrome"); !slices.Equal(set, []string{"code", "chrome"}) {
		t.Fatalf("set of chrome %v", set)
	}
	d.Switch(d.SetOf("chrome"), t0)
	if !d.OnStage("code") || !d.OnStage("chrome") || d.OnStage("music") {
		t.Fatalf("stage %v", d.Stage())
	}
	got = d.Strip([]string{"code", "chrome", "music", "mail"})
	if want := [][]string{{"mail"}, {"music"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
}

func TestSetsLoseAppsThatGoAndSplit(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail", "music"})
	d.Join("chrome", t0)
	d.Join("mail", t0)
	d.Switch([]string{"music"}, t0)
	if got := d.Strip([]string{"music", "code", "chrome", "mail"}); !equalSets(got, [][]string{{"code", "chrome", "mail"}}) {
		t.Fatalf("strip %v", got)
	}
	// Chrome quits: the set keeps the others.
	if got := d.Strip([]string{"music", "code", "mail"}); !equalSets(got, [][]string{{"code", "mail"}}) {
		t.Fatalf("strip %v", got)
	}
	d.Split("mail")
	if got := d.Strip([]string{"music", "code", "mail"}); !equalSets(got, [][]string{{"code"}, {"mail"}}) {
		t.Fatalf("strip %v", got)
	}
}

func TestSendingOneAppToTheStrip(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail"})
	d.Join("chrome", t0)
	// Chrome's window was dragged into the strip: it leaves on its own.
	d.Switch([]string{"code"}, t0)
	got := d.Strip([]string{"code", "chrome", "mail"})
	if want := [][]string{{"chrome"}, {"mail"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
}

func TestJoiningPartOfASetLeavesTheRest(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail", "music"})
	d.Join("chrome", t0)
	d.Switch([]string{"music"}, t0)
	// A window of chrome moved onto the stage brings chrome alone.
	d.Join("chrome", t0)
	got := d.Strip([]string{"music", "code", "chrome", "mail"})
	if want := [][]string{{"mail"}, {"code"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
}

func TestPreviousIsTheLastStageLeft(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail"})
	if p := d.Previous(); p != nil {
		t.Fatalf("previous %v before any switch", p)
	}
	// From the taskbar, to "mail" and then "chrome": "mail" left last.
	d.Switch([]string{"mail"}, t0)
	d.Switch([]string{"chrome"}, t0)
	if p, want := d.Previous(), []string{"mail"}; !slices.Equal(p, want) {
		t.Fatalf("previous %v, want %v", p, want)
	}
	// "chrome" closes: it is no stage to go back to, "mail" is.
	d.Prune(func(a string) bool { return a != "chrome" }, t0.Add(time.Minute))
	d.Strip([]string{"code", "mail"})
	if p, want := d.Previous(), []string{"mail"}; !slices.Equal(p, want) {
		t.Fatalf("previous %v, want %v", p, want)
	}
	// "mail" closes while in the strip: "code" is next.
	d.Strip([]string{"code"})
	if p, want := d.Previous(), []string{"code"}; !slices.Equal(p, want) {
		t.Fatalf("previous %v, want %v", p, want)
	}
}

func TestPreviousComesBackAsItsSet(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail"})
	d.Join("chrome", t0)
	d.Switch([]string{"mail"}, t0)
	if p, want := d.Previous(), []string{"code", "chrome"}; !slices.Equal(p, want) {
		t.Fatalf("previous %v, want %v", p, want)
	}
}

func TestCompanionJoinsTheStage(t *testing.T) {
	var d Display
	d.Init("files", t0)
	d.Strip([]string{"files", "chrome"})
	// A commit dialog opened from the file manager comes beside it.
	if got, want := d.Coming("svn", true), []string{"files", "svn"}; !slices.Equal(got, want) {
		t.Fatalf("coming %v, want %v", got, want)
	}
	if got, want := d.Coming("chrome", false), []string{"chrome"}; !slices.Equal(got, want) {
		t.Fatalf("coming %v, want %v", got, want)
	}
	// On an empty stage it is the stage.
	var e Display
	if got, want := e.Coming("svn", true), []string{"svn"}; !slices.Equal(got, want) {
		t.Fatalf("coming %v on an empty stage, want %v", got, want)
	}
}

func TestCompanionInASetBringsTheSet(t *testing.T) {
	var d Display
	d.Init("files", t0)
	d.Strip([]string{"files", "svn", "chrome"})
	d.Switch(d.Coming("svn", true), t0)
	// The user leaves for another app mid-commit and comes back to the
	// dialog: the file manager comes back with it.
	d.Switch([]string{"chrome"}, t0)
	if got, want := d.Coming("svn", true), []string{"files", "svn"}; !slices.Equal(got, want) {
		t.Fatalf("coming %v, want %v", got, want)
	}
	// Alone in the strip, it joins.
	d.Split("svn")
	if got, want := d.Coming("svn", true), []string{"chrome", "svn"}; !slices.Equal(got, want) {
		t.Fatalf("coming %v, want %v", got, want)
	}
}
