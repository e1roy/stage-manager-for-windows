package app

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/renkai9418/stage-bar/internal/winapi"
)

// Shortcut toggles the floating strip of the display under the pointer.
const Shortcut = "Ctrl+Alt+Space"

func (a *App) setUpTray() {
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:      "stagebar",
		Icon:    a.trayIcon,
		ToolTip: "StageBar",
		Menu:    a.trayMenu(),
	})
	if err != nil {
		log.Fatal("stagebar: no tray icon: ", err)
	}
	a.tray = tray
	tray.OnClick(a.openSettings)
	if err := mygo.GlobalShortcut.Register(Shortcut, a.toggleStrip); err != nil {
		log.Println("stagebar: shortcut:", err)
	}
}

func (a *App) trayMenu() *mygo.Menu {
	pause := "暂停 StageBar"
	if a.settings.Paused {
		pause = "恢复 StageBar"
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "显示 / 隐藏侧边栏\t" + Shortcut, Click: func(*mygo.MenuItem, *mygo.Window) { a.toggleStrip() }},
		{Label: "设置…", Click: func(*mygo.MenuItem, *mygo.Window) { a.openSettings() }},
		mygo.Separator(),
		{Label: pause, Click: func(*mygo.MenuItem, *mygo.Window) {
			s := a.settings.Clone()
			s.Paused = !s.Paused
			a.applySettings(s)
		}},
		{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	})
}

func (a *App) updateTrayMenu() {
	if a.tray != nil {
		a.tray.SetMenu(a.trayMenu())
	}
}

// toggleStrip shows or hides the floating strip of the display under the pointer.
func (a *App) toggleStrip() {
	d := a.displayByHandle(winapi.MonitorFromPoint(winapi.Cursor()))
	if d != nil && d.strip != nil {
		d.strip.toggle()
	}
}

// OpenSettings shows the settings window, as a second launch does.
func (a *App) OpenSettings() { a.openSettings() }
