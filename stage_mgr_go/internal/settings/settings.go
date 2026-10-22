// Package settings loads, normalizes and saves StageBar's preferences.
package settings

import (
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
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

// Limits of the adjustable values.
const (
	MinWidth     = 120
	MaxWidth     = 360
	MinIconScale = 0.6
	MaxIconScale = 1.6
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
	// Perspective turns the cards in 3D, as the macOS strip does.
	Perspective bool `json:"perspective"`
	// Displays turns the strip off on some displays, by device name
	// (\\.\DISPLAY2). Displays not listed have one.
	Displays map[string]bool `json:"displays,omitempty"`
	// Paused leaves every window where it is and hides the strips.
	Paused bool `json:"paused"`
}

// Default returns the settings of a first launch.
func Default() Settings {
	return Settings{
		Mode:               Fixed,
		Width:              200,
		HideInactive:       true,
		ClickDesktopClears: true,
		IconScale:          1,
		Perspective:        true,
	}
}

// DisplayEnabled reports whether a display has a strip.
func (s Settings) DisplayEnabled(device string) bool {
	on, listed := s.Displays[device]
	return !listed || on
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
	return s
}

// Equal reports whether two settings are the same.
func (s Settings) Equal(o Settings) bool {
	a, _ := json.Marshal(s.Normalize())
	b, _ := json.Marshal(o.Normalize())
	return string(a) == string(b)
}

// Clone returns a copy that shares no map with s.
func (s Settings) Clone() Settings {
	if s.Displays != nil {
		m := make(map[string]bool, len(s.Displays))
		for k, v := range s.Displays {
			m[k] = v
		}
		s.Displays = m
	}
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
