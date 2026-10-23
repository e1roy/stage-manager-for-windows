// Package i18n holds the words Stage Manager shows, in each language it
// speaks. It knows nothing of Windows: the app asks the system for its
// language and passes it in.
package i18n

// Lang is a language, by its BCP 47 tag.
type Lang string

const (
	// Auto follows the language of Windows.
	Auto               Lang = ""
	SimplifiedChinese  Lang = "zh-Hans"
	TraditionalChinese Lang = "zh-Hant"
	English            Lang = "en"
	Japanese           Lang = "ja"
)

// Langs are the languages there are words for, in the order the settings
// list them.
var Langs = []Lang{SimplifiedChinese, TraditionalChinese, English, Japanese}

var tables = map[Lang]*Strings{
	SimplifiedChinese:  &zhHans,
	TraditionalChinese: &zhHant,
	English:            &en,
	Japanese:           &ja,
}

// Name is what a language calls itself, as language menus show it so that
// whoever reads it finds their own.
func (l Lang) Name() string {
	switch l {
	case SimplifiedChinese:
		return "简体中文"
	case TraditionalChinese:
		return "繁體中文"
	case English:
		return "English"
	case Japanese:
		return "日本語"
	}
	return string(l)
}

// Known reports whether there are words for l, or l is Auto.
func Known(l Lang) bool {
	_, ok := tables[l]
	return ok || l == Auto
}

// For returns the words of a language, English for one there are none for.
func For(l Lang) *Strings {
	if s, ok := tables[l]; ok {
		return s
	}
	return &en
}

// FromLANGID returns the language of a Windows language identifier, as
// GetUserDefaultUILanguage gives it, or English when there are no words
// for it.
func FromLANGID(id uint16) Lang {
	primary, sub := id&0x3ff, id>>10
	switch primary {
	case 0x04: // Chinese
		switch sub {
		case 0x01, 0x03, 0x05, 0x1f: // Taiwan, Hong Kong, Macao, Traditional
			return TraditionalChinese
		}
		return SimplifiedChinese
	case 0x09:
		return English
	case 0x11:
		return Japanese
	}
	return English
}

// Strings are the words of one language. A field whose comment names
// verbs is a format, which every language fills with the same values in
// the same order.
type Strings struct {
	// The settings window.
	SettingsTitle                                           string
	TabGeneral, TabStrip, TabStage, TabCompanions, TabAbout string

	Language, LanguageSystem         string
	LaunchAtLogin, LaunchAtLoginDesc string
	LaunchAtLoginError               string // %s: why
	Displays, Identify               string
	Display                          string // %d: its number
	DisplayDesc                      string // %d × %d: its pixels; %.0f%%: its scale
	DisplayPrimary                   string
	Troubleshooting                  string
	Rescue, RescueDesc, RescueButton string
	RescueDone                       string // %d: how many windows
	RescueNone                       string

	Mode, ModeDesc                           string
	Fixed, FixedDesc, Floating, FloatingDesc string
	Width                                    string
	WidthValue                               string // %.0f: DIPs
	IconSize, Spacing                        string
	Tilt, TiltDesc, TiltOff                  string
	DefaultValue                             string // %s: the default value

	HideInactive, HideInactiveDesc                        string
	ClickDesktop, ClickDesktopDesc, ClickDesktopNeedsHide string
	Animation, Quality, QualityDesc                       string
	Low, LowDesc, Medium, MediumDesc, High, HighDesc      string

	// CompanionLabel names companions in the settings and on the cards'
	// menus, from where the user is: the app they are using stays when one
	// of these opens. CompanionTip says what they do.
	CompanionLabel, CompanionTip    string
	CompanionsEmpty, CompanionsHint string
	AddCompanion, Browse, Remove    string
	// Programs names executables in the file dialog that adds one.
	Programs        string
	CompanionByName string // %s: the executable's name

	Tagline          string
	Version          string // %s: the version
	Privacy, License string
	Website, Issues  string

	// The strip.
	EmptyStrip, EmptyStripHint                                string
	PickStage, JoinStage, SplitSet, CloseWindow, CloseWindows string

	// The tray.
	Settings, Quit string
}
