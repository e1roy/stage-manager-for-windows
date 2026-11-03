package app

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// window paints a picture of a window: a background, a title bar of
// another color across the top share of it, and scattered words of others.
func window(bg, bar color.RGBA, share float64, words ...color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			c := bg
			switch {
			case float64(y) < share*300:
				c = bar
			case len(words) > 0 && y%12 == 0 && x%7 < 3:
				c = words[(x/7+y/12)%len(words)]
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func hueOf(c color.RGBA) float64 {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	hi, lo := max(r, g, b), min(r, g, b)
	return hue(r, g, b, hi, hi-lo)
}

func TestDominantColor(t *testing.T) {
	dark := color.RGBA{30, 30, 30, 255}
	white := color.RGBA{250, 250, 250, 255}
	orange := color.RGBA{208, 106, 43, 255}
	blue := color.RGBA{60, 95, 190, 255}
	words := []color.RGBA{{86, 156, 214, 255}, {206, 145, 120, 255}, {78, 201, 176, 255}, {220, 220, 170, 255}}
	red := []color.RGBA{{220, 40, 40, 255}}
	for range 30 {
		red = append(red, white)
	}
	for _, tc := range []struct {
		name string
		img  *image.RGBA
		want *color.RGBA // nil for none
	}{
		// Two VS Code windows: a dark editor of colorful words, the title
		// bar orange in one and blue in the other.
		{"orange bar", window(dark, orange, 0.05, words...), &orange},
		{"blue bar", window(dark, blue, 0.05, words...), &blue},
		{"blue on white", window(white, blue, 0.08), &blue},
		// Black, white and gray say nothing; nor do a few words in color.
		{"gray", window(dark, color.RGBA{128, 128, 128, 255}, 0.1), nil},
		{"off-white", window(white, color.RGBA{235, 232, 225, 255}, 0.1), nil},
		{"black", window(color.RGBA{0, 0, 0, 255}, dark, 0.1), nil},
		{"few words", window(white, white, 0, red...), nil},
	} {
		r, g, b, ok := dominantColor(tc.img)
		if tc.want == nil {
			if ok {
				t.Errorf("%s: found %v %v %v", tc.name, r, g, b)
			}
			continue
		}
		if !ok {
			t.Errorf("%s: found no color", tc.name)
			continue
		}
		got := color.RGBA{uint8(r * 255), uint8(g * 255), uint8(b * 255), 255}
		if d := math.Abs(hueOf(got) - hueOf(*tc.want)); d > 15 {
			t.Errorf("%s: hue %v, want %v", tc.name, hueOf(got), hueOf(*tc.want))
		}
	}
	if _, _, _, ok := dominantColor(nil); ok {
		t.Fatal("no picture has a color")
	}
}

func TestToOklch(t *testing.T) {
	near := func(a, b, eps float64) bool { return math.Abs(a-b) < eps }
	if l, c, _ := toOklch(1, 1, 1); !near(l, 1, 1e-3) || c > 1e-3 {
		t.Fatalf("white %v %v", l, c)
	}
	if l, c, h := toOklch(1, 0, 0); !near(l, 0.628, 2e-3) || !near(c, 0.2577, 2e-3) || !near(h, 29.23, 0.5) {
		t.Fatalf("red %v %v %v", l, c, h)
	}
	if l, _, h := toOklch(0, 0, 1); !near(l, 0.452, 2e-3) || !near(h, 264.05, 0.5) {
		t.Fatalf("blue %v %v", l, h)
	}
}

// Tints of any hue are as dark, and keep their hue.
func TestTileTint(t *testing.T) {
	ob, _ := tileTint(208/255.0, 106/255.0, 43/255.0)
	bb, _ := tileTint(60/255.0, 95/255.0, 190/255.0)
	if !(ob.R > ob.B) || !(bb.B > bb.R) {
		t.Fatalf("orange %v blue %v", ob, bb)
	}
	lo, _, _ := toOklch(float64(ob.R)/255, float64(ob.G)/255, float64(ob.B)/255)
	lb, _, _ := toOklch(float64(bb.R)/255, float64(bb.G)/255, float64(bb.B)/255)
	if math.Abs(lo-tintL) > 0.02 || math.Abs(lb-tintL) > 0.02 {
		t.Fatalf("lightness %v and %v, want %v", lo, lb, tintL)
	}
}

func TestBlank(t *testing.T) {
	dark := color.RGBA{5, 5, 5, 255}
	if !blank(nil) || !blank(window(dark, dark, 0.1)) {
		t.Fatal("a copy of one color has something in it")
	}
	if blank(window(dark, color.RGBA{6, 5, 5, 255}, 0.1)) {
		t.Fatal("a copy with a title bar has nothing in it")
	}
}
