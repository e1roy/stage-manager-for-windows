// Package settings loads, normalizes and saves Stage Manager's preferences.
package settings

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/e1roy/stage_manager_for_windows/internal/i18n"
)

// Mode is how the strip shares the screen with windows.
type Mode string

const (
	// Fixed reserves a band of the work area, as the taskbar does:
	// maximized windows leave the strip alone.
	Fixed Mode = "fixed"
	// Floating keeps the strip over the desktop while it has room and
	// moves it aside when a window on the stage covers it.
	Floating Mode = "floating"
)

// Quality is how sharp the stills windows fly between the strip and the
// stage with are, against the memory they take.
type Quality string

const (
	// Low flies windows out of the stage with their card's still and keeps
	// few stills at the size windows show.
	Low Quality = "low"
	// Medium flies windows out at the size they show too.
	Medium Quality = "medium"
	// High keeps many more stills at the size windows show, so that apps
	// switched to without a hover over their card still come in sharp.
	High Quality = "high"
)

// Side is the edge of a display its strip keeps to.
type Side string

const (
	// Left is where macOS keeps the strip.
	Left Side = "left"
	// Right mirrors it, for a display whose neighbor is on its left or a
	// user who reaches for the right.
	Right Side = "right"
)

// Limits of the adjustable values.
const (
	MinWidth     = 120
	MaxWidth     = 360
	MinIconScale = 0.6
	MaxIconScale = 1.6
	MaxTilt      = 10
	MaxSpacing   = 80
)

// Settings are the user's preferences.
type Settings struct {
	Mode Mode `json:"mode"`
	// Width of a strip in DIPs.
	Width float64 `json:"width"`
	// HideInactive moves the windows of apps that are not on the stage
	// off the desktop; otherwise the strip only lists them.
	HideInactive bool `json:"hideInactive"`
	// ClickDesktopClears sends the stage to the strip when the wallpaper
	// is clicked.
	ClickDesktopClears bool `json:"clickDesktopClears"`
	// IconScale sizes the app icons on the cards.
	IconScale float64 `json:"iconScale"`
	// Spacing is the gap between the cards of a strip, in DIPs at their
	// full size.
	Spacing float64 `json:"spacing"`
	// Tilt is how far the cards turn in 3D, as the macOS strip's do, from
	// 0, flat, to MaxTilt.
	Tilt int `json:"tilt"`
	// Quality sets how sharp flights are and how much memory they take.
	Quality Quality `json:"quality"`
	// Companions lists apps whose windows join the stage rather than take
	// it: tools used for a moment beside the app that opened them, as
	// TortoiseSVN's commit dialog is beside the file manager it was opened
	// from. An entry is an executable's name without ".exe", for every
	// copy of it, or its full path, for that one.
	Companions []string `json:"companions"`
	// Displays turns the strip off on some displays, by device name
	// (\\.\DISPLAY2). Displays not listed have one.
	Displays map[string]bool `json:"displays,omitempty"`
	// Sides puts the strip of some displays on their right, by device
	// name. Displays not listed keep it on the left.
	Sides map[string]Side `json:"sides,omitempty"`
	// Language of the words shown; Auto follows Windows.
	Language i18n.Lang `json:"language,omitempty"`
}

// DefaultTilt turns the cards as far as the macOS strip does.
const DefaultTilt = 6

// Default returns the settings of a first launch.
func Default() Settings {
	return Settings{
		Mode:               Fixed,
		Width:              200,
		HideInactive:       true,
		ClickDesktopClears: true,
		IconScale:          1,
		Spacing:            20,
		Tilt:               DefaultTilt,
		Quality:            Low,
		// TortoiseSVN's and TortoiseGit's dialogs all come from these.
		Companions: []string{"tortoiseproc", "tortoisegitproc"},
	}
}

// DisplayEnabled reports whether a display has a strip.
func (s Settings) DisplayEnabled(device string) bool {
	on, listed := s.Displays[device]
	return !listed || on
}

// SideOf returns the edge a display's strip keeps to.
func (s Settings) SideOf(device string) Side {
	if s.Sides[device] == Right {
		return Right
	}
	return Left
}

// Normalize clamps values into their ranges and fills what is missing.
func (s Settings) Normalize() Settings {
	if s.Mode != Fixed && s.Mode != Floating {
		s.Mode = Fixed
	}
	if s.Width == 0 || math.IsNaN(s.Width) {
		s.Width = Default().Width
	}
	s.Width = math.Round(min(max(s.Width, MinWidth), MaxWidth))
	if s.IconScale == 0 || math.IsNaN(s.IconScale) {
		s.IconScale = 1
	}
	s.IconScale = min(max(s.IconScale, MinIconScale), MaxIconScale)
	if math.IsNaN(s.Spacing) {
		s.Spacing = Default().Spacing
	}
	s.Spacing = math.Round(min(max(s.Spacing, 0), MaxSpacing))
	s.Tilt = min(max(s.Tilt, 0), MaxTilt)
	if !i18n.Known(s.Language) {
		s.Language = i18n.Auto
	}
	if s.Quality != Low && s.Quality != Medium && s.Quality != High {
		s.Quality = Low
	}
	var companions []string
	for _, c := range s.Companions {
		if c = companion(c); c != "" && !slices.Contains(companions, c) {
			companions = append(companions, c)
		}
	}
	s.Companions = companions
	// Only the right is kept, as the left is where an unlisted display's
	// strip is anyway: a strip moved there and back is as it was.
	var sides map[string]Side
	for k, v := range s.Sides {
		if v == Right {
			if sides == nil {
				sides = map[string]Side{}
			}
			sides[k] = v
		}
	}
	s.Sides = sides
	return s
}

// Companion reports whether the app of an executable, by its path, is a
// companion: listed by its name or by that path.
func (s Settings) Companion(exe string) bool {
	if exe == "" {
		return false
	}
	return slices.ContainsFunc(s.Companions, func(c string) bool { return CompanionMatches(c, exe) })
}

// CompanionMatches reports whether an entry of the companions, as Normalize
// keeps it, stands for the app of an executable, by its path.
func CompanionMatches(entry, exe string) bool {
	return exe != "" && (entry == companion(exe) || entry == exeName(exe))
}

// WithCompanion makes the app of an executable a companion, by its name,
// or no longer one, however it was listed.
func (s Settings) WithCompanion(exe string, on bool) Settings {
	path, name := companion(exe), exeName(exe)
	s.Companions = slices.DeleteFunc(slices.Clone(s.Companions), func(c string) bool { return c == path || c == name })
	if on && name != "" {
		s.Companions = append(s.Companions, name)
	}
	return s.Normalize()
}

// companion returns an entry of the companions as it is kept: a path, told
// by its folder, in lower case with backslashes; else a name, as exeName
// returns it.
func companion(entry string) string {
	n := strings.ToLower(strings.Trim(strings.TrimSpace(entry), `"`))
	if !strings.ContainsAny(n, `\/`) {
		return exeName(n)
	}
	return strings.ReplaceAll(n, "/", `\`)
}

// exeName returns an executable's name as companions list it: lower case,
// without its folder or ".exe", so "C:\...\TortoiseProc.exe" is
// "tortoiseproc".
func exeName(path string) string {
	n := strings.ToLower(strings.TrimSpace(path))
	n = n[strings.LastIndexAny(n, `\/`)+1:]
	return strings.TrimSpace(strings.TrimSuffix(n, ".exe"))
}

// Equal reports whether two settings are the same.
func (s Settings) Equal(o Settings) bool {
	a, _ := json.Marshal(s.Normalize())
	b, _ := json.Marshal(o.Normalize())
	return string(a) == string(b)
}

// Clone returns a copy that shares no map with s.
func (s Settings) Clone() Settings {
	s.Displays = maps.Clone(s.Displays)
	s.Sides = maps.Clone(s.Sides)
	s.Companions = slices.Clone(s.Companions)
	return s
}

// Store keeps the settings in a JSON file.
type Store struct{ Path string }

// NewStore keeps settings.json in dir.
func NewStore(dir string) Store { return Store{Path: filepath.Join(dir, "settings.json")} }

// Load returns the saved settings, or the defaults when there are none or
// they cannot be read: broken settings must not keep the app from starting.
func (st Store) Load() Settings {
	s := Default()
	data, err := os.ReadFile(st.Path)
	if err != nil {
		return s
	}
	if json.Unmarshal(data, &s) != nil {
		return Default()
	}
	// Settings from before the tilt had a switch for the perspective,
	// whose off is a tilt of 0.
	var old struct {
		Perspective *bool `json:"perspective"`
		Tilt        *int  `json:"tilt"`
	}
	if json.Unmarshal(data, &old) == nil && old.Tilt == nil && old.Perspective != nil && !*old.Perspective {
		s.Tilt = 0
	}
	return s.Normalize()
}

// Save writes the settings atomically.
func (st Store) Save(s Settings) error {
	data, err := json.MarshalIndent(s.Normalize(), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(st.Path), 0o755); err != nil {
		return err
	}
	tmp := st.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, st.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
