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

func TestWinDesktopTakesWinDOncePerPress(t *testing.T) {
	var calls int
	fn := func() { calls++ }
	winDesktop.fn.Store(&fn)
	winDesktop.held.Store(0)
	keyIsDown = func(vk uintptr) bool { return vk == vkLWin }
	sendMask = func() {}
	t.Cleanup(func() {
		winDesktop.fn.Store(nil)
		winDesktop.held.Store(0)
		keyIsDown, sendMask = keyHeld, maskWinKey
	})
	for _, key := range []uint32{vkD, vkM} {
		if taken, fire := winDesktopKey(key, true); !taken || fire == nil {
			t.Fatalf("Win+%c was not taken", key)
		} else {
			fire()
		}
		if taken, fire := winDesktopKey(key, true); !taken || fire != nil {
			t.Fatalf("Win+%c repeat was not suppressed", key)
		}
		if taken, fire := winDesktopKey(key, false); !taken || fire != nil {
			t.Fatalf("Win+%c release was not suppressed", key)
		}
	}
	if calls != 2 {
		t.Fatalf("called %d times, want 2", calls)
	}
}

func pressWinDesktop(t *testing.T, held map[uintptr]bool, mask func()) *int {
	t.Helper()
	calls := new(int)
	fn := func() { *calls++ }
	winDesktop.fn.Store(&fn)
	winDesktop.held.Store(0)
	keyIsDown = func(vk uintptr) bool { return held[vk] }
	sendMask = mask
	t.Cleanup(func() {
		winDesktop.fn.Store(nil)
		winDesktop.held.Store(0)
		keyIsDown, sendMask = keyHeld, maskWinKey
	})
	return calls
}

func TestWinDesktopLeavesOtherChordsUntouched(t *testing.T) {
	cases := map[string]map[uintptr]bool{
		"D alone":   {},
		"M alone":   {},
		"Ctrl+Win":  {vkLWin: true, vkControl: true},
		"Shift+Win": {vkRWin: true, vkShift: true},
		"Alt+Win":   {vkLWin: true, vkMenu: true},
	}
	for name, held := range cases {
		t.Run(name, func(t *testing.T) {
			calls := pressWinDesktop(t, held, func() { t.Error("mask sent") })
			for _, key := range []uint32{vkD, vkM} {
				if taken, fire := winDesktopKey(key, true); taken || fire != nil {
					t.Fatalf("%c press taken", key)
				}
				if taken, fire := winDesktopKey(key, true); taken || fire != nil {
					t.Fatalf("%c repeat taken", key)
				}
				if taken, fire := winDesktopKey(key, false); taken || fire != nil {
					t.Fatalf("%c release taken", key)
				}
			}
			if *calls != 0 {
				t.Fatalf("called %d times", *calls)
			}
		})
	}
}

func TestWinDesktopDoesNotPromoteRepeatAfterModifiersChange(t *testing.T) {
	held := map[uintptr]bool{}
	masks := 0
	pressWinDesktop(t, held, func() { masks++ })
	if taken, _ := winDesktopKey(vkD, true); taken {
		t.Fatal("D alone taken")
	}
	held[vkLWin] = true
	if taken, _ := winDesktopKey(vkD, true); taken {
		t.Fatal("D repeat became Win+D")
	}
	winDesktopKey(vkD, false)
	if masks != 0 {
		t.Fatalf("mask sent %d times before fresh press", masks)
	}
	if taken, fire := winDesktopKey(vkD, true); !taken || fire == nil {
		t.Fatal("fresh Win+D not taken")
	}
	if masks != 1 {
		t.Fatalf("mask sent %d times, want 1", masks)
	}
}

func TestWinDesktopReleaseDuringMask(t *testing.T) {
	var nested bool
	pressWinDesktop(t, map[uintptr]bool{vkRWin: true}, func() {
		nested, _ = winDesktopKey(vkD, false)
	})
	var taken bool
	var fire func()
	within(t, func() { taken, fire = winDesktopKey(vkD, true) })
	if !taken || fire == nil || !nested {
		t.Fatal("release during mask lost or deadlocked")
	}
	if taken, fire := winDesktopKey(vkD, true); !taken || fire == nil {
		t.Fatal("new press after nested release not taken")
	}
}

func TestWinDesktopRepeatDuringMask(t *testing.T) {
	var nested bool
	var again func()
	pressWinDesktop(t, map[uintptr]bool{vkLWin: true}, func() {
		nested, again = winDesktopKey(vkM, true)
	})
	within(t, func() { winDesktopKey(vkM, true) })
	if !nested || again != nil {
		t.Fatal("repeat during mask acted twice or deadlocked")
	}
}

func TestWinDesktopTracksDAndMSeparately(t *testing.T) {
	calls := pressWinDesktop(t, map[uintptr]bool{vkRWin: true}, func() {})
	for _, key := range []uint32{vkD, vkM} {
		if taken, fire := winDesktopKey(key, true); !taken || fire == nil {
			t.Fatalf("%c not taken", key)
		} else {
			fire()
		}
	}
	winDesktopKey(vkD, false)
	if taken, fire := winDesktopKey(vkM, true); !taken || fire != nil {
		t.Fatal("M repeat acted")
	}
	if taken, fire := winDesktopKey(vkD, true); !taken || fire == nil {
		t.Fatal("new D not taken")
	} else {
		fire()
	}
	if *calls != 3 {
		t.Fatalf("called %d times, want 3", *calls)
	}
}

func TestWinDesktopUnhookedAndOtherKeys(t *testing.T) {
	pressWinDesktop(t, map[uintptr]bool{vkLWin: true}, func() { t.Error("mask sent") })
	winDesktop.fn.Store(nil)
	for _, key := range []uint32{vkD, vkM, vkTab, 0x41} {
		if taken, fire := winDesktopKey(key, true); taken || fire != nil {
			t.Fatalf("key %x taken", key)
		}
	}
}

func TestWinDesktopDoesNotChangeWinTabState(t *testing.T) {
	pressWinDesktop(t, map[uintptr]bool{vkLWin: true}, func() {})
	winTab.held.Store(true)
	t.Cleanup(func() { winTab.held.Store(false) })
	winDesktopKey(vkD, true)
	if !winTab.held.Load() {
		t.Fatal("Win+Tab state changed")
	}
}

func TestHookWinDesktopUnhookIsIdempotent(t *testing.T) {
	unhook, err := HookWinDesktop(func() {})
	if err != nil {
		t.Fatal(err)
	}
	within(t, func() { unhook(); unhook() })
	if winDesktop.fn.Load() != nil {
		t.Fatal("callback still registered")
	}
}
