package app

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// The three boxes are as wide, in their variance, as the Gaussian they
// stand for.
func TestGaussBoxes(t *testing.T) {
	for _, sigma := range []float64{0.8, 2, 3.7, 8, 20} {
		var v float64
		for _, w := range gaussBoxes(sigma, 3) {
			if w%2 == 0 || w < 1 {
				t.Fatalf("sigma %v: a box %v wide", sigma, w)
			}
			v += float64(w*w-1) / 12
		}
		if got := math.Sqrt(v); math.Abs(got-sigma) > 0.5 {
			t.Fatalf("sigma %v: the boxes blur as %v", sigma, got)
		}
	}
}

// A blur keeps a flat picture as it is, and softens an edge into a ramp
// that runs one way, as far as the blur reaches and no farther.
func TestFrostImage(t *testing.T) {
	const w, h = 120, 60
	flat := image.NewRGBA(image.Rect(0, 0, w, h))
	edge := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			flat.Set(x, y, color.RGBA{40, 90, 160, 255})
			v := uint8(0)
			if x >= w/2 {
				v = 255
			}
			edge.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	all := image.Rect(0, 0, w, h)
	f := frostImage(flat, all, 6)
	for i := 0; i < len(f.Pix); i += 4 {
		if f.Pix[i] != 40 || f.Pix[i+1] != 90 || f.Pix[i+2] != 160 || f.Pix[i+3] != 255 {
			t.Fatalf("a flat picture blurs to %v", f.Pix[i:i+4])
		}
	}
	for _, sigma := range []float64{1, 4, 12} {
		g := frostImage(edge, all, sigma)
		row := g.Pix[g.Rect.Dy()/2*g.Stride:]
		n := g.Rect.Dx()
		if n < 1 || n > w {
			t.Fatalf("sigma %v: %v pixels wide", sigma, n)
		}
		last := -1
		for x := 0; x < n; x++ {
			if v := int(row[x*4]); v < last {
				t.Fatalf("sigma %v: the ramp goes back down at %v", sigma, x)
			} else {
				last = v
			}
		}
		if row[0] > 8 || row[(n-1)*4] < 247 {
			t.Fatalf("sigma %v: the ends are %v and %v", sigma, row[0], row[(n-1)*4])
		}
		if mid := row[n/2*4]; mid < 60 || mid > 200 {
			t.Fatalf("sigma %v: the middle of the edge is %v", sigma, mid)
		}
	}
	// It returns the part asked for, in the shrunk picture.
	part := frostImage(edge, image.Rect(30, 10, 90, 50), 12)
	if part.Rect.Dx() != 15 || part.Rect.Dy() != 10 {
		t.Fatalf("a part 60 × 40 shrunk by 4 is %v", part.Rect)
	}
}

// A large button blurs in well under a frame.
func BenchmarkFrostImage(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 600, 400))
	for i := range img.Pix {
		img.Pix[i] = uint8(i * 7)
	}
	for b.Loop() {
		frostImage(img, image.Rect(90, 90, 510, 310), 24)
	}
}
