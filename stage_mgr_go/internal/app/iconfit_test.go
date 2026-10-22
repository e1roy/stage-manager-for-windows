package app

import (
	"image"
	"image/color"
	"testing"
)

// tile draws an opaque square of side n at (x, y) on a size by size tile,
// with the faint frame the shell draws around a small icon.
func tile(size, x, y, n int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i := 0; i < size; i++ {
		img.SetNRGBA(i, 0, color.NRGBA{255, 255, 255, 51})
		img.SetNRGBA(0, i, color.NRGBA{0, 0, 0, 77})
	}
	for j := y; j < y+n; j++ {
		for i := x; i < x+n; i++ {
			img.SetNRGBA(i, j, color.NRGBA{255, 0, 0, 255})
		}
	}
	return img
}

func TestTrimIcon(t *testing.T) {
	// Q-Dir from the shell: a 43 pixel icon in the middle of a 256 pixel
	// tile, inside its frame.
	if out := trimIcon(tile(256, 107, 107, 43)); out.Bounds() != image.Rect(0, 0, 43, 43) {
		t.Fatalf("the shell's tile trimmed to %v", out.Bounds())
	}
	out := trimIcon(tile(256, 20, 30, 200))
	if out.Bounds() != image.Rect(0, 0, 200, 200) {
		t.Fatalf("trimmed to %v", out.Bounds())
	}
	if a := out.NRGBAAt(0, 0).A; a != 255 {
		t.Fatalf("corner alpha %d, want the icon", a)
	}
	// A wide icon is centered in a square.
	img := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	for y := 40; y < 60; y++ {
		for x := 10; x < 90; x++ {
			img.SetNRGBA(x, y, color.NRGBA{0, 0, 255, 255})
		}
	}
	out = trimIcon(img)
	if out.Bounds() != image.Rect(0, 0, 80, 80) || out.NRGBAAt(0, 30).A != 255 || out.NRGBAAt(0, 29).A != 0 {
		t.Fatalf("wide icon trimmed to %v", out.Bounds())
	}
	empty := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	if trimIcon(empty) != empty {
		t.Fatal("an empty icon changed")
	}
}
