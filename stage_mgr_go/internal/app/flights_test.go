package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAroundLeavesTheHole(t *testing.T) {
	area := ui.Rect{W: 100, H: 80}
	got := around(area, ui.Rect{X: 30, Y: 20, W: 40, H: 30})
	want := []ui.Rect{
		{X: 0, Y: 0, W: 100, H: 20},
		{X: 0, Y: 50, W: 100, H: 30},
		{X: 0, Y: 20, W: 30, H: 30},
		{X: 70, Y: 20, W: 30, H: 30},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("around %v, want %v", got, want)
	}
	// A window dragged half off the display leaves three bands.
	got = around(area, ui.Rect{X: -50, Y: 20, W: 100, H: 30})
	want = []ui.Rect{
		{X: 0, Y: 0, W: 100, H: 20},
		{X: 0, Y: 50, W: 100, H: 30},
		{X: 50, Y: 20, W: 50, H: 30},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("around %v, want %v", got, want)
	}
}
