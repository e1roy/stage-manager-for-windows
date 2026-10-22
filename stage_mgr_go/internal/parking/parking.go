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
}

// New keeps the journal in dir.
func New(dir string) *Service {
	return &Service{parked: map[winapi.HWND]parked{}, lifted: map[winapi.HWND]bool{}, journal: filepath.Join(dir, "parked-windows.json")}
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

// Unpark brings a window and its parked dialogs home, above other windows.
func (s *Service) Unpark(h winapi.HWND) {
	root, ok := s.parked[h]
	if !ok {
		return
	}
	home := onDesktop(root.Home)
	shiftX, shiftY := home.Left-root.Home.Left, home.Top-root.Home.Top
	for w, p := range s.parked {
		if w == h || p.Owner == h {
			delete(s.parked, w)
			move(w, winapi.Point{X: p.Home.Left + shiftX, Y: p.Home.Top + shiftY}, true)
		}
	}
	// A dialog opened while its owner was parked was placed next to the
	// parked window: bring it along by the same distance.
	dx, dy := home.Left-root.Spot.X, home.Top-root.Spot.Y
	for _, o := range catalog.OwnedWindows(h) {
		if r := winapi.WindowRect(o); winapi.MostlyOffscreen(r) {
			move(o, winapi.Point{X: r.Left + dx, Y: r.Top + dy}, false)
		}
	}
	s.save()
}

// Rehome changes where a parked window goes back to, and moves the homes
// of its parked dialogs along.
func (s *Service) Rehome(h winapi.HWND, home winapi.Rect) {
	p, ok := s.parked[h]
	if !ok {
		return
	}
	home = onDesktop(home)
	dx, dy := home.Left-p.Home.Left, home.Top-p.Home.Top
	for w, q := range s.parked {
		if w == h || q.Owner == h {
			q.Home = q.Home.Offset(dx, dy)
			s.parked[w] = q
		}
	}
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
			move(w, winapi.Point{X: p.Home.Left + dx, Y: p.Home.Top + dy}, false)
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
			move(w, p.Spot, false)
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
			move(h, winapi.Point{X: home.Left, Y: home.Top}, false)
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
			p.Spot = winapi.Point{X: r.Left, Y: r.Top}
			s.parked[h] = p
			changed = true
			continue
		}
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
// still where they were parked, and returns how many it moved.
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
		r := winapi.WindowRect(h)
		if !winapi.IsWindow(h) || r.Left != e.SpotX || r.Top != e.SpotY {
			continue
		}
		home := onDesktop(winapi.Rect{Left: e.HomeX, Top: e.HomeY, Right: e.HomeX + r.W(), Bottom: e.HomeY + r.H()})
		move(h, winapi.Point{X: home.Left, Y: home.Top}, false)
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
		return false
	}
	r := winapi.WindowRect(h)
	// Parking a window that is already off the desktop would make the
	// parking spot its home, and it could never come back.
	if r.Empty() || !winapi.OnAnyMonitor(r) {
		return false
	}
	home, spot := place(r)
	if !move(h, spot, false) {
		return false
	}
	s.parked[h] = parked{Home: home, Spot: spot, Owner: owner, At: time.Now()}
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

func move(h winapi.HWND, p winapi.Point, front bool) bool {
	flags := uint32(moveFlags)
	if front {
		flags &^= winapi.SwpNoZOrder
	}
	return winapi.SetWindowPos(h, winapi.HwndTop, p.X, p.Y, 0, 0, flags)
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
