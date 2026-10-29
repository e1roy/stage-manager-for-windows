//go:build windows

// Package parking hides windows by moving them just outside the desktop.
// Unlike minimizing, a parked window keeps its size and state and returns
// at once, without the app laying it out again. Every move is written to a
// journal, so that windows come back after Stage Manager ends unexpectedly.
package parking

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

const (
	gap = 160
	// No redraw: Windows does not paint windows off the desktop, so one
	// coming back would erase and repaint everything, and apps that paint
	// asynchronously would flash blank. Without it the window keeps the
	// picture it already had.
	moveFlags = winapi.SwpNoRedraw | winapi.SwpNoSize | winapi.SwpNoZOrder | winapi.SwpNoActivate |
		winapi.SwpNoOwnerZOrder | winapi.SwpAsyncWindowPos
	settle = time.Second
	// A window sent home that is not there after chaseAfter is sent again,
	// up to chaseTries times, and then fitted onto the nearest display.
	chaseAfter = 300 * time.Millisecond
	chaseTries = 3
)

type parked struct {
	Home  winapi.Rect  // the window rectangle it had on the desktop
	Spot  winapi.Point // where it waits
	Owner winapi.HWND  // the window it went with, for a dialog
	At    time.Time
}

// Service parks and unparks windows. Use it from one goroutine.
type Service struct {
	parked  map[winapi.HWND]parked
	journal string
	lifted  map[winapi.HWND]bool // parked windows home for a moment, see Lift
	homing  map[winapi.HWND]homing
	// Logf, when set, is told where windows go, for debug runs.
	Logf func(format string, args ...any)
}

func (s *Service) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// homing is a window sent home, until it gets there.
type homing struct {
	to    winapi.Point
	at    time.Time // when it was last sent
	tries int
}

// New keeps the journal in dir.
func New(dir string) *Service {
	return &Service{parked: map[winapi.HWND]parked{}, lifted: map[winapi.HWND]bool{}, homing: map[winapi.HWND]homing{}, journal: filepath.Join(dir, "parked-windows.json")}
}

// IsParked reports whether a window is parked.
func (s *Service) IsParked(h winapi.HWND) bool { _, ok := s.parked[h]; return ok }

// Home returns where a parked window goes back to.
func (s *Service) Home(h winapi.HWND) (winapi.Rect, bool) {
	p, ok := s.parked[h]
	return p.Home, ok
}

// Count is how many windows are parked.
func (s *Service) Count() int { return len(s.parked) }

// Park moves a window, and the dialogs it owns, off the desktop.
func (s *Service) Park(h winapi.HWND) bool {
	if s.IsParked(h) {
		return false
	}
	from := winapi.WindowRect(h)
	if !s.parkOne(h, 0, alone) {
		return false
	}
	// The windows it owns, the shadow some apps draw it in among them, wait
	// and come back where they are from it.
	root := s.parked[h]
	dx, dy := root.Spot.X-from.Left, root.Spot.Y-from.Top
	shiftX, shiftY := root.Home.Left-from.Left, root.Home.Top-from.Top
	along := func(r winapi.Rect) (winapi.Rect, winapi.Point) {
		if at := r.Offset(dx, dy); !winapi.OnAnyMonitor(at) {
			return r.Offset(shiftX, shiftY), winapi.Point{X: at.Left, Y: at.Top}
		}
		// Far enough from its owner to show there still, it waits apart.
		return alone(r)
	}
	for _, o := range catalog.OwnedWindows(h) {
		if !s.IsParked(o) {
			s.parkOne(o, h, along)
		}
	}
	s.save()
	return true
}

// Unpark brings windows and their parked dialogs home, above other
// windows, the first in front.
func (s *Service) Unpark(hs ...winapi.HWND) {
	var home []winapi.HWND
	for _, h := range hs {
		if s.unpark(h) {
			home = append(home, h)
		}
	}
	if len(home) > 0 {
		s.save()
		raise(home)
	}
}

func (s *Service) unpark(h winapi.HWND) bool {
	root, ok := s.parked[h]
	if !ok {
		return false
	}
	home := onDesktop(root.Home)
	shiftX, shiftY := home.Left-root.Home.Left, home.Top-root.Home.Top
	for w, p := range s.parked {
		if w == h || p.Owner == h {
			delete(s.parked, w)
			s.sendHome(w, reachable(p.Home.Offset(shiftX, shiftY)))
		}
	}
	// A dialog opened while its owner was parked was placed next to the
	// parked window: bring it along by the same distance.
	dx, dy := home.Left-root.Spot.X, home.Top-root.Spot.Y
	for _, o := range catalog.OwnedWindows(h) {
		if r := winapi.WindowRect(o); winapi.MostlyOffscreen(r) {
			s.sendHome(o, reachable(r.Offset(dx, dy)))
		}
	}
	return true
}

// reachable is where a window that comes along with another goes: where
// it was, unless no display shows any of it there. It is not fitted whole,
// as the shadow some apps draw a window in may reach past the display.
func reachable(r winapi.Rect) winapi.Point {
	if !winapi.OnAnyMonitor(r) {
		r = winapi.FitOnScreen(r)
	}
	return winapi.Point{X: r.Left, Y: r.Top}
}

// sendHome moves a window back onto the desktop and follows it until it
// gets there (Chase).
func (s *Service) sendHome(h winapi.HWND, p winapi.Point) {
	s.logf("home %x %q from %v to %v", h, winapi.Title(h), at(h), p)
	place(h, p)
	s.homing[h] = homing{to: p, at: time.Now()}
}

// Chase sends once more the windows sent home that are not there yet, as
// an app busy at the time, or one that puts its window back where it
// was, leaves them. One that will not go is fitted onto the nearest
// display instead: a window off the desktop that is not parked is one
// nobody can find. Call it now and then.
func (s *Service) Chase() {
	now := time.Now()
	for h, g := range s.homing {
		switch {
		case !winapi.IsWindow(h) || s.IsParked(h) || arrived(h, g.to):
			delete(s.homing, h)
		case now.Sub(g.at) < chaseAfter:
		case g.tries < chaseTries:
			s.logf("chase %x %q at %v, not home at %v yet", h, winapi.Title(h), at(h), g.to)
			place(h, g.to)
			g.at, g.tries = now, g.tries+1
			s.homing[h] = g
		default:
			s.logf("chase %x %q gave up on %v at %v: fitted on screen", h, winapi.Title(h), g.to, at(h))
			fit(h)
			delete(s.homing, h)
		}
	}
}

// Settle waits up to d for the windows sent home to get there, sending
// them once more halfway, and fits those that do not onto the nearest
// display: for when nothing will chase them later, as Stage Manager quits.
func (s *Service) Settle(d time.Duration) {
	start := time.Now()
	resent := false
	for {
		for h, g := range s.homing {
			if !winapi.IsWindow(h) || s.IsParked(h) || arrived(h, g.to) {
				delete(s.homing, h)
			}
		}
		if len(s.homing) == 0 {
			return
		}
		el := time.Since(start)
		if el >= d {
			break
		}
		if !resent && el >= d/2 {
			resent = true
			for h, g := range s.homing {
				place(h, g.to)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	for h := range s.homing {
		fit(h)
	}
	clear(s.homing)
}

// at is where a window shows, or for a minimized one where it comes back.
func at(h winapi.HWND) winapi.Rect {
	if winapi.IsIconic(h) {
		if r, _, ok := winapi.RestoredRect(h); ok {
			return r
		}
	}
	return winapi.WindowRect(h)
}

// arrived reports whether a window sent to p got there, or at least is
// where the displays show it.
func arrived(h winapi.HWND, p winapi.Point) bool {
	r := at(h)
	return r.Left == p.X && r.Top == p.Y || !winapi.MostlyOffscreen(r)
}

// fit moves a window as little as it takes onto the nearest display.
func fit(h winapi.HWND) {
	r := winapi.FitOnScreen(at(h))
	place(h, winapi.Point{X: r.Left, Y: r.Top})
}

// place moves a window to p, or a minimized one to come back there.
func place(h winapi.HWND, p winapi.Point) {
	if winapi.IsIconic(h) {
		winapi.MoveRestored(h, p)
		return
	}
	move(h, p)
}

// raising counts the calls to raise, for one held up by an app to give way
// to those after it.
var raising atomic.Uint64

// raise puts windows above the others, the first in front, their dialogs
// coming along. Moves sent off without waiting are taken by each app when
// it gets to them, so windows of different apps raised that way end up in
// whatever order the apps happen to answer in, and swap places a moment
// after they land. These wait for each app in turn instead, on a goroutine
// of their own so that a slow app cannot hold the main thread; an app
// that hangs gets its move sent off, not to hold up the windows after it.
func raise(hs []winapi.HWND) {
	const flags = winapi.SwpNoMove | winapi.SwpNoSize | winapi.SwpNoActivate | winapi.SwpNoRedraw
	gen := raising.Add(1)
	go func() {
		for _, h := range slices.Backward(hs) {
			if raising.Load() != gen {
				return
			}
			if winapi.IsHung(h) || !winapi.SetWindowPos(h, winapi.HwndTop, 0, 0, 0, 0, flags) {
				winapi.SetWindowPos(h, winapi.HwndTop, 0, 0, 0, 0, flags|winapi.SwpAsyncWindowPos)
			}
		}
	}()
}

// Rehome changes where a parked window goes back to, and moves the homes
// of its parked dialogs along. A home on another display moves the window
// to wait by that one instead, so that it takes that display's DPI while
// out of sight rather than rescale as it lands.
func (s *Service) Rehome(h winapi.HWND, home winapi.Rect) {
	p, ok := s.parked[h]
	if !ok {
		return
	}
	home = onDesktop(home)
	dx, dy := home.Left-p.Home.Left, home.Top-p.Home.Top
	var sx, sy int32
	if winapi.MonitorFromRect(home) != winapi.MonitorFromRect(p.Home) {
		spot := spotFor(home)
		sx, sy = spot.X-p.Spot.X, spot.Y-p.Spot.Y
	}
	for w, q := range s.parked {
		if w == h || q.Owner == h {
			q.Home = q.Home.Offset(dx, dy)
			if sx != 0 || sy != 0 {
				q.Spot = winapi.Point{X: q.Spot.X + sx, Y: q.Spot.Y + sy}
				q.At = time.Now()
				// One home for a peek goes to its new spot as it is
				// lowered.
				if !s.lifted[w] {
					move(w, q.Spot)
				}
			}
			s.parked[w] = q
		}
	}
	s.logf("rehome %x %q to %v", h, winapi.Title(h), home)
	s.save()
}

// Lift puts a parked window and its parked dialogs home for a moment,
// where they stay parked and keep their place in the z-order, until Lower.
// Windows' peek at a window from its taskbar thumbnail shows it where it
// is, so one off the desktop would show nothing.
func (s *Service) Lift(h winapi.HWND) {
	root, ok := s.parked[h]
	if !ok {
		return
	}
	home := onDesktop(root.Home)
	dx, dy := home.Left-root.Home.Left, home.Top-root.Home.Top
	for w, p := range s.parked {
		if (w == h || p.Owner == h) && !s.lifted[w] {
			move(w, winapi.Point{X: p.Home.Left + dx, Y: p.Home.Top + dy})
			p.At = time.Now()
			s.parked[w] = p
			s.lifted[w] = true
		}
	}
}

// Lower takes the windows Lift put home back to where they wait.
func (s *Service) Lower() {
	for w := range s.lifted {
		if p, ok := s.parked[w]; ok {
			move(w, p.Spot)
			p.At = time.Now()
			s.parked[w] = p
		}
	}
	clear(s.lifted)
}

// RestoreAll brings every parked window home. It is safe while quitting.
func (s *Service) RestoreAll() {
	for h, p := range s.parked {
		if winapi.IsWindow(h) {
			home := onDesktop(p.Home)
			s.sendHome(h, winapi.Point{X: home.Left, Y: home.Top})
			winapi.Redraw(h)
		}
	}
	s.parked = map[winapi.HWND]parked{}
	clear(s.lifted)
	s.save()
}

// CollectEscaped forgets windows that closed and returns the parked
// windows something else moved back onto the desktop, as Windows does when
// it re-maximizes windows after the work area changes.
func (s *Service) CollectEscaped() []winapi.HWND {
	var escaped []winapi.HWND
	changed := false
	for h, p := range s.parked {
		if !winapi.IsWindow(h) {
			delete(s.parked, h)
			changed = true
			continue
		}
		if time.Since(p.At) < settle {
			continue
		}
		r := winapi.WindowRect(h)
		if (r.Left == p.Spot.X && r.Top == p.Spot.Y) || winapi.IsIconic(h) {
			continue
		}
		if winapi.MostlyOffscreen(r) {
			// Some apps, QQ Music among them, will not leave the desktop and
			// stop at its edge. The window is as parked as it gets: where it
			// stopped is its spot, and its home stays.
			s.logf("parked %x %q moved itself from %v to %v", h, winapi.Title(h), p.Spot, r)
			p.Spot = winapi.Point{X: r.Left, Y: r.Top}
			s.parked[h] = p
			changed = true
			continue
		}
		s.logf("parked %x %q escaped to %v", h, winapi.Title(h), r)
		delete(s.parked, h)
		changed = true
		if p.Owner == 0 && winapi.OnAnyMonitor(r) {
			escaped = append(escaped, h)
		}
	}
	if changed {
		s.save()
	}
	return escaped
}

type entry struct {
	Handle       uint64
	HomeX, HomeY int32
	SpotX, SpotY int32
}

// Recover brings back the windows a previous run left parked.
func (s *Service) Recover() {
	restoreJournal(s.journal)
	os.Remove(s.journal)
}

// Rescue brings back every window the journal in dir says is parked, from
// any goroutine and without the Service: the last resort when the thread
// that owns the Service stops answering. It leaves the journal in place.
func Rescue(dir string) int {
	return restoreJournal(filepath.Join(dir, "parked-windows.json"))
}

// restoreJournal moves the windows a journal lists home, those that are
// still where they were parked or, moved by their app since, still off the
// desktop, and returns how many it moved. Skipping one of those would leave
// it where nobody can reach it: the journal is all that knows its home.
func restoreJournal(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var entries []entry
	if json.Unmarshal(data, &entries) != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		h := winapi.HWND(e.Handle)
		r := at(h)
		if !winapi.IsWindow(h) || (r.Left != e.SpotX || r.Top != e.SpotY) && !winapi.MostlyOffscreen(r) {
			continue
		}
		home := onDesktop(winapi.Rect{Left: e.HomeX, Top: e.HomeY, Right: e.HomeX + r.W(), Bottom: e.HomeY + r.H()})
		place(h, winapi.Point{X: home.Left, Y: home.Top})
		winapi.Redraw(h)
		n++
	}
	return n
}

// onDesktop returns where a window can go back to: its home, or, for a
// home the displays hardly show, the nearest place they show it whole. A
// home on a display that has gone since moves to the primary one.
func onDesktop(home winapi.Rect) winapi.Rect {
	switch {
	case !winapi.OnAnyMonitor(home):
		return centerOnPrimary(home)
	case winapi.MostlyOffscreen(home):
		return winapi.FitOnScreen(home)
	}
	return home
}

func centerOnPrimary(r winapi.Rect) winapi.Rect {
	mons := winapi.Monitors()
	if len(mons) == 0 {
		return r
	}
	w := mons[0].Work
	x := w.Left + max(0, (w.W()-r.W())/2)
	y := w.Top + max(0, (w.H()-r.H())/2)
	return winapi.Rect{Left: x, Top: y, Right: x + r.W(), Bottom: y + r.H()}
}

// parkOne moves a window to the spot place picks for it from where it is,
// to come back to the home place picks.
func (s *Service) parkOne(h, owner winapi.HWND, place func(r winapi.Rect) (home winapi.Rect, spot winapi.Point)) bool {
	if !winapi.IsWindow(h) || winapi.IsIconic(h) {
		s.logf("park %x %q refused: minimized or gone", h, winapi.Title(h))
		return false
	}
	r := winapi.WindowRect(h)
	// Parking a window that is already off the desktop would make the
	// parking spot its home, and it could never come back.
	if r.Empty() || !winapi.OnAnyMonitor(r) {
		s.logf("park %x %q refused: off the desktop at %v", h, winapi.Title(h), r)
		return false
	}
	home, spot := place(r)
	if !move(h, spot) {
		return false
	}
	s.parked[h] = parked{Home: home, Spot: spot, Owner: owner, At: time.Now()}
	s.logf("park %x %q from %v home %v spot %v owner %x", h, winapi.Title(h), r, home, spot, owner)
	return true
}

// alone places a window parked on its own. One the displays hardly show,
// as one that stopped at the edge the last time it was parked, comes back
// where they show it whole.
func alone(r winapi.Rect) (winapi.Rect, winapi.Point) {
	home := onDesktop(r)
	return home, spotFor(home)
}

// spotFor picks a place off the desktop whose nearest display is still
// the window's own, so that apps aware of per-monitor DPI never rescale.
func spotFor(home winapi.Rect) winapi.Point {
	desk := winapi.VirtualScreen()
	mon := winapi.MonitorFromRect(home)
	candidates := []winapi.Point{
		{X: desk.Right + gap, Y: home.Top},
		{X: desk.Left - home.W() - gap, Y: home.Top},
		{X: home.Left, Y: desk.Bottom + gap},
		{X: home.Left, Y: desk.Top - home.H() - gap},
	}
	for _, p := range candidates {
		if winapi.MonitorFromRect(winapi.Rect{Left: p.X, Top: p.Y, Right: p.X + home.W(), Bottom: p.Y + home.H()}) == mon {
			return p
		}
	}
	return candidates[0]
}

// move takes a window to p, keeping its place in the z-order.
func move(h winapi.HWND, p winapi.Point) bool {
	return winapi.SetWindowPos(h, 0, p.X, p.Y, 0, 0, moveFlags)
}

func (s *Service) save() {
	if len(s.parked) == 0 {
		os.Remove(s.journal)
		return
	}
	entries := make([]entry, 0, len(s.parked))
	for h, p := range s.parked {
		entries = append(entries, entry{uint64(h), p.Home.Left, p.Home.Top, p.Spot.X, p.Spot.Y})
	}
	data, _ := json.Marshal(entries)
	// Parking still works without the journal; only recovery is lost.
	if os.MkdirAll(filepath.Dir(s.journal), 0o755) == nil {
		os.WriteFile(s.journal, data, 0o644)
	}
}
