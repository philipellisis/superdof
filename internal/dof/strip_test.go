package dof

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"superdof/internal/ftdi"
	"superdof/internal/serial"
)

// fakeWemos emulates the Teensy/Wemos LED controller side of the protocol.
type fakeWemos struct {
	mu       sync.Mutex
	in       []byte // bytes queued for the host to read
	out      []byte // bytes the host wrote
	cmdMode  bool
	maxLeds  int
	frames   map[int][]byte // target position -> data
	parseBuf []byte
	outputs  int
}

func newFakeWemos() *fakeWemos { return &fakeWemos{frames: map[int][]byte{}, maxLeds: 1000} }

func (f *fakeWemos) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.in) == 0 {
		return 0, serial.ErrTimeout
	}
	n := copy(p, f.in)
	f.in = f.in[n:]
	return n, nil
}

func (f *fakeWemos) Available() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.in) }
func (f *fakeWemos) Flush()         { f.mu.Lock(); f.in = nil; f.mu.Unlock() }
func (f *fakeWemos) Close() error   { return nil }

func (f *fakeWemos) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.out = append(f.out, p...)
	f.parseBuf = append(f.parseBuf, p...)
	for f.step() {
	}
	return len(p), nil
}

// step consumes one command from parseBuf; returns false when incomplete.
func (f *fakeWemos) step() bool {
	b := f.parseBuf
	if len(b) == 0 {
		return false
	}
	if !f.cmdMode {
		// any bytes before command mode: reply 'A' to a lone 0
		f.parseBuf = b[1:]
		if b[0] == 0 {
			f.in = append(f.in, 'A')
			f.cmdMode = true
		}
		return true
	}
	switch b[0] {
	case 'M':
		f.in = append(f.in, byte(f.maxLeds>>8), byte(f.maxLeds&255), 'A')
		f.parseBuf = b[1:]
	case 'L':
		if len(b) < 3 {
			return false
		}
		f.in = append(f.in, 'A')
		f.parseBuf = b[3:]
	case 'C', 'O':
		f.in = append(f.in, 'A')
		f.parseBuf = b[1:]
	case 'R':
		if len(b) < 5 {
			return false
		}
		pos := int(b[1])<<8 | int(b[2])
		n := (int(b[3])<<8 | int(b[4])) * 3
		if len(b) < 5+n {
			return false
		}
		f.frames[pos] = append([]byte(nil), b[5:5+n]...)
		f.in = append(f.in, 'A')
		f.parseBuf = b[5+n:]
	default:
		f.parseBuf = b[1:]
	}
	return true
}

func (f *fakeWemos) frame(pos int) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.frames[pos]...)
}

func TestStripControllerProtocol(t *testing.T) {
	fw := newFakeWemos()
	c := NewStripController(StripConfig{Name: "LED Strips 0", ComPort: "COMX", Baud: 2000000, LedsPerStrip: [10]int{4, 3}}, nil)
	c.Open = func(serial.Config) (serial.Port, error) { return fw, nil }
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer c.Stop()
	c.SetValues(0, []byte{1, 2, 3, 4, 5, 6})
	c.SetValues(12, []byte{9, 9, 9}) // first LED of strip 2
	waitFor(t, "strip 1 frame", func() bool {
		f := fw.frame(0)
		return len(f) == 12 && f[0] == 1 && f[5] == 6
	})
	// strip 2 starts at channel offset = max leds per strip (4)
	waitFor(t, "strip 2 frame", func() bool {
		f := fw.frame(4)
		return len(f) == 9 && f[0] == 9
	})
}

func TestCompressRuns(t *testing.T) {
	got := compressRuns([]byte{1, 2, 3, 1, 2, 3, 1, 2, 3, 7, 8, 9})
	want := []byte{3, 1, 2, 3, 1, 7, 8, 9}
	if string(got) != string(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

type fakeFTDI struct {
	mu   sync.Mutex
	vals []byte
}

func (f *fakeFTDI) Write(v byte) error {
	f.mu.Lock()
	f.vals = append(f.vals, v)
	f.mu.Unlock()
	return nil
}
func (f *fakeFTDI) Close() error { return nil }
func (f *fakeFTDI) last() byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.vals) == 0 {
		return 255
	}
	return f.vals[len(f.vals)-1]
}

func TestFTDIControllerThroughDOF(t *testing.T) {
	dir := t.TempDir()
	ini := "[version]\nmindofversion=0.8\n[Colors DOF]\nRed=#FF0000FF\n[Config DOF]\nftrom,S1,S2 I10,S3 I48\n"
	if err := os.WriteFile(filepath.Join(dir, "directoutputconfig41.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	ff := &fakeFTDI{}
	ctl := NewFTDIController(1, "A1B2C3", nil)
	ctl.Open = func(string) (ftdi.Handle, error) { return ff, nil }
	e := New(Config{IniFilesPath: dir, DisablePinOne: true, ExtraControllers: []Controller{ctl}})
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	done := make(chan bool, 1)
	e.LoadRom("ftrom", func(ok bool) { done <- ok })
	if !<-done {
		t.Fatal("rom not found for LedWizEquivalent 41")
	}
	e.SendNamed("S1", 1)
	e.SendNamed("S3", 1)
	waitFor(t, "bits 0 and 2", func() bool { return ff.last() == 0b101 })
	e.SendNamed("S1", 0)
	waitFor(t, "bit 2 only", func() bool { return ff.last() == 0b100 })
}

func TestLedStripAreaEffects(t *testing.T) {
	dir := t.TempDir()
	// LedWizEquivalent 30: output 1 -> strip toy "Right Side". Area column with a
	// white color effect over the lower half and a shift effect in column 2.
	ini := "[version]\nmindofversion=0.8\n[Colors DOF]\nWhite=#FFFFFFFF\nRed=#FF0000FF\n[Config DOF]\n" +
		"striprom,S1 White AT50 AH50 AL0 AW100,S2 Red ADR AS1000 AT0 AH100 AL0 AW100\n"
	if err := os.WriteFile(filepath.Join(dir, "directoutputconfig30.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	fw := newFakeWemos()
	ctl := NewStripController(StripConfig{Name: "LED Strips 0", ComPort: "COMX", LedsPerStrip: [10]int{4}}, nil)
	ctl.Open = func(serial.Config) (serial.Port, error) { return fw, nil }
	spec := &CabinetSpec{
		Controllers: []ControllerSpec{},
		Toys: []ToySpec{
			{Type: "ledstrip", Name: "Right Side", Width: 1, Height: 4, Arrangement: "LeftRightTopDown", ColorOrder: "RGB", FirstLed: 1, Controller: "LED Strips 0"},
			{Type: "ledwizequivalent", Name: "LedWizEquivalent 30", Number: 30, Outputs: []OutputRef{{Output: "Right Side", Number: 1}, {Output: "Right Side", Number: 4}}},
		},
	}
	e := New(Config{IniFilesPath: dir, DisablePinOne: true, Cabinet: spec, ExtraControllers: []Controller{ctl}})
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	done := make(chan bool, 1)
	e.LoadRom("striprom", func(ok bool) { done <- ok })
	if !<-done {
		t.Fatal("rom not found")
	}
	e.SendNamed("S1", 1)
	waitFor(t, "lower half white", func() bool {
		f := fw.frame(0)
		return len(f) == 12 && f[0] == 0 && f[3] == 0 && f[6] == 255 && f[9] == 255
	})
	e.SendNamed("S1", 0)
	waitFor(t, "off", func() bool {
		f := fw.frame(0)
		return len(f) == 12 && f[6] == 0 && f[9] == 0
	})
}

func TestStripArrangement(t *testing.T) {
	s := NewLedStrip("x")
	s.Width, s.Height = 3, 2
	s.Arrangement = "LeftRightAlternateTopDown"
	s.attach(NewCabinet(nil))
	// row 0 goes left->right, row 1 right->left
	want := [][]int{{0, 5}, {1, 4}, {2, 3}}
	for x := 0; x < 3; x++ {
		for y := 0; y < 2; y++ {
			if s.mapping[x][y] != want[x][y]*3 {
				t.Fatalf("(%d,%d) = %d want %d", x, y, s.mapping[x][y]/3, want[x][y])
			}
		}
	}
}

func TestBitmapClipBlend(t *testing.T) {
	b := &bitmap{w: 2, h: 2, px: [][]RGBA{{{255, 0, 0, 255}, {255, 0, 0, 255}}, {{0, 0, 255, 255}, {0, 0, 255, 255}}}}
	c := b.clip(1, 1, 0, 0, -1, -1, "BlendPixels")
	if c[0][0].R != 127 || c[0][0].B != 127 || c[0][0].A != 255 {
		t.Fatalf("blend: %+v", c[0][0])
	}
	s := b.clip(2, 1, 0, 0, -1, -1, "SinglePixelTopLeft")
	if s[0][0].R != 255 || s[1][0].B != 255 {
		t.Fatalf("single: %+v", s)
	}
	_ = time.Now
}
