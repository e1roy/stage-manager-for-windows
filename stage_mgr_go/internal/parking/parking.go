//go:build windows

// Package parking hides windows by moving them just outside the desktop.
// Unlike minimizing, a parked window keeps its size and state and returns
// at once, without the app laying it out again. Every move is written to a
// journal, so that windows come back after StageBar ends unexpectedly.
package parking

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/renkai9418/stage-bar/internal/catalog"
	"github.com/renkai9418/stage-bar/internal/winapi"
)

const (
	gap = 160
	// No redraw: Windows does not paint windows off the desktop, so one
	// coming back would erase and repaint everything, and apps that paint
	// asynchronously would show a blank window for a moment. Without it
	// the window keeps showing its intact picture.
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
}

// New keeps the journal in dir.
func New(dir string) *Service {
	return &Service{parked: map[winapi.HWND]parked{}, journal: filepath.Join(dir, "parked-windows.json")}
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
	if s.IsParked(h) || !s.parkOne(h, 0) {
		return false
	}
	for _, o := range catalog.OwnedWindows(h) {
		if !s.IsParked(o) {
			s.parkOne(o, h)
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
	// A home on a display that has gone since moves to the primary one.
	home := root.Home
	if !winapi.OnAnyMonitor(home) {
		home = centerOnPrimary(home)
	}
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
		if r := winapi.WindowRect(o); !winapi.OnAnyMonitor(r) {
			move(o, winapi.Point{X: r.Left + dx, Y: r.Top + dy}, false)
		}
	}
	s.save()
}

// RestoreAll brings every parked window home. It is safe while quitting.
func (s *Service) RestoreAll() {
	for h, p := range s.parked {
		if winapi.IsWindow(h) {
			home := p.Home
			if !winapi.OnAnyMonitor(home) {
				home = centerOnPrimary(home)
			}
			move(h, winapi.Point{X: home.Left, Y: home.Top}, false)
			winapi.Redraw(h)
		}
	}
	s.parked = map[winapi.HWND]parked{}
	s.save()
}

// CollectEscaped forgets windows that closed, and returns the parked
// windows something else moved back onto the desktop, as Windows does when
// it maximizes windows again after the work area changes.
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
		home := winapi.Rect{Left: e.HomeX, Top: e.HomeY, Right: e.HomeX + r.W(), Bottom: e.HomeY + r.H()}
		if !winapi.OnAnyMonitor(home) {
			home = centerOnPrimary(r)
		}
		move(h, winapi.Point{X: home.Left, Y: home.Top}, false)
		winapi.Redraw(h)
		n++
	}
	return n
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

func (s *Service) parkOne(h, owner winapi.HWND) bool {
	if !winapi.IsWindow(h) || winapi.IsIconic(h) {
		return false
	}
	home := winapi.WindowRect(h)
	// Parking a window that is already off the desktop would make the
	// parking spot its home, and it could never come back.
	if home.Empty() || !winapi.OnAnyMonitor(home) {
		return false
	}
	spot := spotFor(home)
	if !move(h, spot, false) {
		return false
	}
	s.parked[h] = parked{Home: home, Spot: spot, Owner: owner, At: time.Now()}
	return true
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
