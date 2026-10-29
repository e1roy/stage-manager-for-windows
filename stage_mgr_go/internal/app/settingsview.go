package app

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/i18n"
	"github.com/e1roy/stage_manager_for_windows/internal/settings"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// settingsWindow edits a copy of the settings and applies every change at
// once, as Windows' own Settings does.
type settingsWindow struct {
	a       *App
	win     *mygo.Window
	s       settings.Settings
	mode    int
	quality int
	// tab is the pane shown, an index into settingsIcons.
	tab   int
	width float64
	icon  float64
	tilt  float64
	// spacing is the gap between cards, as the slider sets it.
	spacing float64
	// panelW and panelH size the overview's panel, as the sliders set them,
	// and cornerW the button over a display's bottom-left corner.
	panelW, panelH float64
	cornerW        float64
	// slide is how fast a floating strip slides, as the slider sets it.
	slide float64
	login bool
	icon0 *ui.Bitmap
	// fileIcons are the icons of the companions listed by path, by path.
	fileIcons map[string]*ui.Bitmap
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
		Title:     a.tr().SettingsTitle,
		Width:     620,
		Height:    560,
		MinWidth:  480,
		MinHeight: minSettingsHeight,
		Vibrancy:  mygo.VibrancyMica,
		StateKey:  "settings",
		Content:   ui.View(sw.view),
	})
	// The title bar and the taskbar show the icon the tray and the page
	// show, whether or not the build embedded it in the program.
	if a.trayIcon != nil {
		sw.win.SetIcon(a.trayIcon)
	}
	// The window keeps the size the user gives it, as StateKey remembers
	// it; one remembered from a build that fit it to its pane too short
	// starts over.
	if w, h := sw.win.ContentSize(); h < minSettingsHeight {
		sw.win.SetContentSize(w, 560)
	}
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
	sw.quality = slices.Index(qualities, sw.s.Quality)
	sw.width, sw.icon, sw.tilt, sw.spacing = sw.s.Width, sw.s.IconScale, float64(sw.s.Tilt), sw.s.Spacing
	sw.panelW, sw.panelH = sw.s.PanelWidth, sw.s.PanelHeight
	sw.cornerW = sw.s.CornerWidth
	sw.slide = sw.s.SlideSpeed
}

// running returns the apps on the stages and in the strips, one per
// executable: those the companions can be picked from.
func (sw *settingsWindow) running() []*group {
	var out []*group
	seen := map[string]bool{}
	add := func(g *group) {
		if exe := strings.ToLower(g.front().Exe); exe != "" && !seen[exe] {
			seen[exe] = true
			out = append(out, g)
		}
	}
	for _, d := range sw.a.displays {
		for _, g := range d.onStage {
			add(g)
		}
		for _, s := range d.sets {
			for _, g := range s.apps {
				add(g)
			}
		}
	}
	return out
}

// minSettingsHeight keeps the window tall enough for the toolbar and a
// few rows.
const minSettingsHeight = 420

// qualities are the choices of the quality control, in its order.
var qualities = []settings.Quality{settings.Low, settings.Medium, settings.High}

// repo is where the project lives, for the links of the about pane.
const repo = "https://github.com/e1roy/stage-manager-for-windows"

func (sw *settingsWindow) apply() {
	sw.a.applySettings(sw.s)
	sw.s = sw.a.settings.Clone()
}

// The icons of the panes, as the toolbar of a macOS preferences window
// shows them over their names, one pane at a time. They are drawn in the
// text color, so they follow the theme and the choice.
var settingsIcons = []*ui.SVG{
	lineIcon(`<path d="M4 7h9M17 7h3M4 17h3M11 17h9"/><circle cx="15" cy="7" r="2"/><circle cx="9" cy="17" r="2"/>`),
	lineIcon(`<rect x="3" y="4" width="18" height="16" rx="2.5"/><path d="M9 4v16M5.5 8h1M5.5 12h1M5.5 16h1"/>`),
	lineIcon(`<rect x="3" y="4" width="18" height="16" rx="2.5"/><rect x="9" y="8" width="9" height="8" rx="1"/><path d="M5.5 9.5v1M5.5 13.5v1"/>`),
	lineIcon(`<path d="M15 9V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h4"/><rect x="9" y="9" width="12" height="12" rx="2"/>`),
	lineIcon(`<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 7.5v.01"/>`),
}

var (
	appIcon    = lineIcon(`<rect x="3" y="4" width="18" height="16" rx="2.5"/><path d="M3 9h18"/>`)
	removeIcon = lineIcon(`<path d="M6 6l12 12M18 6L6 18"/>`)
)

// lineIcon makes a 24×24 outline icon of the shapes in body.
func lineIcon(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

// form lays out the rows of a pane as Windows' Settings does: groups of
// rows on cards, each row a title, a description and a control.
type form struct {
	t    *ui.Theme
	card ui.Color
}

// option is a line under a row's description that explains one of its
// choices, set apart so that a row of several reads at a glance; on marks
// the one chosen. Without a label it is a line of its own.
type option struct {
	label, text string
	on          bool
}

// rowPadX is the inset of every row in a card, which lines up the text of
// all of them, and their controls.
const rowPadX = 18

// section is a card of rows. A pane's first card goes without a title,
// as the pane's name in the toolbar says it; trailing, if any, goes at the
// end of the title, as a button acting on the whole card.
func (f form) section(c *ui.Context, title string, rows func(), trailing ...func()) {
	if title == "" {
		ui.Box(c).Height(14)
	} else {
		ui.Row(c).AlignItems(ui.Center).Margin(16, 0, 6, 2).Children(func() {
			ui.Text(c, title).FontSize(14).Bold().Grow(1)
			for _, fn := range trailing {
				fn()
			}
		})
	}
	ui.Column(c).Background(f.card).Radius(8).Border(1, f.t.Border.Alpha(0.6)).Dividers(1, f.t.Border.Alpha(0.5)).Children(rows)
}

func (f form) row(c *ui.Context, title, desc string, control func(), opts ...option) {
	f.rowAt(c, false, title, desc, control, opts...)
}

// child is a row of a setting that only applies while the one above it is
// on, dimmed while it is off. It keeps the left edge every row's text
// shares rather than indent under it.
func (f form) child(c *ui.Context, on bool, title, desc string, control func()) {
	f.rowAt(c, !on, title, desc, control)
}

func (f form) rowAt(c *ui.Context, dim bool, title, desc string, control func(), opts ...option) {
	t := f.t
	fg := t.Text
	if dim {
		fg = t.TextMuted
	}
	ui.Row(c).Padding(14, rowPadX).Gap(18).AlignItems(ui.Center).MinHeight(64).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
			ui.Text(c, title).TextColor(fg)
			if desc != "" {
				ui.Text(c, desc).FontSize(12).TextColor(t.TextMuted)
			}
			if len(opts) == 0 {
				return
			}
			ui.Grid(c).ColumnTracks(ui.FitContent(), ui.Fr(1)).GapX(10).GapY(3).Margin(3, 0, 0, 0).Children(func() {
				for _, o := range opts {
					if o.label == "" {
						ui.Text(c, o.text).FontSize(12).TextColor(t.TextMuted).ColumnSpan(-1)
						continue
					}
					fg := t.Text
					if o.on {
						fg = t.Accent
					}
					ui.Text(c, o.label).FontSize(12).Bold().TextColor(fg)
					ui.Text(c, o.text).FontSize(12).TextColor(t.TextMuted)
				}
			})
		})
		control()
	})
}

// The sliders' width, and how far the built-in slider keeps the middle of
// its thumb, and its ticks, from its ends.
const sliderWidth, sliderInset = 180, 10

// slider sets *v between lo and hi, in steps of step when it is not 0,
// with the value beside it, where the eye already is, and a mark under
// the default, which tells it and goes back to it when clicked; label
// writes a value as the row shows it. It reports a change of *v.
func (f form) slider(c *ui.Context, tr *i18n.Strings, v *float64, lo, hi, step, def float64, label func(float64) string) bool {
	t := f.t
	changed := false
	// Padded above as the mark, and the ticks of a slider in steps, are
	// tall below, the track stays in the middle of the row, beside the
	// value.
	pad := float32(8)
	if step > 0 {
		pad += 7
	}
	ui.Column(c).Shrink(0).Padding(pad, 0, 0, 0).Children(func() {
		var s ui.Element
		if step > 0 {
			s = ui.StepSlider(c, v, lo, hi, step)
		} else {
			s = ui.Slider(c, v, lo, hi)
		}
		changed = s.Width(sliderWidth).Changed()
		ui.Box(c).Width(sliderWidth).Height(8).Children(func() {
			x := sliderInset + float32((def-lo)/(hi-lo))*(sliderWidth-2*sliderInset)
			mark := ui.Box(c).Absolute().Left(x-6).Top(0).Size(12, 8).Tooltip(fmt.Sprintf(tr.DefaultValue, label(def)))
			at := math.Abs(*v-def) < (hi-lo)/200
			fg := t.TextMuted
			switch {
			case mark.Hovered():
				fg = t.Text
			case at:
				fg = t.Accent
			}
			mark.Draw(func(p *ui.Painter, r ui.Rect) {
				cx := r.X + r.W/2
				p.FillPath(new(ui.Path).MoveTo(cx, r.Y+1).LineTo(cx+4, r.Y+7).LineTo(cx-4, r.Y+7).Close(), fg)
			})
			if mark.Clicked() && !at {
				*v, changed = def, true
			}
		})
	})
	// Right-aligned, the value ends where the switches of other rows do.
	ui.Text(c, label(*v)).FontSize(12).TextColor(t.TextMuted).Width(64).Shrink(0).SingleLine().TextAlign(ui.End)
	return changed
}

func (sw *settingsWindow) view(c *ui.Context) {
	t, tr := c.Theme(), sw.a.tr()
	if sw.notice != "" {
		c.Toast(sw.notice)
		sw.notice = ""
	}
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	f := form{t: t, card: t.Surface}
	if c.Vibrancy() {
		f.card = ui.RGBA(255, 255, 255, 0.72)
		if t.Dark {
			f.card = ui.RGBA(255, 255, 255, 0.055)
		}
	}

	names := []string{tr.TabGeneral, tr.TabStrip, tr.TabStage, tr.TabCompanions, tr.TabAbout}
	ui.Column(c).Fill().Children(func() {
		ui.Column(c).Children(func() {
			tabs := ui.TabsBase(c, &sw.tab, len(names))
			tabs.List.Justify(ui.Center).Gap(2).Padding(10, 16, 8, 16).Children(func() {
				for i, name := range names {
					tab := tabs.Tab(i).Column().AlignItems(ui.Center).Gap(3).MinWidth(64).Padding(6, 10, 5, 10).Radius(6)
					fg := t.TextMuted
					switch {
					case i == sw.tab:
						tab.Background(t.Text.Alpha(0.07))
						fg = t.Accent
					case tab.Hovered():
						tab.Background(t.Text.Alpha(0.04))
					}
					tab.Children(func() {
						ui.Icon(c, settingsIcons[i]).FontSize(22).TextColor(fg)
						ui.Text(c, name).FontSize(12).TextColor(fg).SingleLine()
					})
				}
			})
			ui.Box(c).Height(1).Background(t.Border.Alpha(0.6))
		})
		// Each pane keeps its own scroll position.
		c.Key(sw.tab)
		ui.Scroll(c).Grow(1).Children(func() {
			// The pane is centered under the toolbar, which is.
			ui.Column(c).WidthPercent(100).MaxWidth(760).Margin(0, ui.Auto).Padding(0, 30, 28, 30).Gap(4).Children(func() {
				switch sw.tab {
				case 0:
					sw.generalPane(c, f, tr)
				case 1:
					sw.stripPane(c, f, tr)
				case 2:
					sw.stagePane(c, f, tr)
				case 3:
					sw.companionsPane(c, f, tr)
				case 4:
					sw.aboutPane(c, f, tr)
				}
			})
		})
	})
}

func (sw *settingsWindow) generalPane(c *ui.Context, f form, tr *i18n.Strings) {
	f.section(c, "", func() {
		f.row(c, tr.Language, "", func() {
			// The languages show in their own words, so that whoever
			// reads the list finds theirs; following Windows, in the words
			// of the language shown now.
			langs := append([]i18n.Lang{i18n.Auto}, i18n.Langs...)
			names := make([]string, len(langs))
			for i, l := range langs {
				names[i] = l.Name()
			}
			names[0] = tr.LanguageSystem
			cur := names[max(slices.Index(langs, sw.s.Language), 0)]
			if ui.Select(c, &cur, names).Width(180).Changed() {
				sw.s.Language = langs[max(slices.Index(names, cur), 0)]
				sw.apply()
			}
		})
		f.row(c, tr.LaunchAtLogin, tr.LaunchAtLoginDesc, func() {
			if ui.Switch(c, &sw.login).Changed() {
				on, format := sw.login, tr.LaunchAtLoginError
				go func() {
					if err := mygo.App.SetOpenAtLogin(on); err != nil {
						msg := fmt.Sprintf(format, err)
						sw.win.Update(func() { sw.login, sw.notice = !on, msg })
					}
				}()
			}
		})
	})

	f.section(c, tr.Displays, func() {
		for i, d := range sw.a.displays {
			m := d.mon
			desc := fmt.Sprintf(tr.DisplayDesc, m.Monitor.W(), m.Monitor.H(), m.Scale()*100)
			if m.Primary {
				desc += tr.DisplayPrimary
			}
			on := sw.s.DisplayEnabled(m.Device)
			c.Key(m.Device)
			f.row(c, fmt.Sprintf(tr.Display, i+1), desc, func() {
				// Which edge the strip keeps to, beside whether there is
				// one: a display whose neighbor is on its left may want it
				// on the right, away from where the pointer crosses over.
				side := 0
				if sw.s.SideOf(m.Device) == settings.Right {
					side = 1
				}
				if ui.Segmented(c.Key(m.Device+"/side"), &side, tr.SideLeft, tr.SideRight).Disabled(!on).Changed() {
					if sw.s.Sides == nil {
						sw.s.Sides = map[string]settings.Side{}
					}
					sw.s.Sides[m.Device] = settings.Left
					if side == 1 {
						sw.s.Sides[m.Device] = settings.Right
					}
					sw.apply()
				}
				if ui.Switch(c.Key(m.Device), &on).Changed() {
					if sw.s.Displays == nil {
						sw.s.Displays = map[string]bool{}
					}
					sw.s.Displays[m.Device] = on
					sw.apply()
				}
			})
		}
	}, func() {
		if ui.Button(c, tr.Identify).Clicked() {
			sw.a.identifyDisplays()
		}
	})

	f.section(c, tr.Troubleshooting, func() {
		f.row(c, tr.Rescue, tr.RescueDesc, func() {
			if ui.Button(c, tr.RescueButton).Clicked() {
				if n := sw.a.rescueOffscreen(); n > 0 {
					c.Toast(fmt.Sprintf(tr.RescueDone, n))
				} else {
					c.Toast(tr.RescueNone)
				}
			}
		})
	})
}

func (sw *settingsWindow) stripPane(c *ui.Context, f form, tr *i18n.Strings) {
	t, def := f.t, settings.Default()
	px := func(v float64) string { return fmt.Sprintf(tr.WidthValue, v) }
	f.section(c, "", func() {
		f.row(c, tr.Mode, tr.ModeDesc, func() {
			if ui.Segmented(c, &sw.mode, tr.Fixed, tr.Floating).Changed() {
				sw.s.Mode = settings.Fixed
				if sw.mode == 1 {
					sw.s.Mode = settings.Floating
				}
				sw.apply()
			}
		}, option{tr.Fixed, tr.FixedDesc, sw.mode == 0}, option{tr.Floating, tr.FloatingDesc, sw.mode == 1})
		// A fixed strip never slides; the speed set meanwhile waits for it
		// to float.
		f.child(c, sw.mode == 1, tr.SlideSpeed, tr.SlideSpeedDesc, func() {
			times := func(v float64) string { return fmt.Sprintf("%.1f×", v) }
			if f.slider(c, tr, &sw.slide, settings.MinSlideSpeed, settings.MaxSlideSpeed, 0, def.SlideSpeed, times) {
				sw.slide = math.Round(sw.slide*10) / 10
				sw.s.SlideSpeed = sw.slide
				sw.apply()
			}
		})
		f.row(c, tr.Width, "", func() {
			if f.slider(c, tr, &sw.width, settings.MinWidth, settings.MaxWidth, 0, def.Width, px) {
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
		f.row(c, tr.Spacing, "", func() {
			if f.slider(c, tr, &sw.spacing, 0, settings.MaxSpacing, 0, def.Spacing, px) {
				sw.spacing = math.Round(sw.spacing)
				sw.s.Spacing = sw.spacing
				sw.apply()
			}
		})
		f.row(c, tr.IconSize, "", func() {
			pct := func(v float64) string { return fmt.Sprintf("%.0f%%", v*100) }
			if f.slider(c, tr, &sw.icon, settings.MinIconScale, settings.MaxIconScale, 0, def.IconScale, pct) {
				sw.s.IconScale = sw.icon
				sw.apply()
			}
		})
		steps := func(v float64) string {
			if n := int(math.Round(v)); n > 0 {
				return fmt.Sprint(n)
			}
			return tr.TiltOff
		}
		f.row(c, tr.Tilt, tr.TiltDesc, func() {
			// A card as the strip turns it, which shows the setting even
			// while the strip is behind this window. It is drawn turned by
			// hand: MyGo loses elements in perspective this far from the
			// left of a window.
			rest, _ := cardTilts(sw.s.Tilt)
			preview := ui.Box(c).Size(40, 30).Shrink(0)
			deg := preview.AnimateTo("tilt", rest, ui.Snappy)
			preview.Draw(func(p *ui.Painter, r ui.Rect) {
				rx, rh := turnedCard(r.W, r.H, deg, 120)
				mid := r.Y + r.H/2
				band := float32(6)
				face := new(ui.Path).MoveTo(r.X, r.Y).LineTo(r.X+rx, mid-rh).LineTo(r.X+rx, mid+rh).LineTo(r.X, r.Y+r.H).Close()
				bar := new(ui.Path).MoveTo(r.X, r.Y).LineTo(r.X+rx, mid-rh).LineTo(r.X+rx, mid-rh+band*rh*2/r.H).LineTo(r.X, r.Y+band).Close()
				p.FillPath(face, t.Accent.Alpha(0.12))
				p.FillPath(bar, t.Accent.Alpha(0.45))
				p.StrokePath(face, 1, t.Accent.Alpha(0.7))
			})
			if f.slider(c, tr, &sw.tilt, 0, settings.MaxTilt, 1, settings.DefaultTilt, steps) {
				if n := int(math.Round(sw.tilt)); n != sw.s.Tilt {
					sw.s.Tilt = n
					sw.apply()
				}
			}
		})
	})
	sw.panelSection(c, f, tr)
	sw.cornerSection(c, f, tr)
	// The third way into the overview, after the panel and the corner.
	f.section(c, "", func() {
		f.row(c, tr.WinTab, tr.WinTabDesc, func() {
			if ui.Switch(c, &sw.s.WinTab).Changed() {
				sw.apply()
			}
		})
	})
	sw.highlightRow(c, f, tr)
}

// panelSection sizes the overview's panel. The strips show it while it is
// sized, as it is hidden till the pointer comes near.
func (sw *settingsWindow) panelSection(c *ui.Context, f form, tr *i18n.Strings) {
	def := settings.Default()
	ui.Column(c).Gap(4).Margin(16, 0, 2, 2).Children(func() {
		ui.Text(c, tr.OverviewPanel).FontSize(14).Bold()
		ui.Text(c, tr.OverviewPanelDesc).FontSize(12).TextColor(f.t.TextMuted)
	})
	f.section(c, "", func() {
		pct := func(v float64) string { return fmt.Sprintf("%.0f%%", v*100) }
		px := func(v float64) string { return fmt.Sprintf(tr.WidthValue, v) }
		f.row(c, tr.Width, "", func() {
			if f.slider(c, tr, &sw.panelW, settings.MinPanelWidth, settings.MaxPanelWidth, 0, def.PanelWidth, pct) {
				sw.panelW = math.Round(sw.panelW*100) / 100
				sw.s.PanelWidth = sw.panelW
				sw.a.showPanel()
				sw.apply()
			}
		})
		f.row(c, tr.Height, "", func() {
			if f.slider(c, tr, &sw.panelH, settings.MinPanelHeight, settings.MaxPanelHeight, 0, def.PanelHeight, px) {
				sw.panelH = math.Round(sw.panelH)
				sw.s.PanelHeight = sw.panelH
				sw.a.showPanel()
				sw.apply()
			}
		})
	})
}

// cornerSection sizes the button over a display's bottom-left corner, which
// opens the same overview wherever a centered taskbar leaves the corner
// empty. The button shows while it is sized, as it is hidden till the
// pointer comes near.
func (sw *settingsWindow) cornerSection(c *ui.Context, f form, tr *i18n.Strings) {
	def := settings.Default()
	px := func(v float64) string { return fmt.Sprintf(tr.WidthValue, v) }
	ui.Column(c).Gap(4).Margin(16, 0, 2, 2).Children(func() {
		ui.Text(c, tr.OverviewCorner).FontSize(14).Bold()
		ui.Text(c, tr.OverviewCornerDesc).FontSize(12).TextColor(f.t.TextMuted)
	})
	f.section(c, "", func() {
		f.row(c, tr.Width, "", func() {
			if f.slider(c, tr, &sw.cornerW, settings.MinCornerWidth, settings.MaxCornerWidth, 0, def.CornerWidth, px) {
				sw.cornerW = math.Round(sw.cornerW)
				sw.s.CornerWidth = sw.cornerW
				sw.apply()
			}
		})
	})
}

// accents are the colors the overview's panel and corner button may take
// under the pointer, with their names. The first is the setting's default,
// the blue the overview rings with.
var accents = []struct {
	value string
	color ui.Color
	name  func(*i18n.Strings) string
}{
	{settings.AccentDefault, ui.RGBA(10, 132, 255, 1), func(tr *i18n.Strings) string { return tr.HighlightBlue }},
	{"#bf5af2", ui.RGBA(191, 90, 242, 1), func(tr *i18n.Strings) string { return tr.HighlightPurple }},
	{"#ff375f", ui.RGBA(255, 55, 95, 1), func(tr *i18n.Strings) string { return tr.HighlightPink }},
	{"#ff453a", ui.RGBA(255, 69, 58, 1), func(tr *i18n.Strings) string { return tr.HighlightRed }},
	{"#ff9f0a", ui.RGBA(255, 159, 10, 1), func(tr *i18n.Strings) string { return tr.HighlightOrange }},
	{"#30d158", ui.RGBA(48, 209, 88, 1), func(tr *i18n.Strings) string { return tr.HighlightGreen }},
}

// highlightRow picks the color of the borders the buttons of the overview
// show under the pointer, or none, which keeps the borders they had.
func (sw *settingsWindow) highlightRow(c *ui.Context, f form, tr *i18n.Strings) {
	t := f.t
	// A swatch is the color itself, ringed while it is the one chosen.
	swatch := func(key string, col ui.Color, on bool) ui.Element {
		el := ui.Box(c.Key(key)).Size(20, 20).Radius(5).Background(col).Cursor(ui.CursorPointer)
		if on {
			el.Border(2, t.Text)
		} else {
			el.Border(1, t.Border)
		}
		return el
	}
	f.section(c, "", func() {
		f.row(c, tr.Highlight, tr.HighlightDesc, func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				// None keeps the plain borders, which the mark says.
				none := ui.Box(c.Key(settings.AccentNone)).Size(20, 20).Radius(5).Background(t.Surface).
					Cursor(ui.CursorPointer).Tooltip(tr.HighlightNone)
				if sw.s.Accent == settings.AccentNone {
					none.Border(2, t.Text)
				} else {
					none.Border(1, t.Border)
				}
				none.Draw(func(p *ui.Painter, r ui.Rect) {
					p.StrokePath(new(ui.Path).MoveTo(r.X+4, r.Y+4).LineTo(r.X+r.W-4, r.Y+r.H-4), 1.5, t.TextMuted)
				})
				if none.Clicked() {
					sw.s.Accent = settings.AccentNone
					sw.apply()
				}
				for _, a := range accents {
					el := swatch(a.value, a.color, sw.s.Accent == a.value)
					if el.Tooltip(a.name(tr)).Clicked() {
						sw.s.Accent = a.value
						sw.apply()
					}
				}
			})
		})
	})
}

func (sw *settingsWindow) stagePane(c *ui.Context, f form, tr *i18n.Strings) {
	f.section(c, "", func() {
		f.row(c, tr.HideInactive, tr.HideInactiveDesc, func() {
			if ui.Switch(c, &sw.s.HideInactive).Changed() {
				sw.apply()
			}
		})
		desc := tr.ClickDesktopDesc
		if !sw.s.HideInactive {
			desc = tr.ClickDesktopNeedsHide
		}
		f.child(c, sw.s.HideInactive, tr.ClickDesktop, desc, func() {
			if ui.Switch(c, &sw.s.ClickDesktopClears).Disabled(!sw.s.HideInactive).Changed() {
				sw.apply()
			}
		})
	})
	f.section(c, tr.Animation, func() {
		f.row(c, tr.Quality, tr.QualityDesc, func() {
			if ui.Segmented(c, &sw.quality, tr.Low, tr.Medium, tr.High).Changed() {
				sw.s.Quality = qualities[sw.quality]
				sw.apply()
			}
		}, option{tr.Low, tr.LowDesc, sw.quality == 0}, option{tr.Medium, tr.MediumDesc, sw.quality == 1}, option{tr.High, tr.HighDesc, sw.quality == 2})
		f.row(c, tr.LightFlights, tr.LightFlightsDesc, func() {
			if ui.Switch(c, &sw.s.LightFlights).Changed() {
				sw.apply()
			}
		})
	})
}

// companion is an entry of the companions as the list shows it: the app's
// name and icon when one of its windows is open, else what the entry says.
type companion struct {
	entry, name, detail string
	icon                *ui.Bitmap
}

func (sw *settingsWindow) companions(tr *i18n.Strings) []companion {
	running := sw.running()
	var out []companion
	for _, e := range sw.s.Companions {
		it := companion{entry: e}
		byPath := strings.ContainsAny(e, `\/`)
		if byPath {
			it.name, it.detail = strings.TrimSuffix(filepath.Base(e), ".exe"), e
			if sw.fileIcons == nil {
				sw.fileIcons = map[string]*ui.Bitmap{}
			}
			if b, ok := sw.fileIcons[e]; ok {
				it.icon = b
			} else if img := winapi.FileIcon(e, 64); img != nil {
				it.icon = ui.NewBitmap(trimIcon(img))
				sw.fileIcons[e] = it.icon
			} else {
				sw.fileIcons[e] = nil
			}
		} else {
			it.name, it.detail = e, fmt.Sprintf(tr.CompanionByName, e)
		}
		for _, g := range running {
			if w := g.front(); settings.CompanionMatches(e, w.Exe) {
				it.name, it.icon = g.name, sw.a.media.icon(w)
				break
			}
		}
		out = append(out, it)
	}
	return out
}

// companionsPane lists the companions to remove and adds them from the
// apps at hand or from a file, rather than have their executables typed.
func (sw *settingsWindow) companionsPane(c *ui.Context, f form, tr *i18n.Strings) {
	t := f.t
	ui.Column(c).Gap(4).Margin(16, 0, 2, 2).Children(func() {
		ui.Text(c, tr.CompanionLabel).FontSize(14).Bold()
		ui.Text(c, tr.CompanionTip).FontSize(12).TextColor(t.TextMuted)
	})
	f.section(c, "", func() {
		items := sw.companions(tr)
		if len(items) == 0 {
			ui.Text(c, tr.CompanionsEmpty).FontSize(12).TextColor(t.TextMuted).Padding(18, rowPadX)
		}
		for _, it := range items {
			ui.Row(c).Key(it.entry).Padding(10, rowPadX).Gap(12).AlignItems(ui.Center).Children(func() {
				if it.icon != nil {
					ui.Image(c, it.icon).Size(28, 28)
				} else {
					ui.Box(c).Size(28, 28).Center().Children(func() {
						ui.Icon(c, appIcon).FontSize(22).TextColor(t.TextMuted)
					})
				}
				ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
					ui.Text(c, it.name).SingleLine()
					ui.Text(c, it.detail).FontSize(12).TextColor(t.TextMuted).SingleLine().Tooltip(it.detail)
				})
				c.Key(it.entry)
				if ui.Button(c, "").Label(tr.Remove).Tooltip(tr.Remove).Children(func() { ui.Icon(c, removeIcon).FontSize(14) }).Clicked() {
					sw.s.Companions = slices.DeleteFunc(slices.Clone(sw.s.Companions), func(e string) bool { return e == it.entry })
					sw.apply()
				}
			})
		}
	})
	ui.Row(c).Gap(14).AlignItems(ui.Center).Margin(10, 0, 0, 0).Children(func() {
		ui.MenuButton(c, tr.AddCompanion, func(m *ui.Menu) {
			for _, g := range sw.running() {
				if exe := g.front().Exe; !sw.s.Companion(exe) && m.Item(g.name).Chosen() {
					sw.s = sw.s.WithCompanion(exe, true)
					sw.apply()
				}
			}
			m.Separator()
			if m.Item(tr.Browse).Chosen() {
				sw.browseCompanion(tr)
			}
		})
		ui.Text(c, tr.CompanionsHint).FontSize(12).TextColor(t.TextMuted).Shrink(1)
	})
}

// browseCompanion adds the program chosen in a file dialog, by its name,
// as the menus add apps: every copy of it is one.
func (sw *settingsWindow) browseCompanion(tr *i18n.Strings) {
	opts := mygo.OpenDialogOptions{Parent: sw.win, Title: tr.AddCompanion, Filters: []mygo.FileFilter{{Name: tr.Programs, Extensions: []string{"exe"}}}}
	go func() {
		paths, err := mygo.Dialog.Open(opts)
		if err != nil || len(paths) == 0 {
			return
		}
		sw.win.Update(func() {
			sw.s = sw.s.WithCompanion(paths[0], true)
			sw.apply()
		})
	}()
}

// aboutPane is the app's own page, as the About window of a Mac app: who
// it is, which version, and where it lives.
func (sw *settingsWindow) aboutPane(c *ui.Context, f form, tr *i18n.Strings) {
	t := f.t
	ui.Column(c).AlignItems(ui.Center).Gap(6).Margin(36, 0, 8, 0).Children(func() {
		if sw.icon0 != nil {
			ui.Image(c, sw.icon0).Size(88, 88).Margin(0, 0, 8, 0)
		}
		ui.Text(c, "Stage Manager").FontSize(24).Bold()
		ui.Text(c, tr.Tagline).TextColor(t.TextMuted).TextAlign(ui.Center)
		ui.Text(c, fmt.Sprintf(tr.Version, mygo.App.Version())).FontSize(12).TextColor(t.TextMuted).Margin(6, 0, 0, 0)
		ui.Row(c).Gap(18).Margin(14, 0, 0, 0).Children(func() {
			ui.Link(c, tr.Website, repo)
			ui.Link(c, tr.Issues, repo+"/issues")
		})
		ui.Text(c, tr.Privacy).FontSize(12).TextColor(t.TextMuted).Margin(22, 0, 0, 0)
		ui.Row(c).Gap(6).Children(func() {
			ui.Text(c, "© 2026 Renkai ·").FontSize(12).TextColor(t.TextMuted)
			ui.Link(c, tr.License, repo+"/blob/main/LICENSE").FontSize(12)
		})
	})
}

// identifyDisplays shows each display's number in its middle for a moment,
// as Windows' display settings do, to tell "Display 2" from the others.
func (a *App) identifyDisplays() {
	const size, shown = 140, 2 * time.Second
	for i, d := range a.displays {
		n := fmt.Sprint(i + 1)
		win := mygo.NewFlyout(mygo.FlyoutOptions{Tray: a.tray, Hidden: true, Width: size, Height: size, Shadow: true, Content: ui.View(func(c *ui.Context) {
			ui.Column(c).Fill().Center().Children(func() {
				ui.Text(c, n).FontSize(88).Bold()
			})
		})})
		// The flyout shows by its tray icon; then it goes to the middle of
		// its display, in the display's pixels.
		win.ShowInactive()
		m, px := d.mon.Monitor, int32(math.Round(size*d.mon.Scale()))
		winapi.SetWindowPos(winapi.HWND(win.NativeHandle()), winapi.HwndTopmost, m.Left+(m.W()-px)/2, m.Top+(m.H()-px)/2, px, px, winapi.SwpNoActivate)
		after(shown, win.Destroy)
	}
}
