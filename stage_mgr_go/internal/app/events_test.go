package app

import (
	"testing"
	"time"

	"github.com/e1roy/stage_manager_for_windows/internal/catalog"
	"github.com/e1roy/stage_manager_for_windows/internal/stage"
	"github.com/e1roy/stage_manager_for_windows/internal/winapi"
)

func TestMinimizeTargetOnlyTakesStagedGroup(t *testing.T) {
	var first, other stage.Display
	first.Init("editor", time.Now())
	first.Switch([]string{"editor", "terminal"}, time.Now())
	other.Init("browser", time.Now())
	a := &App{displays: []*display{{model: first}, {model: other}}}
	// Resolve from the cached window's app and display, before the catalog
	// removes the iconic HWND. A window on another stage is not this target.
	d := a.displays[0]
	if got := minimizeNext(d, "editor"); got == nil || len(got) != 0 {
		t.Fatalf("group should leave stage = %v", got)
	}
	if got := minimizeNext(d, "absent"); got != nil {
		t.Fatalf("unstaged app changed stage: %v", got)
	}
	if got := minimizeNext(a.displays[1], "browser"); len(got) != 0 || got == nil {
		t.Fatalf("last app should clear stage: %v", got)
	}
}

func TestMinimizeBringsBackThePreviousStage(t *testing.T) {
	var m stage.Display
	m.Init("old", time.Now())
	m.Switch([]string{"editor"}, time.Now())
	d := &display{model: m}
	if got := minimizeNext(d, "editor"); len(got) != 1 || got[0] != "old" {
		t.Fatalf("minimize should bring back the previous stage, got %v", got)
	}
}

func TestMinimizeLeftToWindowsKeepsItsAnimation(t *testing.T) {
	var m stage.Display
	m.Init("editor", time.Now())
	a := &App{displays: []*display{{mon: winapi.Monitor{Device: "on"}, model: m}}}
	a.settings.HideInactive = true
	if a.calmWanted(catalog.Window{App: "editor"}) {
		t.Fatal("with the window going to the taskbar, its minimize animation plays")
	}
}

func TestClearStagesSkipsEmptyAndDisabledAndDoesNotToggle(t *testing.T) {
	var filled, empty, disabled stage.Display
	filled.Init("editor", time.Now())
	disabled.Init("other", time.Now())
	a := &App{displays: []*display{{mon: winapi.Monitor{Device: "on"}, model: filled}, {mon: winapi.Monitor{Device: "empty"}, model: empty}, {mon: winapi.Monitor{Device: "off"}, model: disabled}}}
	a.settings.Displays = map[string]bool{"off": false}
	got := a.stagesToClear()
	if len(got) != 1 || got[0] != a.displays[0] {
		t.Fatalf("stages to clear = %v", got)
	}
	a.displays[0].model.Switch(nil, time.Now())
	if got = a.stagesToClear(); len(got) != 0 {
		t.Fatalf("second clear should remain empty: %v", got)
	}
}

func TestMinimizeWaitsForNativeIconicTransition(t *testing.T) {
	target := &minimizeTarget{}
	if restore, done := target.observe(false); restore || done {
		t.Fatal("do not switch before native minimize has happened")
	}
	if restore, done := target.observe(true); !restore || done {
		t.Fatal("iconic window must be restored before parking")
	}
	if restore, done := target.observe(false); restore || !done {
		t.Fatal("switch only after the asynchronous restore has completed")
	}
}

func TestClearStagesHonorsStageInFlight(t *testing.T) {
	var model stage.Display
	model.Init("old", time.Now())
	d := &display{mon: winapi.Monitor{Device: "on"}, model: model}
	a := &App{displays: []*display{d}, queued: map[*display]*switchRequest{}}
	a.running = &switchRequest{d: d, next: []string{"new"}}
	if got := a.stagesToClear(); len(got) != 1 || got[0] != d {
		t.Fatalf("incoming stage must be cleared: %v", got)
	}
	a.queued[d] = &switchRequest{d: d, next: nil}
	if got := a.stagesToClear(); len(got) != 0 {
		t.Fatalf("clear already queued must not toggle: %v", got)
	}
}

func TestDesktopShortcutsOnlyTakenWhenStagesCanBeHidden(t *testing.T) {
	a := &App{displays: []*display{{mon: winapi.Monitor{Device: "enabled"}}}}
	if a.wantsWinDesktop() {
		t.Fatal("native shortcuts must work when hideInactive is off")
	}
	a.settings.HideInactive = true
	if !a.wantsWinDesktop() {
		t.Fatal("managed stage should take shortcuts")
	}
	a.settings.Displays = map[string]bool{"enabled": false}
	if a.wantsWinDesktop() {
		t.Fatal("native shortcuts must work with no managed displays")
	}
	a.settings.Displays = nil
	a.quitting = true
	if a.wantsWinDesktop() {
		t.Fatal("native shortcuts must work after shutdown")
	}
}
