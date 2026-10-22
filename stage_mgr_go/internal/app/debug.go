package app

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"

	"github.com/egoist/mygo"
	"github.com/e1roy/stage_manager_for_windows/internal/parking"
)

// debug logs what Stage Manager decides when STAGEMANAGER_DEBUG is set.
var debugOn = os.Getenv("STAGEMANAGER_DEBUG") != ""

func debugf(format string, args ...any) {
	if debugOn {
		log.Printf(format, args...)
	}
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
