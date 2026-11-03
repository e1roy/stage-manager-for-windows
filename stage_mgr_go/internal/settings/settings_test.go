package settings

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/e1roy/stage_manager_for_windows/internal/i18n"
)

func TestNormalizeClamps(t *testing.T) {
	s := Settings{Mode: "sideways", Width: 9000, IconScale: 0.1, Quality: "ultra"}.Normalize()
	if s.Mode != Fixed || s.Width != MaxWidth || s.IconScale != MinIconScale || s.Quality != Low {
		t.Fatalf("got %+v", s)
	}
	if s := (Settings{Quality: High}).Normalize(); s.Quality != High {
		t.Fatalf("high quality normalized to %q", s.Quality)
	}
	if s := (Settings{}).Normalize(); s.Width != 200 || s.IconScale != 1 || s.Quality != Low {
		t.Fatalf("zero settings normalized to %+v", s)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	st := NewStore(t.TempDir())
	if got := st.Load(); !got.Equal(Default()) {
		t.Fatalf("missing file loaded %+v", got)
	}
	s := Default()
	s.Mode = Floating
	s.Width = 240
	s.Displays = map[string]bool{`\\.\DISPLAY2`: false}
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	got := st.Load()
	if !got.Equal(s) || got.DisplayEnabled(`\\.\DISPLAY2`) || !got.DisplayEnabled(`\\.\DISPLAY1`) {
		t.Fatalf("loaded %+v", got)
	}
}

func TestSides(t *testing.T) {
	const second = `\\.\DISPLAY2`
	s := Default()
	s.Sides = map[string]Side{second: Right, `\\.\DISPLAY3`: Left, `\\.\DISPLAY4`: "top"}
	n := s.Normalize()
	if n.SideOf(second) != Right || n.SideOf(`\\.\DISPLAY1`) != Left || len(n.Sides) != 1 {
		t.Fatalf("sides normalized to %v", n.Sides)
	}
	// A strip moved right and back is as it never moved.
	if !(Settings{Sides: map[string]Side{"x": Left}}).Equal(Settings{}) {
		t.Fatal("a strip on the left differs from one not listed")
	}
	c := n.Clone()
	c.Sides[second] = Left
	if n.SideOf(second) != Right {
		t.Fatal("a clone shares its sides")
	}
}

func TestBrokenFileFallsBack(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{nope"), 0o644)
	if got := NewStore(dir).Load(); !got.Equal(Default()) {
		t.Fatalf("broken file loaded %+v", got)
	}
}

func TestCompanions(t *testing.T) {
	s := Settings{Companions: []string{" TortoiseProc.exe", `"D:/Tools/Paint.EXE"`, "", "tortoiseproc"}}.Normalize()
	if want := []string{"tortoiseproc", `d:\tools\paint.exe`}; !slices.Equal(s.Companions, want) {
		t.Fatalf("companions %q, want %q", s.Companions, want)
	}
	for exe, want := range map[string]bool{
		`C:\Program Files\TortoiseSVN\bin\TortoiseProc.exe`: true,
		`D:\Tools\paint.exe`: true,
		// A path is that copy only.
		`C:\Windows\paint.exe`:    false,
		`C:\Windows\explorer.exe`: false,
		"":                        false,
	} {
		if s.Companion(exe) != want {
			t.Errorf("companion %q = %v", exe, !want)
		}
	}
}

func TestWithCompanion(t *testing.T) {
	s := Settings{Companions: []string{"tortoiseproc", `d:\tools\paint.exe`}}
	s = s.WithCompanion(`C:\Apps\Notes.exe`, true)
	if want := []string{"tortoiseproc", `d:\tools\paint.exe`, "notes"}; !slices.Equal(s.Companions, want) {
		t.Fatalf("companions %q, want %q", s.Companions, want)
	}
	// Turning one off drops it however it is listed.
	s = s.WithCompanion(`D:\Tools\Paint.exe`, false).WithCompanion(`C:\x\TortoiseProc.exe`, false)
	if want := []string{"notes"}; !slices.Equal(s.Companions, want) {
		t.Fatalf("companions %q, want %q", s.Companions, want)
	}
}

func TestNoCompanionsStaysEmpty(t *testing.T) {
	st := NewStore(t.TempDir())
	s := Default()
	s.Companions = nil
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	if got := st.Load(); len(got.Companions) != 0 {
		t.Fatalf("cleared companions loaded as %q", got.Companions)
	}
}

func TestTilt(t *testing.T) {
	if s := (Settings{Tilt: 99}).Normalize(); s.Tilt != MaxTilt {
		t.Fatalf("tilt 99 normalized to %d", s.Tilt)
	}
	if s := (Settings{Tilt: -1}).Normalize(); s.Tilt != 0 {
		t.Fatalf("tilt -1 normalized to %d", s.Tilt)
	}
	// The perspective switch of older settings carries over.
	for json, want := range map[string]int{
		`{"perspective": false}`:            0,
		`{"perspective": true}`:             DefaultTilt,
		`{}`:                                DefaultTilt,
		`{"perspective": false, "tilt": 3}`: 3,
	} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "settings.json"), []byte(json), 0o644)
		if got := NewStore(dir).Load().Tilt; got != want {
			t.Errorf("%s loaded tilt %d, want %d", json, got, want)
		}
	}
}

func TestSpacing(t *testing.T) {
	if s := (Settings{Spacing: 500}).Normalize(); s.Spacing != MaxSpacing {
		t.Fatalf("spacing 500 normalized to %v", s.Spacing)
	}
	// Settings from before spacing had a setting keep the gap they had.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"width": 240}`), 0o644)
	if got := NewStore(dir).Load().Spacing; got != Default().Spacing {
		t.Fatalf("loaded spacing %v", got)
	}
}

func TestCardsAt(t *testing.T) {
	for in, want := range map[float64]float64{0: 0.5, 0.1: MinCardsAt, 0.9: MaxCardsAt, 0.634: 0.63, math.NaN(): 0.5} {
		if s := (Settings{CardsAt: in}).Normalize(); s.CardsAt != want {
			t.Fatalf("cards at %v normalized to %v", in, s.CardsAt)
		}
	}
	// Settings from before it was a setting keep the cards in the middle.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"width": 240}`), 0o644)
	if got := NewStore(dir).Load().CardsAt; got != 0.5 {
		t.Fatalf("loaded cards at %v", got)
	}
}

func TestPanel(t *testing.T) {
	s := (Settings{PanelWidth: 3, PanelHeight: 2}).Normalize()
	if s.PanelWidth != MaxPanelWidth || s.PanelHeight != MinPanelHeight {
		t.Fatalf("panel %v × %v", s.PanelWidth, s.PanelHeight)
	}
	// Settings from before the panel had a size keep the one it had.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"width": 240}`), 0o644)
	if got, def := NewStore(dir).Load(), Default(); got.PanelWidth != def.PanelWidth || got.PanelHeight != def.PanelHeight {
		t.Fatalf("loaded panel %v × %v", got.PanelWidth, got.PanelHeight)
	}
}

// The panel's glass is clamped into its range, a blur of 0 is none, and
// settings from before it had one take the default.
func TestPanelGlass(t *testing.T) {
	s := (Settings{PanelOpacity: 3, PanelBlur: -4}).Normalize()
	if s.PanelOpacity != MaxPanelOpacity || s.PanelBlur != 0 {
		t.Fatalf("glass %v, blur %v", s.PanelOpacity, s.PanelBlur)
	}
	if s := (Settings{PanelOpacity: 0.01, PanelBlur: 99}).Normalize(); s.PanelOpacity != MinPanelOpacity || s.PanelBlur != MaxPanelBlur {
		t.Fatalf("glass %v, blur %v", s.PanelOpacity, s.PanelBlur)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"width": 240}`), 0o644)
	if got, def := NewStore(dir).Load(), Default(); got.PanelOpacity != def.PanelOpacity || got.PanelBlur != def.PanelBlur {
		t.Fatalf("loaded glass %v, blur %v", got.PanelOpacity, got.PanelBlur)
	}
}

func TestCornerWidth(t *testing.T) {
	if s := (Settings{CornerWidth: 9000}).Normalize(); s.CornerWidth != MaxCornerWidth {
		t.Fatalf("corner width 9000 normalized to %v", s.CornerWidth)
	}
	if s := (Settings{CornerWidth: 10}).Normalize(); s.CornerWidth != MinCornerWidth {
		t.Fatalf("corner width 10 normalized to %v", s.CornerWidth)
	}
	// Settings from before the corner had a width keep the button it had.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"width": 240}`), 0o644)
	if got := NewStore(dir).Load().CornerWidth; got != Default().CornerWidth {
		t.Fatalf("loaded corner width %v", got)
	}
}

func TestWinTab(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	// Settings from before Win+Tab opened the overview have it do so.
	os.WriteFile(path, []byte(`{"width": 240}`), 0o644)
	if !NewStore(dir).Load().WinTab {
		t.Fatal("settings without winTab loaded it off")
	}
	os.WriteFile(path, []byte(`{"winTab": false}`), 0o644)
	if NewStore(dir).Load().WinTab {
		t.Fatal("winTab turned off loaded on")
	}
}

func TestStageList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	// Settings from before the stage's list was a setting show it.
	os.WriteFile(path, []byte(`{"width": 240}`), 0o644)
	if !NewStore(dir).Load().StageList {
		t.Fatal("settings without stageList loaded it off")
	}
	os.WriteFile(path, []byte(`{"stageList": false}`), 0o644)
	if NewStore(dir).Load().StageList {
		t.Fatal("stageList turned off loaded on")
	}
}

func TestMarkPrevious(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	// Settings from before the mark was a setting show it.
	os.WriteFile(path, []byte(`{"width": 240}`), 0o644)
	if !NewStore(dir).Load().MarkPrevious {
		t.Fatal("settings without markPrevious loaded it off")
	}
	os.WriteFile(path, []byte(`{"markPrevious": false}`), 0o644)
	if NewStore(dir).Load().MarkPrevious {
		t.Fatal("markPrevious turned off loaded on")
	}
}

func TestAccent(t *testing.T) {
	for in, want := range map[string]string{
		"":        AccentDefault, // settings from before the accent had a choice
		"default": AccentDefault,
		"none":    AccentNone,
		"#BF5AF2": "#bf5af2",
		"purple":  AccentDefault,
		"#12345":  AccentDefault,
		"#12345g": AccentDefault,
	} {
		if got := (Settings{Accent: in}).Normalize().Accent; got != want {
			t.Errorf("accent %q normalized to %q, want %q", in, got, want)
		}
	}
}

func TestLanguage(t *testing.T) {
	if s := (Settings{Language: "fr"}).Normalize(); s.Language != i18n.Auto {
		t.Fatalf("unknown language normalized to %q", s.Language)
	}
	if s := (Settings{Language: i18n.Japanese}).Normalize(); s.Language != i18n.Japanese {
		t.Fatalf("Japanese normalized to %q", s.Language)
	}
}

// Lighter flights are on by default, for settings saved before they were
// a choice too, and stay off once turned off.
func TestLightFlights(t *testing.T) {
	st := NewStore(t.TempDir())
	if err := os.WriteFile(st.Path, []byte(`{"quality": "high"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := st.Load(); !s.LightFlights || s.Quality != High {
		t.Fatalf("older settings loaded %+v", s)
	}
	s := Default()
	s.LightFlights = false
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	if st.Load().LightFlights {
		t.Fatal("turned off, lighter flights came back on")
	}
}

// Settings saved before the flights' speed was a choice fly as they did;
// a speed out of range is clamped, in tenths.
func TestFlightSpeed(t *testing.T) {
	for in, want := range map[float64]float64{0: 1, math.NaN(): 1, 0.1: MinFlightSpeed, 9: MaxFlightSpeed, 1.74: 1.7} {
		if got := (Settings{FlightSpeed: in}).Normalize().FlightSpeed; got != want {
			t.Errorf("speed %v normalized to %v, want %v", in, got, want)
		}
	}
}

// Checking for updates is on by default, for settings saved before it was
// a choice too, and stays off once turned off.
func TestCheckUpdates(t *testing.T) {
	st := NewStore(t.TempDir())
	if err := os.WriteFile(st.Path, []byte(`{"winTab": false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := st.Load(); !s.CheckUpdates {
		t.Fatalf("older settings loaded %+v", s)
	}
	s := Default()
	s.CheckUpdates = false
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	if st.Load().CheckUpdates {
		t.Fatal("turned off, checking for updates came back on")
	}
}

func TestAllStages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	// Off unless turned on, with the ghost of the stage's card as it is by
	// default in settings from before it.
	os.WriteFile(path, []byte(`{"width": 240}`), 0o644)
	got := NewStore(dir).Load()
	if got.AllStages || got.StagedOpacity != Default().StagedOpacity || got.StagedGray != Default().StagedGray {
		t.Fatalf("settings without allStages loaded %v %v %v", got.AllStages, got.StagedOpacity, got.StagedGray)
	}
	os.WriteFile(path, []byte(`{"allStages": true, "stagedOpacity": 0.7, "stagedGray": 0}`), 0o644)
	got = NewStore(dir).Load()
	if !got.AllStages || got.StagedOpacity != 0.7 || got.StagedGray != 0 {
		t.Fatalf("loaded %v %v %v", got.AllStages, got.StagedOpacity, got.StagedGray)
	}
	// A ghost out of its range is brought into it, but never to nothing.
	n := (Settings{StagedOpacity: 5, StagedGray: -1}).Normalize()
	if n.StagedOpacity != MaxStagedOpacity || n.StagedGray != 0 {
		t.Fatalf("normalized %v %v", n.StagedOpacity, n.StagedGray)
	}
	if n := (Settings{StagedOpacity: 0.01}).Normalize(); n.StagedOpacity != MinStagedOpacity {
		t.Fatalf("normalized %v", n.StagedOpacity)
	}
}
