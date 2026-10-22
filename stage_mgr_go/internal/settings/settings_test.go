package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeClamps(t *testing.T) {
	s := Settings{Mode: "sideways", Width: 9000, IconScale: 0.1}.Normalize()
	if s.Mode != Fixed || s.Width != MaxWidth || s.IconScale != MinIconScale {
		t.Fatalf("got %+v", s)
	}
	if s := (Settings{}).Normalize(); s.Width != 200 || s.IconScale != 1 {
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

func TestBrokenFileFallsBack(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{nope"), 0o644)
	if got := NewStore(dir).Load(); !got.Equal(Default()) {
		t.Fatalf("broken file loaded %+v", got)
	}
}
