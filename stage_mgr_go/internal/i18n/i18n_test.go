package i18n

import (
	"reflect"
	"regexp"
	"slices"
	"testing"
)

// verbs finds the formatting verbs of a string, %% included.
var verbs = regexp.MustCompile(`%[-+# 0-9.]*[a-z%]`)

// TestComplete makes sure every language has every string, with the
// verbs of the English one in the same order.
func TestComplete(t *testing.T) {
	ref := reflect.ValueOf(en)
	for _, l := range Langs {
		v := reflect.ValueOf(*For(l))
		for i := range v.NumField() {
			name, s := v.Type().Field(i).Name, v.Field(i).String()
			if s == "" {
				t.Errorf("%s: %s is empty", l, name)
			}
			if got, want := verbs.FindAllString(s, -1), verbs.FindAllString(ref.Field(i).String(), -1); !slices.Equal(got, want) {
				t.Errorf("%s: %s has verbs %q, want %q", l, name, got, want)
			}
		}
	}
}

func TestFromLANGID(t *testing.T) {
	for id, want := range map[uint16]Lang{
		0x0804: SimplifiedChinese,  // zh-CN
		0x1004: SimplifiedChinese,  // zh-SG
		0x0404: TraditionalChinese, // zh-TW
		0x0c04: TraditionalChinese, // zh-HK
		0x1404: TraditionalChinese, // zh-MO
		0x7c04: TraditionalChinese, // zh-Hant
		0x0409: English,
		0x0809: English, // en-GB
		0x0411: Japanese,
		0x0407: English, // German has no words of its own yet
	} {
		if got := FromLANGID(id); got != want {
			t.Errorf("FromLANGID(%#04x) = %q, want %q", id, got, want)
		}
	}
}

func TestFor(t *testing.T) {
	if For("fr") != For(English) || For(Auto) != For(English) {
		t.Fatal("unknown languages should fall back to English")
	}
	if !Known(Auto) || !Known(Japanese) || Known("fr") {
		t.Fatal("Known is wrong")
	}
}
