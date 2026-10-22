package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestStripFitsAndCenters(t *testing.T) {
	l := layoutStrip(3, 200, 1000, 0)
	if l.scale != 1 || len(l.cards) != 3 {
		t.Fatalf("scale %v cards %d", l.scale, len(l.cards))
	}
	top, bottom := l.cards[0].Y, l.cards[2].Y+l.cards[2].H
	if d := (top - 0) - (1000 - bottom); d > 0.5 || d < -0.5 {
		t.Fatalf("not centered: top %v bottom %v", top, bottom)
	}
}

func TestStripShrinksThenScrolls(t *testing.T) {
	l := layoutStrip(8, 200, 900, 0)
	if l.scale >= 1 || l.scale < minCardScale {
		t.Fatalf("scale %v", l.scale)
	}
	last := l.cards[7]
	if last.Y+last.H > 900-stripMargin+0.5 {
		t.Fatalf("last card ends at %v", last.Y+last.H)
	}
	many := layoutStrip(40, 200, 900, 0)
	if many.scale != minCardScale || many.maxScroll(900) <= 0 {
		t.Fatalf("scale %v max scroll %v", many.scale, many.maxScroll(900))
	}
	scrolled := layoutStrip(40, 200, 900, 100)
	if scrolled.cards[0].Y != many.cards[0].Y-100 {
		t.Fatalf("scroll moved the first card from %v to %v", many.cards[0].Y, scrolled.cards[0].Y)
	}
}

func TestStackStaysInCard(t *testing.T) {
	card := ui.Rect{X: 18, Y: 100, W: 160, H: 105}
	for _, aspects := range [][]float64{{16.0 / 9}, {0.6}, {3.5, 1.2, 1.6}, {1.6, 1.6, 1.6, 1.6}} {
		rs := stackWindows(card, aspects)
		if len(rs) != min(len(aspects), maxStacked) {
			t.Fatalf("%d windows for %v", len(rs), aspects)
		}
		for i, r := range rs {
			if r.X < card.X-0.01 || r.Y < card.Y-0.01 || r.X+r.W > card.X+card.W+0.01 || r.Y+r.H > card.Y+card.H+0.01 {
				t.Fatalf("window %d %+v leaves card %+v for %v", i, r, card, aspects)
			}
		}
		if len(rs) > 1 && !(rs[1].Y < rs[0].Y && rs[1].X > rs[0].X) {
			t.Fatalf("window behind does not peek out: %+v", rs)
		}
	}
}
