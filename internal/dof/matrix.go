package dof

import (
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"
)

// ------------------------------------------------------------------ LedStrip

// LedStripToy is a W x H matrix of RGB LEDs (DOF Toys.Hardware.LedStrip).
type LedStripToy struct {
	name        string
	Width       int
	Height      int
	Arrangement string
	ColorOrder  string // RGB, RBG (DOF default), GRB, GBR, BRG, BGR
	FirstLed    int    // 1 based
	CurveName   string
	Brightness  int     // percent
	Gamma       float32 // brightness gamma correction
	Controller  string

	layers  map[int][][]RGBA
	order   []int
	mapping [][]int
	data    []byte
	curve   Curve
	setter  ValueSetter
	cab     *Cabinet
	zeroed  bool
}

// NewLedStrip creates a strip toy with DOF defaults.
func NewLedStrip(name string) *LedStripToy {
	return &LedStripToy{name: name, Width: 1, Height: 1, Arrangement: "LeftRightTopDown", ColorOrder: "RBG",
		FirstLed: 1, Brightness: 100, Gamma: 2.2, layers: map[int][][]RGBA{}}
}

func (t *LedStripToy) Name() string { return t.name }

func (t *LedStripToy) attach(c *Cabinet) {
	t.cab = c
	t.curve = CurveByName(t.CurveName)
	t.buildMapping()
	t.data = make([]byte, t.Width*t.Height*3)
}

// Layer returns (creating) the W x H layer with the given number, indexed [x][y].
func (t *LedStripToy) Layer(n int) [][]RGBA {
	if l, ok := t.layers[n]; ok {
		return l
	}
	l := make([][]RGBA, t.Width)
	for x := range l {
		l[x] = make([]RGBA, t.Height)
	}
	t.layers[n] = l
	t.order = append(t.order, n)
	sort.Ints(t.order)
	return l
}

func (t *LedStripToy) buildMapping() {
	w, h := t.Width, t.Height
	t.mapping = make([][]int, w)
	for x := range t.mapping {
		t.mapping[x] = make([]int, h)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var n int
			switch strings.ToLower(t.Arrangement) {
			case "leftrightbottomup":
				n = (h-1-y)*w + x
			case "rightlefttopdown":
				n = y*w + (w - 1 - x)
			case "rightleftbottomup":
				n = (h-1-y)*w + (w - 1 - x)
			case "topdownleftright":
				n = x*h + y
			case "topdownrightleft":
				n = (w-1-x)*h + y
			case "bottomupleftright":
				n = x*h + (h - 1 - y)
			case "bottomuprightleft":
				n = (w-1-x)*h + (h - 1 - y)
			case "leftrightalternatetopdown":
				n = w*y + alt(y&1 == 0, x, w-1-x)
			case "leftrightalternatebottomup":
				n = w*(h-1-y) + alt((h-1-y)&1 == 0, x, w-1-x)
			case "rightleftalternatetopdown":
				n = w*y + alt(y&1 == 1, x, w-1-x)
			case "rightleftalternatebottomup":
				n = w*(h-1-y) + alt((h-1-y)&1 == 1, x, w-1-x)
			case "topdownalternateleftright":
				n = h*x + alt(x&1 == 0, y, h-1-y)
			case "topdownalternaterightleft":
				n = h*(w-1-x) + alt(x&1 == 1, y, h-1-y)
			case "bottomupalternateleftright":
				n = h*x + alt(x&1 == 1, y, h-1-y)
			case "bottomupalternaterightleft":
				n = h*(w-1-x) + alt(x&1 == 0, y, h-1-y)
			default: // leftrighttopdown
				n = y*w + x
			}
			t.mapping[x][y] = n * 3
		}
	}
}

func alt(c bool, a, b int) int {
	if c {
		return a
	}
	return b
}

func (t *LedStripToy) bind() {
	if t.setter != nil || t.cab == nil {
		return
	}
	for _, c := range t.cab.Controllers {
		if c.Name() == t.Controller {
			if s, ok := c.(ValueSetter); ok {
				t.setter = s
			}
		}
	}
}

func (t *LedStripToy) fadingTable() *Curve {
	c := t.curve
	return &c
}

func (t *LedStripToy) UpdateOutputs() {
	t.bind()
	if t.setter == nil || len(t.order) == 0 {
		return
	}
	t.zeroed = false
	w, h := t.Width, t.Height
	if t.Brightness <= 0 {
		for i := range t.data {
			t.data[i] = 0
		}
		t.setter.SetValues((t.FirstLed-1)*3, t.data)
		return
	}
	val := make([]float32, w*h*3)
	for _, n := range t.order {
		d := t.layers[n]
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				c := d[x][y]
				a := limit(c.A, 0, 255)
				if a == 0 {
					continue
				}
				i := (y*w + x) * 3
				na := float32(255 - a)
				fa := float32(a)
				val[i] = int32f(val[i])*na/255 + float32(limit(c.R, 0, 255))*fa/255
				val[i+1] = int32f(val[i+1])*na/255 + float32(limit(c.G, 0, 255))*fa/255
				val[i+2] = int32f(val[i+2])*na/255 + float32(limit(c.B, 0, 255))*fa/255
			}
		}
	}
	ft := t.curve
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			r := ft[limit(int(val[i]), 0, 255)]
			g := ft[limit(int(val[i+1]), 0, 255)]
			b := ft[limit(int(val[i+2]), 0, 255)]
			o := t.mapping[x][y]
			switch strings.ToUpper(t.ColorOrder) {
			case "RBG":
				t.data[o], t.data[o+2], t.data[o+1] = r, g, b
			case "GRB", "WS2812":
				t.data[o+1], t.data[o], t.data[o+2] = r, g, b
			case "GBR":
				t.data[o+1], t.data[o+2], t.data[o] = r, g, b
			case "BRG":
				t.data[o+2], t.data[o], t.data[o+1] = r, g, b
			case "BGR":
				t.data[o+2], t.data[o+1], t.data[o] = r, g, b
			default: // RGB
				t.data[o], t.data[o+1], t.data[o+2] = r, g, b
			}
		}
	}
	if t.Brightness < 100 {
		f := float32(math.Pow(float64(t.Brightness)/100, float64(t.Gamma)))
		for i := range t.data {
			t.data[i] = byte(float32(t.data[i]) * f)
		}
	}
	t.setter.SetValues((t.FirstLed-1)*3, t.data)
}

// int32f truncates like a C# (int) cast of a float used as table index.
func int32f(v float32) float32 { return float32(int(v)) }

// ------------------------------------------------------------------ effects

type matrixBase struct {
	effectBase
	toyName string
	left    float32
	top     float32
	width   float32
	height  float32
	layerNr int
	fade    FadeMode

	table  *Table
	toy    *LedStripToy
	layer  [][]RGBA
	aL, aT int
	aR, aB int
}

func newMatrixBase(toy string, layer int, l, t, w, h int) matrixBase {
	return matrixBase{toyName: toy, layerNr: layer, left: float32(l), top: float32(t), width: float32(w), height: float32(h)}
}

func (m *matrixBase) areaW() int { return m.aR - m.aL + 1 }
func (m *matrixBase) areaH() int { return m.aB - m.aT + 1 }

func (m *matrixBase) initArea(t *Table) {
	m.table = t
	m.toy, m.layer = nil, nil
	if toy, ok := t.cab.toys[m.toyName].(*LedStripToy); ok {
		m.toy = toy
		m.layer = toy.Layer(m.layerNr)
		W, H := float32(toy.Width), float32(toy.Height)
		cl := func(v float32) float32 { return float32(math.Max(0, math.Min(100, float64(v)))) }
		m.aL = limit(int(math.Floor(float64(W/100*cl(m.left)))), 0, toy.Width-1)
		m.aT = limit(int(math.Floor(float64(H/100*cl(m.top)))), 0, toy.Height-1)
		m.aR = limit(int(math.Floor(float64(W/100*cl(m.left+m.width)))), 0, toy.Width-1)
		m.aB = limit(int(math.Floor(float64(H/100*cl(m.top+m.height)))), 0, toy.Height-1)
	}
}

func (m *matrixBase) alarms() *Alarms { return &m.table.engine.alarms }

func scaleRGBA(inactive, active RGBA, v int) RGBA {
	f := float32(limit(v, 0, 255)) / 255
	return RGBA{
		R: inactive.R + limit(int(float32(active.R-inactive.R)*f), 0, 255),
		G: inactive.G + limit(int(float32(active.G-inactive.G)*f), 0, 255),
		B: inactive.B + limit(int(float32(active.B-inactive.B)*f), 0, 255),
		A: inactive.A + limit(int(float32(active.A-inactive.A)*f), 0, 255),
	}
}

// ---- value (color) effect

type matrixColorEffect struct {
	matrixBase
	active, inactive RGBA
}

func (e *matrixColorEffect) Init(t *Table) { e.initArea(t) }
func (e *matrixColorEffect) Trigger(d TED) {
	if e.layer == nil {
		return
	}
	v := limit(d.Value, 0, 255)
	if v > 0 && e.fade == FadeModeOnOff {
		v = 255
	}
	c := scaleRGBA(e.inactive, e.active, v)
	for x := e.aL; x <= e.aR; x++ {
		for y := e.aT; y <= e.aB; y++ {
			e.layer[x][y] = c
		}
	}
}

// ---- shift effect

const matrixRefreshMs = 30

type matrixShiftEffect struct {
	matrixBase
	direction byte // L R U D
	speed     float32
	accel     float32
	active    RGBA
	inactive  RGBA

	step2     []float32
	buffer    map[int]int
	lastDisc  int
	activeAni bool
	lastTrig  int
	curStep   int
}

func (e *matrixShiftEffect) Init(t *Table) {
	e.initArea(t)
	e.buffer = map[int]int{}
	if e.layer == nil {
		return
	}
	n := float32(e.areaW())
	if e.direction == 'U' || e.direction == 'D' {
		n = float32(e.areaH())
	}
	pos := float32(0)
	spd := n / 100 * (e.speed / float32(1000/matrixRefreshMs))
	acc := n / 100 * (e.accel / float32(1000/matrixRefreshMs))
	var l []float32
	for guard := 0; pos <= n && guard < 100000; guard++ {
		l = append(l, clampF(pos, 0, n))
		pos += spd
		spd = clampF(spd+acc, 0, 10000)
		if spd <= 0 {
			spd = 0.0001
		}
	}
	l = append(l, clampF(pos, 0, n))
	e.step2 = l
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (e *matrixShiftEffect) value(v int) RGBA {
	v = limit(v, 0, 255)
	if v > 0 && e.fade == FadeModeOnOff {
		v = 255
	}
	return scaleRGBA(e.inactive, e.active, v)
}

func (e *matrixShiftEffect) doStep() {
	if !e.activeAni {
		e.alarms().RegisterInterval(matrixRefreshMs*time.Millisecond, e, e.doStep)
		e.activeAni = true
	}
	horizontal := e.direction == 'L' || e.direction == 'R'
	n := e.areaH()
	if horizontal {
		n = e.areaW()
	}
	from := float32(n)
	value := make([]float32, n+1)
	last := e.lastDisc
	keys := make([]int, 0, len(e.buffer))
	for k := range e.buffer {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	floor := func(f float32) float32 { return float32(math.Floor(float64(f))) }
	ceil := func(f float32) float32 { return float32(math.Ceil(float64(f))) }
	isInt := func(f float32) bool { return f == floor(f) }
	fill := func(from, to float32, last int) {
		if floor(from) == floor(to) {
			value[int(floor(from))] += (from - to) * float32(last)
			return
		}
		if !isInt(from) {
			value[int(floor(from))] += (from - floor(from)) * float32(last)
		}
		toNr := int(ceil(to))
		for i := int(floor(from)) - 1; i >= toNr; i-- {
			if i >= 0 && i < len(value) {
				value[i] = float32(last)
			}
		}
		if !isInt(to) {
			value[int(floor(to))] += (ceil(to) - to) * float32(last)
		}
	}
	for _, k := range keys {
		idx := e.curStep - k
		if idx < 0 || idx >= len(e.step2) {
			continue
		}
		to := e.step2[idx]
		fill(from, to, last)
		from = to
		last = e.buffer[k]
	}
	if from != 0 {
		if !isInt(from) && int(floor(from)) < n {
			value[int(floor(from))] += (from - floor(from)) * float32(last)
		}
		toNr := 0
		for i := int(floor(from)) - 1; i >= toNr; i-- {
			value[i] = float32(last)
		}
	}
	for i := 0; i < n; i++ {
		c := e.value(int(value[i]))
		switch e.direction {
		case 'R':
			for y := e.aT; y <= e.aB; y++ {
				e.layer[e.aL+i][y] = c
			}
		case 'D':
			for x := e.aL; x <= e.aR; x++ {
				e.layer[x][e.aT+i] = c
			}
		case 'U':
			for x := e.aL; x <= e.aR; x++ {
				e.layer[x][e.aB-i] = c
			}
		default: // L
			for y := e.aT; y <= e.aB; y++ {
				e.layer[e.aR-i][y] = c
			}
		}
	}
	drop := e.curStep - (len(e.step2) - 1)
	if v, ok := e.buffer[drop]; ok {
		e.lastDisc = v
		delete(e.buffer, drop)
	}
	if len(e.buffer) > 0 || e.lastDisc != 0 {
		e.curStep++
	} else {
		e.alarms().UnregisterInterval(e)
		e.lastDisc = 0
		e.curStep = 0
		e.activeAni = false
	}
}

func (e *matrixShiftEffect) Trigger(d TED) {
	if e.layer == nil || e.lastTrig == d.Value {
		return
	}
	e.lastTrig = d.Value
	e.buffer[e.curStep] = d.Value
	if !e.activeAni {
		e.doStep()
	}
}

func (e *matrixShiftEffect) Finish() {
	if e.table != nil {
		e.alarms().UnregisterInterval(e)
	}
}

// ---- flicker effect

type flickerObj struct {
	x, y  int
	durMs int
	start time.Time
}

type matrixFlickerEffect struct {
	matrixBase
	density    int
	minMs      int
	maxMs      int
	fadeUpMs   int
	fadeDownMs int
	active     RGBA
	inactive   RGBA

	running  bool
	current  int
	activeFO []*flickerObj
	idleFO   []*flickerObj
	rnd      *rand.Rand
}

func (e *matrixFlickerEffect) Init(t *Table) {
	e.initArea(t)
	e.rnd = rand.New(rand.NewSource(time.Now().UnixNano()))
	e.activeFO, e.idleFO = nil, nil
	if e.layer == nil {
		return
	}
	for y := e.aT; y <= e.aB; y++ {
		for x := e.aL; x <= e.aR; x++ {
			e.idleFO = append(e.idleFO, &flickerObj{x: x, y: y})
		}
	}
}

func (e *matrixFlickerEffect) value(v int) RGBA { return scaleRGBA(e.inactive, e.active, v) }

func (e *matrixFlickerEffect) doFlicker() {
	off := e.value(0)
	v := limit(e.current, 0, 255)
	if v <= 0 {
		for _, fo := range e.activeFO {
			e.layer[fo.x][fo.y] = off
		}
		e.idleFO = append(e.idleFO, e.activeFO...)
		e.activeFO = nil
		e.alarms().UnregisterInterval(e)
		e.running = false
		return
	}
	if !e.running {
		e.alarms().RegisterInterval(matrixRefreshMs*time.Millisecond, e, e.doFlicker)
		e.running = true
	}
	if e.fade == FadeModeOnOff {
		v = 255
	}
	n := e.areaW() * e.areaH()
	flick := limit(int(float64(n)/100*float64(e.density)), 1, n)
	min, max := e.minMs, e.maxMs
	if max < min {
		min, max = max, min
	}
	for len(e.activeFO) < flick && len(e.idleFO) > 0 {
		i := e.rnd.Intn(len(e.idleFO))
		fo := e.idleFO[i]
		e.idleFO = append(e.idleFO[:i], e.idleFO[i+1:]...)
		fo.start = time.Now()
		span := max - min
		if span < 1 {
			span = 1
		}
		fo.durMs = min + e.rnd.Intn(span) + e.fadeDownMs
		e.activeFO = append(e.activeFO, fo)
	}
	for i := len(e.activeFO) - 1; i >= 0; i-- {
		fo := e.activeFO[i]
		age := int(time.Since(fo.start) / time.Millisecond)
		var fv int
		switch {
		case age > fo.durMs+e.fadeDownMs:
			if age > (fo.durMs+e.fadeDownMs)*2 || e.rnd.Float64() > .5 {
				e.activeFO = append(e.activeFO[:i], e.activeFO[i+1:]...)
				e.idleFO = append(e.idleFO, fo)
			}
			fv = 0
		case e.fadeUpMs > 0 && age < e.fadeUpMs && age < fo.durMs:
			fv = int(float64(v) / float64(e.fadeUpMs) * float64(age))
		case age > fo.durMs && e.fadeDownMs > 0:
			if fo.durMs < e.fadeUpMs {
				fv = int(float64(v) / float64(e.fadeUpMs) * float64(fo.durMs))
			} else {
				fv = v
			}
			fv -= int(float64(fv) / float64(e.fadeDownMs) * float64(age-fo.durMs))
		default:
			fv = v
		}
		e.layer[fo.x][fo.y] = e.value(limit(fv, 0, 255))
	}
}

func (e *matrixFlickerEffect) Trigger(d TED) {
	if e.layer == nil {
		return
	}
	e.current = d.Value
	if e.current > 0 && !e.running {
		e.doFlicker()
	}
}

func (e *matrixFlickerEffect) Finish() {
	if e.table != nil {
		e.current = 0
		if e.running && e.layer != nil {
			e.doFlicker()
		}
		e.alarms().UnregisterInterval(e)
	}
}

// ---- plasma effect

type matrixPlasmaEffect struct {
	matrixBase
	speed    int
	scale    int
	density  int
	color1   RGBA
	color2   RGBA
	inactive RGBA

	running bool
	current int
	tm      float64
}

func (e *matrixPlasmaEffect) Init(t *Table) { e.initArea(t) }

func (e *matrixPlasmaEffect) Trigger(d TED) {
	if e.layer == nil {
		return
	}
	e.current = d.Value
	if e.current > 0 && !e.running {
		e.doPlasma()
	}
}

func (e *matrixPlasmaEffect) doPlasma() {
	if limit(e.current, 0, 255) > 0 {
		if !e.running {
			e.alarms().RegisterInterval(matrixRefreshMs*time.Millisecond, e, e.doPlasma)
			e.running = true
		}
		e.drawFrame()
		return
	}
	e.alarms().UnregisterInterval(e)
	e.running = false
	for y := 0; y < e.areaH(); y++ {
		for x := 0; x < e.areaW(); x++ {
			e.layer[e.aL+x][e.aT+y] = e.value(0, 0, 0)
		}
	}
}

func (e *matrixPlasmaEffect) value(trig int, tm, v float64) RGBA {
	bl := (math.Sin(v*math.Pi*2+tm) + 1) / 2
	b := RGBA{
		R: int(bl*float64(e.color1.R) + (1-bl)*float64(e.color2.R)),
		G: int(bl*float64(e.color1.G) + (1-bl)*float64(e.color2.G)),
		B: int(bl*float64(e.color1.B) + (1-bl)*float64(e.color2.B)),
		A: int((bl*float64(e.color1.A) + (1-bl)*float64(e.color2.A)) * v),
	}
	return scaleRGBA(e.inactive, b, trig)
}

func (e *matrixPlasmaEffect) drawFrame() {
	f := limit(e.current, 0, 255)
	if e.fade == FadeModeOnOff {
		f = 0
		if e.current > 0 {
			f = 255
		}
	}
	e.tm += float64(e.speed) / 2000
	W, H := e.areaW(), e.areaH()
	var sx, sy float64
	if W >= H {
		sx = float64(e.scale) / 100 / float64(W)
		sy = sx
	} else {
		sy = float64(e.scale) / 100 / float64(H)
		sx = sy
	}
	t := e.tm
	d28 := float64(e.density / 28)
	d56 := float64(e.density / 56)
	cosT := math.Cos(t)
	sinT := .5 * math.Sin(t/1.1767)
	cosT2 := .5 * math.Cos(t/1.833371)
	for x := 0; x < W; x++ {
		xx := sx * float64(x)
		w1 := math.Sin(xx*math.Pi*d28 + t)
		w2 := xx * math.Sin(t/2.567)
		for y := 0; y < H; y++ {
			yy := sy * float64(y)
			v := w1
			v += math.Sin(math.Pi*d28*(w2+yy*cosT) + t)
			cx := xx + sinT
			cy := yy + cosT2
			v += math.Sin(math.Sqrt((math.Pi*d56)*(math.Pi*d56)*(cx*cx+cy*cy)+1) + t)
			v = (v + 3) / 6
			v = math.Max(0, math.Min(1, v))
			e.layer[e.aL+x][e.aT+y] = e.value(f, t, v)
		}
	}
}

func (e *matrixPlasmaEffect) Finish() {
	if e.table != nil {
		e.alarms().UnregisterInterval(e)
	}
}

// ---- bitmap effects

// matrixBitmapEffect shows one bitmap frame scaled into the area.
type matrixBitmapEffect struct {
	matrixBase
	file        string
	frame       int
	bmpTop      int
	bmpLeft     int
	bmpWidth    int
	bmpHeight   int
	mode        string // BlendPixels, SinglePixelTopLeft, SinglePixelCenter
	scaleColors bool
	active      RGBA
	inactive    RGBA
	pixels      [][]RGBA
	ok          bool
}

func (e *matrixBitmapEffect) Init(t *Table) {
	e.initArea(t)
	e.ok, e.pixels = false, nil
	if e.layer == nil || e.file == "" {
		return
	}
	img, err := t.engine.loadImage(e.file)
	if err != nil {
		t.engine.log.Warnf("%s: %v", e.name, err)
		return
	}
	if e.frame >= len(img.frames) || e.frame < 0 {
		t.engine.log.Warnf("%s: frame %d does not exist in %s", e.name, e.frame, e.file)
		return
	}
	e.pixels = img.frames[e.frame].clip(e.areaW(), e.areaH(), e.bmpLeft, e.bmpTop, e.bmpWidth, e.bmpHeight, e.mode)
	if e.scaleColors {
		colorScalePixels(e.pixels, e.active, e.inactive)
	}
	e.ok = true
}

func colorScalePixels(p [][]RGBA, active, inactive RGBA) {
	for x := range p {
		for y := range p[x] {
			c := p[x][y]
			br := float64(c.R+c.G+c.B) / 3
			br = math.Max(0, math.Min(255, br))
			f := float32(br / 255)
			p[x][y] = RGBA{
				R: limit(inactive.R+int(float32(active.R-inactive.R)*f), 0, 255),
				G: limit(inactive.G+int(float32(active.G-inactive.G)*f), 0, 255),
				B: limit(inactive.B+int(float32(active.B-inactive.B)*f), 0, 255),
				A: limit(inactive.A+int(float32(active.A-inactive.A)*f), 0, 255),
			}
		}
	}
}

func (e *matrixBitmapEffect) Trigger(d TED) {
	if !e.ok {
		return
	}
	v := d.Value
	if e.fade == FadeModeOnOff {
		v = 0
		if d.Value >= 1 {
			v = 255
		}
	}
	for y := 0; y < e.areaH(); y++ {
		for x := 0; x < e.areaW(); x++ {
			p := e.pixels[x][y]
			p.A = int(float32(p.A) * float32(v) / 255)
			e.layer[e.aL+x][e.aT+y] = p
		}
	}
}

// matrixBitmapAnimEffect plays an animation made of bitmap frames/tiles.
type matrixBitmapAnimEffect struct {
	matrixBase
	file        string
	frame       int
	bmpTop      int
	bmpLeft     int
	bmpWidth    int
	bmpHeight   int
	mode        string
	stepDir     byte // F R D
	stepSize    int
	frameCount  int
	frameMs     int
	behaviour   byte // O L C
	scaleColors bool
	active      RGBA
	inactive    RGBA

	frames  [][][]RGBA
	ok      bool
	running bool
	step    int
	fadeVal int
}

func (e *matrixBitmapAnimEffect) Init(t *Table) {
	e.initArea(t)
	e.ok, e.frames = false, nil
	if e.layer == nil || e.file == "" {
		return
	}
	img, err := t.engine.loadImage(e.file)
	if err != nil {
		t.engine.log.Warnf("%s: %v", e.name, err)
		return
	}
	if e.frame < 0 || e.frame >= len(img.frames) {
		t.engine.log.Warnf("%s: frame %d does not exist in %s", e.name, e.frame, e.file)
		return
	}
	count := e.frameCount
	if count < 1 {
		count = 1
	}
	W, H := e.areaW(), e.areaH()
	switch e.stepDir {
	case 'F':
		step := e.stepSize
		if step < 1 {
			step = 1
		}
		if e.frame+count*step > len(img.frames) {
			count = (len(img.frames) - e.frame) / step
		}
		for s := 0; s < count; s++ {
			e.frames = append(e.frames, img.frames[e.frame+s].clip(W, H, e.bmpLeft, e.bmpTop, e.bmpWidth, e.bmpHeight, e.mode))
		}
	case 'R':
		for s := 0; s < count; s++ {
			e.frames = append(e.frames, img.frames[e.frame].clip(W, H, e.bmpLeft+s*e.stepSize, e.bmpTop, e.bmpWidth, e.bmpHeight, e.mode))
		}
	case 'D':
		for s := 0; s < count; s++ {
			e.frames = append(e.frames, img.frames[e.frame].clip(W, H, e.bmpLeft, e.bmpTop+s*e.stepSize, e.bmpWidth, e.bmpHeight, e.mode))
		}
	default:
		e.frames = [][][]RGBA{img.frames[e.frame].clip(W, H, e.bmpLeft, e.bmpTop, e.bmpWidth, e.bmpHeight, e.mode)}
	}
	if e.scaleColors {
		for _, f := range e.frames {
			colorScalePixels(f, e.active, e.inactive)
		}
	}
	e.ok = len(e.frames) > 0
}

func (e *matrixBitmapAnimEffect) animate() {
	if e.step <= len(e.frames)-1 {
		f := e.frames[e.step]
		for y := 0; y < e.areaH(); y++ {
			for x := 0; x < e.areaW(); x++ {
				p := f[x][y]
				p.A = int(float32(p.A) * float32(e.fadeVal) / 255)
				e.layer[e.aL+x][e.aT+y] = p
			}
		}
		e.step++
		if e.behaviour != 'O' {
			e.step %= len(e.frames)
		}
		return
	}
	e.step = 0
	if e.behaviour == 'O' {
		e.stop()
	}
}

func (e *matrixBitmapAnimEffect) clear() {
	for y := e.aT; y <= e.aB; y++ {
		for x := e.aL; x <= e.aR; x++ {
			e.layer[x][y] = RGBA{}
		}
	}
}

func (e *matrixBitmapAnimEffect) stop() {
	if e.running {
		e.alarms().UnregisterInterval(e)
		e.running = false
		e.clear()
	}
}

func (e *matrixBitmapAnimEffect) Trigger(d TED) {
	if !e.ok {
		return
	}
	v := d.Value
	if e.fade == FadeModeOnOff {
		v = 0
		if d.Value >= 1 {
			v = 255
		}
	}
	if v > 0 {
		e.fadeVal = v
		if !e.running {
			e.running = true
			if e.behaviour != 'C' {
				e.step = 0
			}
			e.alarms().RegisterInterval(time.Duration(e.frameMs)*time.Millisecond, e, e.animate)
			e.animate()
		}
		return
	}
	e.stop()
}

func (e *matrixBitmapAnimEffect) Finish() {
	if e.table != nil {
		e.stop()
	}
}

// ---- shape effect (a named bitmap region of DirectOutputShapes.xml)

type shapeEffect struct {
	matrixBase
	shape       string
	scaleColors bool
	active      RGBA
	inactive    RGBA
	target      Effect
}

func (e *shapeEffect) Init(t *Table) {
	e.initArea(t)
	e.target = nil
	sd := t.engine.shapes
	if sd == nil {
		return
	}
	def, ok := sd.Shapes[strings.ToUpper(e.shape)]
	if !ok {
		t.engine.log.Warnf("%s: shape %q not found in the shape definitions", e.name, e.shape)
		return
	}
	mb := matrixBase{toyName: e.toyName, layerNr: e.layerNr, fade: e.fade, left: e.left, top: e.top, width: e.width, height: e.height}
	mb.name = e.name + " Target"
	if def.Animated {
		fx := &matrixBitmapAnimEffect{matrixBase: mb, file: sd.BitmapFile, frame: def.Frame, bmpTop: def.Top, bmpLeft: def.Left,
			bmpWidth: def.Width, bmpHeight: def.Height, mode: def.Mode, stepDir: def.StepDir, stepSize: def.StepSize,
			frameCount: def.FrameCount, frameMs: def.FrameMs, behaviour: def.Behaviour, scaleColors: e.scaleColors,
			active: e.active, inactive: e.inactive}
		e.target = fx
	} else {
		fx := &matrixBitmapEffect{matrixBase: mb, file: sd.BitmapFile, frame: def.Frame, bmpTop: def.Top, bmpLeft: def.Left,
			bmpWidth: def.Width, bmpHeight: def.Height, mode: def.Mode, scaleColors: e.scaleColors, active: e.active, inactive: e.inactive}
		e.target = fx
	}
	e.target.Init(t)
}

func (e *shapeEffect) Trigger(d TED) {
	if e.target != nil {
		e.target.Trigger(d)
	}
}

func (e *shapeEffect) Finish() {
	if e.target != nil {
		e.target.Finish()
		e.target = nil
	}
}
