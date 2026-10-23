package app

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// Strip geometry in DIPs of the strip window. The cards share one left
// edge and are centered as a group; they shrink as apps are added so the
// whole strip fits, and scroll past that.
const (
	stripPadLeft    = 18
	stripPadRight   = 22
	stripMargin     = 28
	minCardScale    = 0.46
	tiltStep        = 4 // degrees a resting card turns for each step of the tilt setting: the default 6 turns it 24°
	cardPerspective = 640
	maxStacked      = 3
	maxIcons        = 3 // icons a card of several apps shows
)

// cardTilts returns the degrees a resting card turns at a tilt setting, and
// a hovered one, which turns a quarter of that to face the user.
func cardTilts(level int) (rest, hover float32) {
	rest = float32(level) * tiltStep
	return rest, rest / 4
}

// turnedCard projects a w×h card turned deg degrees about its left edge,
// seen from dist away: its right edge, gone back, comes to rx from the left
// one, and rh above and below the middle.
func turnedCard(w, h, deg, dist float32) (rx, rh float32) {
	sin, cos := math.Sincos(float64(deg) * math.Pi / 180)
	k := dist / (dist + w*float32(sin))
	return w * float32(cos) * k, h / 2 * k
}

// stripLayout places n cards in a strip of the given size, gap DIPs apart
// at full size; the gap shrinks with the cards.
type stripLayout struct {
	cards   []ui.Rect // the area each card's windows take, top first
	scale   float32   // how much the cards shrank to fit, 1 at full size
	content float32   // height of all cards, for scrolling
	extent  float32   // distance from one card to the next
}

func layoutStrip(n int, width, height, scroll, gap float32) stripLayout {
	pw := max(width-stripPadLeft-stripPadRight, 40)
	ph := min(pw*0.66, 172)
	extent := ph + gap
	avail := max(height-2*stripMargin, 1)
	s := float32(1)
	if n > 0 {
		s = min(max(avail/(float32(n)*extent-gap), minCardScale), 1)
	}
	l := stripLayout{scale: s, extent: extent * s}
	total := float32(n)*extent*s - gap*s
	l.content = total + 2*stripMargin
	y0 := (height - total) / 2
	if total > avail {
		y0 = stripMargin - scroll
	}
	for i := 0; i < n; i++ {
		l.cards = append(l.cards, ui.Rect{X: stripPadLeft, Y: y0 + float32(i)*extent*s, W: pw * s, H: ph * s})
	}
	return l
}

// maxScroll is how far a strip with this layout scrolls.
func (l stripLayout) maxScroll(height float32) float32 {
	return max(0, l.content-height)
}

// stackWindows places up to three windows of an app in a card, front
// first. Every window gets the same height, as in Alt+Tab, so wide windows
// are not thin strips; the ones behind step up and to the right.
func stackWindows(card ui.Rect, aspects []float64) []ui.Rect {
	n := min(len(aspects), maxStacked)
	out := make([]ui.Rect, n)
	// Room for the windows behind to peek out above and to the right.
	step := card.H * 0.08
	room := step * float32(n-1)
	var frontTop, frontH float32
	for i := 0; i < n; i++ {
		k := float32(i)
		sc := 1 - 0.1*k
		h := (card.H - room) * sc
		if i > 0 {
			h = frontH * sc // never taller than the window in front
		}
		w := h * float32(aspects[i])
		if maxW := (card.W - room*1.4) * sc; w > maxW {
			w = maxW
			h = w / float32(aspects[i])
		}
		if i == 0 {
			frontTop, frontH = card.Y+(card.H-h-room)/2+room, h
		}
		out[i] = ui.Rect{X: card.X + step*k*1.4, Y: frontTop - step*k, W: w, H: h}
	}
	return out
}

// stack places the windows a set's card shows in card, front first.
func (s *set) stack(card ui.Rect) []ui.Rect {
	ws := s.stacked()
	aspects := make([]float64, len(ws))
	for i, w := range ws {
		aspects[i] = w.Aspect()
	}
	return stackWindows(card, aspects)
}

// lifted is r, in card, scaled by lift around the card's left middle, as
// the card scales.
func lifted(card, r ui.Rect, lift float32) ui.Rect {
	mid := card.Y + card.H/2
	return ui.Rect{X: card.X + (r.X-card.X)*lift, Y: mid + (r.Y-mid)*lift, W: r.W * lift, H: r.H * lift}
}

// iconRect places an app's icon over the bottom-left corner of its front
// window.
func iconRect(front ui.Rect, card ui.Rect, iconScale float32) ui.Rect {
	size := min(max(card.H*0.34, 22), 52) * iconScale
	return ui.Rect{X: front.X - size*0.22, Y: front.Y + front.H - size*0.7, W: size, H: size}
}

// iconRow places the icons of a card's first n apps, which overlap a
// little, from the first app's in the corner rightward.
func iconRow(front ui.Rect, card ui.Rect, n int, iconScale float32) []ui.Rect {
	ir := iconRect(front, card, iconScale)
	out := make([]ui.Rect, min(n, maxIcons))
	for i := range out {
		out[i] = ir
		out[i].X += float32(i) * ir.W * 0.62
	}
	return out
}
