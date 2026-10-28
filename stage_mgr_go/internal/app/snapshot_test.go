package app

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/i18n"
	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo/ui"
)

// TestSnapshots draws the strip and the settings window to PNG files in
// STAGEMANAGER_SNAPSHOT_DIR, to look at the design without running the app.
func TestSnapshots(t *testing.T) {
	dir := os.Getenv("STAGEMANAGER_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("STAGEMANAGER_SNAPSHOT_DIR is not set")
	}
	a := &App{settings: settings.Default(), media: newMedia(), ready: true}
	a.act = func(action, *display, *set) {}
	d := &display{cards: map[string]*cardState{}, landing: map[string]bool{}}
	d.mon = winapi.Monitor{Device: `\\.\DISPLAY1`, Monitor: winapi.Rect{Right: 2560, Bottom: 1440}, Work: winapi.Rect{Right: 2560, Bottom: 1392}, DPI: 96, Primary: true}
	apps := []struct {
		name    string
		windows int
		aspect  float64
		hue     color.RGBA
	}{
		{"Visual Studio Code", 2, 16.0 / 10, color.RGBA{30, 30, 36, 255}},
		{"Google Chrome", 3, 16.0 / 9, color.RGBA{245, 245, 250, 255}},
		{"画图", 1, 4.0 / 3, color.RGBA{250, 250, 252, 255}},
		{"Notepad", 1, 0.8, color.RGBA{240, 240, 235, 255}},
		{"音乐", 1, 1.4, color.RGBA{220, 60, 80, 255}},
	}
	h := winapi.HWND(1)
	var groups []*group
	for i, ap := range apps {
		g := &group{app: ap.name, name: ap.name, z: i}
		for j := 0; j < ap.windows; j++ {
			w := catalog.Window{HWND: h, Title: ap.name, App: ap.name, Exe: `C:\Apps\` + ap.name + ".exe", Frame: winapi.Rect{Right: int32(1000 * ap.aspect), Bottom: 1000}}
			if i != 4 { // the last app shows how a window without a still looks
				bmp := ui.NewBitmap(fakeWindow(int(400*ap.aspect), 400, ap.hue, j))
				a.media.stills[h] = &still{card: bmp, fly: bmp}
			}
			g.windows = append(g.windows, w)
			h++
		}
		a.media.icons[ap.name] = ui.NewBitmap(fakeIcon(i))
		groups = append(groups, g)
	}
	// Code and Chrome were on the stage together, and left it as one card.
	d.sets = append(d.sets, newSet(groups[:2]))
	for _, g := range groups[2:] {
		d.sets = append(d.sets, newSet([]*group{g}))
	}
	s := &strip{a: a, d: d, shown: true, scale: 1, rect: winapi.Rect{Right: 200, Bottom: 1000}}
	d.strip = s
	a.displays = []*display{d}

	shoot := func(name string, view func(*ui.Context), w, h int, setup func(*ui.Tester)) {
		tt := ui.NewTester(func(c *ui.Context) {
			// The wallpaper behind the transparent strip and overview. The
			// settings window has a background of its own.
			if strings.HasPrefix(name, "strip") || strings.HasPrefix(name, "overview") {
				w, h := c.Size()
				ui.Box(c).Absolute().Left(0).Top(0).Size(w, h).Background(ui.Hex("#4a6178"))
			}
			view(c)
		}, w, h)
		tt.Frame()
		if setup != nil {
			setup(tt)
		}
		for i := 0; i < 30; i++ {
			tt.Advance(33_000_000)
			tt.Frame()
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		png.Encode(f, tt.Image())
	}
	shoot("strip.png", s.view, 220, 1000, nil)
	shoot("strip-hover.png", s.view, 220, 1000, func(tt *ui.Tester) {
		x, y := center(d.layout.cards[1])
		tt.Move(x, y)
	})
	// The same strip on the right of its display, its mirror image.
	a.settings.Sides = map[string]settings.Side{d.mon.Device: settings.Right}
	shoot("strip-right.png", s.view, 220, 1000, nil)
	shoot("strip-right-hover.png", s.view, 220, 1000, func(tt *ui.Tester) {
		x, y := center(d.layout.cards[1])
		tt.Move(x, y)
	})
	a.settings.Sides = nil
	// The pointer near the bottom brings up the overview's panel.
	shoot("strip-footer-hover.png", s.view, 220, 1000, func(tt *ui.Tester) {
		tt.Move(center(footerButton(220, 1000, a.panel(), false)))
	})
	// On its way in, the pointer still some way off.
	s.near = panelNearness((panelReach + panelFull) / 2)
	shoot("strip-footer-near.png", s.view, 220, 1000, nil)
	s.near = 0
	// Pressed, it lights up.
	shoot("strip-footer-pressed.png", s.view, 220, 1000, func(tt *ui.Tester) {
		x, y := center(footerButton(220, 1000, a.panel(), false))
		tt.Move(x, y)
		tt.Frame()
		tt.Press(x, y)
	})
	// Under the pointer, the panel outlines in the accent the settings
	// choose, or in the plain edge once it is turned off.
	a.settings.Accent = "#bf5af2"
	shoot("strip-footer-accent.png", s.view, 220, 1000, func(tt *ui.Tester) {
		tt.Move(center(footerButton(220, 1000, a.panel(), false)))
	})
	a.settings.Accent = settings.AccentNone
	shoot("strip-footer-plain.png", s.view, 220, 1000, func(tt *ui.Tester) {
		tt.Move(center(footerButton(220, 1000, a.panel(), false)))
	})
	a.settings.Accent = settings.Default().Accent
	// A narrower, thinner panel, as the settings may size it.
	a.settings.PanelWidth, a.settings.PanelHeight = 0.5, 20
	shoot("strip-footer-small.png", s.view, 220, 1000, func(tt *ui.Tester) {
		tt.Move(center(footerButton(220, 1000, a.panel(), false)))
	})
	a.settings.PanelWidth, a.settings.PanelHeight = settings.Default().PanelWidth, settings.Default().PanelHeight
	// The overview of that display, spread out, the pointer on a window.
	o := &overview{a: a, d: d, rect: d.mon.Work, scale: 1, phase: ovOpen, held: true, flying: true, spreadFrom: 1, spreadTo: 1}
	var ws []catalog.Window
	var items []overviewItem
	for _, g := range groups {
		for _, w := range g.windows {
			items = append(items, overviewItem{w: float32(w.Frame.W()), h: float32(w.Frame.H()), cx: float32(len(ws) * 37 % 300), cy: float32(len(ws) * 140)})
			ws = append(ws, w)
		}
	}
	slots := layoutOverview(ui.Rect{X: overviewMargin, Y: overviewMargin, W: 2560 - 2*overviewMargin, H: 1392 - overviewMargin - overviewBottom},
		items, overviewGapX, overviewGapY)
	for i, w := range ws {
		t := ovTile{w: w, name: w.App, icon: a.media.icons[w.App]}
		if st := a.media.stills[w.HWND]; st != nil {
			t.still = st.card
		}
		t.slot = o.screen(slots[i])
		t.from, t.to, t.fromOp, t.toOp, t.fromSh, t.toSh = t.slot, t.slot, 1, 1, overviewShade, overviewShade
		o.tiles = append(o.tiles, t)
	}
	shoot("overview.png", o.view, 2560, 1392, func(tt *ui.Tester) {
		x, y, w, h := o.local(o.tiles[2].slot)
		tt.Move(x+w/2, y+h/2)
	})
	// The mark in the hot corner, over a dark and a light taskbar: on the
	// pointer's approach, under it, and pressed.
	hint := &cornerHint{a: a, d: d, visible: true}
	for _, light := range []bool{false, true} {
		hint.light = light
		bar, theme := "#1c1c1c", "dark"
		if light {
			bar, theme = "#eef0f3", "light"
		}
		corner := func(name string, near float32, setup func(*ui.Tester)) {
			hint.near = near
			shoot(fmt.Sprintf("corner-%s-%s.png", theme, name), func(c *ui.Context) {
				ui.Box(c).Absolute().Left(0).Top(0).Size(hotCorner, hotCorner).Background(ui.Hex(bar))
				hint.view(c)
			}, hotCorner, hotCorner, setup)
		}
		corner("near", 0.5, nil)
		corner("hover", 1, func(tt *ui.Tester) { tt.Move(24, 24) })
		corner("pressed", 1, func(tt *ui.Tester) {
			tt.Move(24, 24)
			tt.Frame()
			tt.Press(24, 24)
		})
		// As wide as the settings may make it, under the pointer.
		const wide = 200
		hint.near = 1
		shoot(fmt.Sprintf("corner-%s-wide.png", theme), func(c *ui.Context) {
			ui.Box(c).Absolute().Left(0).Top(0).Size(wide, hotCorner).Background(ui.Hex(bar))
			hint.view(c)
		}, wide, hotCorner, func(tt *ui.Tester) { tt.Move(wide/2, hotCorner/2) })
	}
	// One companion has a window open, and shows as its app.
	a.settings.Companions = []string{"tortoiseproc", "notepad", `d:\tools\paint.exe`}
	sw := &settingsWindow{a: a}
	sw.load()
	dark := func(c *ui.Context) {
		c.SetTheme(ui.DarkTheme())
		sw.view(c)
	}
	for _, l := range i18n.Langs {
		a.settings.Language = l
		for i := range settingsIcons {
			sw.tab = i
			shoot(fmt.Sprintf("settings-%s-%d.png", l, i), sw.view, 620, 1000, nil)
			if l == i18n.SimplifiedChinese {
				shoot(fmt.Sprintf("settings-dark-%d.png", i), dark, 620, 1000, nil)
			}
		}
	}
	a.settings.Language = i18n.SimplifiedChinese
	sw.tab = 1
	shoot("settings-wide.png", sw.view, 1100, 600, nil)
}

func fakeWindow(w, h int, bg color.RGBA, k int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	dark := int(bg.R)+int(bg.G)+int(bg.B) < 300
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := bg
			switch {
			case y < 28: // title bar
				c = color.RGBA{bg.R / 2, bg.G / 2, bg.B / 2, 255}
				if !dark {
					c = color.RGBA{225, 228, 235, 255}
				}
			case x < w/5: // side bar
				c = color.RGBA{uint8(int(bg.R) * 9 / 10), uint8(int(bg.G) * 9 / 10), uint8(int(bg.B) * 9 / 10), 255}
			case (y/18)%2 == 0 && x > w/5+20 && x < w-20-(y*7+k*40)%(w/3):
				c = color.RGBA{90, 120, 200, 255}
				if dark {
					c = color.RGBA{150, 200, 255, 255}
				}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func fakeIcon(i int) *image.NRGBA {
	hues := []color.NRGBA{{0, 122, 204, 255}, {66, 133, 244, 255}, {0, 150, 136, 255}, {120, 120, 120, 255}, {233, 30, 99, 255}}
	img := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			dx, dy := x-64, y-64
			r := max(abs(dx), abs(dy))
			if r < 54 || dx*dx+dy*dy < 60*60 && r < 60 {
				img.SetNRGBA(x, y, hues[i%len(hues)])
			}
		}
	}
	return img
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
