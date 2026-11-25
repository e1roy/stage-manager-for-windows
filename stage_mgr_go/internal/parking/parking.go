//go:build windows

// Package parking hides windows by moving them just outside the desktop.
// Unlike minimizing, a parked window keeps its size and state and returns
// at once, without the app laying it out again. Every move is written to a
// journal, so that windows come back after Stage Manager ends unexpectedly.
package parking

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

// journalName is the file in the data directory that lists the parked
// windows and their homes, for Recover and Rescue.
const journalName = "parked-windows.json"

const (
	// gap keeps a parked window this far past the desktop's edge, so that
	// the shadow DWM draws around it does not show at the display's edge.
	gap = 160
	// No redraw: Windows does not paint windows off the desktop, so one
	// coming back would erase and repaint everything, and apps that paint
	// asynchronously would flash blank. Without it the window keeps the
	// picture it already had.
	moveFlags = winapi.SwpNoRedraw | winapi.SwpNoSize | winapi.SwpNoZOrder | winapi.SwpNoActivate |
		winapi.SwpNoOwnerZOrder | winapi.SwpAsyncWindowPos
	// A window coming home also works out its frame again. One that did so
	// while it waited, as Edge and other Chromium apps do now and then, found
	// no display there, and Chromium then makes the whole window its client
	// area: back on the desktop DWM sees a window without a frame and gives it
	// neither rounded corners nor a shadow, until it is resized. Asked here,
	// the app works it out for where the window is going.
	homeFlags = moveFlags | winapi.SwpFrameChanged
	// settle is how long a window just parked or lifted is left alone by
	// CollectEscaped: the move is sent off without waiting, and the app
	// may take a while to get to it.
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
	// closed is set once Stage Manager is quitting (Close): nothing may be
	// parked after the windows went home, or it would stay off the desktop.
	closed bool
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
	return &Service{parked: map[winapi.HWND]parked{}, lifted: map[winapi.HWND]bool{}, homing: map[winapi.HWND]homing{}, journal: filepath.Join(dir, journalName)}
}

// Close makes the Service refuse to park or rehome any window from now on.
// Call it before RestoreAll as Stage Manager quits: a switch whose callback
// was already queued on the main thread could otherwise park a window
// after everything went home, with nobody left to bring it back.
func (s *Service) Close() { s.closed = true }

// IsParked reports whether a window is parked.
func (s *Service) IsParked(h winapi.HWND) bool { _, ok := s.parked[h]; return ok }

// Home returns where a parked window goes back to.
func (s *Service) Home(h winapi.HWND) (winapi.Rect, bool) {
	p, ok := s.parked[h]
	return p.Home, ok
}

// Park moves a window, and the dialogs it owns, off the desktop.
func (s *Service) Park(h winapi.HWND) bool {
	if s.closed {
		s.logf("park %x %q refused: quitting", h, winapi.Title(h))
		return false
	}
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
func (s *Service) Unpark(hs ...winapi.HWND) { s.UnparkUnder(0, hs...) }

// UnparkUnder is Unpark with a window to keep above the ones coming home:
// front, if it is not 0, is raised last in the same order, so that the
// windows it stays over do not come home on top of it. Raised apart, the
// two would race: the raise asked for first may land after the other.
func (s *Service) UnparkUnder(front winapi.HWND, hs ...winapi.HWND) {
	var home []winapi.HWND
	for _, h := range hs {
		if s.unpark(h) {
			home = append(home, h)
		}
	}
	if len(home) == 0 {
		return
	}
	s.save()
	if front != 0 {
		home = append([]winapi.HWND{front}, home...)
	}
	raise(home)
}

func (s *Service) unpark(h winapi.HWND) bool {
	root, ok := s.parked[h]
	if !ok {
		return false
	}
	homes := s.homes(h)
	for w, p := range homes {
		delete(s.parked, w)
		s.sendHome(w, p)
	}
	// A dialog opened while its owner was parked was placed next to the
	// parked window: bring it along by the same distance.
	home := homes[h]
	dx, dy := home.X-root.Spot.X, home.Y-root.Spot.Y
	for _, o := range catalog.OwnedWindows(h) {
		if r := winapi.WindowRect(o); winapi.MostlyOffscreen(r) {
			s.sendHome(o, reachable(r.Offset(dx, dy)))
		}
	}
	return true
}

// group yields a parked window and the parked dialogs that went with it.
func (s *Service) group(h winapi.HWND) iter.Seq2[winapi.HWND, parked] {
	return func(yield func(winapi.HWND, parked) bool) {
		for w, p := range s.parked {
			if (w == h || p.Owner == h) && !yield(w, p) {
				return
			}
		}
	}
}

// homes is where a parked window and its parked dialogs go back to: the
// window to its home, or the nearest place the displays show it whole, and
// its dialogs as far from it as they were, each kept reachable. Every way
// home (Unpark, Lift, RestoreAll) goes by it.
func (s *Service) homes(h winapi.HWND) map[winapi.HWND]winapi.Point {
	root, ok := s.parked[h]
	if !ok {
		return nil
	}
	home := onDesktop(root.Home)
	dx, dy := home.Left-root.Home.Left, home.Top-root.Home.Top
	out := map[winapi.HWND]winapi.Point{}
	for w, p := range s.group(h) {
		if w == h {
			out[w] = winapi.Point{X: home.Left, Y: home.Top}
		} else {
			out[w] = reachable(p.Home.Offset(dx, dy))
		}
	}
	return out
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
	if !place(h, p) {
		s.logf("home %x %q: the move was refused, Chase sends it again", h, winapi.Title(h))
	}
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
		// Short against the time apps take to move a window, so that
		// quitting waits hardly longer than the slowest of them.
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

// place moves a window home to p, or a minimized one to come back there,
// and reports whether Windows took the move.
func place(h winapi.HWND, p winapi.Point) bool {
	if winapi.IsIconic(h) {
		return winapi.MoveRestored(h, p)
	}
	return land(h, p)
}

// raising counts the calls to raise: the last one asked for is the order
// that counts, and one not yet begun gives way to it.
var raising atomic.Uint64

// raiseAsk is how long raise waits for an app to answer before it sends
// its move off instead: far above what a working app takes, short enough
// not to hold the raise up noticeably.
const raiseAsk = 100 * time.Millisecond

// raiseOrder keeps the raises one at a time. They used to run together,
// and the one asked for while another was midway through ended it there,
// leaving the windows it had not raised yet under the ones it had: two
// displays' overviews landing in the same frame kept the wrong window of
// an app in front. A raise begun now finishes its order whole; those
// asked for meanwhile wait, and all but the last give way when they get
// their turn.
var raiseOrder sync.Mutex

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
		// A newer raise was asked for since, whose order is the one that
		// counts: this one would only flash its windows above before it.
		// Asked before waiting too, so that raises piling up behind one a
		// slow app holds give way without waiting for their turn.
		if raising.Load() != gen {
			return
		}
		raiseOrder.Lock()
		defer raiseOrder.Unlock()
		if raising.Load() != gen {
			return
		}
		for _, h := range slices.Backward(hs) {
			// IsHung only tells once an app has not answered for five
			// seconds: one that has just stopped would hold the lock that
			// long, and every raise after it.
			if !winapi.Responsive(h, raiseAsk) || !winapi.SetWindowPos(h, winapi.HwndTop, 0, 0, 0, 0, flags) {
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
	if !ok || s.closed {
		return
	}
	home = onDesktop(home)
	dx, dy := home.Left-p.Home.Left, home.Top-p.Home.Top
	var sx, sy int32
	if winapi.MonitorFromRect(home) != winapi.MonitorFromRect(p.Home) {
		spot := spotFor(home)
		sx, sy = spot.X-p.Spot.X, spot.Y-p.Spot.Y
	}
	for w, q := range s.group(h) {
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
	s.logf("rehome %x %q to %v", h, winapi.Title(h), home)
	s.save()
}

// Lift puts a parked window and its parked dialogs home for a moment,
// where they stay parked and keep their place in the z-order, until Lower.
// Windows' peek at a window from its taskbar thumbnail shows it where it
// is, so one off the desktop would show nothing.
func (s *Service) Lift(h winapi.HWND) {
	for w, to := range s.homes(h) {
		if s.lifted[w] {
			continue
		}
		land(w, to)
		p := s.parked[w]
		p.At = time.Now()
		s.parked[w] = p
		s.lifted[w] = true
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
	to := map[winapi.HWND]winapi.Point{}
	for h, p := range s.parked {
		if p.Owner == 0 {
			maps.Copy(to, s.homes(h))
		}
	}
	// A dialog whose owner is no longer parked goes home on its own.
	for h, p := range s.parked {
		if _, ok := to[h]; !ok {
			home := onDesktop(p.Home)
			to[h] = winapi.Point{X: home.Left, Y: home.Top}
		}
	}
	for h, p := range to {
		if winapi.IsWindow(h) {
			s.sendHome(h, p)
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
		// A window lifted for a peek is home on purpose, not escaped.
		if time.Since(p.At) < settle || s.lifted[h] {
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

// entry is a parked window in the journal, which is a bare JSON array of
// them. The keys are the field names every version so far has written:
// renaming one would decode an older journal with zeros and send its
// windows to the corner of the desktop (TestJournalFormat). The format
// stays readable by older versions, should one be run again.
type entry struct {
	Handle uint64 `json:"Handle"`
	HomeX  int32  `json:"HomeX"`
	HomeY  int32  `json:"HomeY"`
	SpotX  int32  `json:"SpotX"`
	SpotY  int32  `json:"SpotY"`
}

// aside keeps a journal that cannot be read where the next run cannot
// take it: renamed aside when nothing holds it, copied when something
// does, as a scanner may for a moment at startup — a rename over a file
// like that fails, and the copy is the one way the original's bytes,
// all that knows its windows' homes, outlive the run that starts from
// no journal. It reports whether the original is still in place.
func aside(path string) bool {
	bad := asideName(path)
	if err := os.Rename(path, bad); err == nil {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	if err := os.WriteFile(bad, data, 0o644); err != nil {
		log.Println("stagebar: keeping the journal aside:", err)
	}
	return true
}

// asideName is where Recover keeps a journal it cannot read: itself with
// .bad, or, taken already by an earlier one, timestamped beside it, so
// that a second broken journal does not take the first's place: it is
// all that knows where its windows belong.
func asideName(path string) string {
	if _, err := os.Stat(path + ".bad"); err != nil {
		return path + ".bad"
	}
	return fmt.Sprintf("%s.bad.%d", path, time.Now().Unix())
}

// Recover brings back the windows a previous run left parked, and follows
// them until they get there as Unpark does. A journal that cannot be read
// is kept aside rather than deleted: it is all that knows those homes.
func (s *Service) Recover() {
	entries, err := readJournal(s.journal)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		log.Printf("stagebar: the journal of parked windows cannot be read, kept as %s: %v", asideName(s.journal), err)
		aside(s.journal)
		return
	}
	for _, e := range entries {
		if h, p, ok := journalHome(e); ok {
			s.sendHome(h, p)
			winapi.Redraw(h)
		}
	}
	if err := os.Remove(s.journal); err != nil {
		log.Println("stagebar: removing the journal of parked windows:", err)
	}
}

// Rescue brings back every window the journal in dir says is parked, from
// any goroutine and without the Service: the last resort when the thread
// that owns the Service stops answering. It leaves the journal in place.
func Rescue(dir string) int {
	entries, err := readJournal(filepath.Join(dir, journalName))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if h, p, ok := journalHome(e); ok {
			place(h, p)
			winapi.Redraw(h)
			n++
		}
	}
	return n
}

// readJournal reads the entries of the journal at path.
func readJournal(path string) ([]entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return entries, nil
}

// journalHome says where a window the journal lists goes back to: one
// still where it was parked or, moved by its app since, still off the
// desktop. Skipping one of those would leave it where nobody can reach it:
// the journal is all that knows its home.
func journalHome(e entry) (winapi.HWND, winapi.Point, bool) {
	h := winapi.HWND(e.Handle)
	if !winapi.IsWindow(h) {
		return 0, winapi.Point{}, false
	}
	r := at(h)
	if (r.Left != e.SpotX || r.Top != e.SpotY) && !winapi.MostlyOffscreen(r) {
		return 0, winapi.Point{}, false
	}
	home := onDesktop(winapi.Rect{Left: e.HomeX, Top: e.HomeY, Right: e.HomeX + r.W(), Bottom: e.HomeY + r.H()})
	return h, winapi.Point{X: home.Left, Y: home.Top}, true
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

// land is move for a window coming back onto the desktop (homeFlags).
func land(h winapi.HWND, p winapi.Point) bool {
	return winapi.SetWindowPos(h, 0, p.X, p.Y, 0, 0, homeFlags)
}

// save writes the journal. Parking still works without it, only recovery
// is lost, so a failure is logged rather than returned.
func (s *Service) save() {
	if len(s.parked) == 0 {
		if err := os.Remove(s.journal); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Println("stagebar: removing the journal of parked windows:", err)
		}
		return
	}
	entries := make([]entry, 0, len(s.parked))
	for h, p := range s.parked {
		entries = append(entries, entry{uint64(h), p.Home.Left, p.Home.Top, p.Spot.X, p.Spot.Y})
	}
	if err := writeJournal(s.journal, entries); err != nil {
		log.Println("stagebar: writing the journal of parked windows:", err)
	}
}

// writeJournal replaces the journal at path whole: written beside it and
// renamed over it, so that a run killed while writing leaves the journal
// as it was rather than half of one, which recovery could not read. The
// copy is pushed to the disk before the rename takes its place: power
// lost in between would leave a journal that is empty, and the windows
// it alone knew the homes of fit nowhere better than the nearest
// display's middle.
func writeJournal(path string, entries []entry) error {
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := writeSynced(tmp, data); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// writeSynced writes bytes to a new file and sees them to the disk.
func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
