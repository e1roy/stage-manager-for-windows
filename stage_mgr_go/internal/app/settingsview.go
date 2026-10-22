package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/renkai9418/stage-bar/internal/settings"
	"github.com/renkai9418/stage-bar/internal/winapi"
)

// settingsWindow edits a copy of the settings and applies every change at
// once, as Windows' own Settings does.
type settingsWindow struct {
	a     *App
	win   *mygo.Window
	s     settings.Settings
	mode  int
	width float64
	icon  float64
	login bool
	icon0 *ui.Bitmap
	// notice is shown as a toast in the next frame.
	notice string

	widthTimer *time.Timer
}

func (a *App) openSettings() {
	if sw := a.settingsUI; sw != nil {
		sw.win.Show()
		winapi.Activate(winapi.HWND(sw.win.NativeHandle()))
		return
	}
	sw := &settingsWindow{a: a}
	sw.load()
	if a.trayIcon != nil {
		sw.icon0, _ = ui.DecodeBitmap(a.trayIcon)
	}
	sw.win = mygo.NewWindow(mygo.WindowOptions{
		Title:     "StageBar 设置",
		Width:     620,
		Height:    760,
		MinWidth:  480,
		MinHeight: 420,
		Vibrancy:  mygo.VibrancyMica,
		StateKey:  "settings",
		Content:   ui.View(sw.view),
	})
	sw.win.OnClosed(func() { a.settingsUI = nil })
	a.settingsUI = sw
	winapi.Activate(winapi.HWND(sw.win.NativeHandle()))
	go func() {
		on := mygo.App.OpenAtLogin()
		sw.win.Update(func() { sw.login = on })
	}()
}

func (sw *settingsWindow) load() {
	sw.s = sw.a.settings.Clone()
	sw.mode = 0
	if sw.s.Mode == settings.Floating {
		sw.mode = 1
	}
	sw.width, sw.icon = sw.s.Width, sw.s.IconScale
}

func (sw *settingsWindow) apply() {
	sw.a.applySettings(sw.s)
	sw.s = sw.a.settings.Clone()
}

func (sw *settingsWindow) view(c *ui.Context) {
	t := c.Theme()
	if sw.notice != "" {
		c.Toast(sw.notice)
		sw.notice = ""
	}
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	card := t.Surface
	if c.Vibrancy() {
		card = ui.RGBA(255, 255, 255, 0.72)
		if t.Dark {
			card = ui.RGBA(255, 255, 255, 0.055)
		}
	}
	section := func(title string, rows func()) {
		ui.Text(c, title).FontSize(14).Bold().Margin(14, 0, 6, 2)
		ui.Column(c).Background(card).Radius(8).Border(1, t.Border.Alpha(0.6)).Dividers(1, t.Border.Alpha(0.5)).Children(rows)
	}
	row := func(title, desc string, control func()) {
		ui.Row(c).Padding(14, 18).Gap(18).AlignItems(ui.Center).MinHeight(64).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
				ui.Text(c, title)
				if desc != "" {
					ui.Text(c, desc).FontSize(12).TextColor(t.TextMuted)
				}
			})
			control()
		})
	}

	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).Padding(26, 30, 30, 30).Gap(4).MaxWidth(760).Children(func() {
			ui.Row(c).Gap(16).AlignItems(ui.Center).Margin(0, 0, 10, 0).Children(func() {
				if sw.icon0 != nil {
					ui.Image(c, sw.icon0).Size(52, 52)
				}
				ui.Column(c).Gap(2).Children(func() {
					ui.Text(c, "StageBar").FontSize(24).Bold()
					ui.Text(c, "像 macOS 台前调度一样整理 Windows 的窗口").TextColor(t.TextMuted)
				})
			})

			section("侧边栏", func() {
				row("显示方式", "固定：为侧边栏保留屏幕边缘，最大化的窗口不会遮住它。浮动：有空间时显示，被窗口遮住时让开，指针移到屏幕左边缘可唤出。", func() {
					if ui.Segmented(c, &sw.mode, "固定", "浮动").Changed() {
						sw.s.Mode = settings.Fixed
						if sw.mode == 1 {
							sw.s.Mode = settings.Floating
						}
						sw.apply()
					}
				})
				row("宽度", fmt.Sprintf("%.0f 像素", sw.width), func() {
					if ui.Slider(c, &sw.width, settings.MinWidth, settings.MaxWidth).Width(200).Changed() {
						sw.width = float64(int(sw.width/10+0.5) * 10)
						sw.s.Width = sw.width
						// Reserving the band on every step makes the shell
						// shuffle the desktop around; apply once it rests.
						if sw.widthTimer != nil {
							sw.widthTimer.Stop()
						}
						sw.widthTimer = after(180*time.Millisecond, sw.apply)
					}
				})
				row("图标大小", fmt.Sprintf("%.0f%%", sw.icon*100), func() {
					if ui.Slider(c, &sw.icon, settings.MinIconScale, settings.MaxIconScale).Width(200).Changed() {
						sw.s.IconScale = sw.icon
						sw.apply()
					}
				})
				row("立体透视", "卡片像 macOS 一样侧向转动，悬停时转向你。", func() {
					if ui.Switch(c, &sw.s.Perspective).Changed() {
						sw.apply()
					}
				})
			})

			section("舞台", func() {
				row("隐藏舞台之外的窗口", "只有当前应用留在桌面上，其他应用收进侧边栏。关闭后侧边栏只列出它们。", func() {
					if ui.Switch(c, &sw.s.HideInactive).Changed() {
						sw.apply()
					}
				})
				row("点击桌面清空舞台", "点击壁纸时，把舞台上的应用都收进侧边栏。", func() {
					if ui.Switch(c, &sw.s.ClickDesktopClears).Disabled(!sw.s.HideInactive).Changed() {
						sw.apply()
					}
				})
			})

			section("显示器", func() {
				for i, d := range sw.a.displays {
					m := d.mon
					desc := fmt.Sprintf("%d × %d · 缩放 %.0f%%", m.Monitor.W(), m.Monitor.H(), m.Scale()*100)
					if m.Primary {
						desc += " · 主显示器"
					}
					on := sw.s.DisplayEnabled(m.Device)
					c.Key(m.Device)
					row(fmt.Sprintf("显示器 %d", i+1), desc, func() {
						if ui.Switch(c.Key(m.Device), &on).Changed() {
							if sw.s.Displays == nil {
								sw.s.Displays = map[string]bool{}
							}
							sw.s.Displays[m.Device] = on
							sw.apply()
						}
					})
				}
			})

			section("通用", func() {
				row("开机时启动", "登录 Windows 后在后台启动 StageBar。", func() {
					if ui.Switch(c, &sw.login).Changed() {
						on := sw.login
						go func() {
							if err := mygo.App.SetOpenAtLogin(on); err != nil {
								msg := "无法更改开机启动：" + err.Error()
								sw.win.Update(func() { sw.login, sw.notice = !on, msg })
							}
						}()
					}
				})
				row("显示或隐藏侧边栏", "浮动模式下切换指针所在显示器的侧边栏。", func() {
					ui.Row(c).Gap(4).Children(func() {
						for i, k := range []string{"Ctrl", "Alt", "Space"} {
							if i > 0 {
								ui.Text(c, "+").TextColor(t.TextMuted)
							}
							ui.Text(c, k).FontSize(12).Padding(2, 8).Radius(4).Border(1, t.Border).Background(t.Background)
						}
					})
				})
				row("暂停", "暂停时所有窗口回到桌面，侧边栏隐藏。", func() {
					if ui.Switch(c, &sw.s.Paused).Changed() {
						sw.apply()
					}
				})
			})

			ui.Text(c, "StageBar "+mygo.App.Version()+" · 不收集任何数据，也不联网。").FontSize(12).TextColor(t.TextMuted).Margin(18, 0, 0, 2)
		})
	})
}
