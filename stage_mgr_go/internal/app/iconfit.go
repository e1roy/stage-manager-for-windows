package app

import (
	"image"
	"image/draw"
)

// Icons are drawn at one size, but apps leave different margins around
// them, and the shell puts an icon it has no large image of small in the
// middle of a big tile: the strip trims every icon to what it shows.

// iconAlpha is how opaque a pixel must be to count as the icon, which
// leaves out the faint frame of the shell's tiles: up to 77 of 255.
const iconAlpha = 128

// iconBounds returns the part of an icon that shows.
func iconBounds(img *image.NRGBA) image.Rectangle {
	r := img.Bounds()
	minX, minY, maxX, maxY := r.Max.X, r.Max.Y, r.Min.X-1, r.Min.Y-1
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if img.Pix[img.PixOffset(x, y)+3] >= iconAlpha {
				minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
			}
		}
	}
	if maxX < minX {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX+1, maxY+1)
}

// trimIcon crops an icon to what shows, centered in a square.
func trimIcon(img *image.NRGBA) *image.NRGBA {
	b := iconBounds(img)
	if b.Empty() || b == img.Bounds() && b.Dx() == b.Dy() {
		return img
	}
	side := max(b.Dx(), b.Dy())
	out := image.NewNRGBA(image.Rect(0, 0, side, side))
	at := image.Pt((side-b.Dx())/2, (side-b.Dy())/2)
	draw.Draw(out, image.Rectangle{Min: at, Max: at.Add(b.Size())}, img, b.Min, draw.Src)
	return out
}

// toNRGBA converts a decoded image.
func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	out := image.NewNRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out
}
