package app

import (
	"image"
	"math"

	"github.com/egoist/mygo/ui"
)

// A tile of the list of the stage's windows (lineup.go) takes the color its
// window mostly shows, so that windows of one app, as two VS Code windows
// with title bars of their own colors, tell apart at a glance. Black, white
// and grays are left out: most windows are mostly those, and they say
// nothing of which window it is.
const (
	tintDark   = 0.2  // brightness below which a pixel is near black
	tintChroma = 0.16 // colorfulness below which it is near gray, or white
	tintBins   = 24   // hues, 15° each
	// tintShare is how much of a picture must be in color for it to have
	// one: a little, as a title bar is of a window, but more than a few
	// words in color on a page.
	tintShare = 0.012
	// The bands at the top and bottom of a window, where its title bar and
	// status bar are, count tintEdge times as much as the rest: they say
	// which window it is, as VS Code colors them per window, more than
	// what it shows does.
	tintBand = 0.1
	tintEdge = 2.5
)

// dominantColor returns the color a picture is mostly, of its pixels in
// color: those of the hue most of them share, give or take 15°, the more
// colorful and those near its top and bottom edges counting more, averaged. ok is false for a picture with too
// little color.
func dominantColor(img *image.RGBA) (r, g, b float64, ok bool) {
	if img == nil || img.Rect.Empty() {
		return 0, 0, 0, false
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	// Some 4000 pixels are plenty, and cost nothing.
	step := max(1, int(math.Sqrt(float64(w*h)/4000)))
	var weight [tintBins]float64
	var sum [tintBins][3]float64
	n, colored := 0, 0
	band := int(tintBand * float64(h))
	for y := 0; y < h; y += step {
		row := img.Pix[y*img.Stride:]
		k := 1.0
		if y < band || y >= h-band {
			k = tintEdge
		}
		for x := 0; x < w; x += step {
			n++
			pr, pg, pb := float64(row[x*4])/255, float64(row[x*4+1])/255, float64(row[x*4+2])/255
			hi, lo := max(pr, pg, pb), min(pr, pg, pb)
			c := hi - lo
			if hi < tintDark || c < tintChroma {
				continue
			}
			colored++
			bin := int(hue(pr, pg, pb, hi, c)/(360/tintBins)) % tintBins
			wt := c * k
			weight[bin] += wt
			sum[bin][0] += pr * wt
			sum[bin][1] += pg * wt
			sum[bin][2] += pb * wt
		}
	}
	if n == 0 || float64(colored) < tintShare*float64(n) {
		return 0, 0, 0, false
	}
	best, most := 0, -1.0
	for i := range weight {
		prev, next := weight[(i+tintBins-1)%tintBins], weight[(i+1)%tintBins]
		if v := weight[i] + 0.5*(prev+next); v > most {
			best, most = i, v
		}
	}
	var wt, sr, sg, sb float64
	for _, i := range []int{(best + tintBins - 1) % tintBins, best, (best + 1) % tintBins} {
		wt += weight[i]
		sr, sg, sb = sr+sum[i][0], sg+sum[i][1], sb+sum[i][2]
	}
	return sr / wt, sg / wt, sb / wt, true
}

// hue is the hue, in degrees, of a color whose largest channel is hi and
// whose chroma, largest less smallest, is c > 0.
func hue(r, g, b, hi, c float64) float64 {
	var h float64
	switch hi {
	case r:
		h = math.Mod((g-b)/c, 6)
	case g:
		h = (b-r)/c + 2
	default:
		h = (r-g)/c + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h
}

// toOklch converts a color in sRGB, 0 to 1, to OKLCH: lightness, chroma
// and hue in degrees, in which colors of one lightness look as light.
func toOklch(r, g, b float64) (l, c, h float64) {
	lin := func(v float64) float64 {
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	r, g, b = lin(r), lin(g), lin(b)
	lc := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	mc := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	sc := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	l = 0.2104542553*lc + 0.7936177850*mc - 0.0040720468*sc
	a := 1.9779984951*lc - 2.4285922050*mc + 0.4505937099*sc
	bb := 0.0259040371*lc + 0.7827717662*mc - 0.8086757660*sc
	c = math.Hypot(a, bb)
	h = math.Atan2(bb, a) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return l, c, h
}

// The tint of a tile is its window's color made dark, as dark whatever the
// hue, as OKLCH has it, for its white words to read on any; its edge is
// the color lighter. Neither is more colorful than tintMax and a little.
const (
	tintL     = 0.34
	tintEdgeL = 0.66
	tintMax   = 0.1
	tintAlpha = 0.86
)

// tileTint returns the background and the edge of a tile for a window of
// the color r, g, b.
func tileTint(r, g, b float64) (bg, edge ui.Color) {
	_, c, h := toOklch(r, g, b)
	c = min(c, tintMax)
	bg = ui.Oklch(tintL, float32(c), float32(h)).Alpha(tintAlpha)
	edge = ui.Oklch(tintEdgeL, float32(min(c*1.3, 0.14)), float32(h)).Alpha(0.6)
	return bg, edge
}

// blank reports whether a copy of the screen has nothing in it, all of
// one color, as copies come while the screen is locked: no window is.
func blank(img *image.RGBA) bool {
	if img == nil || img.Rect.Empty() {
		return true
	}
	p := img.Pix
	for i := 4; i < len(p); i += 4 {
		if p[i] != p[0] || p[i+1] != p[1] || p[i+2] != p[2] {
			return false
		}
	}
	return true
}
