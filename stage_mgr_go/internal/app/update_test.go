package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// The tray icon with an update has the red dot at its top right, a clear
// ring around it, and the icon as it was elsewhere.
func TestDotted(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	blue := color.NRGBA{B: 255, A: 255}
	for y := range 256 {
		for x := range 256 {
			src.SetNRGBA(x, y, blue)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	data, err := dotted(buf.Bytes(), dottedSize)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != dottedSize || b.Dy() != dottedSize {
		t.Fatalf("size %v", b)
	}
	at := func(fx, fy float64) color.NRGBA {
		return color.NRGBAModel.Convert(img.At(int(fx*dottedSize), int(fy*dottedSize))).(color.NRGBA)
	}
	if c := at(0.76, 0.24); c != dotColor {
		t.Errorf("middle of the dot %v, want %v", c, dotColor)
	}
	// Just outside the dot, inside the ring: cleared.
	if c := at(0.76, 0.24+0.22+0.035); c.A > 40 {
		t.Errorf("ring %v, want clear", c)
	}
	if c := at(0.25, 0.75); c != blue {
		t.Errorf("rest of the icon %v, want %v", c, blue)
	}
}
