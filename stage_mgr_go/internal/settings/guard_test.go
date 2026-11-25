package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// zeroByDefault lists the settings whose default is their zero value. A new
// setting must be either in Default() or here: one forgotten in Default()
// would start out as zero, off or empty without anybody having decided so.
var zeroByDefault = []string{
	"Language", "Displays", "Sides", "SplitWindows", "AllStages", "MinimizeToStrip",
}

func TestDefaultsDecided(t *testing.T) {
	d := reflect.ValueOf(Default())
	for i := range d.NumField() {
		name := d.Type().Field(i).Name
		zero := d.Field(i).IsZero()
		listed := slices.Contains(zeroByDefault, name)
		switch {
		case zero && !listed:
			t.Errorf("%s has no default: set it in Default() or list it in zeroByDefault", name)
		case !zero && listed:
			t.Errorf("%s has a default, yet is listed in zeroByDefault", name)
		}
	}
}

// TestCloneSharesNothing: the settings window edits a clone while the app
// keeps the settings it applied; a map or slice they shared would change
// both. Every field of a reference type is filled, cloned and changed.
func TestCloneSharesNothing(t *testing.T) {
	s := Default()
	v := reflect.ValueOf(&s).Elem()
	for i := range v.NumField() {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.Map:
			m := reflect.MakeMap(f.Type())
			m.SetMapIndex(reflect.New(f.Type().Key()).Elem(), reflect.New(f.Type().Elem()).Elem())
			f.Set(m)
		case reflect.Slice:
			f.Set(reflect.MakeSlice(f.Type(), 1, 1))
		case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Chan:
			t.Errorf("%s: Clone does not know %v fields", v.Type().Field(i).Name, f.Kind())
		}
	}
	c := s.Clone()
	cv := reflect.ValueOf(&c).Elem()
	for i := range cv.NumField() {
		f := cv.Field(i)
		switch f.Kind() {
		case reflect.Map:
			f.SetMapIndex(reflect.New(f.Type().Key()).Elem(), reflect.Value{})
			if v.Field(i).Len() != 1 {
				t.Errorf("%s: the clone shares its map", v.Type().Field(i).Name)
			}
		case reflect.Slice:
			if f.Len() > 0 {
				f.Index(0).Set(reflect.New(f.Type().Elem()).Elem())
			}
			if f.Len() > 0 && f.Pointer() == v.Field(i).Pointer() {
				t.Errorf("%s: the clone shares its slice", v.Type().Field(i).Name)
			}
		}
	}
}

// TestNormalizeIsSettled: settings once normalized stay as they are, so
// that the value a slider shows after apply is the one in effect, and the
// rounding of each value is written in Normalize alone.
func TestNormalizeIsSettled(t *testing.T) {
	odd := Default()
	odd.Width, odd.Spacing, odd.CardsAt, odd.FlightSpeed = 247.3, 13.6, 0.437, 1.27
	odd.PanelWidth, odd.PanelHeight, odd.PanelOpacity, odd.PanelBlur = 0.333, 41.7, 0.666, 9.5
	odd.CornerWidth, odd.StagedOpacity, odd.StagedGray, odd.IconScale = 101.2, 0.555, 0.123, 1.234
	for _, s := range []Settings{Default(), odd, {}} {
		n := s.Normalize()
		if again := n.Normalize(); !reflect.DeepEqual(again, n) {
			t.Errorf("normalized twice differs:\n%+v\n%+v", n, again)
		}
	}
	if w := odd.Normalize().Width; w != 250 {
		t.Errorf("width %v, want it in steps of %v", w, WidthStep)
	}
}

// TestBrokenFileIsKept: settings that cannot be read are not lost to the
// next save.
func TestBrokenFileIsKept(t *testing.T) {
	st := NewStore(t.TempDir())
	broken := []byte(`{"width": "240"}`)
	if err := os.MkdirAll(filepath.Dir(st.Path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	st.Load()
	if err := st.Save(Default()); err != nil {
		t.Fatal(err)
	}
	kept, err := os.ReadFile(st.Path + ".bad")
	if err != nil || string(kept) != string(broken) {
		t.Fatalf("the broken settings were not kept: %q, %v", kept, err)
	}
}

// TestSaveLeavesNoHalfFile: what a save writes reaches the disk as a
// whole file, whatever a power cut takes mid-way; the old settings stand
// until the new ones are in place.
func TestSaveLeavesNoHalfFile(t *testing.T) {
	st := NewStore(t.TempDir())
	if err := st.Save(Default()); err != nil {
		t.Fatal(err)
	}
	// The copy is pushed to the disk before the rename: the .tmp file is
	// gone once Save returns, and what the settings name is whole.
	if _, err := os.Stat(st.Path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("the file written beside the settings is left over: %v", err)
	}
	if _, err := os.Stat(st.Path); err != nil {
		t.Fatal(err)
	}
}
