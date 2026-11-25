package app

import (
	"fmt"
	"log"
	"maps"
	"slices"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo"
)

// hookEvents listens to the system's window events, so that the stage
// follows Alt+Tab, taskbar clicks and new windows at once rather than on a
// polling tick. The hooks deliver on the main thread.
func (a *App) hookEvents() {
	a.unhook = winapi.HookWinEvents(func(event uint32, h winapi.HWND) {
		defer tookLong(time.Now(), func() string { return fmt.Sprintf("win event %#x for %x", event, h) })
		a.onWinEvent(event, h)
	},
		[2]uint32{winapi.EventSystemForeground, winapi.EventSystemForeground},
		[2]uint32{winapi.EventSystemMoveSizeStart, winapi.EventSystemMoveSizeEnd},
		[2]uint32{winapi.EventSystemMinimizeStart, winapi.EventSystemMinimizeStart},
		[2]uint32{winapi.EventSystemMinimizeEnd, winapi.EventSystemMinimizeEnd},
		[2]uint32{winapi.EventObjectCreate, winapi.EventObjectHide},
		[2]uint32{winapi.EventObjectLocationChange, winapi.EventObjectNameChange},
		[2]uint32{winapi.EventObjectCloaked, winapi.EventObjectUncloaked},
	)
}

// syncHooks installs or releases the keyboard and mouse hooks that take
// shortcuts and clicks from Windows (Win+D/Win+M, the minimize button,
// Win+Tab), each as the settings, the displays and quitting ask. Whether a
// hook is wanted depends on all three, so it runs after any of them
// changes: from syncDisplays, which applySettings and every change of the
// displays go through, and from shutdown. It is the only caller of the
// sync* functions of the hooks (TestHooksSyncTogether).
func (a *App) syncHooks() {
	a.syncWinDesktop()
	a.syncMinButton()
	a.syncWinTab()
}

// Give the native shortcuts back whenever stages are not being hidden or
// there is no managed display to send them to.
func (a *App) wantsWinDesktop() bool {
	return a.settings.HideInactive && !a.quitting && slices.ContainsFunc(a.displays, a.managed)
}

func (a *App) syncWinDesktop() {
	want := a.wantsWinDesktop()
	if want == (a.unhookWinDesktop != nil) {
		return
	}
	if !want {
		a.unhookWinDesktop()
		a.unhookWinDesktop = nil
		return
	}
	// The keyboard hook only suppresses the native desktop/minimize action;
	// all stage and window work must happen on the main thread.
	unhook, err := winapi.HookWinDesktop(func() { post(a.clearStages) })
	if err != nil {
		log.Println("stagebar: taking Win+D/Win+M:", err)
	} else {
		a.unhookWinDesktop = unhook
	}
}

// stagesToClear includes a stage still in flight, but never an already
// empty one. A second Win+D must not bring anything back.
func (a *App) stagesToClear() []*display {
	var out []*display
	for _, d := range a.displays {
		if a.managed(d) && len(a.heading(d)) > 0 {
			out = append(out, d)
		}
	}
	return out
}

func (a *App) clearStages() {
	if !a.ready || a.quitting || !a.settings.HideInactive {
		return
	}
	for _, d := range a.stagesToClear() {
		a.switchStage(d, nil, 0, false, false)
	}
}

// minimizeNext is the stage that takes the place of the set a window of
// app is minimized from (settings.MinimizeToStrip): the stage before it,
// or none. The grouped apps leave as one card. It is nil for an app that
// is not on the stage.
func minimizeNext(d *display, app string) []string {
	if !d.model.OnStage(app) {
		return nil
	}
	if prev := d.model.Previous(); prev != nil {
		return prev
	}
	return []string{}
}

// minimizeMode is what minimizing a window on a stage does, as the
// settings have it. Every rule that depends on it asks minimizeMode, so
// that a mode added, or a setting it hangs on, is one change here.
type minimizeMode uint8

const (
	// minimizeOff: stages are not hidden, or Stage Manager is quitting;
	// a minimize is Windows' alone.
	minimizeOff minimizeMode = iota
	// minimizeNative: the window goes to the taskbar, and the stage
	// changes only if it was the last one on it (settleMinimize).
	minimizeNative
	// minimizeToStrip: the window's stage goes to the strip and the one
	// before comes back (settings.MinimizeToStrip), its minimize button
	// taken (syncMinButton) and its animations off (calmWanted).
	minimizeToStrip
)

func (a *App) minimizeMode() minimizeMode {
	switch {
	case a.quitting || !a.settings.HideInactive:
		return minimizeOff
	case a.settings.MinimizeToStrip:
		return minimizeToStrip
	}
	return minimizeNative
}

type minimizeTarget struct {
	d   *display
	app string
	// root is the window as the catalog lists it.
	root winapi.HWND
	// mode is what the minimize does, as it was when it began: minimizeNative
	// or minimizeToStrip.
	mode     minimizeMode
	step     minimizeStep
	timer    *time.Timer
	watchdog *time.Timer
}

// stop stops the timers that follow t.
func (t *minimizeTarget) stop() {
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	if t.watchdog != nil {
		t.watchdog.Stop()
	}
}

// endMinimize ends the minimize t of window h: its timers stop, and the
// foreground and refreshes it held back go on. A timer that fired as it
// was stopped is already queued; it finds t gone and does nothing (follow).
func (a *App) endMinimize(h winapi.HWND, t *minimizeTarget) {
	t.stop()
	if a.minimizing[h] == t {
		delete(a.minimizing, h)
	}
	a.refreshSoon()
}

// follow runs fn for the minimize t of window h after d, unless t has
// ended or another minimize of h took its place meanwhile.
func (a *App) follow(h winapi.HWND, t *minimizeTarget, d time.Duration, fn func(winapi.HWND, *minimizeTarget)) *time.Timer {
	return after(d, func() {
		if a.minimizing[h] == t {
			fn(h, t)
		}
	})
}

// minimizeStep is where a minimize the app follows is:
//
//	iconic → restoring    (MinimizeToStrip: the stage then goes to the strip)
//	iconic → settling     (native: the window stays on the taskbar)
//
// The window leaves App.minimizing as it ends, or as the watchdog gives up
// on it.
type minimizeStep uint8

const (
	// minimizeIconic waits for Windows to make the window iconic: the
	// start of a minimize is reported before.
	minimizeIconic minimizeStep = iota
	// minimizeRestoring has asked the iconic window back, without
	// activating it, and waits for it to show to park it.
	minimizeRestoring
	// minimizeSettling lets the foreground settle for minimizeSettle, the
	// window left on the taskbar (settleMinimize).
	minimizeSettling
)

// minimizeSettle is how long after a window left to minimize is iconic the
// foreground still is not followed: Windows hands it on as the window
// goes, to the next window down, which may be a parked one, and the event
// can come after the window is seen iconic.
const minimizeSettle = 100 * time.Millisecond

// observe ignores the pre-minimize Start event until Windows has actually
// made the window iconic, then waits for our nonactivating restore.
func (t *minimizeTarget) observe(iconic bool) (restore, done bool) {
	if iconic {
		t.step = minimizeRestoring
		return true, false
	}
	return false, t.step == minimizeRestoring
}

// onMinimizeStart saves the window's identity before the catalog removes it.
func (a *App) onMinimizeStart(h winapi.HWND) {
	mode := a.minimizeMode()
	if !a.ready || mode == minimizeOff || a.park.IsParked(h) || a.minimizing[h] != nil {
		return
	}
	root := catalog.RootOwner(h)
	if w, ok := a.listed(root); ok {
		if d := a.stagedOn(w); d != nil {
			t := &minimizeTarget{d: d, app: w.App, root: root, mode: mode}
			a.minimizing[h] = t
			// A crashed/closed window may never report MinimizeEnd. Never
			// leave foreground and refresh processing suppressed indefinitely.
			t.watchdog = a.follow(h, t, minimizeGiveUp, a.endMinimize)
			// Look at once: the sooner it is back, the shorter the moment
			// the stage shows without it.
			t.timer = a.follow(h, t, 0, a.finishMinimize)
		}
		return
	}
}

// minimizeGiveUp is how long a minimize is followed at most. Windows makes
// a window iconic within a frame or two and the app takes the restore as
// fast; one that has done neither by then has closed or hangs.
const minimizeGiveUp = 2 * time.Second

// minimizePoll is how often a minimizing window is looked at until it is
// back: about a frame, as the stage shows without it all that time.
const minimizePoll = 16 * time.Millisecond

// calmWanted reports whether a window should go without DWM's minimize and
// restore animations. A window on a stage that is hidden when inactive
// does not go to the taskbar when minimized: it is brought straight back
// and flies to the strip. Played, the animation shrinks it into the
// taskbar and grows it out again before the flight, a flash. The
// animation has to be off before the minimize begins: by the time the
// minimize is reported, DWM is already playing it.
func (a *App) calmWanted(w catalog.Window) bool {
	if a.minimizeMode() != minimizeToStrip {
		return false
	}
	d := a.displayOf(w)
	return d != nil && a.managed(d) && d.model.OnStage(w.App)
}

// syncCalm turns the animations off for the windows that should go without
// them (calmWanted) and back on for those that no longer should.
func (a *App) syncCalm() {
	want := map[winapi.HWND]bool{}
	for _, w := range a.windows {
		if a.calmWanted(w) {
			want[w.HWND] = true
		}
	}
	for h := range a.calm {
		if !want[h] {
			delete(a.calm, h)
			if winapi.IsWindow(h) {
				winapi.SetTransitionsDisabled(h, false)
			}
		}
	}
	for h := range want {
		if !a.calm[h] {
			a.calm[h] = true
			winapi.SetTransitionsDisabled(h, true)
		}
	}
	a.shareCalm()
}

// shareCalm hands the minimize-button hook a copy of calm.
func (a *App) shareCalm() {
	m := maps.Clone(a.calm)
	a.calmShared.Store(&m)
}

// syncMinButton takes the minimize button of the stage's windows while
// minimizing one sends the stage to the strip (MinimizeToStrip). A window
// minimized from the taskbar or with Win+Down still goes through
// onMinimizeStart.
func (a *App) syncMinButton() {
	want := a.wantsWinDesktop() && a.minimizeMode() == minimizeToStrip
	if want == (a.unhookMinButton != nil) {
		return
	}
	if !want {
		a.unhookMinButton()
		a.unhookMinButton = nil
		a.aim = taskbarAim{}
		return
	}
	calm := func(h winapi.HWND) bool {
		m := a.calmShared.Load()
		return m != nil && (*m)[h]
	}
	unhook, err := winapi.HookMinButton(calm, func(h winapi.HWND) { post(func() { a.minimizeClicked(h) }) })
	if err != nil {
		log.Println("stagebar: taking the minimize button:", err)
		return
	}
	a.unhookMinButton = unhook
}

// minimizeClicked sends the stage the window is on to the strip, as a
// minimize of it does (finishMinimize), but with the window never gone
// from the screen: Windows hides a minimized window at once, and it showed
// again a moment later, to be captured and fly. Here it stays where it is
// until the flight takes it. A window no longer on a stage is minimized,
// as the user asked.
func (a *App) minimizeClicked(h winapi.HWND) {
	if a.ready && a.minimizeMode() == minimizeToStrip && len(a.minimizing) == 0 {
		if w, ok := a.listed(h); ok {
			if d := a.stagedOn(w); d != nil {
				debugf("minimize button %x: stage to the strip", h)
				a.minimizeTo(d, minimizeNext(d, w.App))
				return
			}
		}
	}
	debugf("minimize button %x: minimize", h)
	winapi.Minimize(h)
}

// liven gives a window that came home with its animations off them back,
// unless it is to go without them on the stage (calmWanted): then it is
// left off, not turned on until the next refresh turns it off again.
// livenDelay is how long a window that came home with its animations off
// (calm) waits before they come back: until any DWM transition from its
// flight landing has surely finished, so that it does not fade in as well.
// The same wait as the strip's staying out (landLinger), whose value it is.
const livenDelay = landLinger

// livenLater gives the windows back their animations once one of them
// lands (livenDelay).
func (a *App) livenLater(ws []catalog.Window) {
	after(livenDelay, func() {
		for _, w := range ws {
			a.liven(w)
		}
	})
}

func (a *App) liven(w catalog.Window) {
	switch {
	case !winapi.IsWindow(w.HWND):
	case a.calmWanted(w):
		a.calm[w.HWND] = true
		a.shareCalm()
	case !a.calm[w.HWND]:
		winapi.SetTransitionsDisabled(w.HWND, false)
	}
}

// finishMinimize follows the minimize t of window h until Windows has made
// it iconic, and then, as its mode has it, brings it back to fly to the
// strip or lets it settle on the taskbar.
func (a *App) finishMinimize(h winapi.HWND, t *minimizeTarget) {
	t.timer = nil
	if !a.live(t.d) || !winapi.IsWindow(h) || winapi.IsHung(h) {
		a.endMinimize(h, t)
		return
	}
	iconic := winapi.IsIconic(h)
	if t.mode == minimizeNative {
		if !iconic {
			t.timer = a.follow(h, t, minimizePoll, a.finishMinimize)
			return
		}
		// Iconic, it is followed no more than settleMinimize takes.
		t.watchdog.Stop()
		t.step = minimizeSettling
		t.timer = a.follow(h, t, minimizeSettle, a.settleMinimize)
		return
	}
	restore, done := t.observe(iconic)
	if restore {
		// Post rather than wait for the app's UI thread. Do not activate.
		winapi.ShowNoActivate(h)
	}
	if !done || !winapi.IsWindowVisible(h) {
		t.timer = a.follow(h, t, minimizePoll, a.finishMinimize)
		return
	}
	a.endMinimize(h, t)
	if next := minimizeNext(t.d, t.app); next != nil {
		a.minimizeTo(t.d, next)
	}
}

// minimizeTo switches display d to next for a minimize, the foreground
// Windows handed on meanwhile not followed: the stage that comes back is
// the one to be in, should there be one.
func (a *App) minimizeTo(d *display, next []string) {
	a.fg.h = 0
	if a.fg.timer != nil {
		a.fg.timer.Stop()
		a.fg.timer = nil
	}
	a.switchStage(d, next, 0, len(next) > 0, false)
}

// settleMinimize ends a minimize left to Windows, as Windows' own: the
// window is on the taskbar. Was it the last on its stage, the stage before
// comes back, as when the stage's windows close (refill); otherwise the
// stage stays, with the user in it rather than in a parked window that
// Windows may have handed the foreground to.
func (a *App) settleMinimize(h winapi.HWND, t *minimizeTarget) {
	if t.step != minimizeSettling {
		return
	}
	t.timer = nil
	a.endMinimize(h, t)
	if !a.live(t.d) {
		return
	}
	a.windows = a.cat.Windows()
	var left []catalog.Window
	for _, w := range a.windows {
		if w.HWND != t.root && !a.hidden(w) && a.displayOf(w) == t.d && t.d.model.OnStage(w.App) {
			left = append(left, w)
		}
	}
	if len(left) == 0 {
		prev := t.d.model.Previous()
		debugf("minimized %x, last on the stage: back to %v", h, prev)
		if prev != nil {
			a.minimizeTo(t.d, prev)
		}
		return
	}
	fg := catalog.RootOwner(winapi.Foreground())
	debugf("minimized %x, %d left on the stage, foreground %x parked=%v", h, len(left), fg, a.park.IsParked(fg))
	if a.park.IsParked(fg) {
		winapi.Activate(left[0].HWND)
	}
}

// syncWinTab takes Win+Tab from Task View for the overview while the
// settings ask for it and a display has a strip to open it on, and gives
// it back otherwise, and as Stage Manager quits. Task View spreads out
// every window, as the overview does, but knows nothing of the stages and
// strips; the overview is what Mission Control's key is on macOS.
func (a *App) syncWinTab() {
	want := a.settings.WinTab && !a.quitting && slices.ContainsFunc(a.displays, func(d *display) bool { return d.strip != nil })
	if want == (a.unhookWinTab != nil) {
		return
	}
	if !want {
		a.unhookWinTab()
		a.unhookWinTab = nil
		return
	}
	// The hook calls on its own thread; the press comes here.
	unhook, err := winapi.HookWinTab(func() { post(a.winTab) })
	if err != nil {
		log.Println("stagebar: taking Win+Tab:", err)
		return
	}
	a.unhookWinTab = unhook
}

// winTab spreads out the windows of every display at once, as Windows' Task
// View does, or puts them back if they are out, as a second Win+Tab closes
// Task View.
func (a *App) winTab() {
	if a.anyOverview() {
		debugf("win+tab: overviews close")
		a.closeOverviews(nil)
		return
	}
	a.openOverviews(a.displays, time.Now())
}

func (a *App) onWinEvent(event uint32, h winapi.HWND) {
	if a.quitting {
		return
	}
	switch event {
	case winapi.EventSystemMinimizeStart:
		a.onMinimizeStart(h)
		return
	case winapi.EventSystemMinimizeEnd:
		// MinimizeEnd signals a restore, not completion of a minimize.
		a.refreshSoon()
		return
	case winapi.EventSystemForeground:
		a.onForeground(h)
		return
	case winapi.EventSystemMoveSizeStart:
		a.startDrag(h)
		return
	case winapi.EventObjectCreate:
		if winapi.ClassName(h) == peekClass {
			a.peekStart(h)
		}
		return
	case winapi.EventObjectDestroy:
		if h == a.peek {
			a.peekEnd()
			return
		}
		a.noteClosed(h)
	case winapi.EventObjectHide:
		a.noteClosed(h)
	case winapi.EventSystemMoveSizeEnd:
		a.windowMoved(h)
	case winapi.EventObjectLocationChange:
		if a.drag != nil && h == a.drag.h {
			a.dragOver()
		}
		// Moves of parked windows are ours. Others only matter for whether
		// a floating strip is covered.
		if !a.park.IsParked(h) && a.settings.Mode == settings.Floating && a.coverTimer == nil && winapi.Root(h) == h {
			a.coverTimer = after(50*time.Millisecond, a.updateCovered)
		}
		return
	}
	// Destroyed windows can no longer be asked anything: let them through.
	if event != winapi.EventObjectDestroy && winapi.Root(h) != h {
		return
	}
	a.refreshSoon()
}

func (a *App) isListed(h winapi.HWND) bool {
	_, ok := a.listed(h)
	return ok
}

func (a *App) updateCovered() {
	a.coverTimer = nil
	changed := false
	for _, d := range a.displays {
		if c := a.covered(d); c != d.covered {
			d.covered, changed = c, true
		}
	}
	if changed {
		a.redraw()
	}
}

// onForeground follows the user to another window: its app takes the stage.
func (a *App) onForeground(h winapi.HWND) {
	if !a.ready {
		return
	}
	// Windows activates the next window while minimizing the old one. It
	// must not pull a different group back onto the stage mid-restore.
	if len(a.minimizing) > 0 {
		return
	}
	// The window the user left gets a fresh still, for when its app goes.
	// Not when the focus only moves to a popup or dialog of the same app:
	// the app stays, and some apps flash when asked to draw themselves, as
	// JetBrains IDEs do on every Find Usages popup. Should the app go after
	// all, the switch takes the still.
	if prev := a.fg.prev; prev != 0 && prev != h && a.isListed(prev) && !a.park.IsParked(prev) && !sameProcess(prev, h) {
		if s := a.media.still(prev); s == nil || time.Since(s.at) > 1500*time.Millisecond {
			a.media.capture([]winapi.HWND{prev}, false, time.Second, a.redraw)
		}
	}
	a.fg.prev = catalog.RootOwner(h)
	if a.settings.StageList {
		// The list of the stage's windows rings the one the user is in.
		a.redraw()
	}
	debugf("foreground %x %q at %v iconic=%v parked=%v", h, winapi.ClassName(h), winapi.WindowRect(h), winapi.IsIconic(h), a.park.IsParked(catalog.RootOwner(h)))
	if catalog.IsDesktop(h) {
		// Clicking the wallpaper sends the stage to the strip, as on macOS.
		// The button check keeps Win+D and the like out of it.
		d := a.desktopClick()
		debugf("desktop clicked, clears %v", d != nil)
		if d != nil {
			a.switchStage(d, nil, 0, false, false)
		}
		return
	}
	a.fg.h, a.fg.tries = h, 0
	if a.fg.timer != nil {
		a.fg.timer.Stop()
	}
	a.fg.timer = after(70*time.Millisecond, a.settleForeground)
}

func sameProcess(a, b winapi.HWND) bool {
	pa, _ := winapi.ProcessID(a)
	pb, _ := winapi.ProcessID(b)
	return pa != 0 && pa == pb
}

// desktopClick returns the display whose wallpaper was just clicked, if
// that should clear its stage.
func (a *App) desktopClick() *display {
	if !a.settings.ClickDesktopClears || !a.settings.HideInactive || a.busy() || !winapi.LeftButtonDown() {
		return nil
	}
	cur := winapi.Cursor()
	d := a.displayByHandle(winapi.MonitorFromPoint(cur))
	if d == nil || !a.managed(d) || d.model.Empty() {
		return nil
	}
	// The strip lets clicks through wherever no card is: those reach the
	// wallpaper and count.
	if d.strip != nil && d.strip.visible && d.strip.rect.Contains(cur) && d.strip.over(cur) {
		return nil
	}
	return d
}

func (a *App) settleForeground() {
	a.fg.timer = nil
	h := a.fg.h
	// The events come late and out of order: Alt+Tab's switcher reports
	// itself in front once more after the window it switched to. The window
	// in front now is the one to follow.
	if fg := winapi.Foreground(); h != 0 && fg != 0 && fg != h {
		debugf("foreground %x is %x now", h, fg)
		h, a.fg.h = fg, fg
	}
	if h == 0 || a.busy() {
		debugf("foreground %x waits for the %v", h, a.hold)
		return
	}
	root := catalog.RootOwner(h)
	a.windows = a.cat.Windows()
	w, ok := a.listed(root)
	if !ok {
		// New windows often get their title, or show, a moment after
		// they are activated.
		if a.fg.tries++; a.fg.tries < 6 && winapi.IsWindow(h) {
			a.fg.timer = after(70*time.Millisecond, a.settleForeground)
		} else {
			a.fg.h = 0
		}
		return
	}
	a.fg.h = 0
	d := a.displayOf(w)
	debugf("foreground settled on %q (%s) hold=%v", w.Title, func() string {
		if d == nil {
			return "no display"
		}
		return d.mon.Device
	}(), a.hold)
	if d == nil || !a.managed(d) {
		return
	}
	if d.model.OnStage(w.App) {
		// The foreground stayed on the stage: whatever closed is over.
		a.closed.d = nil
		if a.park.IsParked(w.HWND) {
			a.park.Unpark(w.HWND)
			a.refreshSoon()
		}
		return
	}
	if a.keepStage() {
		return
	}
	// The app comes with the apps of its set, as on macOS. The system
	// already brought its window up: the stage it leaves fades under it.
	// A companion, as a TortoiseSVN dialog opened from the file manager,
	// comes beside the stage instead and leaves it with its windows.
	a.switchStage(d, d.model.Coming(w.App, a.settings.Companion(w.Exe)), w.HWND, false, true)
}

// noteClosed remembers a window that closed or hid while on a stage, for
// keepStage.
func (a *App) noteClosed(h winapi.HWND) {
	w, ok := a.listed(h)
	if !ok || a.park.IsParked(h) {
		return
	}
	if d := a.stagedOn(w); d != nil {
		a.closed.h, a.closed.d, a.closed.at = h, d, time.Now()
	}
}

// keepStage keeps the apps on a stage a window just closed on, should
// any of their windows be left there. Windows hands the foreground of a
// closed window to the window activated before it, which may well be one
// parked in the strip, as the windows of the previous stage are; following
// it would send the apps still on the stage away. On macOS closing a
// window leaves the stage to the others on it: the front one of them
// gets the foreground instead.
func (a *App) keepStage() bool {
	c := a.closed
	if c.d == nil || time.Since(c.at) > closeGrace || !a.live(c.d) {
		return false
	}
	// The windows are listed front first.
	for _, w := range a.windows {
		if w.HWND == c.h || a.hidden(w) || !c.d.model.OnStage(w.App) || a.displayOf(w) != c.d {
			continue
		}
		a.closed.d = nil
		debugf("closed %x on %s: the stage keeps %q", c.h, c.d.mon.Device, w.Title)
		winapi.Activate(w.HWND)
		a.refreshSoon()
		return true
	}
	return false
}

// closeGrace is how long after a window closes the foreground is taken to
// have moved because of it: the system activates another at once, and
// settleForeground follows within a few hundred milliseconds.
const closeGrace = time.Second

// startPolling follows the pointer for the strips: revealing a floating
// strip at the screen's edge, and letting clicks through where no card is.
func (a *App) startPolling() {
	a.stopPoll = make(chan struct{})
	poll := timed(a.poll)
	go func() {
		t := time.NewTicker(16 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-a.stopPoll:
				return
			case <-t.C:
				t0 := time.Now()
				mygo.RunOnMain(poll)
				pollStats.add(time.Since(t0))
			}
		}
	}()
}

func (a *App) poll() {
	if a.quitting {
		return
	}
	cur := winapi.Cursor()
	now := time.Now()
	a.watchTaskbar(cur)
	for _, d := range a.displays {
		if d.strip != nil {
			d.strip.poll(cur, now)
		}
	}
	// After the strips: a corner button joined to a strip's panel comes in
	// as the panel does.
	a.watchCorner(cur, now)
}

// pollRate measures how long the pointer poll waits for the main thread,
// in debug runs.
type pollRate struct {
	n         int
	sum, peak time.Duration
	since     time.Time
}

var pollStats pollRate

func (ps *pollRate) add(d time.Duration) {
	if !debugOn {
		return
	}
	ps.n++
	ps.sum += d
	ps.peak = max(ps.peak, d)
	if ps.since.IsZero() {
		ps.since = time.Now()
	}
	if el := time.Since(ps.since); el > 5*time.Second {
		debugf("poll: %.1f/s, wait avg %v, peak %v", float64(ps.n)/el.Seconds(), ps.sum/time.Duration(ps.n), ps.peak)
		*ps = pollRate{since: time.Now()}
	}
}
