package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
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
			w := catalog.Window{HWND: h, Title: ap.name, App: ap.name, Frame: winapi.Rect{Right: int32(1000 * ap.aspect), Bottom: 1000}}
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
			// The wallpaper behind the transparent strip.
			w, h := c.Size()
			ui.Box(c).Absolute().Left(0).Top(0).Size(w, h).Background(ui.Hex("#4a6178"))
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
	sw := &settingsWindow{a: a}
	sw.load()
	shoot("settings.png", sw.view, 620, 1100, nil)
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
