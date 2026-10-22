// Package stage models what Stage Manager keeps per display: the apps on
// the stage, and the order of the others in the strip. It knows nothing of
// windows, so the rules are easy to test.
package stage

import (
	"slices"
	"time"
)

// SettleTime is how long a stage keeps an app whose windows are not
// visible yet, as when it is being restored or its windows are moving.
const SettleTime = time.Second

// Display is the stage of one display.
type Display struct {
	stage     []string // apps on the stage, in the order they came
	order     []string // the strip, top first
	changedAt time.Time
}

// OnStage reports whether an app is on the stage.
func (d *Display) OnStage(app string) bool { return slices.Contains(d.stage, app) }

// Stage returns the apps on the stage.
func (d *Display) Stage() []string { return slices.Clone(d.stage) }

// Empty reports whether nothing is on the stage.
func (d *Display) Empty() bool { return len(d.stage) == 0 }

// ChangedAt is when the stage last changed.
func (d *Display) ChangedAt() time.Time { return d.changedAt }

// Settling reports whether the stage changed too recently for missing
// windows to mean anything.
func (d *Display) Settling(now time.Time) bool { return now.Sub(d.changedAt) < SettleTime }

// Strip returns the apps of present that are not on the stage, in strip
// order. present lists the apps that have windows on the display, front
// first. Apps the strip has not seen enter at the top in that order, so
// the very first strip follows the windows' z-order; apps that are gone
// leave it.
func (d *Display) Strip(present []string) []string {
	d.order = slices.DeleteFunc(d.order, func(a string) bool { return !slices.Contains(present, a) })
	var newcomers []string
	for _, a := range present {
		if !d.OnStage(a) && !slices.Contains(d.order, a) && !slices.Contains(newcomers, a) {
			newcomers = append(newcomers, a)
		}
	}
	d.order = append(newcomers, d.order...)
	out := make([]string, 0, len(d.order))
	for _, a := range d.order {
		if !d.OnStage(a) {
			out = append(out, a)
		}
	}
	return out
}

// Switch makes next the apps on the stage. The apps leaving it take the
// strip slot of the first app coming in, as on macOS, so the rest of the
// strip stays where it was; with no slot to take they enter at the top.
func (d *Display) Switch(next, leaving []string, now time.Time) {
	slot := -1
	for i, a := range d.order {
		if slices.Contains(next, a) {
			slot = i
			break
		}
	}
	gone := func(a string) bool { return slices.Contains(next, a) || slices.Contains(leaving, a) }
	// The slot is counted among the apps that stay.
	kept := 0
	for i := 0; i < slot; i++ {
		if !gone(d.order[i]) {
			kept++
		}
	}
	d.order = slices.DeleteFunc(d.order, gone)
	at := min(kept, len(d.order))
	if slot < 0 {
		at = 0
	}
	d.order = slices.Insert(d.order, at, leaving...)
	d.stage = slices.Clone(next)
	d.changedAt = now
}

// Join adds an app to the stage beside those already there.
func (d *Display) Join(app string, now time.Time) {
	if d.OnStage(app) {
		return
	}
	d.stage = append(d.stage, app)
	d.order = slices.DeleteFunc(d.order, func(a string) bool { return a == app })
	d.changedAt = now
}

// Leave takes an app off the stage into the top of the strip, as when all
// its windows are closed.
func (d *Display) Leave(app string) {
	if !d.OnStage(app) {
		return
	}
	d.stage = slices.DeleteFunc(d.stage, func(a string) bool { return a == app })
	d.order = slices.Insert(slices.DeleteFunc(d.order, func(a string) bool { return a == app }), 0, app)
}

// Prune takes the apps that no longer show a window off the stage, once
// the stage has settled. It returns the apps it took off.
func (d *Display) Prune(visible func(app string) bool, now time.Time) []string {
	if d.Settling(now) {
		return nil
	}
	var gone []string
	for _, a := range d.stage {
		if !visible(a) {
			gone = append(gone, a)
		}
	}
	for _, a := range gone {
		d.Leave(a)
	}
	return gone
}

// Init puts app, or nothing, on the stage without a transition, as the
// app starts.
func (d *Display) Init(app string, now time.Time) {
	d.stage = nil
	if app != "" {
		d.stage = []string{app}
	}
	d.changedAt = now
}
