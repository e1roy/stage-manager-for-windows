package app

import (
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo"
)

// pacer asks a window for its frames no faster than DWM composes them.
// MyGo hands a window that animates a frame as soon as it asks for one: the
// flight layer, over a whole 5K display, drew some 400 a second, millions
// of pixels each, for a display showing 60 or 120, and the GPU, busy with
// frames nobody saw, was late with those that were seen. A frame is asked
// for once DWM has composed the last one (DwmFlush), as MyGo itself paces
// the frames it draws in memory. The windows that animate over a whole
// display, the flight layers and the overview, ask for their frames here
// rather than with Context.AnimationFrame.
type pacer struct{ waiting bool }

const (
	// paceMin keeps frames apart should DwmFlush return at once, as with
	// nothing to compose; paceMax lets a frame come should it not return,
	// so that an animation never stops.
	paceMin = 4 * time.Millisecond
	paceMax = 40 * time.Millisecond
)

// frame has win draw a frame after DWM's next composition pass, unless
// one is already on its way.
func (p *pacer) frame(win *mygo.Window) {
	if p.waiting {
		return
	}
	p.waiting = true
	go func() {
		start := time.Now()
		flushed := make(chan struct{})
		go func() {
			winapi.DwmFlush()
			close(flushed)
		}()
		select {
		case <-flushed:
		case <-time.After(paceMax):
		}
		if d := paceMin - time.Since(start); d > 0 {
			time.Sleep(d)
		}
		mygo.RunOnMain(func() {
			p.waiting = false
			win.Invalidate()
		})
	}()
}
