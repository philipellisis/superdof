package dof

import "time"

// Effect is a node in the effect chain of a table element.
type Effect interface {
	Name() string
	SetName(string)
	Trigger(TED)
	Init(t *Table)
	Finish()
}

type effectBase struct{ name string }

func (b *effectBase) Name() string     { return b.name }
func (b *effectBase) SetName(n string) { b.name = n }
func (b *effectBase) Finish()          {}

// chainEffect is an effect that forwards (modified) data to a target effect.
type chainEffect struct {
	effectBase
	targetName string
	target     Effect
	table      *Table
}

func (c *chainEffect) Init(t *Table) {
	c.table = t
	if e, ok := t.Effects[c.targetName]; ok {
		c.target = e
	}
}

func (c *chainEffect) fire(d TED) {
	if c.target != nil {
		c.target.Trigger(d)
	}
}

func (c *chainEffect) alarms() *Alarms { return &c.table.engine.alarms }

// FadeMode selects whether leaf effects scale with the trigger value.
type FadeMode int

const (
	FadeModeFade FadeMode = iota
	FadeModeOnOff
)

// ---------------------------------------------------------------- leaf effects

// analogEffect writes a value/alpha pair into a layer of an analog toy.
type analogEffect struct {
	effectBase
	toyName  string
	layerNr  int
	active   AnalogAlpha
	inactive AnalogAlpha
	mode     FadeMode
	layer    *AnalogAlpha
}

func (e *analogEffect) Init(t *Table) {
	if toy, ok := t.cab.toys[e.toyName].(*AnalogAlphaToy); ok {
		e.layer = toy.Layer(e.layerNr)
	}
}

func (e *analogEffect) Trigger(d TED) {
	if e.layer == nil {
		return
	}
	v := d.Value
	if e.mode == FadeModeOnOff && v > 0 {
		v = 255
	}
	e.layer.Value = e.inactive.Value + limit(int(float32(e.active.Value-e.inactive.Value)*float32(v)/255), 0, 255)
	e.layer.Alpha = e.inactive.Alpha + limit(int(float32(e.active.Alpha-e.inactive.Alpha)*float32(v)/255), 0, 255)
}

// rgbaEffect writes a color into a layer of an RGBA toy.
type rgbaEffect struct {
	effectBase
	toyName  string
	layerNr  int
	active   RGBA
	inactive RGBA
	mode     FadeMode
	layer    *RGBA
}

func (e *rgbaEffect) Init(t *Table) {
	if toy, ok := t.cab.toys[e.toyName].(*RGBAToy); ok {
		e.layer = toy.Layer(e.layerNr)
	}
}

func (e *rgbaEffect) Trigger(d TED) {
	if e.layer == nil {
		return
	}
	v := d.Value
	if e.mode == FadeModeOnOff && v > 0 {
		v = 255
	}
	f := float32(v) / 255
	e.layer.R = e.inactive.R + limit(int(float32(e.active.R-e.inactive.R)*f), 0, 255)
	e.layer.G = e.inactive.G + limit(int(float32(e.active.G-e.inactive.G)*f), 0, 255)
	e.layer.B = e.inactive.B + limit(int(float32(e.active.B-e.inactive.B)*f), 0, 255)
	e.layer.A = e.inactive.A + limit(int(float32(e.active.A-e.inactive.A)*f), 0, 255)
}

// ------------------------------------------------------------- chain effects

type fadeEffect struct {
	chainEffect
	up, down  int
	targetVal float32
	current   float32
	step      float32
	lastTrig  int
	data      TED
}

func newFade(name, target string, up, down int) *fadeEffect {
	f := &fadeEffect{up: up, down: down, targetVal: -1, lastTrig: -1}
	f.name, f.targetName = name, target
	return f
}

const fadingRefreshMs = 30

func (f *fadeEffect) Trigger(d TED) {
	if f.target == nil || float32(d.Value) == f.targetVal {
		return
	}
	f.targetVal = float32(limit(d.Value, 0, 255))
	f.data = d
	dur := float64(f.down)
	if f.current < f.targetVal {
		dur = float64(f.up)
	}
	steps := 0
	if dur > 0 {
		steps = int(dur / fadingRefreshMs)
	}
	if steps > 0 {
		f.step = (f.targetVal - f.current) / float32(steps)
		f.lastTrig = -1
		f.fadingStep()
		return
	}
	f.alarms().UnregisterInterval(f)
	f.current = f.targetVal
	f.lastTrig = -1
	f.fire(d)
}

func (f *fadeEffect) fadingStep() {
	f.current += f.step
	if (f.current < f.targetVal && f.step > 0) || (f.current > f.targetVal && f.step < 0) {
		f.alarms().RegisterInterval(fadingRefreshMs*time.Millisecond, f, f.fadingStep)
	} else {
		f.alarms().UnregisterInterval(f)
		f.current = f.targetVal
	}
	if f.lastTrig != int(f.current) {
		f.lastTrig = int(f.current)
		f.data.Value = f.lastTrig
		f.fire(f.data)
	}
}

func (f *fadeEffect) Finish() { f.alarms().UnregisterInterval(f) }

type blinkEffect struct {
	chainEffect
	high       int // -1 = use trigger value
	low        int
	activeMs   int
	inactiveMs int
	enabled    bool
	state      bool
	origValue  int
	data       TED
}

func (b *blinkEffect) Trigger(d TED) {
	if b.target == nil {
		return
	}
	if d.Value != 0 {
		b.data = d
		b.origValue = d.Value
		if !b.enabled {
			b.enabled = true
			b.state = false
			b.doBlink()
		} else if b.state {
			b.data.Value = b.highValue()
			b.fire(b.data)
		}
		return
	}
	b.enabled = false
	b.alarms().Unregister(b)
	b.data.Value = 0
	b.fire(b.data)
}

func (b *blinkEffect) highValue() int {
	if b.high >= 0 {
		return b.high
	}
	return b.origValue
}

func (b *blinkEffect) doBlink() {
	b.state = !b.state
	if b.state {
		b.data.Value = b.highValue()
		b.alarms().Register(time.Duration(b.activeMs)*time.Millisecond, b, b.doBlink, false)
	} else if b.enabled {
		b.data.Value = b.low
		b.alarms().Register(time.Duration(b.inactiveMs)*time.Millisecond, b, b.doBlink, false)
	} else {
		b.data.Value = 0
	}
	b.fire(b.data)
}

func (b *blinkEffect) Finish() { b.alarms().Unregister(b) }

type durationEffect struct {
	chainEffect
	ms     int
	active bool
}

func (e *durationEffect) Trigger(d TED) {
	if e.target == nil || d.Value == 0 {
		return
	}
	dd := d
	end := func() {
		dd.Value = 0
		e.fire(dd)
		e.active = false
	}
	if !e.active {
		e.fire(d)
		e.active = true
	}
	e.alarms().Register(time.Duration(e.ms)*time.Millisecond, e, end, false)
}

func (e *durationEffect) Finish() { e.alarms().Unregister(e); e.active = false }

type maxDurationEffect struct {
	chainEffect
	ms     int
	active bool
	untrig TED
}

func (e *maxDurationEffect) Trigger(d TED) {
	if d.Value != 0 {
		e.fire(d)
		e.untrig = d
		e.untrig.Value = 0
		e.alarms().Register(time.Duration(e.ms)*time.Millisecond, e, func() {
			e.fire(e.untrig)
			e.active = false
		}, false)
		e.active = true
		return
	}
	if e.active && e.untrig.Type == d.Type && e.untrig.Number == d.Number {
		e.fire(d)
		e.alarms().Unregister(e)
		e.active = false
	}
}

func (e *maxDurationEffect) Finish() { e.alarms().Unregister(e); e.active = false }

type minDurationEffect struct {
	chainEffect
	ms     int
	active bool
	untrig TED
	start  time.Time
}

func (e *minDurationEffect) Trigger(d TED) {
	if e.target == nil {
		return
	}
	if d.Value != 0 {
		e.start = time.Now()
		e.untrig = d
		e.fire(d)
		e.active = true
		return
	}
	if e.active && e.untrig.Type == d.Type && e.untrig.Number == d.Number {
		el := time.Since(e.start)
		if el >= time.Duration(e.ms)*time.Millisecond {
			e.end()
		} else {
			e.alarms().Register(time.Duration(e.ms)*time.Millisecond-el, e, e.end, false)
		}
	}
}

func (e *minDurationEffect) end() {
	if e.active {
		d := e.untrig
		d.Value = 0
		e.fire(d)
	}
	e.active = false
}

func (e *minDurationEffect) Finish() { e.alarms().Unregister(e); e.active = false }

type extendDurationEffect struct {
	chainEffect
	ms int
}

func (e *extendDurationEffect) Trigger(d TED) {
	if e.target == nil {
		return
	}
	if d.Value != 0 {
		e.fire(d)
		return
	}
	if e.ms > 0 {
		dd := d
		e.alarms().Register(time.Duration(e.ms)*time.Millisecond, e, func() { e.fire(dd) }, true)
	} else {
		e.fire(d)
	}
}

func (e *extendDurationEffect) Finish() { e.alarms().Unregister(e) }

type delayEffect struct {
	chainEffect
	ms int
}

func (e *delayEffect) Trigger(d TED) {
	if e.target == nil {
		return
	}
	if e.ms > 0 {
		dd := d
		e.alarms().Register(time.Duration(e.ms)*time.Millisecond, e, func() { e.fire(dd) }, true)
	} else {
		e.fire(d)
	}
}

func (e *delayEffect) Finish() { e.alarms().Unregister(e) }

type invertEffect struct{ chainEffect }

func (e *invertEffect) Trigger(d TED) { d.Value = 255 - d.Value; e.fire(d) }

type elemKeyName struct {
	t ElementType
	n int
	s string
}

type fullRangeEffect struct {
	chainEffect
	prev map[elemKeyName]int
}

func (e *fullRangeEffect) Trigger(d TED) {
	if d.Value != 0 {
		d.Value = 255
	}
	k := elemKeyName{d.Type, d.Number, d.Name}
	if p, ok := e.prev[k]; !ok || p != d.Value {
		e.fire(d)
		e.prev[k] = d.Value
	}
}
