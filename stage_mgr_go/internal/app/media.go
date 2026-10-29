package app

import (
	"bytes"
	"image"
	"image/png"
	"sync"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
)

// cardStillHeight is the most pixels a card needs of a window's height.
const cardStillHeight = 640

// flyBudgets bound the memory the stills flights draw take, by quality: a
// window on a 5K display is some 59 MB at the size it shows, and one of
// those per app would add up. The stills used longest ago fall back to
// their card's and fly to the stage less sharp. Low keeps two such stills,
// medium six and high some seventeen.
var flyBudgets = map[settings.Quality]int{
	settings.Low:    128 << 20,
	settings.Medium: 384 << 20,
	settings.High:   1 << 30,
}

// stillHeight is how tall a window's still is: the height the window shows
// at, so that a flight to the stage is as sharp as the window it stands in
// for. Smaller stills blur as a flight grows them, which reads as the
// window sharpening when it lands.
//
// Light, it is the height the window shows at in DIPs instead, as on a
// display at 100% (settings.LightFlights).
func stillHeight(h winapi.HWND, light bool) int {
	height := 0
	if r := winapi.FrameBounds(h); r.H() > 0 {
		height = int(r.H())
	}
	// A window cannot show bigger than its display.
	mon := winapi.MonitorInfo(winapi.MonitorFromWindow(h))
	if mon.Monitor.H() > 0 {
		height = min(height, int(mon.Monitor.H()))
	}
	if scale := mon.Scale(); light && scale > 1 {
		height = max(int(float64(height)/scale), cardStillHeight)
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
	flyBytes int // what all the flight pictures take, against budget
	budget   int
	// full takes every still at the size the window shows, so that windows
	// fly out of the stage as sharp as they fly in.
	full bool
	// light takes the stills windows fly with at the size they show in
	// DIPs (settings.LightFlights).
	light bool
}

func newMedia() *media {
	m := &media{
		stills:  map[winapi.HWND]*still{},
		pending: map[winapi.HWND]bool{},
		tried:   map[winapi.HWND]time.Time{},
		icons:   map[string]*ui.Bitmap{},
		// A few at a time: each asks an app to draw itself.
		sem: make(chan struct{}, 3),
	}
	m.apply(settings.Default())
	return m
}

// apply takes what the settings say of the stills: how sharp flights are
// and how much memory their stills may take, a lower budget letting go of
// what is over it at once. Stills taken before keep their size until they
// are taken again.
func (m *media) apply(s settings.Settings) {
	q := s.Normalize().Quality
	m.budget = flyBudgets[q]
	m.full = q == settings.Medium || q == settings.High
	m.light = s.LightFlights
	m.trim()
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
// thread once all are in, or after timeout, so that a window that does not
// answer cannot hold up a switch; stills arriving later still reach the
// strip. A full capture is the size the window shows at, for a flight into
// the stage; the others are a card's size, which a window flying out shrinks
// into and a flight back in grows from.
func (m *media) capture(hs []winapi.HWND, full bool, timeout time.Duration, done func()) {
	if len(hs) == 0 {
		if done != nil {
			done()
		}
		return
	}
	// Read here: the goroutines must not touch the media.
	full = full || m.full
	light := m.light
	// Some apps flash when asked to draw themselves: a capture nobody
	// expected is worth seeing in the log.
	debugf("capture %x full=%v", hs, full)
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
				height = stillHeight(h, light)
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
	for m.flyBytes > m.budget {
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
	var img *image.NRGBA
	if w.Exe != "" {
		// The program's own icon, drawn from its largest image: the shell
		// would put that of an app with no large one, as Q-Dir, small in
		// the middle of a tile.
		img = winapi.FileIcon(w.Exe, 256)
		if img == nil {
			if data, err := mygo.Shell.FileIcon(w.Exe, 256); err == nil {
				if d, err := png.Decode(bytes.NewReader(data)); err == nil {
					img = toNRGBA(d)
				}
			}
		}
	}
	if img == nil {
		img = winapi.WindowIcon(w.HWND)
	}
	var b *ui.Bitmap
	if img != nil {
		b = ui.NewBitmap(trimIcon(img))
	}
	m.icons[w.App] = b
	return b
}
