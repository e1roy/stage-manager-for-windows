package app

import (
	"slices"
	"testing"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
)

// A card shows every app of its set, and the windows it shows overlap as
// they do on the desktop, so that they keep their order as they fly home.
func TestSetShowsWindowsInZOrder(t *testing.T) {
	w := func(z int, app string) catalog.Window { return catalog.Window{App: app, Z: z} }
	a := &group{app: "a", z: 0, windows: []catalog.Window{w(0, "a"), w(1, "a"), w(3, "a")}}
	b := &group{app: "b", z: 1, windows: []catalog.Window{w(2, "b")}}
	s := newSet([]*group{b, a})
	var got []int
	for _, x := range s.windows {
		got = append(got, x.Z)
	}
	if want := []int{0, 1, 2, 3}; !slices.Equal(got, want) {
		t.Fatalf("z-order %v, want %v", got, want)
	}
	// With room for one window of each app only, both apps still show.
	a.windows = []catalog.Window{w(0, "a"), w(1, "a"), w(2, "a")}
	b.windows = []catalog.Window{w(3, "b")}
	s = newSet([]*group{a, b})
	if !slices.ContainsFunc(s.stacked(), func(x catalog.Window) bool { return x.App == "b" }) {
		t.Fatalf("b does not show: %v", s.stacked())
	}
	got = got[:0]
	for _, x := range s.stacked() {
		got = append(got, x.Z)
	}
	if want := []int{0, 1, 3}; !slices.Equal(got, want) {
		t.Fatalf("shown %v, want %v", got, want)
	}
}
