//go:build windows

package winapi

import (
	"testing"
	"time"
)

// pressWinTab sets the hook up as HookWinTab does, with the given keys held
// and no key sent or read for real. fired counts the calls of the hook's fn.
func pressWinTab(t *testing.T, held map[uintptr]bool, mask func()) (fired *int) {
	t.Helper()
	fired = new(int)
	fn := func() { *fired++ }
	winTab.held.Store(false)
	winTab.fn.Store(&fn)
	keyIsDown = func(vk uintptr) bool { return held[vk] }
	sendMask = mask
	t.Cleanup(func() {
		winTab.fn.Store(nil)
		winTab.held.Store(false)
		keyIsDown, sendMask = keyHeld, maskWinKey
	})
	return fired
}

// within fails the test when f, which would hang on a lock the hook waits
// for itself on, has not returned in time.
func within(t *testing.T, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the hook is stuck")
	}
}

func TestWinTabTakesPressRepeatsAndRelease(t *testing.T) {
	fired := pressWinTab(t, map[uintptr]bool{vkLWin: true}, func() {})
	taken, fire := winTabKey(true)
	if !taken || fire == nil {
		t.Fatal("Win+Tab was not taken")
	}
	fire()
	if taken, fire = winTabKey(true); !taken || fire != nil {
		t.Fatal("a repeat of the press was not taken without acting again")
	}
	if taken, fire = winTabKey(false); !taken || fire != nil {
		t.Fatal("the release was not taken")
	}
	if taken, _ = winTabKey(false); taken {
		t.Fatal("a release nothing pressed was taken")
	}
	if *fired != 1 {
		t.Fatalf("fn called %d times, want 1", *fired)
	}
}

func TestWinTabLeavesOtherChordsToWindows(t *testing.T) {
	cases := map[string]map[uintptr]bool{
		"Tab alone":     {},
		"Alt+Tab":       {vkLWin: true, vkMenu: true},
		"Ctrl+Win+Tab":  {vkLWin: true, vkControl: true},
		"Shift+Win+Tab": {vkRWin: true, vkShift: true},
	}
	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			pressWinTab(t, held, func() { t.Error("the mask was sent") })
			if taken, _ := winTabKey(true); taken {
				t.Fatal("taken")
			}
		})
	}
}

// The mask goes out through the hook, and Windows may hand it the user's
// release of Tab while it waits: the hook was left waiting on its own lock.
func TestWinTabReleaseWhileMaskGoesOut(t *testing.T) {
	var nested, released bool
	pressWinTab(t, map[uintptr]bool{vkLWin: true}, func() {
		nested, _ = winTabKey(false)
		released = true
	})
	var taken bool
	var fire func()
	within(t, func() { taken, fire = winTabKey(true) })
	if !released || !nested {
		t.Fatal("the release that came while the mask went out was not taken")
	}
	if !taken || fire == nil {
		t.Fatal("the press was not taken")
	}
	// Nothing is left held: the next Win+Tab acts again.
	pressed, fire := winTabKey(true)
	if !pressed || fire == nil {
		t.Fatal("the next press was not taken as a new one")
	}
}

// Likewise a repeat of the press.
func TestWinTabRepeatWhileMaskGoesOut(t *testing.T) {
	var nested bool
	var again func()
	pressWinTab(t, map[uintptr]bool{vkLWin: true}, func() {
		nested, again = winTabKey(true)
	})
	within(t, func() { winTabKey(true) })
	if !nested || again != nil {
		t.Fatal("the repeat that came while the mask went out acted again")
	}
}

func TestWinTabUnhookedTakesNothing(t *testing.T) {
	pressWinTab(t, map[uintptr]bool{vkLWin: true}, func() { t.Error("the mask was sent") })
	winTab.fn.Store(nil)
	if taken, _ := winTabKey(true); taken {
		t.Fatal("taken with no hook")
	}
}
