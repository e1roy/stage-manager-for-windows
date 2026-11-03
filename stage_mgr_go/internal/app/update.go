package app

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/update"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo"
	xdraw "golang.org/x/image/draw"
)

const (
	// firstCheck waits out the start, when the stages are set up, before
	// going online.
	firstCheck = 10 * time.Second
	// checkEvery looks again while the app runs, as it may run for weeks:
	// often enough to learn of a release the same day, rarely enough to
	// stay far under GitHub's 60 requests an hour without a token.
	checkEvery = 6 * time.Hour
	// checkTimeout gives up on a slow network; the next check tries again.
	checkTimeout = 15 * time.Second
	// dottedSize is the size of the tray icon with the dot: the tray shows
	// icons at 16 to 32 pixels, and a 1024-pixel one would make the dot's
	// edge, and the ring around it, blur away as Windows shrinks it.
	dottedSize = 64
)

// dotColor is the red of a badge on a phone's app icon, which reads as
// "something new" on a light or a dark taskbar alike.
var dotColor = color.NRGBA{R: 0xff, G: 0x3b, B: 0x30, A: 0xff}

// startUpdateChecks asks GitHub for the latest version soon after the
// start and then every checkEvery, while the settings allow it.
func (a *App) startUpdateChecks() {
	after(firstCheck, a.checkUpdate)
	ticker := time.NewTicker(checkEvery)
	go func() {
		for range ticker.C {
			post(a.checkUpdate)
		}
	}()
}

// checkUpdate asks GitHub for the latest release, off the main thread, and
// shows it once it comes back if it is newer than the one running.
func (a *App) checkUpdate() {
	// A build without a version, as "go run" makes, has nothing to compare
	// a release with and stays offline.
	current := mygo.App.Version()
	if !a.settings.CheckUpdates || a.checking || current == "" {
		return
	}
	a.checking = true
	// The about pane's button greys out until the check is done.
	a.redrawSettings()
	// The goroutine makes the dotted icon too, which takes a while at the
	// 1024 pixels of the icon; trayIcon never changes.
	src := a.trayIcon
	if a.dotted != nil {
		src = nil
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
		defer cancel()
		// Read at every check: a proxy client turns the system proxy on
		// and off as it starts and quits.
		server, bypass := winapi.SystemProxy()
		debugf("update: system proxy %q, bypass %q", server, bypass)
		r, err := update.Latest(ctx, update.VersionURL, update.SystemProxy(server, bypass))
		if err != nil {
			// Offline, or GitHub out of reach: as if there had been no
			// check, whatever the last one found stays. The about pane
			// says it could not tell and links to the releases instead.
			debugf("update: %v", err)
			post(func() {
				a.checking = false
				a.checkFailed = true
				a.redrawSettings()
			})
			return
		}
		newer := update.Newer(r.Version, current)
		debugf("update: latest %s, running %q, newer %v", r.Version, current, newer)
		var icon []byte
		if newer && src != nil {
			if icon, err = dotted(src, dottedSize); err != nil {
				debugf("update: tray icon: %v", err)
			}
		}
		found := r
		post(func() {
			a.checking = false
			if a.checkFailed || found != a.published {
				a.checkFailed = false
				a.published = found
			}
			// The about pane's button comes back, and says what it found.
			a.redrawSettings()
			if icon != nil {
				a.dotted = icon
			}
			if !newer {
				r = update.Release{}
			}
			if r != a.latest {
				a.latest = r
				a.showUpdate()
			}
		})
	}()
}

// newer returns the release newer than the one running, if there is one
// and the settings show it.
func (a *App) newer() (update.Release, bool) {
	return a.latest, a.settings.CheckUpdates && a.latest.Version != ""
}

// unchecked reports whether the about pane says the last check could not
// reach GitHub: only while checks are on and none has found a release.
func (a *App) unchecked() bool {
	_, ok := a.newer()
	return a.settings.CheckUpdates && a.checkFailed && !ok
}

func (a *App) redrawSettings() {
	if a.settingsUI != nil {
		a.settingsUI.win.Invalidate()
	}
}

// showUpdate puts the red dot on the tray icon, and the new version in its
// tooltip and menu, or takes them away; the settings window draws its own
// dot from newer the next time it draws.
func (a *App) showUpdate() {
	r, ok := a.newer()
	if a.tray != nil {
		icon, tip := a.trayIcon, a.tr().AppName
		if ok {
			if a.dotted != nil {
				icon = a.dotted
			}
			tip += "\n" + fmt.Sprintf(a.tr().NewVersion, r.Version)
		}
		if icon != nil {
			a.tray.SetIcon(icon, false)
		}
		a.tray.SetToolTip(tip)
		a.tray.SetMenu(a.trayMenu())
	}
	a.redrawSettings()
}

// openRelease opens the page of the newer release, where its installer
// is, in the browser.
func (a *App) openRelease() {
	r, ok := a.newer()
	if !ok || r.URL == "" {
		return
	}
	go mygo.Shell.OpenExternal(r.URL)
}

// dotMenuIcon returns the red dot beside the tray menu's update item,
// made the first time.
func (a *App) dotMenuIcon() *mygo.MenuIcon {
	if a.dotIcon == nil {
		img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
		paintDot(img, 16, 16, 7, 0)
		var buf bytes.Buffer
		if png.Encode(&buf, img) == nil {
			a.dotIcon, _ = mygo.NewMenuIcon(buf.Bytes(), false)
		}
	}
	return a.dotIcon
}

// dotted returns the PNG icon at size pixels with the red dot of an update
// at its top right, cut out of the icon by a thin clear ring so that it
// stands apart from whatever it covers, as a badge on a phone's app icon.
func dotted(icon []byte, size int) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(icon))
	if err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(img, img.Rect, src, src.Bounds(), xdraw.Src, nil)
	s := float64(size)
	paintDot(img, s*0.76, s*0.24, s*0.22, s*0.07)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// paintDot paints a red dot of radius r centered on cx, cy, its edge
// smoothed over a pixel, after clearing a ring ring wide around it.
func paintDot(img *image.NRGBA, cx, cy, r, ring float64) {
	b := img.Rect
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			clear := clamp01(r + ring + 0.5 - d)
			fill := clamp01(r + 0.5 - d)
			if clear == 0 {
				continue
			}
			p := img.NRGBAAt(x, y)
			a := float64(p.A) / 255 * (1 - clear)
			// The dot goes over what the ring left, non-premultiplied.
			out := fill + a*(1-fill)
			if out == 0 {
				img.SetNRGBA(x, y, color.NRGBA{})
				continue
			}
			mix := func(dot, under uint8) uint8 {
				return uint8(math.Round((float64(dot)*fill + float64(under)*a*(1-fill)) / out))
			}
			img.SetNRGBA(x, y, color.NRGBA{
				R: mix(dotColor.R, p.R),
				G: mix(dotColor.G, p.G),
				B: mix(dotColor.B, p.B),
				A: uint8(math.Round(out * 255)),
			})
		}
	}
}

func clamp01(v float64) float64 { return min(max(v, 0), 1) }
