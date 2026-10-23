// Package stage models what Stage Manager keeps per display: the apps on
// the stage, and the sets of apps in the strip. As on macOS, apps put on
// the stage together stay together: they leave it as one card and come
// back as one. It knows nothing of windows, so the rules are easy to test.
package stage

import (
	"slices"
	"time"
)

// SettleTime is how long a stage keeps an app whose windows are not
// visible yet, as when it is being restored or its windows are moving.
const SettleTime = time.Second

// Display is the stage of one display. An app is on the stage, in one
// set of the strip, or nowhere.
type Display struct {
	stage []string   // apps on the stage, in the order they came
	sets  [][]string // the strip, top first: the apps of each card
	// above is where in the strip the apps on the stage came from: the
	// first app of the set right above their card, or "" for the top.
	above     string
	changedAt time.Time
	// recent lists the apps that were switched off the stage, the last
	// to leave first, for the stage to fall back on when what is on it
	// closes.
	recent []string
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

// SetOf returns the apps that go on the stage with app: the stage if it
// is there, its set if it is in the strip, else app alone.
func (d *Display) SetOf(app string) []string {
	if d.OnStage(app) {
		return d.Stage()
	}
	if i := d.setIndex(app); i >= 0 {
		return slices.Clone(d.sets[i])
	}
	return []string{app}
}

// Coming returns the apps on the stage once the user goes to a window of
// app: its set, in place of those there. A companion, a tool used for a
// moment beside the app it was opened from, joins them instead, unless it
// is in a set with other apps: then the user is going back to that set.
func (d *Display) Coming(app string, companion bool) []string {
	if companion && !d.Empty() && !d.OnStage(app) {
		if i := d.setIndex(app); i < 0 || len(d.sets[i]) == 1 {
			return append(d.Stage(), app)
		}
	}
	return d.SetOf(app)
}

func (d *Display) setIndex(app string) int {
	return slices.IndexFunc(d.sets, func(s []string) bool { return slices.Contains(s, app) })
}

// drop takes the apps gone tells of out of the sets of the strip; sets
// left empty go.
func (d *Display) drop(gone func(app string) bool) {
	for i := range d.sets {
		d.sets[i] = slices.DeleteFunc(d.sets[i], gone)
	}
	d.sets = slices.DeleteFunc(d.sets, func(s []string) bool { return len(s) == 0 })
}

// without takes app out of its set in the strip.
func (d *Display) without(app string) {
	d.drop(func(a string) bool { return a == app })
}

// Strip returns the sets of the strip, top first. present lists the apps
// that have windows on the display, front first. Apps the strip has not
// seen enter at the top on their own, in that order, so the very first
// strip follows the windows' z-order; apps that are gone leave their set.
func (d *Display) Strip(present []string) [][]string {
	d.drop(func(a string) bool { return !slices.Contains(present, a) })
	d.recent = slices.DeleteFunc(d.recent, func(a string) bool { return !slices.Contains(present, a) })
	var newcomers [][]string
	for _, a := range present {
		if !d.OnStage(a) && d.setIndex(a) < 0 && !slices.ContainsFunc(newcomers, func(s []string) bool { return s[0] == a }) {
			newcomers = append(newcomers, []string{a})
		}
	}
	d.sets = append(newcomers, d.sets...)
	out := make([][]string, len(d.sets))
	for i, s := range d.sets {
		out[i] = slices.Clone(s)
	}
	return out
}

// Switch makes next the apps on the stage. The apps leaving it go to the
// strip together, as one set, in the slot of the first set coming in, as
// on macOS, so the rest of the strip stays where it was; with no slot to
// take, as when the wallpaper is clicked, they go back where they came
// from, or enter at the top. What is left of a set some of whose apps came
// in stays a set.
func (d *Display) Switch(next []string, now time.Time) {
	coming := func(a string) bool { return slices.Contains(next, a) }
	leaving := slices.DeleteFunc(slices.Clone(d.stage), coming)
	slot := slices.IndexFunc(d.sets, func(s []string) bool { return slices.ContainsFunc(s, coming) })
	above := d.above
	if !slices.ContainsFunc(d.stage, coming) {
		// A new stage, rather than apps joining the one there.
		above = ""
		if slot > 0 {
			above = d.sets[slot-1][0]
		}
	}
	if len(leaving) > 0 {
		if slot < 0 {
			slot = d.cameFrom()
		}
		d.sets = slices.Insert(d.sets, slot, leaving)
		d.recent = slices.DeleteFunc(d.recent, func(a string) bool { return slices.Contains(leaving, a) })
		d.recent = slices.Insert(d.recent, 0, leaving...)
	}
	d.drop(coming)
	d.stage = slices.Clone(next)
	d.above = above
	d.changedAt = now
}

// cameFrom is the slot of the strip the apps on the stage came from: right
// below the set that was above them, or the top once that app left the strip.
func (d *Display) cameFrom() int {
	if d.above == "" {
		return 0
	}
	return d.setIndex(d.above) + 1
}

// Join adds an app to the stage beside those already there. It leaves its
// set; the others of the set stay in the strip.
func (d *Display) Join(app string, now time.Time) {
	if d.OnStage(app) {
		return
	}
	d.stage = append(d.stage, app)
	d.without(app)
	d.changedAt = now
}

// Leave takes an app off the stage into a set of its own at the top of
// the strip, as when all its windows are closed.
func (d *Display) Leave(app string) {
	if !d.OnStage(app) {
		return
	}
	d.stage = slices.DeleteFunc(d.stage, func(a string) bool { return a == app })
	d.without(app)
	d.sets = slices.Insert(d.sets, 0, []string{app})
	// Its windows closed: it is no stage to go back to.
	d.recent = slices.DeleteFunc(d.recent, func(a string) bool { return a == app })
}

// Previous returns the set in the strip of the app last switched off the
// stage, or nil: what comes back when the apps that took its place close,
// as when the user went to them from the taskbar rather than from it.
func (d *Display) Previous() []string {
	for _, a := range d.recent {
		if i := d.setIndex(a); i >= 0 {
			return slices.Clone(d.sets[i])
		}
	}
	return nil
}

// Split breaks the set of app in the strip into sets of one app each, in
// its place.
func (d *Display) Split(app string) {
	i := d.setIndex(app)
	if i < 0 || len(d.sets[i]) < 2 {
		return
	}
	apart := make([][]string, len(d.sets[i]))
	for j, a := range d.sets[i] {
		apart[j] = []string{a}
	}
	d.sets = slices.Replace(d.sets, i, i+1, apart...)
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
	d.stage, d.above = nil, ""
	if app != "" {
		d.stage = []string{app}
		d.without(app)
	}
	d.changedAt = now
}
