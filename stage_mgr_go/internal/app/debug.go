package app

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/parking"
	"github.com/egoist/mygo"
)

// debug logs what Stage Manager decides when STAGEMANAGER_DEBUG is set.
var debugOn = os.Getenv("STAGEMANAGER_DEBUG") != ""

func debugf(format string, args ...any) {
	if debugOn {
		log.Printf(format, args...)
	}
}

// logToFile keeps the debug log in the data directory too, as debug.log,
// the previous run's as debug.prev.log: the app has no console to print to
// once started from Explorer or a script.
func logToFile(dataDir string) {
	if !debugOn {
		return
	}
	name := filepath.Join(dataDir, "debug.log")
	os.MkdirAll(dataDir, 0o755)
	os.Rename(name, filepath.Join(dataDir, "debug.prev.log"))
	f, err := os.Create(name)
	if err != nil {
		return
	}
	// The file first: with no console, writing to stderr fails, and a
	// MultiWriter stops at the first writer that fails.
	log.SetOutput(io.MultiWriter(f, os.Stderr))
	log.SetFlags(log.Ltime | log.Lmicroseconds)
}

// hangTimeout is how long the main thread may not answer before the
// watchdog steps in.
const hangTimeout = 5 * time.Second

// watchMainThread guards against the main thread hanging, which would leave
// windows parked off the desktop with nothing to bring them back: after
// hangTimeout it brings every parked window home from the journal, on its
// own goroutine, and writes every goroutine's stack to the data directory.
// Stage Manager parks again what should be parked once the thread answers.
func watchMainThread(dataDir string) {
	go func() {
		for {
			time.Sleep(2 * time.Second)
			done := make(chan struct{})
			go mygo.RunOnMain(func() { close(done) })
			select {
			case <-done:
				continue
			case <-time.After(hangTimeout):
			}
			n := parking.Rescue(dataDir)
			name := filepath.Join(dataDir, fmt.Sprintf("hang-%s.log", time.Now().Format("20060102-150405")))
			if f, err := os.Create(name); err == nil {
				fmt.Fprintf(f, "Stage Manager's main thread did not answer for %v; %d parked windows were brought home.\n\n", hangTimeout, n)
				pprof.Lookup("goroutine").WriteTo(f, 2)
				f.Close()
			}
			log.Printf("stagebar: main thread not answering; brought %d windows home; stacks in %s", n, name)
			<-done
		}
	}()
}

// DebugHang blocks the main thread for a while in debug runs, to try the
// watchdog. Call it on the main thread.
func DebugHang() {
	if debugOn {
		log.Println("stagebar: blocking the main thread on purpose")
		time.Sleep(12 * time.Second)
	}
}

// frameStats measures, in debug runs, how evenly a window draws while
// something moves in it: how many frames, the longest gap between two and
// what was going on then, and the longest its view took to build. A gap
// of a frame and a half or more (25 ms at 60 Hz) is a frame dropped; one
// whose view built quickly lost its time elsewhere on the main thread
// (see timed) or in rendering.
type frameStats struct {
	n           int
	start, last time.Time
	gap         time.Duration
	gapAt       string // what was going on at the longest gap
	build       time.Duration
	// idle, for a window that only draws while something moves in it,
	// reports once it has drawn nothing for a while (burst).
	idle *time.Timer
}

// frame counts a frame drawn at now, at describing what moves in it. A
// frame may build its view more than once (MyGo's passes), all at the
// frame's time: those count once.
func (f *frameStats) frame(now time.Time, at string) {
	if !debugOn || f.n > 0 && now.Equal(f.last) {
		return
	}
	if f.n == 0 {
		f.start = now
	} else if g := now.Sub(f.last); g > f.gap {
		f.gap, f.gapAt = g, at
	}
	f.n++
	f.last = now
}

// built notes how long a frame's view took to build.
func (f *frameStats) built(d time.Duration) { f.build = max(f.build, d) }

// report logs what was measured as what, and starts over.
func (f *frameStats) report(what string) {
	if !debugOn || f.n == 0 {
		return
	}
	debugf("%s: %d frames in %v, longest gap %v (%s), longest build %v", what, f.n,
		f.last.Sub(f.start).Round(time.Millisecond), f.gap.Round(100*time.Microsecond), f.gapAt,
		f.build.Round(100*time.Microsecond))
	idle := f.idle
	*f = frameStats{idle: idle}
}

// burst reports as what once no frame came for burstEnd, for a window that
// draws in bursts, as a strip whose cards slide or grow. Bursts of a few
// frames, as a press, say nothing.
func (f *frameStats) burst(what string) {
	if !debugOn {
		return
	}
	if f.idle != nil {
		f.idle.Reset(burstEnd)
		return
	}
	f.idle = after(burstEnd, func() {
		if f.n >= 8 {
			f.report(what)
		} else {
			*f = frameStats{idle: f.idle}
		}
	})
}

const burstEnd = 200 * time.Millisecond

// stopwatch times the steps of a task on the main thread, in debug runs,
// and logs them should the whole take long enough to cost a frame.
type stopwatch struct {
	start, last time.Time
	laps        []string
}

func newStopwatch() *stopwatch {
	now := time.Now()
	return &stopwatch{start: now, last: now}
}

// lap ends a step named name.
func (s *stopwatch) lap(name string) {
	if !debugOn {
		return
	}
	now := time.Now()
	s.laps = append(s.laps, fmt.Sprintf("%s %v", name, now.Sub(s.last).Round(100*time.Microsecond)))
	s.last = now
}

// report logs the steps as what, if they took longer than slowTask.
func (s *stopwatch) report(what string) {
	if el := time.Since(s.start); debugOn && el > slowTask {
		debugf("%s took %v: %s", what, el.Round(100*time.Microsecond), strings.Join(s.laps, ", "))
	}
}

// slowTask is how long a task may hold the main thread before the debug
// log names it: a frame's worth at 120 Hz.
const slowTask = 8 * time.Millisecond

// timed wraps a task for the main thread so that, in debug runs, one that
// holds it up long enough to cost a frame is logged with its name.
func timed(fn func()) func() {
	if !debugOn {
		return fn
	}
	return func() {
		defer tookLong(time.Now(), func() string { return funcName(fn) })
		fn()
	}
}

// tookLong logs a task on the main thread begun at start, named by what,
// should it have held the thread up for longer than slowTask.
func tookLong(start time.Time, what func() string) {
	if el := time.Since(start); debugOn && el > slowTask {
		debugf("main thread: %s took %v", what(), el.Round(100*time.Microsecond))
	}
}

// progress names how far along an animation is, p of the way, for the
// debug log.
func progress(p float64) string { return fmt.Sprintf("at %.0f%%", p*100) }

// funcName is the name of fn, for the debug log: a closure is named after
// the function it is in, as App.moveSet.func1.
func funcName(fn func()) string {
	name := "?"
	if f := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()); f != nil {
		name = f.Name()
	}
	return strings.TrimPrefix(name, "github.com/e1roy/stage_manager_for_windows/internal/")
}
