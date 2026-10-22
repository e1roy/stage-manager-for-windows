// Package app is StageBar: the stage of each display, the strip beside it,
// and the hand-over of apps between the two. Everything here runs on
// MyGo's main thread; captures and timers come back to it through post.
package app

import (
	"log"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/renkai9418/stage-bar/internal/catalog"
	"github.com/renkai9418/stage-bar/internal/parking"
	"github.com/renkai9418/stage-bar/internal/settings"
	"github.com/renkai9418/stage-bar/internal/stage"
	"github.com/renkai9418/stage-bar/internal/winapi"
)

// post runs fn on the main thread without waiting for it.
func post(fn func()) { go mygo.RunOnMain(fn) }

// after runs fn on the main thread once d has passed.
func after(d time.Duration, fn func()) *time.Timer {
	return time.AfterFunc(d, func() { mygo.RunOnMain(fn) })
}

// group is the windows one app has on one display, front-most first.
type group struct {
	app     string
	name    string
	windows []catalog.Window
}

func (g *group) front() catalog.Window { return g.windows[0] }

// display is one monitor, its stage and its strip.
type display struct {
	mon     winapi.Monitor
	model   stage.Display
	strip   *strip
	groups  []*group // the strip's cards, top first
	onStage []*group
	covered bool // a window on the stage covers the cards (floating mode)

	cards   map[string]*cardState // per app, what a card remembers between frames
	scroll  float32
	layout  stripLayout // as the view last laid the cards out
	viewW   float32
	viewH   float32
	// landing lists the cards whose windows are still flying in, and
	// whether their icons show yet.
	landing map[string]bool
	// dropPoses is where the windows of a card dropped on the stage were
	// let go, for their flight home.
	dropPoses map[winapi.HWND]pose
}

// App is StageBar.
type App struct {
	settings settings.Settings
	store    settings.Store
	cat      *catalog.Catalog
	park     *parking.Service
	media    *media

	displays []*display // primary first
	windows  []catalog.Window
	seen     map[winapi.HWND]time.Time // when a window was first listed

	ready    bool // the first stage is set
	busy     bool // a switch is running
	quitting bool

	refreshTimer *time.Timer
	coverTimer   *time.Timer
	fg           struct {
		h     winapi.HWND
		tries int
		timer *time.Timer
		prev  winapi.HWND
	}
	unhook   func()
	stopPoll chan struct{}

	// act, when set, receives what the cards ask for instead of App.
	act func(action, *display, *group)

	tray       *mygo.Tray
	trayIcon   []byte
	settingsUI *settingsWindow
	flights    *flights
}

// Options configure Start.
type Options struct {
	DataDir  string
	TrayIcon []byte // PNG
}

// Start sets StageBar up. Call it once the app is ready, on the main thread.
func Start(o Options) *App {
	a := &App{
		store:    settings.NewStore(o.DataDir),
		cat:      catalog.New(),
		park:     parking.New(o.DataDir),
		media:    newMedia(),
		seen:     map[winapi.HWND]time.Time{},
		trayIcon: o.TrayIcon,
	}
	a.settings = a.store.Load()
	// Windows a crashed run left off the desktop come back first.
	a.park.Recover()
	watchMainThread(o.DataDir)
	a.setUpTray()
	a.flights = newFlights(a)
	a.syncDisplays()
	a.hookEvents()
	a.startPolling()
	mygo.Screen.OnDisplaysChanged(func() { after(300*time.Millisecond, a.displaysChanged) })
	// Quitting closes every window, which the strips only allow once
	// shutdown has begun; it puts every window back first.
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.shutdown() })
	mygo.App.OnQuit(a.shutdown)
	// Let app bars take their room, and Windows re-maximize windows into
	// what is left, before the first stage is measured.
	after(400*time.Millisecond, a.initStage)
	ticker := time.NewTicker(1500 * time.Millisecond)
	go func() {
		for range ticker.C {
			mygo.RunOnMain(a.refresh)
		}
	}()
	return a
}

// managed reports whether StageBar arranges the windows of a display.
func (a *App) managed(d *display) bool {
	return !a.settings.Paused && a.settings.DisplayEnabled(d.mon.Device)
}

func (a *App) displayByDevice(dev string) *display {
	for _, d := range a.displays {
		if d.mon.Device == dev {
			return d
		}
	}
	return nil
}

func (a *App) displayByHandle(h winapi.HMONITOR) *display {
	for _, d := range a.displays {
		if d.mon.Handle == h {
			return d
		}
	}
	return nil
}

// home returns where a window shows when it is on the stage.
func (a *App) home(w catalog.Window) winapi.Rect {
	if r, ok := a.park.Home(w.HWND); ok {
		x, y := r.Left+w.Frame.Left-w.Rect.Left, r.Top+w.Frame.Top-w.Rect.Top
		return winapi.Rect{Left: x, Top: y, Right: x + w.Frame.W(), Bottom: y + w.Frame.H()}
	}
	return w.Frame
}

// displayOf returns the display a window belongs to.
func (a *App) displayOf(w catalog.Window) *display {
	return a.displayByHandle(winapi.MonitorFromRect(a.home(w)))
}

// hidden reports whether a window is off the desktop: parked. Minimized
// windows are not listed at all, so they never reach here.
func (a *App) hidden(w catalog.Window) bool { return a.park.IsParked(w.HWND) }

// groupsOf groups the windows of a display by app, in z-order.
func (a *App) groupsOf(d *display) []*group {
	var out []*group
	by := map[string]*group{}
	for _, w := range a.windows {
		if a.displayOf(w) != d {
			continue
		}
		g := by[w.App]
		if g == nil {
			g = &group{app: w.App, name: a.cat.Name(w)}
			by[w.App] = g
			out = append(out, g)
		}
		g.windows = append(g.windows, w)
	}
	return out
}

func showing(a *App, g *group) bool {
	return slices.ContainsFunc(g.windows, func(w catalog.Window) bool { return !a.hidden(w) })
}

// syncDisplays matches the displays and their strips to the monitors.
func (a *App) syncDisplays() {
	mons := winapi.Monitors()
	var next []*display
	for _, m := range mons {
		d := a.displayByDevice(m.Device)
		if d == nil {
			d = &display{cards: map[string]*cardState{}, landing: map[string]bool{}}
		}
		d.mon = m
		next = append(next, d)
	}
	for _, d := range a.displays {
		if !slices.Contains(next, d) && d.strip != nil {
			// The display is gone: its windows come back, onto the
			// displays that remain.
			a.release(d)
			d.strip.close()
			d.strip = nil
		}
	}
	a.displays = next
	for _, d := range a.displays {
		if a.managed(d) {
			if d.strip == nil {
				if a.ready {
					// A display StageBar starts arranging while it runs keeps
					// the app in front there on its stage.
					d.model.Init(a.frontApp(d), time.Now())
				}
				d.strip = newStrip(a, d)
				// The flight layer is made now rather than in the middle
				// of a switch.
				a.flights.layer(d)
			}
			d.strip.place()
		} else if d.strip != nil {
			a.release(d)
			d.strip.close()
			d.strip = nil
		}
	}
	// App bars change the work areas.
	for _, d := range a.displays {
		d.mon = winapi.MonitorInfo(d.mon.Handle)
	}
}

// frontApp returns the front-most app showing on a display, or "".
func (a *App) frontApp(d *display) string {
	a.windows = a.cat.Windows()
	for _, g := range a.groupsOf(d) {
		if showing(a, g) {
			return g.app
		}
	}
	return ""
}

// release brings back the parked windows of a display StageBar stops
// arranging, and forgets its stage.
func (a *App) release(d *display) {
	for _, w := range a.windows {
		if a.park.IsParked(w.HWND) && a.displayOf(w) == d {
			a.park.Unpark(w.HWND)
		}
	}
	d.model.Init("", time.Now())
	d.groups, d.onStage = nil, nil
}

func (a *App) displaysChanged() {
	if a.quitting {
		return
	}
	a.syncDisplays()
	a.refresh()
}

// initStage puts on each display's stage the app the user is in, or the
// front-most app showing there, and parks the others.
func (a *App) initStage() {
	a.windows = a.cat.Windows()
	now := time.Now()
	fg := catalog.RootOwner(winapi.Foreground())
	var leaving []winapi.HWND
	for _, d := range a.displays {
		if !a.managed(d) {
			continue
		}
		groups := a.groupsOf(d)
		pick := ""
		for _, g := range groups {
			if showing(a, g) && slices.ContainsFunc(g.windows, func(w catalog.Window) bool { return w.HWND == fg }) {
				pick = g.app
			}
		}
		if pick == "" {
			for _, g := range groups {
				if showing(a, g) {
					pick = g.app
					break
				}
			}
		}
		d.model.Init(pick, now)
		if a.settings.HideInactive {
			for _, g := range groups {
				if g.app == pick {
					continue
				}
				for _, w := range g.windows {
					if !a.hidden(w) {
						leaving = append(leaving, w.HWND)
					}
				}
			}
		}
	}
	// Every window gets a still before it leaves, and the windows that
	// stay one too, for when they go.
	var rest []winapi.HWND
	for _, w := range a.windows {
		if !slices.Contains(leaving, w.HWND) {
			rest = append(rest, w.HWND)
		}
	}
	a.media.capture(leaving, false, 1500*time.Millisecond, func() {
		for _, h := range leaving {
			a.park.Park(h)
		}
		a.ready = true
		a.refresh()
		a.media.capture(rest, false, 5*time.Second, a.redraw)
	})
}

// refresh lists the windows again and brings strips and stages up to date.
func (a *App) refresh() {
	if a.refreshTimer != nil {
		a.refreshTimer.Stop()
		a.refreshTimer = nil
	}
	if !a.ready || a.busy || a.quitting {
		return
	}
	a.reparkEscaped()
	a.windows = a.cat.Windows()
	now := time.Now()
	alive := map[winapi.HWND]bool{}
	for _, w := range a.windows {
		alive[w.HWND] = true
		if _, ok := a.seen[w.HWND]; !ok {
			a.seen[w.HWND] = now
		}
	}
	for h := range a.seen {
		if !alive[h] {
			delete(a.seen, h)
		}
	}
	a.media.forget(alive)
	var want []winapi.HWND
	for _, d := range a.displays {
		if !a.managed(d) {
			d.groups, d.onStage = nil, nil
			continue
		}
		groups := a.groupsOf(d)
		visible := map[string]bool{}
		for _, g := range groups {
			if showing(a, g) {
				visible[g.app] = true
			}
		}
		d.model.Prune(func(app string) bool { return visible[app] }, now)
		a.parkStrays(d, groups, now)
		a.arrange(d, groups, now)
		for _, g := range d.groups {
			for _, w := range g.windows[:min(len(g.windows), maxStacked)] {
				// Windows on the desktop change; refresh their stills now and then.
				s := a.media.still(w.HWND)
				stale := s == nil || (!a.hidden(w) && time.Since(s.at) > 5*time.Second)
				if stale && !a.media.pending[w.HWND] && time.Since(a.media.tried[w.HWND]) > 3*time.Second {
					want = append(want, w.HWND)
				}
			}
			a.media.icon(g.front())
		}
		d.covered = a.covered(d)
		debugf("display %s stage=%v strip=%d covered=%v", d.mon.Device, d.model.Stage(), len(d.groups), d.covered)
	}
	if len(want) > 0 {
		a.media.capture(want, false, 3*time.Second, a.redraw)
	}
	a.redraw()
}

// refreshSoon coalesces the refreshes a burst of events asks for.
func (a *App) refreshSoon() {
	if a.refreshTimer == nil {
		a.refreshTimer = after(60*time.Millisecond, a.refresh)
	}
}

// parkStrays parks the windows of apps that are not on the stage but show
// on the desktop, as windows opened in the background do, once they have
// had a moment to take the foreground instead.
func (a *App) parkStrays(d *display, groups []*group, now time.Time) {
	if !a.settings.HideInactive || d.model.Settling(now) {
		return
	}
	var strays []winapi.HWND
	for _, g := range groups {
		if d.model.OnStage(g.app) {
			continue
		}
		for _, w := range g.windows {
			if !a.hidden(w) && !w.Foreground && now.Sub(a.seen[w.HWND]) > 1500*time.Millisecond {
				strays = append(strays, w.HWND)
			}
		}
	}
	if len(strays) == 0 {
		return
	}
	a.media.capture(strays, false, time.Second, func() {
		for _, h := range strays {
			if fg := catalog.RootOwner(winapi.Foreground()); fg != h {
				a.park.Park(h)
			}
		}
		a.refreshSoon()
	})
}

// reparkEscaped parks again the windows that came back onto the desktop
// while their app was off the stage.
func (a *App) reparkEscaped() {
	fg := winapi.Foreground()
	for _, h := range a.park.CollectEscaped() {
		i := slices.IndexFunc(a.windows, func(w catalog.Window) bool { return w.HWND == h })
		if i < 0 || h == fg || !a.settings.HideInactive {
			continue
		}
		w := a.windows[i]
		if d := a.displayOf(w); d != nil && a.managed(d) && !d.model.OnStage(w.App) {
			a.park.Park(h)
		}
	}
}

// covered reports whether a window on the stage overlaps a floating strip's cards.
func (a *App) covered(d *display) bool {
	if d.strip == nil || a.settings.Mode != settings.Floating {
		return false
	}
	band := d.strip.cardBand()
	if band.Empty() {
		return false
	}
	for _, g := range d.onStage {
		for _, w := range g.windows {
			if !a.hidden(w) && winapi.IsWindow(w.HWND) && !winapi.IsIconic(w.HWND) && winapi.FrameBounds(w.HWND).Intersects(band) {
				return true
			}
		}
	}
	return false
}

// redraw asks every strip to draw again.
func (a *App) redraw() {
	for _, d := range a.displays {
		if d.strip != nil {
			d.strip.win.Invalidate()
		}
	}
}

// applySettings takes new settings from the settings window.
func (a *App) applySettings(s settings.Settings) {
	s = s.Normalize()
	if s.Equal(a.settings) {
		return
	}
	old := a.settings
	a.settings = s.Clone()
	if err := a.store.Save(a.settings); err != nil {
		log.Println("stagebar: saving settings:", err)
	}
	if old.Paused != s.Paused || old.HideInactive != s.HideInactive {
		if s.Paused || !s.HideInactive {
			a.park.RestoreAll()
		}
	}
	if old.Mode != s.Mode || old.Width != s.Width {
		for _, d := range a.displays {
			if d.strip != nil {
				d.strip.relayout()
			}
		}
	}
	a.syncDisplays()
	// A display that just became managed starts with what shows there.
	if (!old.Paused && s.Paused) || (old.Paused && !s.Paused) || (!old.HideInactive && s.HideInactive) {
		a.ready = false
		after(300*time.Millisecond, a.initStage)
	}
	a.refresh()
	a.updateTrayMenu()
}

// shutdown puts every window back and gives the work area back. It runs
// as the app quits and as the session ends, and only once; MyGo closes
// the strips' windows itself.
func (a *App) shutdown() {
	if a.quitting {
		return
	}
	a.quitting = true
	if a.unhook != nil {
		a.unhook()
	}
	if a.stopPoll != nil {
		close(a.stopPoll)
	}
	a.park.RestoreAll()
	for _, d := range a.displays {
		if d.strip != nil {
			d.strip.bar.Remove()
		}
	}
}
