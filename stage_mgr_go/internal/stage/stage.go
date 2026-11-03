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
	// keep lists the apps on the stage in the strip too, as one set in the
	// slot it holds (see Keep): the strip is then the same list whichever
	// app the stage is on, and no card moves for a switch.
	keep bool
}

// Keep has the strip list the apps on the stage as well, or no longer. On
// macOS the apps leaving the stage take the slot of the card coming in, so
// the cards drift with every switch; kept, each set has a slot of its own,
// the stage's included, where it stays whether it is on the stage or not.
// The stage's set is then one of those Strip returns, and Switch leaves the
// others where they are.
func (d *Display) Keep(on bool) {
	if on == d.keep {
		return
	}
	d.keep = on
	if on {
		d.seat()
		return
	}
	d.drop(d.OnStage)
}

// seat makes the apps on the stage a set of the strip, in the slot they came
// from, unless the strip holds them already.
func (d *Display) seat() {
	if len(d.stage) == 0 || slices.ContainsFunc(d.stage, func(a string) bool { return d.setIndex(a) >= 0 }) {
		return
	}
	d.sets = slices.Insert(d.sets, min(d.cameFrom(), len(d.sets)), slices.Clone(d.stage))
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
// A kept strip (Keep) lists the apps on the stage too, as one set, and
// takes new apps at the bottom, so that the cards before them stay put.
func (d *Display) Strip(present []string) [][]string {
	d.drop(func(a string) bool { return !slices.Contains(present, a) })
	d.recent = slices.DeleteFunc(d.recent, func(a string) bool { return !slices.Contains(present, a) })
	var newcomers [][]string
	seated := false
	for _, a := range present {
		switch {
		case d.setIndex(a) >= 0:
		case d.OnStage(a):
			// Only a kept strip has the stage in it; the stage's apps it
			// holds none of yet come in together, as they are on the stage.
			if d.keep && !seated {
				seated = true
				newcomers = append(newcomers, slices.DeleteFunc(slices.Clone(d.stage), func(s string) bool {
					return d.setIndex(s) >= 0 || !slices.Contains(present, s)
				}))
			}
		case !slices.ContainsFunc(newcomers, func(s []string) bool { return s[0] == a }):
			newcomers = append(newcomers, []string{a})
		}
	}
	if d.keep {
		d.sets = append(d.sets, newcomers...)
	} else {
		d.sets = append(newcomers, d.sets...)
	}
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
	if d.keep {
		d.switchKept(next, now)
		return
	}
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

// switchKept is Switch for a kept strip, where the stage is a set of the
// strip like the others: the apps leaving it stay in their slot, and the
// set going on the stage is made in its own. A stage that stays in part, as
// when apps join it, keeps its slot; otherwise it takes that of the first set
// coming in.
func (d *Display) switchKept(next []string, now time.Time) {
	d.seat()
	coming := func(a string) bool { return slices.Contains(next, a) }
	leaving := slices.DeleteFunc(slices.Clone(d.stage), coming)
	has := func(f func(string) bool) func([]string) bool {
		return func(s []string) bool { return slices.ContainsFunc(s, f) }
	}
	anchor := slices.IndexFunc(d.sets, func(s []string) bool { return has(d.OnStage)(s) && has(coming)(s) })
	if anchor < 0 {
		anchor = slices.IndexFunc(d.sets, has(coming))
	}
	// The slot once the sets that go on the stage are out of the strip: the
	// ones before the anchor that stay.
	at := len(d.sets)
	if anchor >= 0 {
		at = 0
		for _, s := range d.sets[:anchor] {
			if slices.ContainsFunc(s, func(a string) bool { return !coming(a) }) {
				at++
			}
		}
	}
	d.drop(coming)
	d.above = ""
	if len(next) > 0 {
		d.sets = slices.Insert(d.sets, at, slices.Clone(next))
		if at > 0 {
			d.above = d.sets[at-1][0]
		}
	}
	if len(leaving) > 0 {
		d.recent = slices.DeleteFunc(d.recent, func(a string) bool { return slices.Contains(leaving, a) })
		d.recent = slices.Insert(d.recent, 0, leaving...)
	}
	d.stage = slices.Clone(next)
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

// Insert puts apps into the strip as one set at slot at, top first, as
// when their card is dragged in from the strip of another display. Apps
// on the stage stay there, and the others leave the sets they were in.
// Call it once the apps have windows on the display: Strip drops those
// that have none.
func (d *Display) Insert(apps []string, at int) {
	apps = slices.DeleteFunc(slices.Clone(apps), d.OnStage)
	if len(apps) == 0 {
		return
	}
	// The slot is counted before the apps leave theirs, as the strip
	// showed it.
	for i := range min(at, len(d.sets)) {
		if !slices.ContainsFunc(d.sets[i], func(a string) bool { return !slices.Contains(apps, a) }) {
			at--
		}
	}
	d.drop(func(a string) bool { return slices.Contains(apps, a) })
	d.sets = slices.Insert(d.sets, min(max(at, 0), len(d.sets)), apps)
}

// Move puts the set of app at slot at of the strip, top first, the
// others keeping their order, as when its card is dragged up or down.
func (d *Display) Move(app string, at int) {
	i := d.setIndex(app)
	if i < 0 {
		return
	}
	s := d.sets[i]
	d.sets = slices.Delete(d.sets, i, i+1)
	d.sets = slices.Insert(d.sets, min(max(at, 0), len(d.sets)), s)
}

// Leave takes an app off the stage into a set of its own at the top of
// the strip, as when all its windows are closed.
func (d *Display) Leave(app string) {
	if !d.OnStage(app) {
		return
	}
	d.stage = slices.DeleteFunc(d.stage, func(a string) bool { return a == app })
	if d.keep {
		// It stays where it is in the strip; if the stage's set was of
		// more apps, the others stay on the stage in a set of their own.
		if i := d.setIndex(app); i >= 0 && len(d.sets[i]) > 1 {
			rest := slices.DeleteFunc(slices.Clone(d.sets[i]), func(a string) bool { return a == app })
			d.sets = slices.Replace(d.sets, i, i+1, rest, []string{app})
		}
	} else {
		d.without(app)
		d.sets = slices.Insert(d.sets, 0, []string{app})
	}
	// Its windows closed: it is no stage to go back to.
	d.recent = slices.DeleteFunc(d.recent, func(a string) bool { return a == app })
}

// Previous returns the set in the strip of the app last switched off the
// stage, or nil: what comes back when the apps that took its place close,
// as when the user went to them from the taskbar rather than from it.
func (d *Display) Previous() []string {
	for _, a := range d.recent {
		// A kept strip holds the stage too: that is no stage to go back to.
		if d.OnStage(a) {
			continue
		}
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
	// The apps on the stage are one set while they are there.
	if i < 0 || len(d.sets[i]) < 2 || d.OnStage(app) {
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
		if !d.keep {
			d.without(app)
		} else if i := d.setIndex(app); i < 0 {
			d.seat()
		} else {
			// In the slot it has, as a set of its own.
			if len(d.sets[i]) > 1 {
				rest := slices.DeleteFunc(slices.Clone(d.sets[i]), func(a string) bool { return a == app })
				d.sets = slices.Replace(d.sets, i, i+1, []string{app}, rest)
			}
			if i > 0 {
				d.above = d.sets[i-1][0]
			}
		}
	}
	d.changedAt = now
}
