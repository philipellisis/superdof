package dof

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"superdof/internal/pinone"
)

type memTransport struct {
	mu      sync.Mutex
	packets [][]byte
}

func (m *memTransport) Write(p []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.packets = append(m.packets, append([]byte(nil), p...))
	return nil
}
func (m *memTransport) Close() error     { return nil }
func (m *memTransport) Describe() string { return "mem" }

// outputs reconstructs the current output values from the packet stream.
func (m *memTransport) outputs() [PinOneOutputs]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	var v [PinOneOutputs]byte
	for _, p := range m.packets {
		bank := int(p[1]) - 200
		for i := 0; i < 7; i++ {
			if bank*7+i < PinOneOutputs {
				v[bank*7+i] = p[2+i]
			}
		}
	}
	return v
}

const testIni = `[version]
mindofversion=0.8
[Colors DOF]
Black=#000000FF
Red=#FF0000FF
Blue=#0000FFFF
White=#FFFFFFFF
[Config DOF]
testrom,S1 I48,W5 Red,S2 400 I24,E10 Blink 100,(S3=1 AND S4=1) I48,S6 F300
`

func newTestEngine(t *testing.T) (*Engine, *memTransport) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "directoutputconfig11.ini"), []byte(testIni), 0o644); err != nil {
		t.Fatal(err)
	}
	mem := &memTransport{}
	ctl := NewPinOneController(1, "mem", 1, nil)
	ctl.Opener = func(string) (pinone.Transport, error) { return mem, nil }
	e := New(Config{IniFilesPath: dir, DisablePinOne: true, ExtraControllers: []Controller{ctl}})
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Stop)
	return e, mem
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func TestParseSetting(t *testing.T) {
	s, err := ParseSetting("S12 Red 400 AH100 AW14")
	if err != nil {
		t.Fatal(err)
	}
	if s.Control != ControlControlled || s.TableElement != "S12" || s.ColorName != "RED" || s.DurationMs != 400 || !s.IsArea {
		t.Fatalf("unexpected: %+v", s)
	}
	s, err = ParseSetting("W7|W8 I24 Blink 200 M50 L3 F100")
	if err != nil {
		t.Fatal(err)
	}
	if s.TableElement != "W7|W8" || s.Intensity() != 127 || s.Blink != -1 || s.BlinkIntervalMs != 200 || s.MinDurationMs != 50 || *s.Layer != 3 || s.FadeUpMs != 100 {
		t.Fatalf("unexpected: %+v", s)
	}
	s, err = ParseSetting("(W8 = 0 and S14 = 1) 300")
	if err != nil {
		t.Fatal(err)
	}
	if s.Control != ControlCondition || s.Condition != "(W8 = 0 AND S14 = 1)" || s.DurationMs != 300 {
		t.Fatalf("unexpected: %+v", s)
	}
	if _, err = ParseSetting("W12 Yellow 400 AH100 AL60AT0"); err == nil {
		t.Fatal("expected error for malformed part")
	}
	s, _ = ParseSetting("E101 200 3")
	if s.DurationMs != 0 || s.Blink != 3 || s.BlinkIntervalMs != 66 {
		t.Fatalf("duration/blink count: %+v", s)
	}
}

func TestCondition(t *testing.T) {
	c, err := compileCondition("(E229=1 OR E220=1) AND NOT W8=0")
	if err != nil {
		t.Fatal(err)
	}
	vals := map[string]float64{"E229": 0, "E220": 1, "W8": 1}
	if c.root.eval(func(n string) float64 { return vals[n] }) != 1 {
		t.Fatal("expected true")
	}
	vals["W8"] = 0
	if c.root.eval(func(n string) float64 { return vals[n] }) != 0 {
		t.Fatal("expected false")
	}
}

func TestEndToEndSimpleOutputs(t *testing.T) {
	e, mem := newTestEngine(t)
	done := make(chan bool, 1)
	e.LoadRom("testrom", func(ok bool) { done <- ok })
	if !<-done {
		t.Fatal("rom not found")
	}
	// column 1 -> output 1, column 2 is RGB -> outputs 2,3,4, column 3 -> 5
	e.SendNamed("S1", 1)
	waitFor(t, "output 1 on", func() bool { return mem.outputs()[0] == 255 })
	e.SendNamed("S1", 0)
	waitFor(t, "output 1 off", func() bool { return mem.outputs()[0] == 0 })

	e.SendNamed("W5", 1)
	waitFor(t, "rgb red", func() bool { o := mem.outputs(); return o[1] == 255 && o[2] == 0 && o[3] == 0 })
	e.SendNamed("W5", 0)
	waitFor(t, "rgb off", func() bool { o := mem.outputs(); return o[1] == 0 })
}

func TestDurationAndCondition(t *testing.T) {
	e, mem := newTestEngine(t)
	done := make(chan bool, 1)
	e.LoadRom("testrom", func(ok bool) { done <- ok })
	<-done
	// column 3 "S2 400 I24": on for 400ms, output 5 at intensity 127
	e.SendNamed("S2", 1)
	waitFor(t, "output 5 on", func() bool { return mem.outputs()[4] == 127 })
	waitFor(t, "output 5 off after duration", func() bool { return mem.outputs()[4] == 0 })

	// column 5: condition (S3=1 AND S4=1) -> output index: col1=1,col2=2-4,col3=5,col4=6,col5=7
	e.SendNamed("S3", 1)
	time.Sleep(60 * time.Millisecond)
	if mem.outputs()[6] != 0 {
		t.Fatal("condition must not fire with only S3")
	}
	e.SendNamed("S4", 1)
	waitFor(t, "condition output", func() bool { return mem.outputs()[6] == 255 })
}

func TestPulse(t *testing.T) {
	e, mem := newTestEngine(t)
	done := make(chan bool, 1)
	e.LoadRom("testrom", func(ok bool) { done <- ok })
	<-done
	e.Pulse("S1", 200)
	waitFor(t, "on", func() bool { return mem.outputs()[0] == 255 })
	waitFor(t, "off", func() bool { return mem.outputs()[0] == 0 })
}

func TestRealIniFiles(t *testing.T) {
	dir := os.Getenv("SUPERDOF_REAL_INI_DIR")
	if dir == "" {
		dir = `C:\DirectOutput\Config`
	}
	f := filepath.Join(dir, "directoutputconfig11.ini")
	if _, err := os.Stat(f); err != nil {
		t.Skip("real ini not available")
	}
	lc, err := ParseLedControl(f, 11)
	if err != nil {
		t.Fatal(err)
	}
	if len(lc.Tables) < 500 {
		t.Fatalf("expected many tables, got %d", len(lc.Tables))
	}
	t.Logf("tables=%d colors=%d warnings=%d", len(lc.Tables), len(lc.Colors), len(lc.Warnings))
	if !lc.HasRom("bttf") && !lc.HasRom("afm") {
		t.Fatal("expected afm or bttf")
	}
}
