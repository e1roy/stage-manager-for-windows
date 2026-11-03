package app

import (
	"image"
	"math"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo/ui"
)

// glass is the frosted backdrop of the overview's panel, and of the corner
// button joined to it: the screen behind them, blurred. DWM blurs only
// whole windows, and the panel is a part of the strip's window, so the
// strip copies what the screen shows there as the panel comes in, while it
// is not yet drawn over it, and blurs the copy itself. It holds still as
// the panel shows, which is only while the pointer is near.
type glass struct {
	// raw is the screen behind the button and around it, as last copied,
	// and at where the button is in it, for the blur to be done again as
	// the settings change it.
	raw *image.RGBA
	at  image.Rectangle
	// bmp is the backdrop, the button's size, blurred: the strip's panel
	// draws its top, a joined corner button the rest.
	bmp *ui.Bitmap
	// seq counts the copies and blurs asked for: only the last is kept.
	seq int
}

// panelFill is the color of the overview's panel, at rest and under the
// pointer, at the opacity the settings give: a dark glass, lighter under
// the pointer.
func panelFill(opacity, hot float32) ui.Color {
	rest := ui.RGBA(18, 18, 22, opacity)
	under := ui.RGBA(52, 52, 62, min(1, opacity+0.15))
	return rest.Mix(under, hot)
}

// glassSigma is the Gaussian's sigma, in pixels, of a blur of the settings'
// DIPs: what reaches that far is a few percent.
func glassSigma(blur, scale float64) float64 { return blur * scale / 2 }

// frost asks for the glass of the button at r, in pixels: a copy of the
// screen there, blurred, off the main thread. The pointer's approach
// brings it, before the button shows.
func (s *strip) frost(r winapi.Rect) {
	blur := s.a.settings.PanelBlur
	if blur <= 0 || r.Empty() {
		s.glass = glass{seq: s.glass.seq + 1}
		return
	}
	if s.win == nil {
		// The headless tests have no screen to copy; they give the
		// glass themselves.
		return
	}
	sigma := glassSigma(blur, s.scale)
	// Around the button, as far as the blur reaches, so that its edges
	// blur what is beyond them as the middle does; not past the display.
	// As far as the most blur the settings allow, for a blur made larger
	// to find it all there.
	m := int32(math.Ceil(3 * glassSigma(settings.MaxPanelBlur, s.scale)))
	mon := s.d.mon.Monitor
	c := winapi.Rect{Left: max(r.Left-m, mon.Left), Top: max(r.Top-m, mon.Top), Right: min(r.Right+m, mon.Right), Bottom: min(r.Bottom+m, mon.Bottom)}
	at := image.Rect(int(r.Left-c.Left), int(r.Top-c.Top), int(r.Right-c.Left), int(r.Bottom-c.Top))
	s.glass.seq++
	seq := s.glass.seq
	go func() {
		t0 := time.Now()
		raw := winapi.CaptureScreen(c)
		if blank(raw) {
			// The screen is locked: there is nothing to blur.
			raw = nil
		}
		var img *image.RGBA
		if raw != nil {
			img = frostImage(raw, at, sigma)
		}
		took := time.Since(t0)
		post(func() {
			if seq != s.glass.seq {
				return
			}
			debugf("glass %s: %v at %+v, sigma %.1f, %v", s.d.mon.Device, c, at, sigma, took)
			s.glass.raw, s.glass.at = raw, at
			s.glass.bmp = nil
			if img != nil {
				s.glass.bmp = ui.NewBitmap(img)
			}
			s.redraw()
			if h := s.joint(); h != nil {
				h.redraw()
			}
		})
	}()
}

// buttonScreen is where the overview's button at r, in DIPs of the strip,
// shows on the screen, in pixels: with the corner button joined to it,
// down to the display's bottom.
func (s *strip) buttonScreen(r ui.Rect) winapi.Rect {
	x, y, w, h := s.toScreen(r)
	b := winapi.Rect{Left: int32(math.Floor(x)), Top: int32(math.Floor(y)), Right: int32(math.Ceil(x + w)), Bottom: int32(math.Ceil(y + h))}
	if s.joined {
		b.Bottom = s.d.mon.Monitor.Bottom
	}
	return b
}

// reblur blurs the copy it has again, as the settings change the blur.
func (s *strip) reblur() {
	s.glass.seq++
	seq, raw, at := s.glass.seq, s.glass.raw, s.glass.at
	blur := s.a.settings.PanelBlur
	if raw == nil || blur <= 0 {
		s.glass.bmp = nil
		s.redraw()
		return
	}
	sigma := glassSigma(blur, s.scale)
	go func() {
		img := frostImage(raw, at, sigma)
		post(func() {
			if seq != s.glass.seq {
				return
			}
			s.glass.bmp = ui.NewBitmap(img)
			s.redraw()
			if h := s.joint(); h != nil {
				h.redraw()
			}
		})
	}()
}

// frostImage blurs src as a Gaussian of sigma pixels would and returns
// the part of it at at. It is cheap whatever the blur: the picture is
// shrunk by a whole factor first, as a large blur loses what that drops,
// and three box blurs across and down follow, each a running sum, which
// costs the same whatever its width, and which together come within a
// few percent of a Gaussian. The result is that much smaller than at; it
// is drawn scaled up, smoothly.
func frostImage(src *image.RGBA, at image.Rectangle, sigma float64) *image.RGBA {
	f := min(max(int(sigma/3), 1), 8)
	small := shrinkBy(src, f)
	sw, sh := small.Rect.Dx(), small.Rect.Dy()
	if s := sigma / float64(f); s >= 0.5 {
		tmp := make([]uint8, len(small.Pix))
		for _, b := range gaussBoxes(s, 3) {
			r := (b - 1) / 2
			boxBlur(small.Pix, tmp, sw, sh, r, 4, sw*4)
			boxBlur(tmp, small.Pix, sh, sw, r, sw*4, 4)
		}
	}
	// The button's part, in the shrunk picture, to the nearest pixel.
	near := func(v int) int { return (v + f/2) / f }
	crop := image.Rect(near(at.Min.X), near(at.Min.Y), near(at.Max.X), near(at.Max.Y)).Intersect(small.Rect)
	out := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	for y := 0; y < crop.Dy(); y++ {
		copy(out.Pix[y*out.Stride:], small.Pix[(crop.Min.Y+y)*small.Stride+crop.Min.X*4:(crop.Min.Y+y)*small.Stride+crop.Max.X*4])
	}
	return out
}

// shrinkBy scales img down by f, each pixel the average of the f by f it
// stands for; those past the last whole block count as far as they go.
func shrinkBy(img *image.RGBA, f int) *image.RGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if f <= 1 {
		out := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			copy(out.Pix[y*out.Stride:], img.Pix[y*img.Stride:y*img.Stride+w*4])
		}
		return out
	}
	sw, sh := (w+f-1)/f, (h+f-1)/f
	out := image.NewRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			var sum [4]int
			n := 0
			for yy := y * f; yy < min((y+1)*f, h); yy++ {
				p := img.Pix[yy*img.Stride+x*f*4:]
				for xx := x * f; xx < min((x+1)*f, w); xx++ {
					o := (xx - x*f) * 4
					sum[0] += int(p[o])
					sum[1] += int(p[o+1])
					sum[2] += int(p[o+2])
					sum[3] += int(p[o+3])
					n++
				}
			}
			o := y*out.Stride + x*4
			for i := range sum {
				out.Pix[o+i] = uint8((sum[i] + n/2) / n)
			}
		}
	}
	return out
}

// gaussBoxes is the odd widths of n box blurs which, one after another,
// come closest to a Gaussian of sigma: as wide, in their variance, as it.
func gaussBoxes(sigma float64, n int) []int {
	ideal := math.Sqrt(12*sigma*sigma/float64(n) + 1)
	lo := int(math.Floor(ideal))
	if lo%2 == 0 {
		lo--
	}
	hi := lo + 2
	m := int(math.Round((12*sigma*sigma - float64(n*lo*lo+4*n*lo+3*n)) / float64(-4*lo-4)))
	out := make([]int, n)
	for i := range out {
		if i < m {
			out[i] = lo
		} else {
			out[i] = hi
		}
	}
	return out
}

// boxBlur averages each pixel of src with the r on either side of it along
// one direction, into dst: lines lines of n pixels, the pixels step bytes
// apart and the lines stride. Past the ends, the end pixels repeat. A
// running sum makes it the same cost whatever r is.
func boxBlur(src, dst []uint8, n, lines, r, step, stride int) {
	if r <= 0 {
		copy(dst, src)
		return
	}
	w := 2*r + 1
	for l := 0; l < lines; l++ {
		base := l * stride
		at := func(i int) int { return base + min(max(i, 0), n-1)*step }
		for ch := 0; ch < 4; ch++ {
			sum := 0
			for i := -r; i <= r; i++ {
				sum += int(src[at(i)+ch])
			}
			for i := 0; i < n; i++ {
				dst[base+i*step+ch] = uint8((sum + w/2) / w)
				sum += int(src[at(i+r+1)+ch]) - int(src[at(i-r)+ch])
			}
		}
	}
}
