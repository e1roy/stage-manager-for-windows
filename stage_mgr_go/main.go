// StageBar brings macOS Stage Manager to Windows: the app you work in
// stays on the stage, the others wait in a strip beside it, one strip per
// display.
package main

import (
	_ "embed"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"slices"

	"github.com/egoist/mygo"
	"github.com/renkai9418/stage-bar/internal/app"
	"github.com/renkai9418/stage-bar/internal/winapi"
)

//go:embed resources/icon.png
var icon []byte

// version is set by the build.
var version = "0.1.0"

func main() {
	mygo.App.SetName("StageBar")
	mygo.App.SetVersion(version)
	if !mygo.App.RequestSingleInstanceLock() {
		// The running instance opens its settings; let it come to the front.
		winapi.AllowAnyForeground()
		return
	}
	// The strips are the app's windows; closing settings leaves it running.
	mygo.App.OnWindowAllClosed(func() {})

	var a *app.App
	mygo.App.WhenReady(func() {
		a = app.Start(app.Options{DataDir: dataDir(), TrayIcon: icon})
	})
	// A second launch opens the settings; "--quit" ends the running
	// instance instead, which puts every window back, as installers need.
	mygo.App.OnSecondInstance(func(args []string, _ string) {
		if slices.Contains(args, "--quit") {
			mygo.App.Quit()
			return
		}
		if slices.Contains(args, "--debug-hang") {
			app.DebugHang()
			return
		}
		if a != nil {
			a.OpenSettings()
		}
	})
	// Ctrl+C in a console still puts every window back.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		mygo.App.Quit()
	}()
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// dataDir holds settings and the parking journal. STAGEBAR_DATA_DIR moves
// it, so that a test run keeps its own.
func dataDir() string {
	if d := os.Getenv("STAGEBAR_DATA_DIR"); d != "" {
		return d
	}
	base, err := os.UserCacheDir() // %LOCALAPPDATA%
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "StageBar")
}
