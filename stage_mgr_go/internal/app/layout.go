package app

import (
	"cmp"
	"math"
	"slices"

	"github.com/egoist/mygo/ui"
)

// Strip geometry in DIPs of the strip window. The cards share one left
// edge and are centered as a group; they shrink as apps are added so the
// whole strip fits, and scroll past that. A strip on the right of its
// display is the mirror image of one on the left: everything that keeps to
// the display's edge there, the cards' outer edges, the axis they turn
// about and the icons, keeps to the right edge, and the windows behind a
// card peek out toward the stage.
const (
	stripPadLeft    = 18
	stripPadRight   = 22
	stripMargin     = 28
	minCardScale    = 0.46
	tiltStep        = 4 // degrees a resting card turns for each step of the tilt setting: the default 6 turns it 24°
	cardPerspective = 640
	maxStacked      = 3
	maxIcons        = 3 // icons a card of several apps shows
	// The cards keep clear of a band at the bottom of the strip, where the
	// panel that opens the overview shows, footerInset above the strip's
	// bottom edge and as far below the cards.
	footerInset = 14
)

// panel is the size of the panel that opens the overview, as the settings
// give it: its width a share of the cards', and its height in DIPs.
type panel struct{ share, h float32 }

// cardTilts returns the degrees a resting card turns at a tilt setting, and
// a hovered one, which turns a quarter of that to face the user.
func cardTilts(level int) (rest, hover float32) {
	rest = float32(level) * tiltStep
	return rest, rest / 4
}

// mirror reflects r across the middle of within, as a strip on the right
// is the one on the left reflected.
func mirror(r, within ui.Rect) ui.Rect {
	r.X = 2*within.X + within.W - r.X - r.W
	return r
}

// turnSign is which way the cards of a strip turn: those on the right turn
// the other way, about their right edge.
func turnSign(right bool) float32 {
	if right {
		return -1
	}
	return 1
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

func layoutStrip(n int, width, height, scroll, gap float32, p panel, right bool) stripLayout {
	pw := max(width-stripPadLeft-stripPadRight, 40)
	ph := min(pw*0.66, 172)
	extent := ph + gap
	// The lowest a card may end: above the panel's band.
	foot := p.h + footerInset
	bottom := height - stripMargin - foot
	avail := max(bottom-stripMargin, 1)
	s := float32(1)
	if n > 0 {
		s = min(max(avail/(float32(n)*extent-gap), minCardScale), 1)
	}
	l := stripLayout{scale: s, extent: extent * s}
	total := float32(n)*extent*s - gap*s
	l.content = total + 2*stripMargin + foot
	// Centered on the display, as on macOS, unless that reaches the band.
	y0 := min((height-total)/2, bottom-total)
	if total > avail {
		y0 = stripMargin - scroll
	}
	for i := 0; i < n; i++ {
		card := ui.Rect{X: stripPadLeft, Y: y0 + float32(i)*extent*s, W: pw * s, H: ph * s}
		if right {
			card = mirror(card, ui.Rect{W: width})
		}
		l.cards = append(l.cards, card)
	}
	return l
}

// slotAt is the slot a card dropped at height y goes into: before the
// first card whose middle is below it.
func (l stripLayout) slotAt(y float32) int {
	for i, c := range l.cards {
		if y < c.Y+c.H/2 {
			return i
		}
	}
	return len(l.cards)
}

// nearestSlot is the slot whose card's middle is nearest to height y: where
// a card of the strip dragged to there goes.
func (l stripLayout) nearestSlot(y float32) int {
	best, dist := 0, float32(math.MaxFloat32)
	for i, c := range l.cards {
		if d := float32(math.Abs(float64(c.Y + c.H/2 - y))); d < dist {
			best, dist = i, d
		}
	}
	return best
}

// makeRoom places the n cards of a strip as it makes room at slot to for
// a card: card from of its own on its way there, the others closing up
// behind it and parting before it, or, with from -1, a card coming in from
// another strip; to -1 makes none. It returns the slot each card shows in
// and how many slots the strip lays out.
func makeRoom(n, from, to int) (slots []int, total int) {
	slots = make([]int, n)
	for i := range slots {
		slots[i] = i
	}
	switch {
	case to < 0:
		return slots, n
	case from < 0:
		for i := to; i < n; i++ {
			slots[i]++
		}
		return slots, n + 1
	}
	for i := range slots {
		j := i
		if j > from {
			j--
		}
		if j >= to {
			j++
		}
		slots[i] = j
	}
	slots[from] = to
	return slots, n
}

// maxScroll is how far a strip with this layout scrolls.
func (l stripLayout) maxScroll(height float32) float32 {
	return max(0, l.content-height)
}

// stackWindows places up to three windows of an app in a card, front
// first. Every window gets the same height, as in Alt+Tab, so wide windows
// are not thin strips; the ones behind step up and toward the stage: to
// the right, or to the left in a strip on the right.
func stackWindows(card ui.Rect, aspects []float64, right bool) []ui.Rect {
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
		if right {
			out[i] = mirror(out[i], card)
		}
	}
	return out
}

// stack places the windows a set's card shows in card, front first.
func (s *set) stack(card ui.Rect, right bool) []ui.Rect {
	ws := s.stacked()
	aspects := make([]float64, len(ws))
	for i, w := range ws {
		aspects[i] = w.Aspect()
	}
	return stackWindows(card, aspects, right)
}

// lifted is r, in card, scaled by lift around the middle of the card's
// outer edge, as the card scales.
func lifted(card, r ui.Rect, lift float32, right bool) ui.Rect {
	x, mid := card.X, card.Y+card.H/2
	if right {
		x += card.W
	}
	return ui.Rect{X: x + (r.X-x)*lift, Y: mid + (r.Y-mid)*lift, W: r.W * lift, H: r.H * lift}
}

// iconRect places an app's icon over the outer bottom corner of its front
// window: the bottom-left one, or the bottom-right in a strip on the right.
func iconRect(front ui.Rect, card ui.Rect, iconScale float32, right bool) ui.Rect {
	size := min(max(card.H*0.34, 22), 52) * iconScale
	r := ui.Rect{X: front.X - size*0.22, Y: front.Y + front.H - size*0.7, W: size, H: size}
	if right {
		r = mirror(r, front)
	}
	return r
}

// iconRow places the icons of a card's first n apps, which overlap a
// little, from the first app's in the corner toward the stage.
func iconRow(front ui.Rect, card ui.Rect, n int, iconScale float32, right bool) []ui.Rect {
	ir := iconRect(front, card, iconScale, right)
	step := ir.W * 0.62 * turnSign(right)
	out := make([]ui.Rect, min(n, maxIcons))
	for i := range out {
		out[i] = ir
		out[i].X += float32(i) * step
	}
	return out
}

// footerButton is where the panel shows: under the cards and centered on
// them, its share of their width.
func footerButton(width, height float32, p panel, right bool) ui.Rect {
	cw := max(width-stripPadLeft-stripPadRight, 40)
	w := cw * p.share
	r := ui.Rect{X: stripPadLeft + (cw-w)/2, Y: height - footerInset - p.h, W: w, H: p.h}
	if right {
		r = mirror(r, ui.Rect{W: width})
	}
	return r
}

// footerBand is where the pointer brings up the panel, and a click opens
// the overview: the panel and footerInset around it, down to the strip's
// bottom edge. A narrow panel leaves the rest of the bottom to the desktop.
func footerBand(width, height float32, p panel, right bool) ui.Rect {
	r := footerButton(width, height, p, right)
	x0, x1 := max(r.X-footerInset, 0), min(r.X+r.W+footerInset, width)
	return ui.Rect{X: x0, Y: r.Y - footerInset, W: x1 - x0, H: height - r.Y + footerInset}
}

// The overview's panel begins to come in with the pointer panelReach DIPs
// from it and is in full panelFull from it, so it is there by the time the
// pointer gets to it, as the Dock is when it comes up.
const (
	panelReach = 90
	panelFull  = 16
)

// panelNearness is how far the panel shows, 0 to 1, with the pointer dist
// DIPs from it.
func panelNearness(dist float32) float32 { return nearness(dist, panelReach, panelFull) }

// nearness is how far something that comes in as the pointer nears it
// shows, 0 to 1, with the pointer dist DIPs from it: nothing from reach
// away, in full from full away, easing in and out of the ends.
func nearness(dist, reach, full float32) float32 {
	k := min(max((reach-dist)/(reach-full), 0), 1)
	return k * k * (3 - 2*k)
}

// rectDist is how far a point is from r, 0 inside it.
func rectDist(r ui.Rect, x, y float32) float32 {
	dx := max(r.X-x, 0, x-r.X-r.W)
	dy := max(r.Y-y, 0, y-r.Y-r.H)
	return float32(math.Hypot(float64(dx), float64(dy)))
}

// overviewItem is a window the overview spreads out: its size, and where
// its middle is as the overview opens, in DIPs.
type overviewItem struct{ w, h, cx, cy float32 }

// layoutOverview spreads windows out in rows across area, gx apart side by
// side and gy between rows, as Mission Control does. The rows take them top
// to bottom, and each row left to right, by where they are as it opens, so
// that each moves little. Every window gets the height of the tallest row
// that lets them all fit, as Task View gives them, though none shows larger
// than it is; the rows, and the block of them, are centered. Of the ways to
// deal the windows into rows, the one that shows the most of them wins. It
// returns where each goes, in the order given.
func layoutOverview(area ui.Rect, items []overviewItem, gx, gy float32) []ui.Rect {
	out := make([]ui.Rect, len(items))
	if len(items) == 0 || area.W <= 0 || area.H <= 0 {
		return out
	}
	aspect := func(i int) float32 { return max(items[i].w, 1) / max(items[i].h, 1) }
	shownH := func(i int, h float32) float32 { return min(h, max(items[i].h, 1)) }
	byY := make([]int, len(items))
	for i := range byY {
		byY[i] = i
	}
	slices.SortStableFunc(byY, func(x, y int) int { return cmp.Compare(items[x].cy, items[y].cy) })
	var rows [][]int
	var height float32
	best := float32(-1)
	for n := 1; n <= len(items); n++ {
		split := splitRows(byY, n, items)
		h := (area.H - gy*float32(n-1)) / float32(n)
		for _, row := range split {
			sum := float32(0)
			for _, i := range row {
				sum += aspect(i)
			}
			h = min(h, (area.W-gx*float32(len(row)-1))/sum)
		}
		if h <= 0 {
			continue
		}
		shown := float32(0)
		for _, i := range byY {
			th := shownH(i, h)
			shown += th * th * aspect(i)
		}
		// On a tie, as when every window shows at its own size, the fewer rows.
		if shown > best*1.0001 {
			best, rows, height = shown, split, h
		}
	}
	if rows == nil {
		return out
	}
	bands := make([]float32, len(rows))
	total := gy * float32(len(rows)-1)
	for r, row := range rows {
		for _, i := range row {
			bands[r] = max(bands[r], shownH(i, height))
		}
		total += bands[r]
	}
	y := area.Y + (area.H-total)/2
	for r, row := range rows {
		w := gx * float32(len(row)-1)
		for _, i := range row {
			w += shownH(i, height) * aspect(i)
		}
		x := area.X + (area.W-w)/2
		for _, i := range row {
			th := shownH(i, height)
			tw := th * aspect(i)
			out[i] = ui.Rect{X: x, Y: y + (bands[r]-th)/2, W: tw, H: th}
			x += tw + gx
		}
		y += bands[r] + gy
	}
	return out
}

// splitRows deals windows, given top first, into n rows as evenly as can
// be, the upper rows taking one more where they do not divide evenly, and
// orders each row left to right.
func splitRows(byY []int, n int, items []overviewItem) [][]int {
	rows := make([][]int, n)
	at := 0
	for r := range rows {
		k := len(byY) / n
		if r < len(byY)%n {
			k++
		}
		rows[r] = slices.Clone(byY[at : at+k])
		slices.SortStableFunc(rows[r], func(x, y int) int { return cmp.Compare(items[x].cx, items[y].cx) })
		at += k
	}
	return rows
}
