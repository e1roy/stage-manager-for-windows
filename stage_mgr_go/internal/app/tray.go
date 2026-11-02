package app

import (
	"fmt"
	"log"

	"github.com/egoist/mygo"
)

func (a *App) setUpTray() {
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:      "stagebar",
		Icon:    a.trayIcon,
		ToolTip: "Stage Manager",
		Menu:    a.trayMenu(),
	})
	if err != nil {
		log.Fatal("stagebar: no tray icon: ", err)
	}
	a.tray = tray
	tray.OnClick(a.openSettings)
}

func (a *App) trayMenu() *mygo.Menu {
	tr := a.tr()
	var items []*mygo.MenuItem
	// A newer release goes first, with the red dot the icon shows, as
	// apps on Windows and macOS offer an update at the top of their menu.
	if r, ok := a.newer(); ok {
		items = append(items,
			&mygo.MenuItem{Label: fmt.Sprintf(tr.NewVersion, r.Version) + "…", Icon: a.dotMenuIcon(), Click: func(*mygo.MenuItem, *mygo.Window) { a.openRelease() }},
			mygo.Separator(),
		)
	}
	return mygo.NewMenu(append(items, []*mygo.MenuItem{
		{Label: tr.Rescue, Click: func(*mygo.MenuItem, *mygo.Window) { a.rescueOffscreen() }},
		{Label: tr.Settings, Click: func(*mygo.MenuItem, *mygo.Window) { a.openSettings() }},
		mygo.Separator(),
		// Relaunch quits as Quit does, putting every parked window back,
		// and starts the app again once the single instance lock is free.
		{Label: tr.Restart, Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Relaunch() }},
		{Label: tr.Quit, Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	}...))
}

// OpenSettings shows the settings window, as a second launch does.
func (a *App) OpenSettings() { a.openSettings() }
