package app

import (
	"testing"

	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo/ui"
)

// TestStripConversions: DIPs of a strip and pixels of the screen go back
// and forth within a pixel at the scales Windows offers, and toPixels
// covers all of the rectangle it was given.
func TestStripConversions(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		s := &strip{scale: scale, rect: winapi.Rect{Left: -1920, Top: 120, Right: -1620, Bottom: 1200}}
		r := ui.Rect{X: 13.3, Y: 401.7, W: 151.1, H: 99.9}
		px := s.toPixels(r)
		back := s.rectToLocal(px)
		if back.X > r.X || back.Y > r.Y || back.X+back.W < r.X+r.W || back.Y+back.H < r.Y+r.H {
			t.Errorf("at %v: %+v in pixels %+v does not cover it: %+v", scale, r, px, back)
		}
		k := float32(1 / scale)
		if back.X < r.X-k || back.Y < r.Y-k || back.W > r.W+2*k || back.H > r.H+2*k {
			t.Errorf("at %v: %+v came back as %+v, more than a pixel off", scale, r, back)
		}
		x, y := s.toLocal(winapi.Point{X: px.Left, Y: px.Top})
		if x != back.X || y != back.Y {
			t.Errorf("at %v: toLocal and rectToLocal disagree", scale)
		}
	}
}

func TestInside(t *testing.T) {
	r := ui.Rect{X: 10, Y: 10, W: 20, H: 20}
	for _, c := range []struct {
		x, y, slop float32
		want       bool
	}{{10, 10, 0, true}, {30, 20, 0, false}, {33, 20, 6, true}, {37, 20, 6, false}} {
		if got := inside(r, c.x, c.y, c.slop); got != c.want {
			t.Errorf("%v, %v slop %v: %v", c.x, c.y, c.slop, got)
		}
	}
	if inside(ui.Rect{}, 0, 0, 6) {
		t.Error("an empty rectangle holds a point")
	}
}
