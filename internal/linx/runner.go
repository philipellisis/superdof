package linx

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DOF is the part of the DOF engine the runner needs.
type DOF interface {
	LoadRom(rom string, done func(found bool))
	Pulse(descriptor string, ms int)
	Clear()
}

// Runner executes DOFLinx command lists against the DOF engine and the
// directly linked PinOne outputs (LINK_xx devices, flashers, buttons, RGB).
type Runner struct {
	cfg     *Config
	dof     DOF
	sink    Sink
	log     Logger
	colours Colours
	links   *Links
	anim    *animator

	mu        sync.Mutex
	cmds      *CommandSet
	palette   []string
	btnColour map[string]string // BUT_xx -> colour override
	lightAct  []lightAction
	rgbDef    RGB
	rgbTemp   bool
	rgbSeq    int
	unsupp    map[string]bool
	game      string
	prevMode  string
}

type lightAction struct {
	button, event, state string
	action, ms           int
	colour               string
}

// NewRunner creates a runner. colours may be nil.
func NewRunner(cfg *Config, d DOF, sink Sink, colours Colours, log Logger) *Runner {
	if log == nil {
		log = StdLogger()
	}
	if colours == nil {
		colours = DefaultColours()
	}
	return &Runner{cfg: cfg, dof: d, sink: sink, log: log, colours: colours, links: LoadLinks(cfg),
		anim: newAnimator(), cmds: NewCommandSet(), btnColour: map[string]string{}, unsupp: map[string]bool{}}
}

// Links exposes the parsed LINK_ configuration.
func (r *Runner) Links() *Links { return r.links }

// Game returns the active game name.
func (r *Runner) Game() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.game
}

// Begin activates a command set (All_Pre + game file + All_Post already
// merged) and runs the [STARTUP] actions of the given files.
func (r *Runner) Begin(game string, cmds *CommandSet, files []*FXFile) {
	r.End()
	r.mu.Lock()
	r.game = game
	r.cmds = cmds
	r.palette = nil
	r.btnColour = map[string]string{}
	r.lightAct = nil
	r.mu.Unlock()

	// Parameters first (palette / button colours / light actions), then
	// FF_ROM, then all other startup actions in file order.
	for _, f := range files {
		for k, vs := range f.Params {
			for _, v := range vs {
				r.applyParam(k, v)
			}
		}
	}
	var rest []Action
	for _, f := range files {
		for _, a := range f.Startup {
			if a.Name == "FF_ROM" {
				r.Execute(a)
			} else {
				rest = append(rest, a)
			}
		}
	}
	for _, a := range rest {
		r.Execute(a)
	}
	for _, p := range r.links.Fixed {
		r.setFixed(p, true)
	}
}

func (r *Runner) applyParam(k, v string) {
	switch k {
	case "COLOUR_PALETTE":
		r.mu.Lock()
		r.palette = nil
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				r.palette = append(r.palette, p)
			}
		}
		r.mu.Unlock()
	case "BUTTON_COLOUR_CHANGE":
		p := strings.Split(v, ",")
		for i := 0; i+1 < len(p); i += 2 {
			b := strings.ToUpper(strings.TrimSpace(p[i]))
			c := strings.TrimSpace(p[i+1])
			if strings.EqualFold(c, "RANDOM") {
				c = r.randomColourName()
			}
			r.mu.Lock()
			r.btnColour[b] = c
			r.mu.Unlock()
		}
	case "BUTTON_LIGHT_ACTION":
		p := strings.Split(v, ",")
		r.mu.Lock()
		for i := 0; i+5 < len(p); i += 6 {
			r.lightAct = append(r.lightAct, lightAction{
				button: strings.ToUpper(strings.TrimSpace(p[i])), event: strings.ToUpper(strings.TrimSpace(p[i+1])),
				state: strings.ToUpper(strings.TrimSpace(p[i+2])), action: atoi(p[i+3]), ms: atoi(p[i+4]),
				colour: strings.TrimSpace(p[i+5])})
		}
		r.mu.Unlock()
	}
}

// End runs the shutdown actions, stops all animations and clears outputs.
func (r *Runner) End() {
	r.mu.Lock()
	had := r.game != ""
	r.game = ""
	r.mu.Unlock()
	if !had {
		return
	}
	r.anim.cancelAll()
	r.ReleaseAll()
	r.dof.Clear()
}

// ReleaseAll turns off every output this runner may have driven.
func (r *Runner) ReleaseAll() {
	for _, ls := range r.links.Dev {
		for _, l := range ls {
			r.set(l.Addr, "dev", 0)
		}
	}
	for code, ls := range r.links.Flasher {
		for _, l := range ls {
			r.rgbSet(l.Addr, "fl:"+code, RGB{}, "")
		}
	}
	for code, ls := range r.links.Button {
		for _, l := range ls {
			r.rgbSet(l.Addr, "but:"+code, RGB{}, r.links.Order[code])
		}
	}
	for _, l := range r.links.RGB {
		r.rgbSet(l.Addr, "rgb", RGB{}, "")
	}
	for _, p := range r.links.Fixed {
		r.setFixed(p, false)
	}
}

func (r *Runner) set(addr int, key any, v uint8) {
	d, p := Addr(addr)
	r.sink.SetOutput(d, p, key, v)
}

func (r *Runner) rgbSet(addr int, key any, c RGB, order string) {
	d, p := Addr(addr)
	ch := [3]uint8{c.R, c.G, c.B}
	o := colourOrder(order)
	r.sink.SetOutput(d, p, key, ch[o[0]])
	r.sink.SetOutput(d, p+1, key, ch[o[1]])
	r.sink.SetOutput(d, p+2, key, ch[o[2]])
}

func (r *Runner) setFixed(addr int, on bool) {
	if c, ok := r.links.FixedC[addr]; ok {
		if on {
			r.rgbSet(addr, "fixed", r.resolveColour(c), "")
		} else {
			r.rgbSet(addr, "fixed", RGB{}, "")
		}
		return
	}
	var v uint8
	if on {
		v = 255
	}
	r.set(addr, "fixed", v)
}

// ---------------------------------------------------------------- colours

func (r *Runner) randomColourName() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.palette) > 0 {
		return r.palette[randInt(len(r.palette))]
	}
	names := make([]string, 0, len(r.colours))
	for k := range r.colours {
		if k != "BLACK" {
			names = append(names, k)
		}
	}
	if len(names) == 0 {
		return "White"
	}
	return names[randInt(len(names))]
}

func (r *Runner) resolveColour(name string) RGB {
	name = strings.TrimSpace(name)
	if strings.EqualFold(name, "RANDOM") {
		name = r.randomColourName()
	}
	if c, ok := r.colours.Lookup(name); ok {
		return c
	}
	return RGB{}
}

// ---------------------------------------------------------------- execute

// Execute runs one action.
func (r *Runner) Execute(a Action) {
	args := splitArgs(a.Args)
	switch a.Name {
	case "FF_ROM":
		rom := strings.TrimSpace(a.Args)
		if rom != "" {
			r.dof.LoadRom(rom, func(ok bool) {
				if !ok {
					r.log.Warnf("FF_ROM %s: no DOF configuration found in the directoutputconfig ini files", rom)
				}
			})
		}
	case "FF_DOF":
		for i := 0; i < len(args); i += 2 {
			ms := -1
			if i+1 < len(args) && isNum(args[i+1]) {
				ms = atoi(args[i+1])
			}
			r.dof.Pulse(args[i], ms)
		}
	case "FF_DEV":
		if len(args) >= 1 {
			ms := -1
			if len(args) > 1 {
				ms = atoi(args[1])
			}
			r.dev(devCode(args[0]), ms)
		}
	case "FF_FLASHER":
		// FF_Flasher DV_FLOL,FL_TT,1,150,100,Red
		if len(args) >= 2 {
			it, tm, pct, col := 1, 0, 100, "White"
			if len(args) > 2 {
				it = atoi(args[2])
			}
			if len(args) > 3 {
				tm = atoi(args[3])
			}
			if len(args) > 4 {
				pct = atoi(args[4])
			}
			if len(args) > 5 {
				col = args[5]
			}
			r.flasher(devCode(args[0]), strings.ToUpper(args[1]), it, tm, pct, col)
		}
	case "FF_BUTTON":
		if len(args) >= 2 {
			it, tm := 0, 0
			if len(args) > 2 {
				it = atoi(args[2])
			}
			if len(args) > 3 {
				tm = atoi(args[3])
			}
			r.button(strings.ToUpper(args[0]), strings.ToUpper(args[1]), it, tm)
		}
	case "FF_COLOUR":
		if len(args) >= 2 {
			ms := 0
			if len(args) > 2 {
				ms = atoi(args[2])
			}
			r.colour(args[0], strings.ToUpper(args[1]), ms)
		}
	default:
		r.mu.Lock()
		first := !r.unsupp[a.Name]
		r.unsupp[a.Name] = true
		r.mu.Unlock()
		if first {
			r.log.Infof("action %s is not applicable to PinOne force feedback and is ignored", a.Name)
		}
	}
}

func splitArgs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		out = append(out, strings.TrimSpace(p))
	}
	if len(out) == 1 && out[0] == "" {
		return nil
	}
	return out
}

func devCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	return strings.TrimPrefix(s, "DV_")
}

func (r *Runner) dev(code string, ms int) {
	links := r.links.Dev[code]
	if len(links) == 0 {
		return
	}
	for i, l := range links {
		key := fmt.Sprintf("dev:%s:%d", code, i)
		r.anim.cancel(key)
		if ms == 0 {
			r.set(l.Addr, key, 0)
			continue
		}
		dur := ms
		if dur < 0 {
			dur = l.DefaultMs
		}
		if l.MaxMs > 0 && dur > l.MaxMs {
			dur = l.MaxMs
		}
		if l.Mode == "ON" && dur <= 0 {
			dur = 1000
		}
		if dur <= 0 {
			dur = 100
		}
		val := uint8(l.Intensity)
		if l.Mode == "FLASH" && l.FlashMs > 0 {
			l := l
			r.anim.run(key, func(stop <-chan struct{}) {
				end := time.Now().Add(time.Duration(dur) * time.Millisecond)
				for time.Now().Before(end) {
					r.set(l.Addr, key, val)
					if !sleep(stop, time.Duration(l.FlashMs)*time.Millisecond) {
						r.set(l.Addr, key, 0)
						return
					}
					r.set(l.Addr, key, 0)
					if !sleep(stop, time.Duration(l.FlashMs)*time.Millisecond) {
						return
					}
				}
			})
			continue
		}
		l := l
		r.set(l.Addr, key, val)
		r.anim.run(key, func(stop <-chan struct{}) {
			if sleep(stop, time.Duration(dur)*time.Millisecond) {
				r.set(l.Addr, key, 0)
			}
		})
	}
}

// drive plays the effect of an RGB group (flasher/button) at one port.
type rgbTarget struct {
	addr  int
	key   string
	order string
	mono  bool
}

func (t rgbTarget) write(r *Runner, c RGB) {
	if t.mono {
		m := c.R
		if c.G > m {
			m = c.G
		}
		if c.B > m {
			m = c.B
		}
		r.set(t.addr, t.key, m)
		return
	}
	r.rgbSet(t.addr, t.key, c, t.order)
}

func (r *Runner) flasher(code, action string, it, tm, pct int, colour string) {
	links := r.links.Flasher[code]
	if len(links) == 0 {
		return
	}
	key := "fl:" + code
	col := r.resolveColour(colour)
	col = RGB{scale(col.R, pct), scale(col.G, pct), scale(col.B, pct)}
	var targets []rgbTarget
	for _, l := range links {
		targets = append(targets, rgbTarget{addr: l.Addr, key: key, order: r.links.Order[code]})
	}
	r.playRGB(key, targets, strings.TrimPrefix(action, "FL_"), "FL", it, tm, col)
}

func (r *Runner) button(code, action string, it, tm int) {
	links := r.links.Button[code]
	if len(links) == 0 {
		return
	}
	key := "but:" + code
	r.mu.Lock()
	name := r.btnColour[code]
	r.mu.Unlock()
	var targets []rgbTarget
	var col RGB
	for i, l := range links {
		c := l.Colour
		if i == 0 && name != "" {
			c = name
		}
		if c == "" || strings.EqualFold(c, "MONO") {
			c = "White"
		}
		if i == 0 {
			col = r.resolveColour(c)
		}
		targets = append(targets, rgbTarget{addr: l.Addr, key: key, order: r.links.Order[code], mono: l.Mono})
	}
	r.playRGB(key, targets, strings.TrimPrefix(action, "BA_"), "BA", it, tm, col)
}

// playRGB implements ON / OFF / TT / FL / FD for flashers and buttons.
func (r *Runner) playRGB(key string, targets []rgbTarget, action, _ string, it, tm int, col RGB) {
	write := func(c RGB) {
		for _, t := range targets {
			t.write(r, c)
		}
	}
	switch action {
	case "ON":
		r.anim.cancel(key)
		write(col)
	case "OFF":
		r.anim.cancel(key)
		write(RGB{})
	case "TT":
		if tm <= 0 {
			tm = it
		}
		write(col)
		r.anim.run(key, func(stop <-chan struct{}) {
			if sleep(stop, time.Duration(tm)*time.Millisecond) {
				write(RGB{})
			}
		})
	case "FL":
		if it < 1 {
			it = 1
		}
		if tm <= 0 {
			tm = 100
		}
		r.anim.run(key, func(stop <-chan struct{}) {
			for i := 0; i < it; i++ {
				write(col)
				if !sleep(stop, time.Duration(tm)*time.Millisecond) {
					return
				}
				write(RGB{})
				if !sleep(stop, time.Duration(tm)*time.Millisecond) {
					return
				}
			}
		})
	case "FD":
		if it < 1 {
			it = 1
		}
		if tm <= 0 {
			tm = 500
		}
		r.anim.run(key, func(stop <-chan struct{}) {
			const step = 25 * time.Millisecond
			half := time.Duration(tm) * time.Millisecond / 2
			n := int(half / step)
			if n < 1 {
				n = 1
			}
			for i := 0; i < it; i++ {
				for s := 0; s <= 2*n; s++ {
					f := s
					if s > n {
						f = 2*n - s
					}
					p := f * 100 / n
					write(RGB{scale(col.R, p), scale(col.G, p), scale(col.B, p)})
					if !sleep(stop, step) {
						return
					}
				}
			}
			write(RGB{})
		})
	}
}

func (r *Runner) colour(name, action string, ms int) {
	if len(r.links.RGB) == 0 {
		return
	}
	var c RGB
	if strings.TrimSpace(name) != "" {
		c = r.resolveColour(name)
	}
	apply := func(c RGB) {
		for _, l := range r.links.RGB {
			r.rgbSet(l.Addr, "rgb", c, r.links.Order["RGB"])
		}
	}
	switch action {
	case "RGB_DF", "RGB_CH":
		r.mu.Lock()
		r.rgbDef = c
		r.rgbSeq++
		r.mu.Unlock()
		r.anim.cancel("rgb")
		apply(c)
	case "RGB_TT", "RGB_FL":
		r.mu.Lock()
		def := r.rgbDef
		r.mu.Unlock()
		if ms <= 0 {
			ms = 500
		}
		apply(c)
		r.anim.run("rgb", func(stop <-chan struct{}) {
			if action == "RGB_FL" {
				// flash: toggle at 100ms until time is up
				end := time.Now().Add(time.Duration(ms) * time.Millisecond)
				on := true
				for time.Now().Before(end) {
					if !sleep(stop, 100*time.Millisecond) {
						return
					}
					on = !on
					if on {
						apply(c)
					} else {
						apply(RGB{})
					}
				}
			} else if !sleep(stop, time.Duration(ms)*time.Millisecond) {
				return
			}
			apply(def)
		})
	}
}

// ---------------------------------------------------------------- events

// Event dispatches a named game event (e.g. BUMPER01) with a value.
func (r *Runner) Event(name string, value int) {
	state := "ON"
	if value == 0 {
		state = "OFF"
	}
	r.mu.Lock()
	cmds := r.cmds
	la := r.lightAct
	r.mu.Unlock()
	if cmds == nil {
		return
	}
	for _, a := range cmds.Lookup(name, state) {
		r.Execute(a)
	}
	up := strings.ToUpper(name)
	for _, l := range la {
		if l.event == up && l.state == state {
			r.lightAction(l)
		}
	}
}

// lightAction implements BUTTON_LIGHT_ACTION codes 1 on, 2 off, 3 on for T,
// 6 off for T, 7 alternate colour for T.
func (r *Runner) lightAction(l lightAction) {
	code := strings.TrimPrefix(l.button, "BUT_")
	_ = code
	links := r.links.Button[l.button]
	if len(links) == 0 {
		return
	}
	key := "but:" + l.button
	r.mu.Lock()
	base := r.btnColour[l.button]
	r.mu.Unlock()
	if base == "" {
		base = links[0].Colour
	}
	if base == "" || strings.EqualFold(base, "MONO") {
		base = "White"
	}
	on := r.resolveColour(base)
	var targets []rgbTarget
	for _, k := range links {
		targets = append(targets, rgbTarget{addr: k.Addr, key: key, order: r.links.Order[l.button], mono: k.Mono})
	}
	write := func(c RGB) {
		for _, t := range targets {
			t.write(r, c)
		}
	}
	switch l.action {
	case 1:
		r.anim.cancel(key)
		write(on)
	case 2:
		r.anim.cancel(key)
		write(RGB{})
	case 3, 6, 7:
		first, second := on, RGB{}
		switch l.action {
		case 6:
			first, second = RGB{}, on
		case 7:
			first, second = r.resolveColour(l.colour), on
		}
		write(first)
		ms := l.ms
		if ms <= 0 {
			ms = 250
		}
		r.anim.run(key, func(stop <-chan struct{}) {
			if sleep(stop, time.Duration(ms)*time.Millisecond) {
				write(second)
			}
		})
	}
}

// Status handles a game status change from the trigger DLL (S,<name>).
// Statuses map onto the *_MODE events used in the FX files.
func (r *Runner) Status(name string) {
	mode := ""
	switch strings.ToUpper(strings.ReplaceAll(name, " ", "")) {
	case "MENU":
		mode = "MENU_MODE"
	case "PLAY":
		mode = "PLAY_MODE"
	case "INLANE":
		mode = "INLANE_MODE"
	default:
		mode = strings.ToUpper(name) + "_MODE"
	}
	r.mu.Lock()
	prev := r.prevMode
	r.prevMode = mode
	r.mu.Unlock()
	if prev != "" && prev != mode {
		r.Event(prev, 0)
	}
	r.Event(mode, 1)
}

// Nudge handles N,Left|Right|Forward.
func (r *Runner) Nudge(dir string) {
	name := "Nudge_" + dir
	r.Event(name, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		r.Event(name, 0)
	}()
}

// OutputNow handles OUTPUT_NOW_TIMER=DOOO,MMMM[,...] real time commands.
func (r *Runner) OutputNow(addr, ms int) {
	key := fmt.Sprintf("now:%d", addr)
	r.anim.cancel(key)
	r.set(addr, key, 255)
	r.anim.run(key, func(stop <-chan struct{}) {
		if sleep(stop, time.Duration(ms)*time.Millisecond) {
			r.set(addr, key, 0)
		}
	})
}

// OutputColourNow handles OUTPUT_NOW_COLOUR=DOOO,MMMM,CCCC.
func (r *Runner) OutputColourNow(addr, ms int, colour string) {
	key := fmt.Sprintf("nowc:%d", addr)
	r.anim.cancel(key)
	r.rgbSet(addr, key, r.resolveColour(colour), "")
	r.anim.run(key, func(stop <-chan struct{}) {
		if sleep(stop, time.Duration(ms)*time.Millisecond) {
			r.rgbSet(addr, key, RGB{}, "")
		}
	})
}

// OutputsOff turns everything off (OUTPUTS_OFF).
func (r *Runner) OutputsOff() {
	r.anim.cancelAll()
	r.ReleaseAll()
}

// parseInt is a small helper used by the command pipe.
func parseInt(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil
}

// EventNames lists the events ("NAME|STATE") of the active command set.
func (r *Runner) EventNames() []string {
	r.mu.Lock()
	c := r.cmds
	r.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.Names()
}

// Actions returns the actions the active command set runs for an event.
func (r *Runner) Actions(name, state string) []Action {
	r.mu.Lock()
	c := r.cmds
	r.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.Lookup(name, state)
}
