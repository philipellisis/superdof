package dof

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"superdof/internal/pinone"
	"sync"
	"time"
)

// Config configures the engine.
type Config struct {
	// IniFilesPath is the folder containing directoutputconfigNN.ini files.
	IniFilesPath string
	// CabinetFile is an optional DOF cabinet.xml.
	CabinetFile string
	// Cabinet is a ready made cabinet description (JSON config); wins over CabinetFile.
	Cabinet *CabinetSpec
	// PinOne selects the board connection: "" or "auto" (detect HID, then
	// legacy serial), "hid", or a serial port name such as "COM4".
	PinOne string
	// DisablePinOne runs without a PinOne controller at all (tests).
	DisablePinOne bool
	// DryRun uses a logging transport instead of the real board.
	DryRun bool
	// MinCommandIntervalMs is the minimum time between PinOne USB commands.
	MinCommandIntervalMs int
	Options              ConfiguratorOptions
	Log                  Logger
	// ExtraControllers allows tests to inject controllers instead of PinOne.
	ExtraControllers []Controller
}

// Engine is the unified DOF runtime: table elements in, PinOne output out.
type Engine struct {
	cfg     Config
	log     Logger
	cab     *Cabinet
	table   *Table
	files   []*LedControlFile
	alarms  Alarms
	in      chan func()
	quit    chan struct{}
	done    chan struct{}
	mu      sync.Mutex
	started bool
	dirty   bool
	images  map[string]*imageData
	shapes  *ShapeDefs
}

// New creates an engine; call Start to load configuration and run.
func New(cfg Config) *Engine {
	if cfg.Log == nil {
		cfg.Log = nopLogger{}
	}
	return &Engine{cfg: cfg, log: cfg.Log, in: make(chan func(), 4096), quit: make(chan struct{}), done: make(chan struct{})}
}

// Cabinet returns the cabinet (valid after Start).
func (e *Engine) Cabinet() *Cabinet { return e.cab }

// Table returns the active table (only touch it from Do callbacks).
func (e *Engine) Table() *Table { return e.table }

// LoadedFiles lists the ini files that were loaded.
func (e *Engine) LoadedFiles() []*LedControlFile { return e.files }

// Start loads ini files and the cabinet, starts controllers and the main loop.
func (e *Engine) Start() error {
	e.cab = NewCabinet(e.log)
	spec := e.cfg.Cabinet
	if spec == nil && e.cfg.CabinetFile != "" {
		s, err := LoadCabinetXML(e.cfg.CabinetFile, e.log)
		if err != nil {
			e.log.Warnf("cabinet config %s: %v", e.cfg.CabinetFile, err)
		}
		spec = s
	}
	if spec == nil {
		spec = &CabinetSpec{}
	}
	hasPinOne, hasFTDI := false, false
	for _, cs := range spec.Controllers {
		ctl, err := buildController(cs, e.cfg, e.log)
		if err != nil {
			e.log.Warnf("cabinet: %v", err)
			continue
		}
		switch c := ctl.(type) {
		case *PinOneController:
			hasPinOne = true
			e.setupPinOne(c)
		case *FTDIController:
			hasFTDI = true
		}
		e.cab.AddController(ctl)
	}
	if !e.cfg.DisablePinOne && !hasPinOne && spec.autoConfig() {
		ctl := NewPinOneController(1, e.cfg.PinOne, e.cfg.MinCommandIntervalMs, e.log)
		e.setupPinOne(ctl)
		e.cab.AddController(ctl)
	}
	if !hasFTDI && spec.autoConfig() && !e.cfg.DryRun {
		for _, c := range DetectFTDI(e.log) {
			e.cab.AddController(c)
		}
	}
	for _, c := range e.cfg.ExtraControllers {
		e.cab.AddController(c)
	}
	e.cab.applyToys(spec.Toys, e.log)
	e.cab.autoConfigLWE(e.log)
	if err := e.loadIniFiles(); err != nil {
		return err
	}
	if sd, err := LoadShapeDefs(filepath.Join(e.cfg.IniFilesPath, "DirectOutputShapes.xml")); err == nil {
		e.shapes = sd
		e.log.Infof("loaded %d shape definitions", len(sd.Shapes))
	}
	for _, c := range e.cab.Controllers {
		if err := c.Start(); err != nil {
			e.log.Warnf("controller %s: %v", c.Name(), err)
		}
	}
	e.table = NewTable()
	e.table.init(e, e.cab)
	e.started = true
	go e.loop()
	return nil
}

func (e *Engine) setupPinOne(c *PinOneController) {
	if e.cfg.DryRun {
		c.Opener = func(string) (pinone.Transport, error) { return &logTransport{log: e.log}, nil }
	}
}

func (e *Engine) loadIniFiles() error {
	dir := e.cfg.IniFilesPath
	if dir == "" {
		return fmt.Errorf("no ini files path configured")
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type nf struct {
		n    int
		path string
	}
	var found []nf
	for _, pre := range []string{"directoutputconfig", "ledcontrol"} {
		for _, en := range ents {
			if en.IsDir() {
				continue
			}
			l := strings.ToLower(en.Name())
			if strings.HasPrefix(l, pre) && strings.HasSuffix(l, ".ini") {
				if n, ok := LedControlFileNumber(en.Name()); ok {
					found = append(found, nf{n, filepath.Join(dir, en.Name())})
				}
			}
		}
		if len(found) > 0 {
			break
		}
	}
	if len(found) == 0 {
		return fmt.Errorf("no directoutputconfig*.ini files found in %s", dir)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].n < found[j].n })
	for _, f := range found {
		lc, err := ParseLedControl(f.path, f.n)
		if err != nil {
			e.log.Warnf("%s: %v", f.path, err)
			continue
		}
		for _, w := range lc.Warnings {
			e.log.Warnf("%s: %s", filepath.Base(f.path), w)
		}
		e.log.Infof("loaded %s (LedWiz #%d, %d tables)", filepath.Base(f.path), f.n, len(lc.Tables))
		e.files = append(e.files, lc)
	}
	return nil
}

// HasRom reports whether any loaded ini file has a config for the ROM.
func (e *Engine) HasRom(rom string) bool {
	for _, f := range e.files {
		if f.HasRom(rom) {
			return true
		}
	}
	return false
}

// Do runs fn on the engine goroutine.
func (e *Engine) Do(fn func()) {
	select {
	case e.in <- fn:
	case <-e.quit:
	}
}

// Send queues table element data (the DOF "ReceiveData").
func (e *Engine) Send(d TED) {
	e.Do(func() { e.table.UpdateElement(d); e.dirty = true })
}

// SendNamed sends a descriptor such as "E101" / "$NAME" with a value.
func (e *Engine) SendNamed(descriptor string, value int) { e.Send(NewTED(descriptor, value)) }

// Pulse implements DOFLinx "FF_DOF Ennn,ms": ms>0 turn on then off after ms,
// 0 turn off, -1 turn on and, when the ini effect chain is self terminating,
// automatically switch the element off again afterwards.
func (e *Engine) Pulse(descriptor string, ms int) {
	d := NewTED(descriptor, 0)
	e.Do(func() {
		key := fmt.Sprintf("pulse:%c:%d:%s", d.Type, d.Number, d.Name)
		e.alarms.Unregister(key)
		switch {
		case ms == 0:
			d.Value = 0
			e.table.UpdateElement(d)
		case ms > 0:
			d.Value = 255
			e.table.UpdateElement(d)
			off := d
			off.Value = 0
			e.alarms.Register(time.Duration(ms)*time.Millisecond, key, func() { e.table.UpdateElement(off); e.dirty = true }, false)
		default:
			el := e.table.Lookup(d)
			// retrigger: an element that is already on must go off first
			if el != nil && el.Value != 0 && el.Value != -1<<31 {
				z := d
				z.Value = 0
				e.table.UpdateElement(z)
			}
			d.Value = 255
			e.table.UpdateElement(d)
			if el = e.table.Lookup(d); el != nil && el.AutoOffMs > 0 {
				off := d
				off.Value = 0
				e.alarms.Register(time.Duration(el.AutoOffMs+5)*time.Millisecond, key, func() { e.table.UpdateElement(off); e.dirty = true }, false)
			}
		}
		e.dirty = true
	})
}

// LoadRom switches the active table to the given DOF ROM name. It reports
// (via the callback, if not nil) whether a configuration was found.
func (e *Engine) LoadRom(rom string, done func(found bool)) {
	e.Do(func() {
		e.alarms.Clear()
		if e.table != nil {
			e.table.finish()
		}
		e.cab.Reset()
		t := NewTable()
		t.Name = rom
		ok := Configure(e.files, rom, t, e.cab, e.cfg.Options, e.log)
		t.init(e, e.cab)
		e.table = t
		t.triggerStatic()
		e.cab.Update()
		e.notifyControllers()
		if ok {
			e.log.Infof("table %q: %d elements, %d effects", rom, t.ElementCount(), len(t.Effects))
		} else {
			e.log.Warnf("no ini configuration for ROM %q", rom)
		}
		if done != nil {
			done(ok)
		}
	})
}

// Clear drops the active table (all effect outputs go off).
func (e *Engine) Clear() {
	e.Do(func() {
		e.alarms.Clear()
		if e.table != nil {
			e.table.finish()
		}
		e.cab.Reset()
		e.table = NewTable()
		e.table.init(e, e.cab)
		e.cab.Update()
		e.notifyControllers()
	})
}

func (e *Engine) notifyControllers() {
	for _, c := range e.cab.Controllers {
		c.Notify()
	}
}

const maxInputBatch = 10 * time.Millisecond

func (e *Engine) loop() {
	defer close(e.done)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		wait := 50 * time.Millisecond
		if next, ok := e.alarms.Next(); ok {
			if d := time.Until(next); d < wait {
				wait = d
			}
			if wait < time.Millisecond {
				wait = time.Millisecond
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wait)
		select {
		case <-e.quit:
			return
		case fn := <-e.in:
			start := time.Now()
			fn()
			for time.Since(start) < maxInputBatch {
				select {
				case fn := <-e.in:
					fn()
					continue
				default:
				}
				break
			}
		case <-timer.C:
		}
		if e.alarms.Execute(time.Now().Add(time.Millisecond)) {
			e.dirty = true
		}
		if e.dirty {
			e.dirty = false
			e.cab.Update()
		}
	}
}

// Stop shuts the engine and controllers down (outputs are turned off).
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.started {
		return
	}
	e.started = false
	close(e.quit)
	<-e.done
	e.alarms.Clear()
	if e.table != nil {
		e.table.finish()
	}
	e.cab.Reset()
	for _, c := range e.cab.Controllers {
		c.Stop()
	}
}

// logTransport is the dry run "board": it logs every bank that is written.
type logTransport struct {
	log  Logger
	vals [PinOneOutputs]byte
}

func (l *logTransport) Write(p []byte) error {
	bank := int(p[1]) - 200
	for i := 0; i < 7; i++ {
		n := bank*7 + i
		if n >= PinOneOutputs || l.vals[n] == p[2+i] {
			continue
		}
		l.vals[n] = p[2+i]
		l.log.Infof("output %02d = %3d", n+1, p[2+i])
	}
	return nil
}
func (l *logTransport) Close() error     { return nil }
func (l *logTransport) Describe() string { return "dry run (log only)" }
