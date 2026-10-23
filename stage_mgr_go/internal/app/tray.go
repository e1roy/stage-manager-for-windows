package app

import (
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
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: tr.Rescue, Click: func(*mygo.MenuItem, *mygo.Window) { a.rescueOffscreen() }},
		{Label: tr.Settings, Click: func(*mygo.MenuItem, *mygo.Window) { a.openSettings() }},
		mygo.Separator(),
		{Label: tr.Quit, Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})
}

// OpenSettings shows the settings window, as a second launch does.
func (a *App) OpenSettings() { a.openSettings() }
