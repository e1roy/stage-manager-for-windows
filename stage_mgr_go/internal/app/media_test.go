package app

import (
	"image"
	"sync/atomic"
	"testing"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

// TestCaptureAsksOnceAtATime: a window whose app does not answer holds the
// capture it is in, and one capture slot with it. Asked again with every
// switch away from it, it would take the slots one by one until no window
// could be captured at all.
func TestCaptureAsksOnceAtATime(t *testing.T) {
	m := newMedia()
	var calls atomic.Int32
	release := make(chan struct{})
	defer close(release)
	m.shoot = func(winapi.HWND, int) *image.RGBA {
		calls.Add(1)
		<-release
		return nil
	}
	const h = winapi.HWND(0x7ffffff0)
	for range cap(m.sem) + 1 {
		m.capture([]winapi.HWND{h}, false, time.Millisecond, nil)
	}
	time.Sleep(50 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Fatalf("asked the window %d times, want once", n)
	}
	// Another window still gets a slot.
	m.capture([]winapi.HWND{h + 1}, false, time.Millisecond, nil)
	time.Sleep(50 * time.Millisecond)
	if n := calls.Load(); n != 2 {
		t.Fatalf("another window was not captured: %d calls", n)
	}
}

// TestForgetDropsPending: a window that closed while captured is not
// waited for any longer.
func TestForgetDropsPending(t *testing.T) {
	m := newMedia()
	m.pending[1] = true
	m.forget(map[winapi.HWND]bool{})
	if m.pending[1] {
		t.Error("a gone window is still counted as being captured")
	}
}
