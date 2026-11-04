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
	"github.com/e1roy/stage_manager_for_windows/internal/update"
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
	// spacing is the gap between cards, as the slider sets it, and
	// cardsAt how far down the strip they sit.
	spacing, cardsAt float64
	// panelW and panelH size the overview's panel, as the sliders set them,
	// and cornerW the button over a display's bottom-left corner.
	panelW, panelH float64
	panelOp, blur  float64
	cornerW        float64
	// stagedOp and stagedGray are how opaque and how gray the card of the
	// stage is in a strip that lists it, as the sliders set them.
	stagedOp, stagedGray float64
	// flight is how fast windows fly, as the slider sets it.
	flight float64
	login  bool
	icon0  *ui.Bitmap
	// fileIcons are the icons of the companions listed by path, by path.
	fileIcons map[string]*ui.Bitmap
	// notice is shown as a toast in the next frame.
	notice string

	widthTimer *time.Timer
}

func (a *App) openSettings() {
	// The last check could not reach GitHub: the about pane tries again,
	// rather than wait hours for the next.
	if a.checkFailed {
		a.checkUpdate()
	}
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
		Width:     760,
		Height:    580,
		MinWidth:  480,
		MinHeight: minSettingsHeight,
		Vibrancy:  mygo.VibrancyMica,
		// The panes moved from a toolbar to a sidebar, which wants a wider
		// window: the size remembered for the toolbar is left behind.
		StateKey: "settings-nav",
		Content:  ui.View(sw.view),
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
		sw.win.SetContentSize(w, 580)
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
	sw.cardsAt = sw.s.CardsAt
	sw.panelW, sw.panelH = sw.s.PanelWidth, sw.s.PanelHeight
	sw.panelOp, sw.blur = sw.s.PanelOpacity, sw.s.PanelBlur
	sw.stagedOp, sw.stagedGray = sw.s.StagedOpacity, sw.s.StagedGray
	sw.cornerW = sw.s.CornerWidth
	sw.flight = sw.s.FlightSpeed
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

// The icons of the panes, beside their names in the window's sidebar as
// Windows' Settings and macOS' System Settings list theirs. They are drawn
// in the text color, so they follow the theme.
var settingsIcons = []*ui.SVG{
	lineIcon(`<path d="M4 7h9M17 7h3M4 17h3M11 17h9"/><circle cx="15" cy="7" r="2"/><circle cx="9" cy="17" r="2"/>`),
	lineIcon(`<rect x="3" y="4" width="18" height="16" rx="2.5"/><path d="M9 4v16M5.5 8h1M5.5 12h1M5.5 16h1"/>`),
	lineIcon(`<rect x="3" y="4" width="18" height="16" rx="2.5"/><rect x="9" y="8" width="9" height="8" rx="1"/><path d="M5.5 9.5v1M5.5 13.5v1"/>`),
	lineIcon(`<rect x="3" y="4" width="8" height="7" rx="1.5"/><rect x="13" y="4" width="8" height="7" rx="1.5"/><rect x="3" y="13" width="8" height="7" rx="1.5"/><rect x="13" y="13" width="8" height="7" rx="1.5"/>`),
	lineIcon(`<path d="M15 9V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h4"/><rect x="9" y="9" width="12" height="12" rx="2"/>`),
	lineIcon(`<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 7.5v.01"/>`),
}

// The panes' indexes in settingsIcons. The overview's has its own pane, as
// the three ways into it and the looks of its buttons made the strip's
// pane the longest by far.
const (
	overviewTab = 3
	aboutTab    = 5
)

// The sidebar's width, and its width as icons alone in a window too
// narrow for both it and rows that read in a line or two.
const navWidth, navCompact, compactBelow = 184, 48, 700

// paneMaxWidth keeps a pane's rows from running so wide in a wide window
// that a row's title is far from its control.
const paneMaxWidth = 720

// uiDotColor is dotColor, the red dot of a newer release, as the panes
// paint it.
var uiDotColor = ui.RGBA(dotColor.R, dotColor.G, dotColor.B, 1)

var (
	appIcon    = lineIcon(`<rect x="3" y="4" width="18" height="16" rx="2.5"/><path d="M3 9h18"/>`)
	removeIcon = lineIcon(`<path d="M6 6l12 12M18 6L6 18"/>`)
	infoIcon   = lineIcon(`<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 7.5v.01"/>`)
	// refreshIcon asks again, now, rather than at the next check.
	refreshIcon = lineIcon(`<path d="M20.5 12a8.5 8.5 0 1 1-2.5-6"/><path d="M20.5 3.5V9H15"/>`)
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
	// disabled turns off the controls of a row that only applies while a
	// switch above it is on, as child hands the form to them.
	disabled bool
}

// option is a line of a row's tip that explains one of its choices, set
// apart so that a row of several reads at a glance; on marks the one
// chosen. Without a label it is a line of its own.
type option struct {
	label, text string
	on          bool
}

// rowPadX is the inset of every row in a card, which lines up the text of
// all of them, and their controls.
const rowPadX = 14

// section is a card of rows. A pane's first card goes without a title,
// as the pane's name over it says it; trailing, if any, goes at the end of
// the title, as a button acting on the whole card.
func (f form) section(c *ui.Context, title string, rows func(), trailing ...func()) {
	f.group(c, title, nil, rows, trailing...)
}

// group is a section whose title has an info mark, for a thing the titles
// of its rows don't say what it is, such as a button the user may not have
// met yet.
func (f form) group(c *ui.Context, title string, tp *tip, rows func(), trailing ...func()) {
	if title == "" {
		ui.Box(c).Height(8)
	} else {
		ui.Row(c).AlignItems(ui.Center).Gap(12).MinHeight(28).Margin(14, 0, 6, 2).Children(func() {
			ui.Row(c).Grow(1).Shrink(1).Children(func() { f.title(c, title, f.t.Text, true, tp) })
			for _, fn := range trailing {
				fn()
			}
		})
	}
	ui.Column(c).Background(f.card).Radius(8).Border(1, f.t.Border.Alpha(0.6)).Dividers(1, f.t.Border.Alpha(0.5)).Children(rows)
}

// row is a setting: its title, and its control at the end. What it does,
// desc, and what each of its choices means, opts, go in the tip of an info
// mark after the title rather than under it: every row is one line, so a
// pane reads as a list of titles and controls in two even columns, and
// the words are there for one who wants them.
func (f form) row(c *ui.Context, title, desc string, control func(), opts ...option) {
	f.rowAt(c, false, title, "", explain(desc, opts), control)
}

// tip is what an info mark explains: a line saying what the setting is,
// then its points, each a label and what it means.
type tip struct {
	lead   string
	points []option
}

// explain is the tip of a row of desc and opts, or nil without either.
func explain(desc string, opts []option) *tip {
	if desc == "" && len(opts) == 0 {
		return nil
	}
	return &tip{desc, opts}
}

// child is a row of a setting that only applies while the one above it is
// on, dimmed while it is off, and its control is handed the form, disabled
// then. It keeps the left edge every row's text shares rather than indent
// under it.
func (f form) child(c *ui.Context, on bool, title, desc string, control func(form)) {
	f.disabled = !on
	f.rowAt(c, !on, title, "", explain(desc, nil), func() { control(f) })
}

// rowH is the height of every row, which lines them all up whether their
// control is a switch, a slider or a list.
const rowH = 48

// rowAt lays out every row: dim while it does not apply, with sub, if any, a
// line under its title that tells which thing it is, such as a display's
// size, rather than what the setting does, and the info mark of tip.
func (f form) rowAt(c *ui.Context, dim bool, title, sub string, tip *tip, control func()) {
	t := f.t
	fg := t.Text
	if dim {
		fg = t.TextMuted
	}
	ui.Row(c).Padding(6, rowPadX).Gap(16).AlignItems(ui.Center).MinHeight(rowH).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
			f.title(c, title, fg, false, tip)
			if sub != "" {
				ui.Text(c, sub).FontSize(12).TextColor(t.TextMuted).SingleLine()
			}
		})
		control()
	})
}

// title is a row's or a section's title, with the info mark of tp after
// it, if any. The title and the mark show the tip alike while the pointer
// rests on them, as the title is the bigger target and where the eye is:
// below them, a card of the window's own look rather than the dark strip
// of a tooltip, as it is a few lines to read, with the labels of the
// points in a column of their own so they read at a glance, the one
// chosen in the accent.
func (f form) title(c *ui.Context, title string, fg ui.Color, bold bool, tp *tip) {
	t := f.t
	mark := ui.Row(c).AlignSelf(ui.Start).AlignItems(ui.Center).Gap(6).Shrink(1)
	over := tp != nil && mark.Hovered()
	mark.Children(func() {
		txt := ui.Text(c, title).TextColor(fg).Shrink(1)
		if bold {
			txt.Bold()
		}
		if tp == nil {
			return
		}
		ic := t.TextMuted
		if over {
			ic = t.Accent
		}
		ui.Icon(c, infoIcon).FontSize(14).TextColor(ic).Shrink(0)
	})
	if tp == nil {
		return
	}
	ui.TooltipBase(c, mark, func(el ui.Element) {
		el.AttachTo(mark, ui.AnchorTopLeft, ui.AnchorBottomLeft).Margin(6, 0, 0, -8)
		el.Width(340).Padding(10, 12).Radius(8).Background(t.Surface).Border(1, t.Border).
			Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.18)).TextColor(t.Text).FontSize(12)
		el.Children(func() {
			ui.Column(c).Gap(8).Children(func() {
				if tp.lead != "" {
					ui.Text(c, tp.lead)
				}
				if len(tp.points) == 0 {
					return
				}
				ui.Grid(c).ColumnTracks(ui.FitContent(), ui.Fr(1)).GapX(12).GapY(6).Children(func() {
					for _, p := range tp.points {
						if p.label == "" {
							ui.Text(c, p.text).TextColor(t.TextMuted).ColumnSpan(-1)
							continue
						}
						fg := t.Text
						if p.on {
							fg = t.Accent
						}
						ui.Text(c, p.label).Bold().TextColor(fg)
						ui.Text(c, p.text).TextColor(t.TextMuted)
					}
				})
			})
		})
	})
}

// The sliders' width, and how far the built-in slider keeps the middle of
// its thumb, and its ticks, from its ends.
const sliderWidth, sliderInset = 160, 10

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
		changed = s.Width(sliderWidth).Disabled(f.disabled).Changed()
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
			if mark.Clicked() && !at && !f.disabled {
				*v, changed = def, true
			}
		})
	})
	// Right-aligned, the value ends where the switches of other rows do.
	ui.Text(c, label(*v)).FontSize(12).TextColor(t.TextMuted).Width(72).Shrink(0).SingleLine().TextAlign(ui.End)
	return changed
}

func (sw *settingsWindow) view(c *ui.Context) {
	// Smaller text and tighter controls than MyGo's defaults, as the
	// window is a list of many rows to scan rather than a page to read.
	th := *c.Theme()
	th.FontSize, th.Spacing = 13, 3.5
	c.SetTheme(&th)
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

	names := []string{tr.TabGeneral, tr.TabStrip, tr.TabStage, tr.TabOverview, tr.TabCompanions, tr.TabAbout}
	w, _ := c.Size()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		sw.nav(c, t, names, w < compactBelow)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Justify(ui.Start).Children(func() {
			// The pane's name stays over it as it scrolls, which tells where
			// one is, and names its first card.
			ui.Column(c).WidthPercent(100).MaxWidth(paneMaxWidth).Margin(0, ui.Auto).Padding(14, 20, 0, 20).Children(func() {
				ui.Text(c, names[sw.tab]).FontSize(18).Bold().SingleLine()
			})
			// Each pane keeps its own scroll position.
			c.Key(sw.tab)
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).WidthPercent(100).MaxWidth(paneMaxWidth).Margin(0, ui.Auto).Padding(0, 20, 20, 20).Gap(2).Children(func() {
					switch sw.tab {
					case 0:
						sw.generalPane(c, f, tr)
					case 1:
						sw.stripPane(c, f, tr)
					case 2:
						sw.stagePane(c, f, tr)
					case overviewTab:
						sw.overviewPane(c, f, tr)
					case 4:
						sw.companionsPane(c, f, tr)
					case aboutTab:
						sw.aboutPane(c, f, tr)
					}
				})
			})
		})
	})
}

// nav lists the panes down the window's left side, as Windows' Settings
// does: an icon and a name each, the pane shown marked with the accent's
// bar. Every pane is a click away and the list stays put, rather than a
// toolbar that runs out of room; too narrow, it keeps the icons alone, with
// their names as tooltips.
func (sw *settingsWindow) nav(c *ui.Context, t *ui.Theme, names []string, compact bool) {
	width := float32(navWidth)
	if compact {
		width = navCompact
	}
	_, dot := sw.a.newer()
	tabs := ui.TabsBase(c, &sw.tab, len(names))
	tabs.List.Column().Justify(ui.Start).AlignItems(ui.Stretch).Width(width).Shrink(0).Gap(1).Padding(10, 6).Children(func() {
		for i, name := range names {
			tab := tabs.Tab(i).Row().AlignItems(ui.Center).Gap(10).Height(32).Padding(0, 10).Radius(5)
			if compact {
				tab.Justify(ui.Center).Padding(0).Tooltip(name)
			}
			switch {
			case i == sw.tab:
				tab.Background(t.Text.Alpha(0.07))
			case tab.Hovered():
				tab.Background(t.Text.Alpha(0.04))
			}
			tab.Children(func() {
				if i == sw.tab {
					ui.Box(c).Absolute().Left(0).Top(9).Size(3, 14).Radius(1.5).Background(t.Accent)
				}
				ui.Box(c).Size(16, 16).Shrink(0).Children(func() {
					ui.Icon(c, settingsIcons[i]).FontSize(16).TextColor(t.Text)
					// A newer release puts the tray icon's red dot on the
					// about pane too.
					if dot && i == aboutTab && compact {
						ui.Box(c).Absolute().Top(-2).Right(-3).Size(7, 7).Radius(3.5).Background(uiDotColor)
					}
				})
				if compact {
					return
				}
				ui.Text(c, name).SingleLine().Grow(1).Shrink(1)
				if dot && i == aboutTab {
					ui.Box(c).Size(8, 8).Shrink(0).Radius(4).Background(uiDotColor)
				}
			})
		}
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
			// Under its title, the display's size tells it from its
			// neighbors without pointing at each.
			f.rowAt(c, false, fmt.Sprintf(tr.Display, i+1), desc, nil, func() {
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
	})
	f.section(c, tr.Cards, func() {
		f.row(c, tr.Spacing, "", func() {
			if f.slider(c, tr, &sw.spacing, 0, settings.MaxSpacing, 0, def.Spacing, px) {
				sw.spacing = math.Round(sw.spacing)
				sw.s.Spacing = sw.spacing
				sw.apply()
			}
		})
		pct := func(v float64) string { return fmt.Sprintf("%.0f%%", v*100) }
		f.row(c, tr.CardsAt, tr.CardsAtTip, func() {
			if f.slider(c, tr, &sw.cardsAt, settings.MinCardsAt, settings.MaxCardsAt, 0, def.CardsAt, pct) {
				sw.cardsAt = math.Round(sw.cardsAt*100) / 100
				sw.s.CardsAt = sw.cardsAt
				sw.apply()
			}
		},
			option{label: tr.CardsAtMid, text: tr.CardsAtMidDesc},
			option{label: tr.CardsAtDown, text: tr.CardsAtDownDesc},
			option{label: tr.CardsAtUp, text: tr.CardsAtUpDesc},
			option{label: tr.CardsAtList, text: tr.CardsAtListDesc},
			option{label: tr.CardsAtCrowd, text: tr.CardsAtCrowdDesc})
		f.row(c, tr.IconSize, "", func() {
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
	// What the strip shows besides the cards: the windows on the stage,
	// which card is the stage last left, and the stage's own.
	f.section(c, tr.StripShows, func() {
		f.row(c, tr.StageList, tr.StageListDesc, func() {
			if ui.Switch(c, &sw.s.StageList).Changed() {
				sw.apply()
			}
		})
		f.row(c, tr.MarkPrevious, tr.MarkPreviousDesc, func() {
			if ui.Switch(c, &sw.s.MarkPrevious).Changed() {
				sw.apply()
			}
		})
		// The stage's own card, listed in its slot, as a ghost.
		pct := func(v float64) string { return fmt.Sprintf("%.0f%%", v*100) }
		f.row(c, tr.AllStages, "", func() {
			if ui.Switch(c, &sw.s.AllStages).Changed() {
				sw.apply()
			}
		})
		f.child(c, sw.s.AllStages, tr.Opacity, "", func(f form) {
			if f.slider(c, tr, &sw.stagedOp, settings.MinStagedOpacity, settings.MaxStagedOpacity, 0, def.StagedOpacity, pct) {
				sw.stagedOp = math.Round(sw.stagedOp*100) / 100
				sw.s.StagedOpacity = sw.stagedOp
				sw.apply()
			}
		})
		f.child(c, sw.s.AllStages, tr.Grayscale, "", func(f form) {
			if f.slider(c, tr, &sw.stagedGray, 0, 1, 0, def.StagedGray, pct) {
				sw.stagedGray = math.Round(sw.stagedGray*100) / 100
				sw.s.StagedGray = sw.stagedGray
				sw.apply()
			}
		})
	})
}

// overviewPane sets the three ways into the overview of every window: the
// keys first, as they need no button to be found, then the panel at the
// bottom of the strip and the button over the corner, and the color both
// buttons light up in.
func (sw *settingsWindow) overviewPane(c *ui.Context, f form, tr *i18n.Strings) {
	f.section(c, "", func() {
		f.row(c, tr.WinTab, tr.WinTabDesc, func() {
			if ui.Switch(c, &sw.s.WinTab).Changed() {
				sw.apply()
			}
		})
	})
	sw.panelSection(c, f, tr)
	sw.cornerSection(c, f, tr)
	sw.highlightRow(c, f, tr)
}

// panelSection sizes the overview's panel. The strips show it while it is
// sized, as it is hidden till the pointer comes near.
func (sw *settingsWindow) panelSection(c *ui.Context, f form, tr *i18n.Strings) {
	def := settings.Default()
	f.group(c, tr.OverviewPanel, &tip{lead: tr.OverviewPanelDesc}, func() {
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
		// How much of what is behind the panel shows through it, and how
		// blurred, as the corner button joined to it shows it too.
		f.row(c, tr.Opacity, "", func() {
			if f.slider(c, tr, &sw.panelOp, settings.MinPanelOpacity, settings.MaxPanelOpacity, 0, def.PanelOpacity, pct) {
				sw.panelOp = math.Round(sw.panelOp*100) / 100
				sw.s.PanelOpacity = sw.panelOp
				sw.a.showPanel()
				sw.apply()
			}
		})
		blur := func(v float64) string {
			if v == 0 {
				return tr.BlurOff
			}
			return fmt.Sprintf(tr.WidthValue, v)
		}
		f.row(c, tr.Blur, "", func() {
			if f.slider(c, tr, &sw.blur, 0, settings.MaxPanelBlur, 0, def.PanelBlur, blur) {
				sw.blur = math.Round(sw.blur)
				sw.s.PanelBlur = sw.blur
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
	f.group(c, tr.OverviewCorner, &tip{lead: tr.OverviewCornerDesc}, func() {
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
	def := settings.Default()
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
		f.child(c, sw.s.HideInactive, tr.ClickDesktop, desc, func(f form) {
			if ui.Switch(c, &sw.s.ClickDesktopClears).Disabled(f.disabled).Changed() {
				sw.apply()
			}
		})
	})
	f.section(c, tr.Animation, func() {
		f.row(c, tr.FlightSpeed, tr.FlightSpeedDesc, func() {
			times := func(v float64) string { return fmt.Sprintf("%.1f×", v) }
			if f.slider(c, tr, &sw.flight, settings.MinFlightSpeed, settings.MaxFlightSpeed, 0, def.FlightSpeed, times) {
				sw.flight = math.Round(sw.flight*10) / 10
				sw.s.FlightSpeed = sw.flight
				sw.apply()
			}
		})
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
// The button that adds one is at the head of the list, where the eye is
// when the list is empty or long.
func (sw *settingsWindow) companionsPane(c *ui.Context, f form, tr *i18n.Strings) {
	t := f.t
	add := func() {
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
	}
	f.group(c, tr.CompanionLabel, &tip{lead: tr.CompanionTip}, func() {
		items := sw.companions(tr)
		if len(items) == 0 {
			ui.Text(c, tr.CompanionsEmpty).FontSize(12).TextColor(t.TextMuted).Padding(14, rowPadX)
		}
		for _, it := range items {
			ui.Row(c).Key(it.entry).Padding(7, rowPadX).Gap(10).AlignItems(ui.Center).Children(func() {
				if it.icon != nil {
					ui.Image(c, it.icon).Size(24, 24)
				} else {
					ui.Box(c).Size(24, 24).Center().Children(func() {
						ui.Icon(c, appIcon).FontSize(20).TextColor(t.TextMuted)
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
	}, add)
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
	ui.Column(c).AlignItems(ui.Center).Gap(4).Margin(20, 0, 8, 0).Children(func() {
		if sw.icon0 != nil {
			ui.Image(c, sw.icon0).Size(64, 64).Margin(0, 0, 6, 0)
		}
		ui.Text(c, tr.AppName).FontSize(20).Bold()
		ui.Text(c, tr.Tagline).TextColor(t.TextMuted).TextAlign(ui.Center)
		ui.Text(c, fmt.Sprintf(tr.Version, mygo.App.Version())).FontSize(12).TextColor(t.TextMuted).Margin(6, 0, 0, 0)
		// The version released last, as the last check found it: with a
		// red dot and the way to it when it is newer than the one running,
		// and with a way to ask again now rather than at the next check.
		// Nothing of it shows while checks are off.
		if sw.a.settings.CheckUpdates {
			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				if r, ok := sw.a.newer(); ok {
					ui.Box(c).Size(8, 8).Radius(4).Background(uiDotColor)
					ui.Text(c, fmt.Sprintf(tr.LatestVersion, r.Version)).FontSize(12)
					ui.Link(c, tr.Download, r.URL).FontSize(12).Margin(0, 0, 0, 4)
				} else if sw.a.unchecked() {
					ui.Text(c, tr.UpdateUnknown).FontSize(12).TextColor(t.TextMuted)
					ui.Link(c, tr.SeeReleases, update.ReleasesURL).FontSize(12)
				} else if v := sw.a.published.Version; v != "" {
					ui.Text(c, fmt.Sprintf(tr.LatestVersion, v)).FontSize(12).TextColor(t.TextMuted)
				}
				// A check on its way is not asked for twice: the button
				// greys out until it comes back.
				if ui.Button(c, "").Label(tr.CheckNow).Tooltip(tr.CheckNow).Disabled(sw.a.checking).Children(func() {
					ui.Icon(c, refreshIcon).FontSize(14).TextColor(t.TextMuted)
				}).Clicked() {
					sw.a.checkUpdate()
				}
			})
		}
		ui.Row(c).Gap(16).Margin(10, 0, 0, 0).Children(func() {
			ui.Link(c, tr.Website, repo)
			ui.Link(c, tr.Issues, repo+"/issues")
		})
		ui.Text(c, tr.Privacy).FontSize(12).TextColor(t.TextMuted).Margin(16, 0, 0, 0)
		ui.Row(c).Gap(6).Children(func() {
			ui.Text(c, "© 2026 Renkai ·").FontSize(12).TextColor(t.TextMuted)
			ui.Link(c, tr.License, repo+"/blob/main/LICENSE").FontSize(12)
		})
	})
	f.section(c, "", func() {
		f.row(c, tr.CheckUpdates, tr.CheckUpdatesDesc, func() {
			if ui.Switch(c, &sw.s.CheckUpdates).Changed() {
				sw.apply()
			}
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
