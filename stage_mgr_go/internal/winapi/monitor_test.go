//go:build windows

package winapi

import "testing"

func TestMostlyOutside(t *testing.T) {
	left := Rect{0, 0, 5120, 2880}
	right := Rect{5120, 0, 10240, 2880}
	both := []Rect{left, right}
	cases := []struct {
		name string
		r    Rect
		want bool
	}{
		{"inside", Rect{1000, 100, 3000, 1100}, false},
		{"across two displays", Rect{4000, 100, 6000, 1100}, false},
		// QQ Music stops 100 pixels short of leaving the desktop.
		{"sliver at the edge", Rect{10140, 490, 14900, 2266}, true},
		{"half shown", Rect{9240, 0, 11240, 1000}, false},
		{"gone", Rect{10400, 0, 12000, 1000}, true},
		{"empty", Rect{}, false},
	}
	for _, c := range cases {
		if got := mostlyOutside(c.r, both); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFitInto(t *testing.T) {
	work := Rect{340, 0, 5120, 2784}
	cases := []struct {
		name string
		r    Rect
		want Rect
	}{
		{"already inside", Rect{1000, 100, 3000, 1100}, Rect{1000, 100, 3000, 1100}},
		{"off the right", Rect{4900, 100, 6900, 1100}, Rect{3120, 100, 5120, 1100}},
		{"under the strip", Rect{0, 100, 2000, 1100}, Rect{340, 100, 2340, 1100}},
		{"above the top", Rect{1000, -500, 3000, 500}, Rect{1000, 0, 3000, 1000}},
		{"larger than the area", Rect{6000, 3000, 12000, 6000}, Rect{340, 0, 6340, 3000}},
	}
	for _, c := range cases {
		if got := fitInto(c.r, work); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
