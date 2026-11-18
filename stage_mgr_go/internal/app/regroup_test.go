package app

import (
	"testing"

	"github.com/e1roy/stage_manager_for_windows/internal/settings"
)

// Turning the split on renames the stages at the next refresh, unless a
// switch asked for meanwhile still names the apps as they were.
func TestRegroupDue(t *testing.T) {
	a := &App{settings: settings.Default(), grouping: settings.Default(), queued: map[*display]*switchRequest{}}
	if a.regroupDue() {
		t.Fatal("regroup due with nothing changed")
	}
	a.settings.SplitWindows = true
	if !a.regroupDue() {
		t.Fatal("turning the split on is not followed")
	}
	a.queued[&display{}] = &switchRequest{}
	if a.regroupDue() {
		t.Fatal("regroup due while a switch is queued")
	}
	clear(a.queued)
	// The apps kept together only matter while windows are split.
	a.grouping = a.settings.Clone()
	a.settings.KeepTogether = nil
	if !a.regroupDue() {
		t.Fatal("a change of the apps kept together is not followed")
	}
	a.settings.SplitWindows, a.grouping.SplitWindows = false, false
	if a.regroupDue() {
		t.Fatal("regroup due for the apps kept together while windows are not split")
	}
}
