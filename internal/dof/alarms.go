package dof

import (
	"sort"
	"time"
)

type alarm struct {
	at  time.Time
	key any
	fn  func()
}

type intervalAlarm struct {
	key      any
	interval time.Duration
	next     time.Time
	fn       func()
}

// Alarms is the single-threaded timer facility of the engine (AlarmHandler in
// DOF). All methods must only be called from the engine goroutine.
type Alarms struct {
	list      []alarm
	intervals []*intervalAlarm
}

// Register schedules fn after d. Unless keepExisting is true, previous alarms
// registered for the same key are replaced (DOF's "DoNotUnregister" flag).
func (a *Alarms) Register(d time.Duration, key any, fn func(), keepExisting bool) {
	if !keepExisting {
		a.Unregister(key)
	}
	a.list = append(a.list, alarm{at: time.Now().Add(d), key: key, fn: fn})
}

func (a *Alarms) Unregister(key any) {
	out := a.list[:0]
	for _, x := range a.list {
		if x.key != key {
			out = append(out, x)
		}
	}
	a.list = out
}

func (a *Alarms) RegisterInterval(d time.Duration, key any, fn func()) {
	a.UnregisterInterval(key)
	a.intervals = append(a.intervals, &intervalAlarm{key: key, interval: d, next: time.Now().Add(d), fn: fn})
}

func (a *Alarms) UnregisterInterval(key any) {
	out := a.intervals[:0]
	for _, x := range a.intervals {
		if x.key != key {
			out = append(out, x)
		}
	}
	a.intervals = out
}

// Next returns the time of the next pending alarm or the zero time.
func (a *Alarms) Next() (time.Time, bool) {
	var best time.Time
	found := false
	for _, x := range a.list {
		if !found || x.at.Before(best) {
			best, found = x.at, true
		}
	}
	for _, x := range a.intervals {
		if !found || x.next.Before(best) {
			best, found = x.next, true
		}
	}
	return best, found
}

// Execute runs all alarms due at or before now and reports whether any ran.
func (a *Alarms) Execute(now time.Time) bool {
	ran := false
	var due []alarm
	rest := a.list[:0]
	for _, x := range a.list {
		if !x.at.After(now) {
			due = append(due, x)
		} else {
			rest = append(rest, x)
		}
	}
	a.list = rest
	sort.SliceStable(due, func(i, j int) bool { return due[i].at.Before(due[j].at) })
	for _, x := range due {
		x.fn()
		ran = true
	}
	for _, x := range append([]*intervalAlarm(nil), a.intervals...) {
		if x.next.After(now) {
			continue
		}
		x.fn()
		ran = true
		if x.next.Add(x.interval).After(now) {
			x.next = x.next.Add(x.interval)
		} else {
			x.next = now.Add(time.Millisecond)
		}
	}
	return ran
}

func (a *Alarms) Clear() {
	a.list = nil
	a.intervals = nil
}
