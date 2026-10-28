// Stage Manager for Windows brings macOS Stage Manager to Windows: the app
// you work in stays on the stage, the others wait in a strip beside it, one
// strip per display.
package main

import (
	_ "embed"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"slices"

	"github.com/egoist/mygo"
	"github.com/e1roy/stage_manager_for_windows/internal/app"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

//go:embed resources/icon.png
var icon []byte

func main() {
	mygo.App.SetName("Stage Manager for Windows")
	// The version is mygo.json's: mygo build writes it into the
	// executable's version resource, which the About pane reads back, so
	// the app cannot show a version its installer does not have. A go run
	// has no such resource and is a development build.
	if mygo.App.Version() == "" {
		mygo.App.SetVersion("dev")
	}
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

// dataDir holds settings and the parking journal. STAGEMANAGER_DATA_DIR moves
// it, so that a test run keeps its own.
func dataDir() string {
	if d := os.Getenv("STAGEMANAGER_DATA_DIR"); d != "" {
		return d
	}
	base, err := os.UserCacheDir() // %LOCALAPPDATA%
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "Stage Manager")
}
