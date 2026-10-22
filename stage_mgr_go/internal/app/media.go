package app

import (
	"image"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
	"github.com/renkai9418/stage-bar/internal/catalog"
	"github.com/renkai9418/stage-bar/internal/winapi"
)

const (
	// cardStillHeight is the most pixels a card needs of a window's height.
	cardStillHeight = 640
	// flyBudget bounds the memory the stills flights draw take: a window on
	// a 5K display is some 59 MB at the size it shows, and keeping one of
	// those for every app would add up. The stills used longest ago fall
	// back to their card's, and fly to the stage less sharp.
	flyBudget = 128 << 20
)

// stillHeight is how tall a window's still is: the height the window shows
// at, so that a flight to the stage is as sharp as the window it stands in
// for. Smaller stills blur as a flight grows them, which reads as the
// window sharpening when it lands.
func stillHeight(h winapi.HWND) int {
	height := 0
	if r := winapi.FrameBounds(h); r.H() > 0 {
		height = int(r.H())
	}
	// A window cannot show bigger than its display.
	if mon := winapi.MonitorInfo(winapi.MonitorFromWindow(h)).Monitor; mon.H() > 0 {
		height = min(height, int(mon.H()))
	}
	if height <= 0 {
		height = cardStillHeight
	}
	return height
}

// still is a picture of a window, taken when it last showed: one the size
// its card draws, and one the size the window shows at, which a flight to
// the stage draws it at.
type still struct {
	card *ui.Bitmap
	fly  *ui.Bitmap // the same as card while the window is a card's size
	// flyBytes is what fly takes in memory, counted only while it is a
	// picture of its own.
	flyBytes int
	at       time.Time
}

// media holds the pictures the strips show: stills of windows and icons of
// apps. It lives on the main thread; captures run on goroutines.
type media struct {
	stills   map[winapi.HWND]*still
	pending  map[winapi.HWND]bool
	tried    map[winapi.HWND]time.Time
	icons    map[string]*ui.Bitmap // by app
	sem      chan struct{}
	flyBytes int // what all the flight pictures take, against flyBudget
}

func newMedia() *media {
	return &media{
		stills:  map[winapi.HWND]*still{},
		pending: map[winapi.HWND]bool{},
		tried:   map[winapi.HWND]time.Time{},
		icons:   map[string]*ui.Bitmap{},
		// A few at a time: each asks an app to draw itself.
		sem: make(chan struct{}, 3),
	}
}

func (m *media) still(h winapi.HWND) *still { return m.stills[h] }

// preview takes the stills of the windows of a card the pointer rests on,
// at the size the windows show: a switch may follow, and a flight is only
// as sharp as the picture it draws. It runs while the pointer is on the
// card, so it costs nothing on the way of a click.
func (m *media) preview(ws []catalog.Window) {
	var hs []winapi.HWND
	for _, w := range ws {
		if m.pending[w.HWND] {
			continue
		}
		if st := m.stills[w.HWND]; st != nil && st.fly != nil && st.fly != st.card {
			continue
		}
		hs = append(hs, w.HWND)
	}
	m.capture(hs, true, 2*time.Second, nil)
}

// shot is a capture on its way to the main thread: the picture the window
// shows and the smaller one its card draws, which is the same picture while
// the window is a card's size.
type shot struct {
	h    winapi.HWND
	img  *image.RGBA
	card *image.RGBA
}

// capture takes stills of windows on goroutines and calls done on the main
// thread once all are in, or after timeout, whichever comes first, so that a
// window that does not answer cannot hold up a switch. Stills that come in
// later still reach the strip. A full capture is the size the window shows
// at, for a flight into the stage; every other one is a card's size, which
// a window flying out shrinks into and a flight back in grows from.
func (m *media) capture(hs []winapi.HWND, full bool, timeout time.Duration, done func()) {
	if len(hs) == 0 {
		if done != nil {
			done()
		}
		return
	}
	var mu sync.Mutex
	var got []shot
	var wg sync.WaitGroup
	for _, h := range hs {
		m.pending[h] = true
		m.tried[h] = time.Now()
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.sem <- struct{}{}
			height := cardStillHeight
			if full {
				height = stillHeight(h)
			}
			img := winapi.Capture(h, height)
			<-m.sem
			card := img
			if img != nil && img.Rect.Dy() > cardStillHeight {
				card = shrink(img, cardStillHeight)
			}
			mu.Lock()
			got = append(got, shot{h, img, card})
			mu.Unlock()
		}()
	}
	drain := func() []shot {
		mu.Lock()
		defer mu.Unlock()
		out := got
		got = nil
		return out
	}
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	go func() {
		select {
		case <-finished:
		case <-time.After(timeout):
		}
		first := drain()
		post(func() {
			m.take(first)
			if done != nil {
				done()
			}
		})
		<-finished
		if late := drain(); len(late) > 0 {
			post(func() { m.take(late) })
		}
	}()
}

// take keeps captured pictures as bitmaps. Main thread.
func (m *media) take(shots []shot) {
	for _, s := range shots {
		delete(m.pending, s.h)
		if s.img == nil {
			continue
		}
		st := m.stills[s.h]
		if st == nil {
			st = &still{}
			m.stills[s.h] = st
		}
		m.flyBytes -= st.flyBytes
		st.flyBytes = 0
		// The flight's picture may be the card's one; a new one must not
		// take the card's bitmap over.
		if st.fly == st.card {
			st.fly = nil
		}
		put(&st.card, s.card)
		if s.card == s.img {
			st.fly = st.card
		} else {
			put(&st.fly, s.img)
			w, h := st.fly.Size()
			st.flyBytes = w * h * 4
		}
		m.flyBytes += st.flyBytes
		st.at = time.Now()
		// Right away, so that a batch of captures never holds more than
		// the budget at once.
		m.trim()
	}
}

// put gives *bmp the picture, reusing the bitmap when it is the right size.
func put(bmp **ui.Bitmap, img *image.RGBA) {
	if *bmp != nil {
		if w, h := (*bmp).Size(); w == img.Rect.Dx() && h == img.Rect.Dy() {
			(*bmp).Update(img)
			return
		}
	}
	*bmp = ui.NewBitmap(img)
}

// shrink returns img scaled down to height pixels tall.
func shrink(img *image.RGBA, height int) *image.RGBA {
	w := max(1, int(float64(img.Rect.Dx())*float64(height)/float64(img.Rect.Dy())))
	small := image.NewRGBA(image.Rect(0, 0, w, height))
	xdraw.ApproxBiLinear.Scale(small, small.Rect, img, img.Rect, xdraw.Over, nil)
	return small
}

// trim drops the flight pictures used longest ago once they take more
// memory than the budget allows; those windows fly to the stage at their
// card's size, which is less sharp.
func (m *media) trim() {
	for m.flyBytes > flyBudget {
		var oldest *still
		for _, st := range m.stills {
			if st.flyBytes > 0 && (oldest == nil || st.at.Before(oldest.at)) {
				oldest = st
			}
		}
		if oldest == nil {
			return
		}
		oldest.fly = oldest.card
		m.flyBytes -= oldest.flyBytes
		oldest.flyBytes = 0
	}
}

// forget drops what is kept of windows that are gone.
func (m *media) forget(alive map[winapi.HWND]bool) {
	for h, st := range m.stills {
		if !alive[h] {
			m.flyBytes -= st.flyBytes
			delete(m.stills, h)
		}
	}
	for h := range m.tried {
		if !alive[h] {
			delete(m.tried, h)
		}
	}
}

// icon returns the icon of an app, loading it on first use. Main thread:
// the shell draws icons there, in a few milliseconds each.
func (m *media) icon(w catalog.Window) *ui.Bitmap {
	if b, ok := m.icons[w.App]; ok {
		return b
	}
	var b *ui.Bitmap
	if w.Exe != "" {
		if png, err := mygo.Shell.FileIcon(w.Exe, 256); err == nil {
			b, _ = ui.DecodeBitmap(png)
		}
	}
	if b == nil {
		if img := winapi.WindowIcon(w.HWND); img != nil {
			b = ui.NewBitmap(img)
		}
	}
	m.icons[w.App] = b
	return b
}
