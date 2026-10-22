package stage

import (
	"slices"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func TestFirstStripFollowsZOrder(t *testing.T) {
	var d Display
	d.Init("code", t0)
	got := d.Strip([]string{"code", "chrome", "mail", "music"})
	if want := []string{"chrome", "mail", "music"}; !slices.Equal(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
}

func TestSwitchTakesPickedSlot(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail", "music"})
	// Picking "mail" (second card) sends "code" into its slot.
	d.Switch([]string{"mail"}, []string{"code"}, t0)
	got := d.Strip([]string{"mail", "code", "chrome", "music"})
	if want := []string{"chrome", "code", "music"}; !slices.Equal(got, want) {
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
	d.Switch(nil, []string{"code"}, t0)
	got := d.Strip([]string{"code", "chrome", "mail"})
	if want := []string{"code", "chrome", "mail"}; !slices.Equal(got, want) {
		t.Fatalf("strip %v, want %v", got, want)
	}
	if !d.Empty() {
		t.Fatalf("stage %v", d.Stage())
	}
}

func TestNewcomersEnterAtTopAndGoneLeave(t *testing.T) {
	var d Display
	d.Init("code", t0)
	d.Strip([]string{"code", "chrome", "mail"})
	got := d.Strip([]string{"notes", "code", "mail"})
	if want := []string{"notes", "mail"}; !slices.Equal(got, want) {
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
	if got := d.Strip([]string{"code", "chrome", "mail"}); got[0] != "mail" {
		t.Fatalf("pruned app not at the top: %v", got)
	}
}
