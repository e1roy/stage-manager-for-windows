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
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "找回屏幕外的窗口", Click: func(*mygo.MenuItem, *mygo.Window) { a.rescueOffscreen() }},
		{Label: "设置…", Click: func(*mygo.MenuItem, *mygo.Window) { a.openSettings() }},
		mygo.Separator(),
		{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})
}

// OpenSettings shows the settings window, as a second launch does.
func (a *App) OpenSettings() { a.openSettings() }
