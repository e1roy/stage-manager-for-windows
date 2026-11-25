package app

import (
	"testing"

	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

// TestLive: work coming back to the main thread acts on a display only
// while it is still there, managed, and Stage Manager is not quitting;
// each of the others has sent its windows home already.
func TestLive(t *testing.T) {
	d := newDisplay(winapi.Monitor{Device: `\\.\DISPLAY1`})
	a := &App{settings: settings.Default(), displays: []*display{d}}
	if !a.live(d) {
		t.Fatal("a managed display that is there is not live")
	}
	a.settings.Displays = map[string]bool{d.mon.Device: false}
	if a.live(d) {
		t.Error("a display no longer managed is live")
	}
	a.settings = settings.Default()
	a.displays = []*display{newDisplay(winapi.Monitor{Device: `\\.\DISPLAY2`})}
	if a.live(d) {
		t.Error("a display unplugged is live")
	}
	a.displays = []*display{d}
	a.quitting = true
	if a.live(d) {
		t.Error("a display is live while quitting")
	}
}
