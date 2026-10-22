package dof

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// AnalogAlpha is a single layer value of an analog toy.
type AnalogAlpha struct{ Value, Alpha int }

// RGBA is a single layer color of an RGB toy.
type RGBA struct{ R, G, B, A int }

// Curve maps a 0-255 value to an output value.
type Curve [256]byte

func linearCurve(max int) Curve {
	var c Curve
	for i := range c {
		c[i] = byte(limit(int(float64(max)/255*float64(i)), 0, 255))
	}
	return c
}

// CurveByName returns a built in curve by DOF curve type name (default linear).
func CurveByName(name string) Curve {
	switch strings.ToLower(name) {
	case "swisslizardsledcurve":
		return Curve(swissLizardCurve)
	case "invertedlinear":
		var c Curve
		for i := range c {
			c[i] = byte(255 - i)
		}
		return c
	}
	if m := regexp.MustCompile(`(?i)^linear0to(\d+)$`).FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return linearCurve(n)
	}
	var c Curve
	for i := range c {
		c[i] = byte(i)
	}
	return c
}

// Output is a single physical output channel. Its effective value is the
// maximum of the DOF toy value and values forced by external sources
// (the DOFLinx layer), so the two never fight over the hardware.
type Output struct {
	Name   string
	Number int
	mu     sync.Mutex
	toy    byte
	ext    map[any]byte
	extMax byte
	onChg  func()
}

func (o *Output) recalc() byte {
	m := o.toy
	if o.extMax > m {
		m = o.extMax
	}
	return m
}

// Value returns the effective output value.
func (o *Output) Value() byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.recalc()
}

// SetToy sets the value written by DOF toys.
func (o *Output) SetToy(v byte) {
	o.mu.Lock()
	changed := o.toy != v
	o.toy = v
	o.mu.Unlock()
	if changed && o.onChg != nil {
		o.onChg()
	}
}

// SetExternal sets the value of an external source identified by key.
// A value of 0 removes the source.
func (o *Output) SetExternal(key any, v byte) {
	o.mu.Lock()
	before := o.recalc()
	if o.ext == nil {
		o.ext = map[any]byte{}
	}
	if v == 0 {
		delete(o.ext, key)
	} else {
		o.ext[key] = v
	}
	o.extMax = 0
	for _, x := range o.ext {
		if x > o.extMax {
			o.extMax = x
		}
	}
	changed := before != o.recalc()
	o.mu.Unlock()
	if changed && o.onChg != nil {
		o.onChg()
	}
}

// Toy updates outputs from its layers.
type Toy interface {
	Name() string
	UpdateOutputs()
}

type layers[T any] struct {
	m     map[int]*T
	order []int
}

func (l *layers[T]) get(n int) *T {
	if l.m == nil {
		l.m = map[int]*T{}
	}
	if v, ok := l.m[n]; ok {
		return v
	}
	v := new(T)
	l.m[n] = v
	l.order = append(l.order, n)
	sort.Ints(l.order)
	return v
}

// AnalogAlphaToy drives one output from layered value/alpha pairs.
type AnalogAlphaToy struct {
	name   string
	l      layers[AnalogAlpha]
	out    *Output
	curve  Curve
	OutRef string
}

func (t *AnalogAlphaToy) Name() string { return t.name }

// Layer returns (creating) the layer with the given number.
func (t *AnalogAlphaToy) Layer(n int) *AnalogAlpha { return t.l.get(n) }

func (t *AnalogAlphaToy) result() int {
	v := float32(0)
	for _, n := range t.l.order {
		la := t.l.m[n]
		if la.Alpha != 0 {
			v = v*float32(255-la.Alpha)/255 + float32(la.Value)*float32(la.Alpha)/255
		}
	}
	return int(v)
}

func (t *AnalogAlphaToy) UpdateOutputs() {
	if t.out != nil {
		t.out.SetToy(t.curve[limit(t.result(), 0, 255)])
	}
}

// RGBAToy drives three outputs from layered colors.
type RGBAToy struct {
	name    string
	l       layers[RGBA]
	r, g, b *Output
	curve   Curve
}

func (t *RGBAToy) Name() string { return t.name }

// Layer returns (creating) the layer with the given number.
func (t *RGBAToy) Layer(n int) *RGBA { return t.l.get(n) }

func (t *RGBAToy) UpdateOutputs() {
	var r, g, b float32
	for _, n := range t.l.order {
		c := t.l.m[n]
		if c.A != 0 {
			na := float32(255 - c.A)
			a := float32(c.A)
			r = r*na/255 + float32(c.R)*a/255
			g = g*na/255 + float32(c.G)*a/255
			b = b*na/255 + float32(c.B)*a/255
		}
	}
	if t.r != nil {
		t.r.SetToy(t.curve[limit(int(r), 0, 255)])
	}
	if t.g != nil {
		t.g.SetToy(t.curve[limit(int(g), 0, 255)])
	}
	if t.b != nil {
		t.b.SetToy(t.curve[limit(int(b), 0, 255)])
	}
}

// LWEOutput maps a LedWiz equivalent output number to a named output/toy.
type LWEOutput struct {
	OutputName string
	Number     int
}

// LedWizEquivalent is the toy through which ini file columns address outputs.
type LedWizEquivalent struct {
	name    string
	Number  int
	Outputs []LWEOutput
}

func (l *LedWizEquivalent) Name() string   { return l.name }
func (l *LedWizEquivalent) UpdateOutputs() {}

func (l *LedWizEquivalent) outputName(n int) (string, bool) {
	for _, o := range l.Outputs {
		if o.Number == n {
			return o.OutputName, true
		}
	}
	return "", false
}

// Controller is a physical output controller.
type Controller interface {
	Name() string
	Outputs() []*Output
	Start() error
	Stop()
	// Notify is called by outputs when a value changed.
	Notify()
}

// Cabinet holds controllers, outputs and toys.
type Cabinet struct {
	Controllers []Controller
	outputs     map[string]*Output
	toys        map[string]Toy
	toyOrder    []string
	lwe         map[int]*LedWizEquivalent
	log         Logger
}

func NewCabinet(log Logger) *Cabinet {
	if log == nil {
		log = nopLogger{}
	}
	return &Cabinet{outputs: map[string]*Output{}, toys: map[string]Toy{}, lwe: map[int]*LedWizEquivalent{}, log: log}
}

// AddController registers a controller and indexes its outputs.
func (c *Cabinet) AddController(ct Controller) {
	c.Controllers = append(c.Controllers, ct)
	for _, o := range ct.Outputs() {
		o.onChg = ct.Notify
		c.outputs[o.Name] = o
		c.outputs[fmt.Sprintf("%s\\%s", ct.Name(), o.Name)] = o
		c.outputs[fmt.Sprintf("%s\\%s.%02d", ct.Name(), ct.Name(), o.Number)] = o
		c.outputs[fmt.Sprintf("%s\\%s.%d", ct.Name(), ct.Name(), o.Number)] = o
	}
}

// Output finds an output by (possibly qualified) name.
func (c *Cabinet) Output(name string) *Output { return c.outputs[name] }

func (c *Cabinet) AddToy(t Toy) {
	if a, ok := t.(interface{ attach(*Cabinet) }); ok {
		a.attach(c)
	}
	if _, ok := c.toys[t.Name()]; !ok {
		c.toyOrder = append(c.toyOrder, t.Name())
	}
	c.toys[t.Name()] = t
	if l, ok := t.(*LedWizEquivalent); ok {
		c.lwe[l.Number] = l
	}
}

func (c *Cabinet) HasToy(name string) bool { _, ok := c.toys[name]; return ok }

// Toy returns a toy by name.
func (c *Cabinet) Toy(name string) Toy { return c.toys[name] }

// LedWizEquivalent returns the LWE with the given number.
func (c *Cabinet) LedWizEquivalent(n int) *LedWizEquivalent { return c.lwe[n] }

// Update recomputes toy outputs.
func (c *Cabinet) Update() {
	for _, n := range c.toyOrder {
		c.toys[n].UpdateOutputs()
	}
}

// Reset clears all toy layers (used when switching tables).
func (c *Cabinet) Reset() {
	for _, n := range c.toyOrder {
		switch t := c.toys[n].(type) {
		case *AnalogAlphaToy:
			t.l = layers[AnalogAlpha]{}
			if t.out != nil {
				t.out.SetToy(0)
			}
		case *LedStripToy:
			t.layers = map[int][][]RGBA{}
			t.order = nil
			t.bind()
			if t.setter != nil {
				t.setter.SetValues((t.FirstLed-1)*3, make([]byte, t.Width*t.Height*3))
			}
		case *RGBAToy:
			t.l = layers[RGBA]{}
			for _, o := range []*Output{t.r, t.g, t.b} {
				if o != nil {
					o.SetToy(0)
				}
			}
		}
	}
}

// NewAnalogToy creates an analog toy bound to the named output.
func (c *Cabinet) NewAnalogToy(name, outputName, curve string) *AnalogAlphaToy {
	return &AnalogAlphaToy{name: name, out: c.outputs[outputName], curve: CurveByName(curve), OutRef: outputName}
}

// NewRGBToy creates an RGB toy bound to three named outputs.
func (c *Cabinet) NewRGBToy(name, r, g, b, curve string) *RGBAToy {
	return &RGBAToy{name: name, r: c.outputs[r], g: c.outputs[g], b: c.outputs[b], curve: CurveByName(curve)}
}
