//go:build windows

package parking

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

// TestJournalFormat reads a journal as every version so far has written
// it. A field renamed would decode with zeros and send windows to the
// corner of the desktop without any error.
func TestJournalFormat(t *testing.T) {
	const old = `[{"Handle":1311234,"HomeX":120,"HomeY":-40,"SpotX":5280,"SpotY":-40}]`
	path := filepath.Join(t.TempDir(), journalName)
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []entry{{Handle: 1311234, HomeX: 120, HomeY: -40, SpotX: 5280, SpotY: -40}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	// And it is written back the same way.
	data, _ := json.Marshal(want)
	if string(data) != old {
		t.Errorf("written as %s, want %s", data, old)
	}
}

func TestWriteJournalReplacesWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", journalName)
	first := []entry{{Handle: 1, HomeX: 10}}
	second := []entry{{Handle: 2, HomeX: 20}, {Handle: 3, HomeX: 30}}
	for _, es := range [][]entry{first, second} {
		if err := writeJournal(path, es); err != nil {
			t.Fatal(err)
		}
	}
	got, err := readJournal(path)
	if err != nil || len(got) != 2 || got[1] != second[1] {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("the file written beside the journal is left over: %v", err)
	}
}

// TestRecoverKeepsBrokenJournal: a journal cut short by a crash is all
// that knows where its windows belong, so it must not be deleted.
func TestRecoverKeepsBrokenJournal(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	broken := []byte(`[{"Handle":1311234,"HomeX":12`)
	if err := os.WriteFile(s.journal, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	s.Recover()
	kept, err := os.ReadFile(s.journal + ".bad")
	if err != nil || string(kept) != string(broken) {
		t.Fatalf("the broken journal was not kept: %q, %v", kept, err)
	}
}

func TestRecoverRemovesJournalItRead(t *testing.T) {
	s := New(t.TempDir())
	// No such window: nothing to move, and the journal is done with.
	if err := writeJournal(s.journal, []entry{{Handle: 0x7ffffff0}}); err != nil {
		t.Fatal(err)
	}
	s.Recover()
	if _, err := os.Stat(s.journal); !os.IsNotExist(err) {
		t.Errorf("journal still there: %v", err)
	}
}

// TestRecoverSendsParkedHome: the journal's own spot tells that a window
// still sits where it was parked — only that window goes home, and one a
// user has since moved back on their own stays where they put it. Getting
// either way round would strand a window or move a user's window.
func TestRecoverSendsParkedHome(t *testing.T) {
	// The windows answer on the thread that made them, which pumps no
	// messages: everything must stay on that thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	s := New(t.TempDir())
	home := winapi.Point{X: 100, Y: 100}
	still := hiddenWindow(t)
	defer destroyWindow(still)
	moved := hiddenWindow(t)
	defer destroyWindow(moved)
	// Where they were parked, far off the desktop. The first still sits
	// there; the second its app has since brought back onto the desktop,
	// where it was made, so the journal must leave it be.
	spot := winapi.Point{X: winapi.WindowRect(moved).Right + 5000, Y: 100}
	winapi.Move(still, spot.X, spot.Y)
	entries := []entry{
		{Handle: uint64(still), HomeX: home.X, HomeY: home.Y, SpotX: spot.X, SpotY: spot.Y},
		{Handle: uint64(moved), HomeX: home.X, HomeY: home.Y, SpotX: spot.X, SpotY: spot.Y},
	}
	if err := writeJournal(s.journal, entries); err != nil {
		t.Fatal(err)
	}
	before := winapi.WindowRect(moved)
	s.Recover()
	// land sends the move without waiting: give the window a moment to
	// take it before judging where it ended up.
	for i := 0; i < 50 && at(still).Left != home.X; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if r := at(still); r.Left != home.X || r.Top != home.Y {
		t.Errorf("window still parked stayed at %v, want %v", r, home)
	}
	if r := winapi.WindowRect(moved); r != before {
		t.Errorf("window moved back by its app was sent to %v, want left at %v", r, before)
	}
}

// TestSecondBrokenJournalDoesNotTakeTheFirst: an unreadable journal kept
// aside is all that knows where its windows belong; a second one must
// not take its place, or the first is lost with them.
func TestSecondBrokenJournalDoesNotTakeTheFirst(t *testing.T) {
	s := New(t.TempDir())
	first := []byte(`[{"Handle":1,"HomeX":10`)
	second := []byte(`[{"Handle":2,"HomeX":20`)
	if err := os.WriteFile(s.journal, first, 0o644); err != nil {
		t.Fatal(err)
	}
	s.Recover()
	if err := os.WriteFile(s.journal, second, 0o644); err != nil {
		t.Fatal(err)
	}
	s.Recover()
	kept, err := os.ReadFile(s.journal + ".bad")
	if err != nil || string(kept) != string(first) {
		t.Fatalf("the first broken journal was not kept: %q, %v", kept, err)
	}
	beside := s.journal + ".bad."
	matches, _ := filepath.Glob(beside + "*")
	if len(matches) == 0 {
		t.Error("the second broken journal was dropped")
	}
}

// TestClosedParksNothing: once quitting, nothing goes off the desktop and
// nothing is written to the journal.
func TestClosedParksNothing(t *testing.T) {
	// The window answers on the thread that made it, which pumps no
	// messages: everything must stay on that thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	h := hiddenWindow(t)
	defer destroyWindow(h)
	s := New(t.TempDir())
	if !s.Park(h) {
		t.Fatal("could not park the test's window at all")
	}
	s.Unpark(h)
	s.Close()
	if s.Park(h) {
		t.Error("parked a window after Close")
	}
	s.Rehome(h, winapi.Rect{Right: 100, Bottom: 100})
	if _, err := os.Stat(s.journal); !os.IsNotExist(err) {
		t.Errorf("journal written after Close: %v", err)
	}
}

// hiddenWindow creates a hidden window of the system's STATIC class on the
// primary display, which nobody sees. It dies only on the thread that made
// it, and a t.Cleanup runs after the test's deferred UnlockOSThread lets
// the goroutine wander: the caller destroys it with a defer, which runs
// first.
func hiddenWindow(t *testing.T) winapi.HWND {
	t.Helper()
	user32 := syscall.NewLazyDLL("user32.dll")
	create := user32.NewProc("CreateWindowExW")
	class, _ := syscall.UTF16PtrFromString("STATIC")
	const wsPopup = 0x80000000
	h, _, err := create.Call(0, uintptr(unsafe.Pointer(class)), 0, wsPopup, 100, 100, 200, 100, 0, 0, 0, 0)
	if h == 0 {
		t.Fatal("CreateWindowExW:", err)
	}
	return winapi.HWND(h)
}

// destroyWindow takes a window down on the thread that made it, where
// DestroyWindow works.
func destroyWindow(h winapi.HWND) {
	syscall.NewLazyDLL("user32.dll").NewProc("DestroyWindow").Call(uintptr(h))
}
