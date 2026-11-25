package app

import (
	"testing"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

// TestMinimizeMode: what a minimize does follows from the two settings and
// quitting, and only from minimizeMode.
func TestMinimizeMode(t *testing.T) {
	for _, c := range []struct {
		hide, toStrip, quitting bool
		want                    minimizeMode
	}{
		{false, false, false, minimizeOff},
		{false, true, false, minimizeOff},
		{true, false, false, minimizeNative},
		{true, true, false, minimizeToStrip},
		{true, true, true, minimizeOff},
	} {
		a := &App{settings: settings.Default(), quitting: c.quitting}
		a.settings.HideInactive, a.settings.MinimizeToStrip = c.hide, c.toStrip
		if got := a.minimizeMode(); got != c.want {
			t.Errorf("hide %v, to strip %v, quitting %v: %v, want %v", c.hide, c.toStrip, c.quitting, got, c.want)
		}
	}
}

// TestEndMinimizeStopsTheFollow: a minimize that ended (endMinimize) is
// followed no longer: its timer is stopped and dropped, so a tick of it
// after finds no minimize to act for.
func TestEndMinimizeStopsTheFollow(t *testing.T) {
	a := &App{minimizing: map[winapi.HWND]*minimizeTarget{}}
	const h = winapi.HWND(1)
	cur := &minimizeTarget{timer: time.NewTimer(time.Hour)}
	a.minimizing[h] = cur
	a.endMinimize(h, cur)
	if a.minimizing[h] != nil || cur.timer != nil {
		t.Error("an ended minimize is still followed")
	}
}
