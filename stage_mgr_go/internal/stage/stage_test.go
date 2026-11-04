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

func TestPrune(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail"})
	d.Switch(append(d.Stage(), "mail"), t0)
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
	d.Switch(append(d.Stage(), "chrome"), t0)
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
	d.Switch(append(d.Stage(), "chrome"), t0)
	d.Switch(append(d.Stage(), "mail"), t0)
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
	d.Switch(append(d.Stage(), "chrome"), t0)
	// Chrome's window was dragged into the strip: it leaves on its own.
	d.Switch([]string{"code"}, t0)
	got := d.Strip([]string{"code", "chrome", "mail"})
	if want := [][]string{{"chrome"}, {"mail"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
}

func TestDroppingPartOfASetLeavesTheRest(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail", "music"})
	d.Switch(append(d.Stage(), "chrome"), t0)
	d.Switch([]string{"music"}, t0)
	d.Switch([]string{"chrome"}, t0)
	if got, want := d.Stage(), []string{"chrome"}; !slices.Equal(got, want) {
		t.Fatalf("stage %v, want %v", got, want)
	}
	got := d.Strip([]string{"music", "code", "chrome", "mail"})
	if want := [][]string{{"mail"}, {"music"}, {"code"}}; !equalSets(got, want) {
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
	d.Switch(append(d.Stage(), "chrome"), t0)
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

func TestInsertPutsSetInSlot(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail"})
	// A card dragged in from another display, dropped between the two.
	d.Insert([]string{"music", "notes"}, 1)
	got := d.Strip([]string{"code", "chrome", "mail", "music", "notes"})
	if want := [][]string{{"chrome"}, {"music", "notes"}, {"mail"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	// An app already in a card here leaves it; one on the stage stays.
	// The slot counts the card it left, as the strip showed it.
	d.Insert([]string{"code", "chrome"}, 2)
	got = d.Strip([]string{"code", "chrome", "mail", "music", "notes"})
	if want := [][]string{{"music", "notes"}, {"chrome"}, {"mail"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	if !d.OnStage("code") {
		t.Fatalf("stage %v", d.Stage())
	}
	// Past the end, it goes last.
	d.Insert([]string{"music"}, 9)
	got = d.Strip([]string{"code", "chrome", "mail", "music", "notes"})
	if want := [][]string{{"notes"}, {"chrome"}, {"mail"}, {"music"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
}

func TestMoveReordersStrip(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail", "music"})
	d.Move("chrome", 2)
	got := d.Strip([]string{"code", "chrome", "mail", "music"})
	if want := [][]string{{"mail"}, {"music"}, {"chrome"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	d.Move("chrome", 0)
	got = d.Strip([]string{"code", "chrome", "mail", "music"})
	if want := [][]string{{"chrome"}, {"mail"}, {"music"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	// An app on the stage has no card to move.
	d.Move("code", 1)
	if !d.OnStage("code") {
		t.Fatalf("stage %v", d.Stage())
	}
}

// kept makes a display whose strip is kept (Keep), with the stage on none.
func kept(apps ...string) *Display {
	d := &Display{}
	d.Keep(true)
	d.Strip(apps)
	return d
}

func TestKeptStripListsTheStageInItsSlot(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Keep(true)
	got := d.Strip([]string{"code", "chrome", "mail", "music"})
	if want := [][]string{{"code"}, {"chrome"}, {"mail"}, {"music"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	// Turned off, the stage leaves the strip, and turned on it comes back to
	// the slot it came from.
	d.Switch(d.SetOf("mail"), t0)
	d.Keep(false)
	if got := d.Strip([]string{"mail", "code", "chrome", "music"}); !equalSets(got, [][]string{{"code"}, {"chrome"}, {"music"}}) {
		t.Fatalf("strip %v", got)
	}
	d.Keep(true)
	if got := d.Strip([]string{"mail", "code", "chrome", "music"}); !equalSets(got, [][]string{{"code"}, {"chrome"}, {"mail"}, {"music"}}) {
		t.Fatalf("strip %v", got)
	}
}

// The strip of 1 to 6 with 6 on the stage never reorders, whichever app is
// picked: the apps that leave go back to their slots.
func TestKeptSwitchMovesNoCard(t *testing.T) {
	apps := []string{"1", "2", "3", "4", "5", "6"}
	want := [][]string{{"1"}, {"2"}, {"3"}, {"4"}, {"5"}, {"6"}}
	d := kept(apps...)
	d.Switch(d.SetOf("6"), t0)
	for _, pick := range []string{"3", "1", "5", "6", "2"} {
		d.Switch(d.SetOf(pick), t0)
		if got := d.Strip(apps); !equalSets(got, want) {
			t.Fatalf("strip %v after picking %s, want %v", got, pick, want)
		}
		if got := d.Stage(); !slices.Equal(got, []string{pick}) {
			t.Fatalf("stage %v, want %s", got, pick)
		}
	}
}

func TestKeptPreviousIsTheLastStageLeft(t *testing.T) {
	apps := []string{"1", "2", "3", "4"}
	d := kept(apps...)
	d.Switch(d.SetOf("4"), t0)
	if p := d.Previous(); p != nil {
		t.Fatalf("previous %v before any app left", p)
	}
	d.Switch(d.SetOf("2"), t0)
	// 4 is the stage last left; 2, on the stage, is no stage to go back to.
	if p, want := d.Previous(), []string{"4"}; !slices.Equal(p, want) {
		t.Fatalf("previous %v, want %v", p, want)
	}
	d.Switch(d.SetOf("4"), t0)
	if p, want := d.Previous(), []string{"2"}; !slices.Equal(p, want) {
		t.Fatalf("previous %v, want %v", p, want)
	}
	// The wallpaper was clicked: 4 is back in the strip, and the one to
	// return to is still the last off the stage before it.
	d.Switch(nil, t0)
	if p, want := d.Previous(), []string{"4"}; !slices.Equal(p, want) {
		t.Fatalf("previous %v, want %v", p, want)
	}
	if got := d.Strip(apps); !equalSets(got, [][]string{{"1"}, {"2"}, {"3"}, {"4"}}) {
		t.Fatalf("strip %v", got)
	}
}

func TestKeptSetsStayTogetherInTheirSlots(t *testing.T) {
	apps := []string{"1", "2", "3", "4", "5"}
	d := kept(apps...)
	d.Switch(d.SetOf("4"), t0)
	// 2 joins the stage: they make one card, in the slot of the stage's.
	d.Switch(append(d.Stage(), "2"), t0)
	if got, want := d.Strip(apps), [][]string{{"1"}, {"3"}, {"4", "2"}, {"5"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	d.Switch(d.SetOf("5"), t0)
	if got, want := d.Strip(apps), [][]string{{"1"}, {"3"}, {"4", "2"}, {"5"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	// Picking the set brings both, and the stage's card stays where it is.
	d.Switch(d.SetOf("2"), t0)
	if got, want := d.Stage(), []string{"4", "2"}; !slices.Equal(got, want) {
		t.Fatalf("stage %v, want %v", got, want)
	}
	if got, want := d.Strip(apps), [][]string{{"1"}, {"3"}, {"4", "2"}, {"5"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	// One of them goes on its own: the other stays on the stage, beside it.
	d.Switch([]string{"4"}, t0)
	if got, want := d.Strip(apps), [][]string{{"1"}, {"3"}, {"4"}, {"2"}, {"5"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	// The stage's set splits in its slot; its apps stay on the stage until
	// the next switch, which leaves each where it is.
	d.Switch(d.SetOf("5"), t0)
	d.Switch(append(d.Stage(), "1", "3"), t0)
	if got, want := d.Strip(apps), [][]string{{"4"}, {"2"}, {"5", "1", "3"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	d.Split("1")
	if got, want := d.Stage(), []string{"5", "1", "3"}; !slices.Equal(got, want) {
		t.Fatalf("stage %v, want %v", got, want)
	}
	d.Switch([]string{"1"}, t0)
	if got, want := d.Strip(apps), [][]string{{"4"}, {"2"}, {"5"}, {"1"}, {"3"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
}

// Elsewhere the stage's set cannot be split: it is not in the strip.
func TestStageSetDoesNotSplit(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail"})
	d.Switch(append(d.Stage(), "chrome"), t0)
	d.Split("code")
	if got := d.SetOf("code"); !slices.Equal(got, []string{"code", "chrome"}) {
		t.Fatalf("set of code %v after splitting the stage", got)
	}
}

func TestKeptNewcomersEnterAtTheBottom(t *testing.T) {
	d := kept("1", "2", "3")
	d.Switch(d.SetOf("2"), t0)
	got := d.Strip([]string{"4", "1", "2", "3", "5"})
	if want := [][]string{{"1"}, {"2"}, {"3"}, {"4"}, {"5"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	// An app that has not been in the strip yet, going on the stage first,
	// takes the bottom too, and the stage it leaves stays.
	d.Switch([]string{"6"}, t0)
	got = d.Strip([]string{"6", "1", "2", "3", "4", "5"})
	if want := [][]string{{"1"}, {"2"}, {"3"}, {"4"}, {"5"}, {"6"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	// Gone apps leave their cards, the stage's too.
	got = d.Strip([]string{"1", "3", "4", "5"})
	if want := [][]string{{"1"}, {"3"}, {"4"}, {"5"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
}

func TestKeptLeaveStaysInItsSlot(t *testing.T) {
	apps := []string{"1", "2", "3"}
	d := kept(apps...)
	d.Switch(d.SetOf("2"), t0)
	d.Leave("2")
	if !d.Empty() {
		t.Fatalf("stage %v", d.Stage())
	}
	if got := d.Strip(apps); !equalSets(got, [][]string{{"1"}, {"2"}, {"3"}}) {
		t.Fatalf("strip %v", got)
	}
	// Of two on the stage, one leaves and the other stays on it.
	d.Switch([]string{"2", "3"}, t0)
	d.Leave("2")
	if got, want := d.Stage(), []string{"3"}; !slices.Equal(got, want) {
		t.Fatalf("stage %v, want %v", got, want)
	}
	if got := d.Strip(apps); !equalSets(got, [][]string{{"1"}, {"3"}, {"2"}}) {
		t.Fatalf("strip %v", got)
	}
}

func TestKeptInitSeatsTheStage(t *testing.T) {
	d := kept("1", "2", "3")
	d.Init("2", t0)
	if got := d.Strip([]string{"1", "2", "3"}); !equalSets(got, [][]string{{"1"}, {"2"}, {"3"}}) {
		t.Fatalf("strip %v", got)
	}
	var e Display
	e.Keep(true)
	e.Init("3", t0)
	got := e.Strip([]string{"3", "1", "2"})
	if want := [][]string{{"3"}, {"1"}, {"2"}}; !equalSets(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
}

func TestKeptMoveAndInsertCountTheStage(t *testing.T) {
	apps := []string{"1", "2", "3", "4"}
	d := kept(apps...)
	d.Switch(d.SetOf("2"), t0)
	// The strip as it shows has the stage in it, so slots count it.
	d.Move("4", 1)
	if got := d.Strip(apps); !equalSets(got, [][]string{{"1"}, {"4"}, {"2"}, {"3"}}) {
		t.Fatalf("strip %v", got)
	}
	d.Insert([]string{"5"}, 2)
	if got := d.Strip(append(apps, "5")); !equalSets(got, [][]string{{"1"}, {"4"}, {"5"}, {"2"}, {"3"}}) {
		t.Fatalf("strip %v", got)
	}
	// The stage's own card can be moved too, and stays the stage.
	d.Move("2", 0)
	if got := d.Strip(append(apps, "5")); !equalSets(got, [][]string{{"2"}, {"1"}, {"4"}, {"5"}, {"3"}}) {
		t.Fatalf("strip %v", got)
	}
	if !d.OnStage("2") {
		t.Fatalf("stage %v", d.Stage())
	}
	// Picking 3 moves no card, the one moved included.
	d.Switch(d.SetOf("3"), t0)
	if got := d.Strip(append(apps, "5")); !equalSets(got, [][]string{{"2"}, {"1"}, {"4"}, {"5"}, {"3"}}) {
		t.Fatalf("strip %v", got)
	}
}
