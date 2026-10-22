package dof

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// This file holds the read only / test hooks used by `superdof test`.

// testKey identifies the manual test source on an Output so it never fights
// with DOF toys or DOFLinx sources.
type testKey struct{}

// TestLayer is the toy layer number used by manual toy tests (above any ini layer).
const TestLayer = 99

// Sync runs fn on the engine goroutine and waits for it (max 3 s).
func (e *Engine) Sync(fn func()) {
	done := make(chan struct{})
	e.Do(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
}

// Roms lists every ROM name in the loaded ini files (sorted, unique).
func (e *Engine) Roms() []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range e.files {
		for _, t := range f.Tables {
			k := strings.ToLower(t.ShortRomName)
			if !seen[k] {
				seen[k] = true
				out = append(out, t.ShortRomName)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// OutRefInfo names one physical output.
type OutRefInfo struct {
	Number int    `json:"n"`
	Name   string `json:"name"`
}

// ControllerInfo describes a controller for the tester UI.
type ControllerInfo struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // pinone, ftdi, strip
	Outputs int    `json:"outputs"`
	Leds    int    `json:"leds,omitempty"`
	Strips  []int  `json:"strips,omitempty"`
	// LedWiz is the LedWizEquivalent number the ini files use for this controller (0 = unknown).
	LedWiz int `json:"ledwiz,omitempty"`
}

// ToyInfo describes a toy for the tester UI.
type ToyInfo struct {
	Name       string      `json:"name"`
	Kind       string      `json:"kind"` // ledwiz, analog, rgb, strip
	Number     int         `json:"number,omitempty"`
	Outputs    []OutRefInfo `json:"outputs,omitempty"`
	Width      int         `json:"width,omitempty"`
	Height     int         `json:"height,omitempty"`
	Controller string      `json:"controller,omitempty"`
	FirstLed   int         `json:"firstLed,omitempty"`
	ColorOrder string      `json:"colorOrder,omitempty"`
}

// ElementInfo describes a table element and its effects.
type ElementInfo struct {
	Desc    string   `json:"desc"`
	Value   int      `json:"value"`
	Effects []string `json:"effects"`
	AutoOff int      `json:"autoOffMs,omitempty"`
}

// Inventory lists controllers and toys.
func (e *Engine) Inventory() ([]ControllerInfo, []ToyInfo) {
	var cs []ControllerInfo
	for _, c := range e.cab.Controllers {
		ci := ControllerInfo{Name: c.Name(), Outputs: len(c.Outputs())}
		switch x := c.(type) {
		case *PinOneController:
			ci.Kind, ci.LedWiz = "pinone", x.Number()+10
		case *FTDIController:
			ci.Kind, ci.LedWiz = "ftdi", 40+x.ID()
		case *StripController:
			ci.Kind, ci.Leds, ci.Strips = "strip", x.NumberOfLeds(), trimStrips(x.cfg.LedsPerStrip[:])
		default:
			ci.Kind = "other"
		}
		cs = append(cs, ci)
	}
	var ts []ToyInfo
	for _, n := range e.cab.toyOrder {
		switch t := e.cab.toys[n].(type) {
		case *LedWizEquivalent:
			ti := ToyInfo{Name: t.name, Kind: "ledwiz", Number: t.Number}
			for _, o := range t.Outputs {
				ti.Outputs = append(ti.Outputs, OutRefInfo{Number: o.Number, Name: o.OutputName})
			}
			ts = append(ts, ti)
		case *AnalogAlphaToy:
			ts = append(ts, ToyInfo{Name: t.name, Kind: "analog", Outputs: []OutRefInfo{{Name: t.OutRef}}})
		case *RGBAToy:
			ti := ToyInfo{Name: t.name, Kind: "rgb"}
			for _, o := range []*Output{t.r, t.g, t.b} {
				if o != nil {
					ti.Outputs = append(ti.Outputs, OutRefInfo{Number: o.Number, Name: o.Name})
				}
			}
			ts = append(ts, ti)
		case *LedStripToy:
			ts = append(ts, ToyInfo{Name: t.name, Kind: "strip", Width: t.Width, Height: t.Height,
				Controller: t.Controller, FirstLed: t.FirstLed, ColorOrder: t.ColorOrder})
		}
	}
	return cs, ts
}

// Values returns the current output values of a controller (for the live view).
func (e *Engine) Values(controller string) (outputs []byte, leds []byte) {
	for _, c := range e.cab.Controllers {
		if c.Name() != controller {
			continue
		}
		if s, ok := c.(*StripController); ok {
			return nil, e.StripPreview(s)
		}
		for _, o := range c.Outputs() {
			outputs = append(outputs, o.Value())
		}
	}
	return
}

// Elements lists the active table's elements (call from Sync).
func (e *Engine) Elements() []ElementInfo {
	t := e.table
	if t == nil {
		return nil
	}
	conv := func(el *TableElement, desc string) ElementInfo {
		v := el.Value
		if v == math.MinInt32 {
			v = 0
		}
		ei := ElementInfo{Desc: desc, Value: v, AutoOff: el.AutoOffMs, Effects: []string{}}
		for _, a := range el.assigned {
			ei.Effects = append(ei.Effects, a.name)
		}
		return ei
	}
	var out []ElementInfo
	for k, el := range t.numbered {
		out = append(out, conv(el, fmt.Sprintf("%c%d", k.t, k.n)))
	}
	for n, el := range t.named {
		out = append(out, conv(el, "$"+n))
	}
	order := "SLWGMNEDCB?"
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Desc, out[j].Desc
		if a[0] != b[0] {
			ia, ib := strings.IndexByte(order, a[0]), strings.IndexByte(order, b[0])
			if a[0] == '$' {
				ia = 99
			}
			if b[0] == '$' {
				ib = 99
			}
			return ia < ib
		}
		var na, nb int
		fmt.Sscanf(a[1:], "%d", &na)
		fmt.Sscanf(b[1:], "%d", &nb)
		if na != nb {
			return na < nb
		}
		return a < b
	})
	return out
}

// TableName is the ROM of the active table (call from Sync).
func (e *Engine) TableName() string {
	if e.table == nil {
		return ""
	}
	return e.table.Name
}

// ForceOutput drives one output directly (bypassing ini effects) from the
// tester. value 0 releases it. It is combined with DOF/DOFLinx by max().
func (e *Engine) ForceOutput(controller string, number int, value byte) bool {
	for _, c := range e.cab.Controllers {
		if c.Name() != controller {
			continue
		}
		for _, o := range c.Outputs() {
			if o.Number == number {
				o.SetExternal(testKey{}, value)
				return true
			}
		}
	}
	return false
}

// ReleaseTestOutputs removes every forced test value.
func (e *Engine) ReleaseTestOutputs() {
	for _, c := range e.cab.Controllers {
		for _, o := range c.Outputs() {
			o.SetExternal(testKey{}, 0)
		}
	}
}

// ForceOutputByName drives the output named like the toy mapping (e.g. "PinOne Controller 01.5").
func (e *Engine) ForceOutputByName(name string, value byte) bool {
	o := e.cab.Output(name)
	if o == nil {
		return false
	}
	o.SetExternal(testKey{}, value)
	return true
}

// TestToy lights a toy through its normal path (curve, color order,
// brightness, mapping) with a test layer. kind: analog/rgb take r (value) or
// r,g,b; strip takes a pattern. Returns an error text or "".
func (e *Engine) TestToy(name string, pattern string, r, g, b int, index int) string {
	var msg string
	e.Sync(func() {
		switch t := e.cab.Toy(name).(type) {
		case nil:
			msg = "unknown toy " + name
		case *AnalogAlphaToy:
			l := t.Layer(TestLayer)
			l.Value, l.Alpha = limit(r, 0, 255), 255
			if r == 0 {
				l.Alpha = 0
			}
		case *RGBAToy:
			l := t.Layer(TestLayer)
			*l = RGBA{limit(r, 0, 255), limit(g, 0, 255), limit(b, 0, 255), 255}
			if r == 0 && g == 0 && b == 0 {
				l.A = 0
			}
		case *LedStripToy:
			t.attachIfNeeded()
			paintStrip(t, pattern, r, g, b, index)
		case *LedWizEquivalent:
			msg = "use the output tests for LedWiz equivalents"
		default:
			msg = "toy type not testable"
		}
		e.cab.Update()
		e.notifyControllers()
	})
	return msg
}

func (t *LedStripToy) attachIfNeeded() {
	if t.mapping == nil {
		t.buildMapping()
	}
}

// paintStrip writes a pattern into the test layer of a strip toy.
func paintStrip(t *LedStripToy, pattern string, r, g, b, index int) {
	l := t.Layer(TestLayer)
	n := t.Width * t.Height
	set := func(i int, c RGBA) {
		x, y := i%t.Width, i/t.Width
		if x < t.Width && y < t.Height {
			l[x][y] = c
		}
	}
	for i := 0; i < n; i++ {
		set(i, RGBA{})
	}
	solid := RGBA{limit(r, 0, 255), limit(g, 0, 255), limit(b, 0, 255), 255}
	switch pattern {
	case "off":
	case "solid":
		for i := 0; i < n; i++ {
			set(i, solid)
		}
	case "one":
		if index >= 0 && index < n {
			set(index, solid)
		}
	case "bands": // first third red, second green, last blue: verifies color order
		for i := 0; i < n; i++ {
			switch i * 3 / n {
			case 0:
				set(i, RGBA{255, 0, 0, 255})
			case 1:
				set(i, RGBA{0, 255, 0, 255})
			default:
				set(i, RGBA{0, 0, 255, 255})
			}
		}
	case "rainbow":
		for i := 0; i < n; i++ {
			rr, gg, bb := hsv(float64(i)/float64(n), 1, 1)
			set(i, RGBA{rr, gg, bb, 255})
		}
	case "gradient": // dim to bright in the chosen color: verifies direction and mapping
		for i := 0; i < n; i++ {
			f := float64(i+1) / float64(n)
			set(i, RGBA{int(float64(solid.R) * f), int(float64(solid.G) * f), int(float64(solid.B) * f), 255})
		}
	case "chase": // a short tail ending at index
		for k := 0; k < 6; k++ {
			i := index - k
			if i < 0 {
				i += n
			}
			f := 1 - float64(k)/6
			set(i%n, RGBA{int(float64(solid.R) * f), int(float64(solid.G) * f), int(float64(solid.B) * f), 255})
		}
	}
}

func hsv(h, s, v float64) (int, int, int) {
	i := int(h * 6)
	f := h*6 - float64(i)
	p, q, t := v*(1-s), v*(1-f*s), v*(1-(1-f)*s)
	var r, g, b float64
	switch i % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}
	return int(r * 255), int(g * 255), int(b * 255)
}

// ClearTestToys removes every test layer.
func (e *Engine) ClearTestToys() {
	e.Sync(func() {
		for _, n := range e.cab.toyOrder {
			switch t := e.cab.toys[n].(type) {
			case *AnalogAlphaToy:
				l := t.Layer(TestLayer)
				*l = AnalogAlpha{}
			case *RGBAToy:
				l := t.Layer(TestLayer)
				*l = RGBA{}
			case *LedStripToy:
				if _, ok := t.layers[TestLayer]; ok {
					paintStrip(t, "off", 0, 0, 0, 0)
				}
			}
		}
		e.cab.Update()
		e.notifyControllers()
	})
}

// StripSize returns the pixel count of a strip toy (0 if not a strip).
func (e *Engine) StripSize(name string) int {
	if t, ok := e.cab.Toy(name).(*LedStripToy); ok {
		return t.Width * t.Height
	}
	return 0
}

func trimStrips(v []int) []int {
	n := len(v)
	for n > 0 && v[n-1] == 0 {
		n--
	}
	return append([]int(nil), v[:n]...)
}

// StripPreview returns the strip data as the LEDs should look (R,G,B per
// LED): the toys' color order permutation is undone.
func (e *Engine) StripPreview(c *StripController) []byte {
	d := c.Snapshot()
	for _, n := range e.cab.toyOrder {
		t, ok := e.cab.toys[n].(*LedStripToy)
		if !ok || t.Controller != c.Name() {
			continue
		}
		ri, gi, bi := 0, 1, 2
		switch strings.ToUpper(t.ColorOrder) {
		case "RBG":
			ri, gi, bi = 0, 2, 1
		case "GRB", "WS2812":
			ri, gi, bi = 1, 0, 2
		case "GBR":
			ri, gi, bi = 1, 2, 0
		case "BRG":
			ri, gi, bi = 2, 0, 1
		case "BGR":
			ri, gi, bi = 2, 1, 0
		}
		start := (t.FirstLed - 1) * 3
		for i := 0; i < t.Width*t.Height; i++ {
			o := start + i*3
			if o < 0 || o+2 >= len(d) {
				break
			}
			r, g, b := d[o+ri], d[o+gi], d[o+bi]
			d[o], d[o+1], d[o+2] = r, g, b
		}
	}
	return d
}
