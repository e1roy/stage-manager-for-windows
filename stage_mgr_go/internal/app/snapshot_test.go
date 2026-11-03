package app

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/i18n"
	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/update"
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
	// Back from 画图 to Notepad, the card of 画图 is marked as the stage
	// last left.
	names := []string{}
	for _, g := range groups {
		names = append(names, g.app)
	}
	d.model.Strip(names)
	d.model.Switch([]string{"画图"}, time.Now())
	d.model.Switch([]string{"Notepad"}, time.Now())
	shoot("strip-previous.png", s.view, 220, 1000, nil)
	// With every stage listed, the stage's own card is a ghost in its slot,
	// and the card of the stage last left is outlined among the others.
	a.settings.AllStages = true
	d.sets[2].staged = true
	shoot("strip-staged.png", s.view, 220, 1000, nil)
	a.settings.StagedOpacity, a.settings.StagedGray = 0.7, 0.3
	shoot("strip-staged-light.png", s.view, 220, 1000, nil)
	a.settings.StagedOpacity, a.settings.StagedGray = settings.Default().StagedOpacity, settings.Default().StagedGray
	// Of several apps, with their windows stacked, it fades as one.
	d.sets[2].staged, d.sets[0].staged = false, true
	shoot("strip-staged-set.png", s.view, 220, 1000, nil)
	d.sets[0].staged, a.settings.AllStages = false, false
	// The badge of the stage's card as it is, and larger to see how it is made.
	shoot("badge-large.png", func(c *ui.Context) {
		ui.Box(c).Absolute().Left(0).Top(0).Size(300, 130).Background(ui.Hex("#4a6178"))
		for _, r := range []ui.Rect{{X: 20, Y: 17, W: 96, H: 96}, {X: 150, Y: 49, W: 32, H: 32}, {X: 210, Y: 54, W: 22, H: 22}, {X: 250, Y: 59, W: 12, H: 12}} {
			ui.Box(c.Key(r.X)).Absolute().Left(0).Top(0).Size(300, 130).PassThrough().Children(func() { stageBadge(c, r, 1, 0) })
		}
	}, 300, 130, nil)
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
	// A stage of several windows lists them at the top of the strip. DWM
	// draws their pictures in the app; here their stills stand in.
	var stage []*group
	for i, ap := range []struct {
		name   string
		titles []string
		aspect float64
		hue    color.RGBA
		bars   []color.RGBA
	}{
		// Two VS Code windows with title bars of their own colors take
		// tiles of those colors; a page of black and white takes none.
		{"Terminal", []string{"NewKing - jvm.g1_10001.options", "aoemai [SSH: devcloud]"}, 16.0 / 10, color.RGBA{24, 24, 30, 255},
			[]color.RGBA{{196, 98, 30, 255}, {52, 84, 196, 255}}},
		{"Edge", []string{"Stage Manager for Windows - GitHub"}, 16.0 / 9, color.RGBA{248, 248, 252, 255}, []color.RGBA{{248, 248, 252, 255}}},
		{"Notes", []string{"会议记录 - 记事本"}, 0.75, color.RGBA{250, 246, 225, 255}, []color.RGBA{{250, 246, 225, 255}}},
	} {
		g := &group{app: ap.name, name: ap.name, z: i}
		for j, title := range ap.titles {
			w := catalog.Window{HWND: h, Title: title, App: ap.name, Z: len(stage) + j, Frame: winapi.Rect{Right: int32(1000 * ap.aspect), Bottom: 1000}}
			// A dark editor of words in a few colors under its title bar.
			img := window(ap.hue, ap.bars[j], 0.06,
				color.RGBA{86, 156, 214, 255}, color.RGBA{206, 145, 120, 255}, color.RGBA{220, 220, 170, 255}, color.RGBA{200, 200, 200, 255})
			if cr, cg, cb, ok := dominantColor(img); ok {
				if s.line.tints == nil {
					s.line.tints = map[winapi.HWND]lineTint{}
				}
				var t lineTint
				t.bg, t.edge = tileTint(cr, cg, cb)
				s.line.tints[h] = t
			}
			bmp := ui.NewBitmap(img)
			a.media.stills[h] = &still{card: bmp, fly: bmp}
			g.windows = append(g.windows, w)
			h++
		}
		a.media.icons[ap.name] = ui.NewBitmap(fakeIcon(i + 2))
		stage = append(stage, g)
	}
	d.onStage = stage[:2]
	a.fg.prev = stage[0].windows[1].HWND
	shoot("strip-lineup.png", s.view, 220, 1000, nil)
	shoot("strip-lineup-hover.png", s.view, 220, 1000, func(tt *ui.Tester) {
		tt.Move(center(layoutLineup(3, 220, 1000, false).tiles[2]))
	})
	a.settings.Sides = map[string]settings.Side{d.mon.Device: settings.Right}
	shoot("strip-lineup-right.png", s.view, 220, 1000, nil)
	a.settings.Sides = nil
	// More windows than the list has room for, on a shorter display.
	more := *stage[2]
	for i := 0; i < 8; i++ {
		w := more.windows[0]
		w.HWND = h
		a.media.stills[h] = a.media.stills[more.windows[0].HWND]
		more.windows = append(more.windows, w)
		h++
	}
	d.onStage = []*group{stage[0], stage[1], &more}
	shoot("strip-lineup-many.png", s.view, 220, 760, nil)
	d.onStage, a.fg.prev = nil, 0
	s.line = lineup{}
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
	// Joined to the strip's panel above it, the corner button makes one
	// button with it, lit as one: the bottom of the strip, the corner
	// over a dark taskbar below it, put together as the screen shows them.
	hint.light, hint.near = false, 1
	a.hint = hint
	d.mon.Monitor = winapi.Rect{Right: 2560, Bottom: 1048}
	s.rect = winapi.Rect{Right: 220, Bottom: 1000}
	s.joined = true
	joint := func(name string, setup func(*ui.Tester)) {
		run := func(view func(*ui.Context), w, h int, setup func(*ui.Tester)) image.Image {
			tt := ui.NewTester(view, w, h)
			tt.Frame()
			if setup != nil {
				setup(tt)
			}
			for i := 0; i < 30; i++ {
				tt.Advance(33_000_000)
				tt.Frame()
			}
			return tt.Image()
		}
		top := run(func(c *ui.Context) {
			w, h := c.Size()
			ui.Box(c).Absolute().Left(0).Top(0).Size(w, h).Background(ui.Hex("#4a6178"))
			s.view(c)
		}, 220, 1000, setup)
		bw := int(math.Ceil(float64(s.jointWidth())))
		below := run(func(c *ui.Context) {
			ui.Box(c).Absolute().Left(0).Top(0).Size(float32(bw), hotCorner).Background(ui.Hex("#1c1c1c"))
			hint.view(c)
		}, bw, hotCorner, nil)
		const crop = 160
		out := image.NewRGBA(image.Rect(0, 0, 320, crop+hotCorner))
		for y := 0; y < crop+hotCorner; y++ {
			for x := 0; x < 320; x++ {
				var px color.Color = color.RGBA{0x4a, 0x61, 0x78, 255}
				switch {
				case y < crop && x < 220:
					px = top.At(x, 1000-crop+y)
				case y >= crop && x < bw:
					px = below.At(x, y-crop)
				case y >= crop:
					px = color.RGBA{0x1c, 0x1c, 0x1c, 255}
				}
				out.Set(x, y, px)
			}
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		png.Encode(f, out)
	}
	at := jointButton(220, 1000, a.panel())
	// The glass blurs what is behind: here a window's corner over the
	// wallpaper, which the button's top half covers.
	behind := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			px := color.RGBA{0x4a, 0x61, 0x78, 255}
			if x > 150 && y < 150 {
				px = color.RGBA{235, 235, 240, 255}
			}
			if y >= 200 {
				px = color.RGBA{0x1c, 0x1c, 0x1c, 255}
			}
			behind.Set(x, y, px)
		}
	}
	s.glass.bmp = ui.NewBitmap(frostImage(behind, image.Rect(0, 112, 200, 248), glassSigma(a.settings.PanelBlur, 1)))
	joint("joint-hover.png", func(tt *ui.Tester) { tt.Move(at.X+at.W/2, at.Y+at.H/2) })
	s.near = 1
	joint("joint-near.png", nil)
	joint("joint-pressed.png", func(tt *ui.Tester) {
		tt.Move(at.X+at.W/2, at.Y+at.H/2)
		tt.Frame()
		tt.Press(at.X+at.W/2, at.Y+at.H/2)
	})
	s.near, s.joined, a.hint, s.glass = 0, false, nil, glass{}
	s.footHover, s.footPress = false, false
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
	// The pointer resting on the info mark after a title shows its tip.
	tipAt := func(tt *ui.Tester) {
		if r, ok := tt.Find(a.tr().CardsAt); ok {
			tt.Move(r.X+r.W+6+8, r.Y+r.H/2)
		}
	}
	shoot("settings-tip.png", sw.view, 620, 1000, tipAt)
	shoot("settings-tip-dark.png", dark, 620, 1000, tipAt)
	// A newer release puts a red dot on the about pane's icon, and its
	// version on the pane.
	a.latest = update.Release{Version: "9.9.9", URL: update.ReleasesURL}
	a.published = a.latest
	for _, i := range []int{0, aboutTab} {
		sw.tab = i
		shoot(fmt.Sprintf("settings-update-%d.png", i), sw.view, 620, 1000, nil)
		shoot(fmt.Sprintf("settings-update-dark-%d.png", i), dark, 620, 1000, nil)
	}
	a.latest = update.Release{}
	// Up to date, the pane still says which version is the latest.
	sw.tab = aboutTab
	shoot("settings-update-current.png", sw.view, 620, 1000, nil)
	// Without a network, the pane says it could not tell.
	a.checkFailed = true
	sw.tab = aboutTab
	shoot("settings-update-failed.png", sw.view, 620, 1000, nil)
	a.checkFailed = false
	// The tray icon with the dot, as the tray shows it.
	if icon, err := os.ReadFile(filepath.Join("..", "..", "resources", "icon.png")); err == nil {
		if data, err := dotted(icon, dottedSize); err == nil {
			os.WriteFile(filepath.Join(dir, "tray-update.png"), data, 0o644)
		}
	}
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
