package linx

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Bytes captured from the real DOFLinxTrigger.dll (V3) while calling its
// exports in a test harness (see docs/protocol.md).
func TestDecodeRealDLLCapture(t *testing.T) {
	cases := []struct {
		raw  []byte
		want string
	}{
		{[]byte{0x05, 0x3B, 0x22, 0x38, 0x4B, 0x43, 0x46, 0x3B, 0x47, 0x25, 0x26, 0x21, 0x26, 0x0D, 0x0A}, "E,BUMPER01,1"},
		{[]byte{0x05, 0x3B, 0x22, 0x38, 0x4B, 0x43, 0x46, 0x3B, 0x47, 0x25, 0x26, 0x21, 0x27, 0x2A, 0x2A, 0x0D, 0x0A}, "E,BUMPER01,255"},
		{[]byte{0x05, 0x49, 0x22, 0x3F, 0x64, 0x42, 0x57, 0x64, 0x5A, 0x0D, 0x0A}, "S,InLane"},
		{[]byte{0x05, 0x44, 0x22, 0x3C, 0x65, 0x68, 0x6D, 0x57, 0x67, 0x59, 0x0D, 0x0A}, "N,Forward"},
		{[]byte{0x05, 0x3D, 0x22, 0x29, 0x0D, 0x0A}, "G,3"},
	}
	for _, c := range cases {
		var carry []byte
		recs := SplitTriggerRecords(c.raw, &carry)
		if len(recs) != 1 || recs[0] != c.want {
			t.Errorf("got %q want %q", recs, c.want)
		}
		// round trip
		var carry2 []byte
		if r := SplitTriggerRecords(EncodeTrigger(c.want), &carry2); len(r) != 1 || r[0] != c.want {
			t.Errorf("round trip %q -> %q", c.want, r)
		}
	}
	// split across reads and multiple records in one read
	var carry []byte
	a := EncodeTrigger("E,Flipper_Left,1")
	b := EncodeTrigger("E,Flipper_Left,0")
	all := append(append([]byte{}, a...), b...)
	r1 := SplitTriggerRecords(all[:10], &carry)
	r2 := SplitTriggerRecords(all[10:], &carry)
	got := append(r1, r2...)
	if len(got) != 2 || got[0] != "E,Flipper_Left,1" || got[1] != "E,Flipper_Left,0" {
		t.Fatalf("stream split: %q", got)
	}
}

func TestParseTrigger(t *testing.T) {
	m, ok := ParseTrigger("E,Flipper_Left,1")
	if !ok || m.Kind != 'E' || m.Arg != "Flipper_Left" || m.Value != 1 {
		t.Fatalf("%+v", m)
	}
	m, ok = ParseTrigger("S,Menu")
	if !ok || m.Kind != 'S' || m.Arg != "Menu" {
		t.Fatalf("%+v", m)
	}
}

func TestParseLinks(t *testing.T) {
	c := NewConfig()
	c.addLine("LINK_LF=2017,50,5000,255")
	c.addLine("LINK_SR=3001,ON,0,255")
	c.addLine("LINK_BK=3017,FLASH,250,128")
	c.addLine("LINK_FLCN=3024,3002")
	c.addLine("LINK_BUT_ST=1003,Cyan,31")
	c.addLine("LINK_BUT_LB=1010,MONO")
	c.addLine("LINK_MC=1030,75,1000")
	c.addLine("RGB_OUTPUT=2009,2012")
	c.addLine("LED_COLOUR_ORDER=BUT_B3,BGR")
	l := LoadLinks(c)
	if x := l.Dev["LF"]; len(x) != 1 || x[0].Addr != 2017 || x[0].DefaultMs != 50 || x[0].MaxMs != 5000 || x[0].Intensity != 255 {
		t.Fatalf("LF %+v", x)
	}
	if x := l.Dev["SR"]; len(x) != 1 || x[0].Mode != "ON" {
		t.Fatalf("SR %+v", x)
	}
	if x := l.Dev["BK"]; len(x) != 1 || x[0].Mode != "FLASH" || x[0].FlashMs != 250 || x[0].Intensity != 128 {
		t.Fatalf("BK %+v", x)
	}
	if x := l.Flasher["FLCN"]; len(x) != 2 || x[1].Addr != 3002 {
		t.Fatalf("FLCN %+v", x)
	}
	if x := l.Button["BUT_ST"]; len(x) != 1 || x[0].Colour != "Cyan" || x[0].Key != 31 {
		t.Fatalf("BUT_ST %+v", x)
	}
	if x := l.Button["BUT_LB"]; len(x) != 1 || !x[0].Mono {
		t.Fatalf("BUT_LB %+v", x)
	}
	if x := l.Dev["MC"]; len(x) != 1 || x[0].DefaultMs != 75 || x[0].MaxMs != 1000 {
		t.Fatalf("MC %+v", x)
	}
	if len(l.RGB) != 2 || l.Order["BUT_B3"] != "BGR" {
		t.Fatalf("rgb %+v %+v", l.RGB, l.Order)
	}
}

func TestParseRealFXFiles(t *testing.T) {
	dir := `C:\Users\phili\Downloads\DOFLinx_V10.03`
	if _, err := os.Stat(dir); err != nil {
		t.Skip("DOFLinx distribution not available")
	}
	f, err := ParseFXFile(filepath.Join(dir, "FX", "BackToTheFuture.FX"))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Params["FF_ROM"]; len(got) != 1 || got[0] != "fx3_bttf" {
		t.Fatalf("FF_ROM %v", got)
	}
	cs := NewCommandSet()
	pre, _ := ParseFXFile(filepath.Join(dir, "FX", "All_Pre.FX"))
	cs.Merge(pre)
	cs.Merge(f)
	acts := cs.Lookup("bumper01", "ON")
	if len(acts) == 0 {
		t.Fatal("BUMPER01 ON missing")
	}
	hasDOF := false
	for _, a := range acts {
		if a.Name == "FF_DOF" {
			hasDOF = true
		}
	}
	if !hasDOF {
		t.Fatalf("expected FF_DOF in %+v", acts)
	}
	// alias
	if len(cs.Lookup("BUMPER_01", "ON")) == 0 {
		t.Fatal("alias BUMPER_01 not resolved")
	}
	// FX2 memory triggers
	f2, err := ParseFXFile(filepath.Join(`C:\Users\phili\Downloads\DOFLinx-10.03\DOFLinx-10.03`, "FX2", "AlienIsolationPinball.FX2"))
	if err == nil {
		core := 0
		for _, tr := range f2.Triggers {
			if tr.Core {
				core++
			}
		}
		if core != 8 || len(f2.Triggers) < 12 {
			t.Fatalf("FX2 triggers core=%d total=%d", core, len(f2.Triggers))
		}
	}
	// FX3 lines are opaque
	f3, err := ParseFXFile(filepath.Join(`C:\Users\phili\Downloads\DOFLinx-10.03\DOFLinx-10.03`, "FX3", "BackToTheFuture.FX3"))
	if err == nil && f3.EncryptedLines == 0 {
		t.Fatal("expected opaque lines in FX3 file")
	}
	// every FX file must parse
	n := 0
	filepath.Walk(filepath.Join(dir, "FX"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(strings.ToLower(p), ".fx") {
			if _, e := ParseFXFile(p); e != nil {
				t.Errorf("%s: %v", p, e)
			}
			n++
		}
		return nil
	})
	if n < 100 {
		t.Fatalf("only %d FX files parsed", n)
	}
}

type fakeSink struct {
	mu   sync.Mutex
	vals map[[2]int]map[any]byte
}

func (f *fakeSink) SetOutput(d, p int, key any, v byte) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.vals == nil {
		f.vals = map[[2]int]map[any]byte{}
	}
	k := [2]int{d, p}
	if f.vals[k] == nil {
		f.vals[k] = map[any]byte{}
	}
	f.vals[k][key] = v
	return true
}

func (f *fakeSink) get(d, p int) byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	var m byte
	for _, v := range f.vals[[2]int{d, p}] {
		if v > m {
			m = v
		}
	}
	return m
}

type fakeDOF struct {
	mu     sync.Mutex
	roms   []string
	pulses []string
}

func (f *fakeDOF) LoadRom(rom string, done func(bool)) {
	f.mu.Lock()
	f.roms = append(f.roms, rom)
	f.mu.Unlock()
	if done != nil {
		done(true)
	}
}
func (f *fakeDOF) Pulse(d string, ms int) {
	f.mu.Lock()
	f.pulses = append(f.pulses, d)
	f.mu.Unlock()
}
func (f *fakeDOF) Clear()                {}
func (f *fakeDOF) SendNamed(string, int) {}

func waitUntil(t *testing.T, what string, c func() bool) {
	t.Helper()
	end := time.Now().Add(3 * time.Second)
	for time.Now().Before(end) {
		if c() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout: %s", what)
}

func TestRunnerCommands(t *testing.T) {
	cfg := NewConfig()
	cfg.addLine("LINK_LF=1017,50,5000,200")
	cfg.addLine("LINK_FLCN=1030")
	cfg.addLine("LINK_BUT_LB=1040,Cyan,0")
	cfg.addLine("RGB_OUTPUT=1050")
	fs, fd := &fakeSink{}, &fakeDOF{}
	r := NewRunner(cfg, fd, fs, nil, nopLog{})

	r.Execute(Action{Name: "FF_DEV", Args: "DV_LF,10000"})
	waitUntil(t, "LF on", func() bool { return fs.get(1, 17) == 200 })
	r.Execute(Action{Name: "FF_DEV", Args: "DV_LF,0"})
	waitUntil(t, "LF off", func() bool { return fs.get(1, 17) == 0 })

	r.Execute(Action{Name: "FF_FLASHER", Args: "DV_FLCN,FL_ON,1,1,100,Red"})
	waitUntil(t, "flasher red", func() bool { return fs.get(1, 30) == 255 && fs.get(1, 31) == 0 && fs.get(1, 32) == 0 })
	r.Execute(Action{Name: "FF_FLASHER", Args: "DV_FLCN,FL_OFF,1,1,100,Red"})
	waitUntil(t, "flasher off", func() bool { return fs.get(1, 30) == 0 })

	r.Execute(Action{Name: "FF_FLASHER", Args: "DV_FLCN,FL_TT,1,60,50,White"})
	waitUntil(t, "flasher 50%", func() bool { return fs.get(1, 30) == 127 })
	waitUntil(t, "flasher timed off", func() bool { return fs.get(1, 30) == 0 })

	r.Execute(Action{Name: "FF_BUTTON", Args: "BUT_LB,BA_ON,0,0"})
	waitUntil(t, "button cyan", func() bool { return fs.get(1, 40) == 0 && fs.get(1, 41) == 255 && fs.get(1, 42) == 255 })
	r.Execute(Action{Name: "FF_BUTTON", Args: "BUT_LB,BA_OFF,0,0"})
	waitUntil(t, "button off", func() bool { return fs.get(1, 41) == 0 })

	r.Execute(Action{Name: "FF_COLOUR", Args: "Blue,RGB_DF,0"})
	waitUntil(t, "rgb blue", func() bool { return fs.get(1, 52) == 255 })
	r.Execute(Action{Name: "FF_COLOUR", Args: "Red,RGB_TT,80"})
	waitUntil(t, "rgb red", func() bool { return fs.get(1, 50) == 255 && fs.get(1, 52) == 0 })
	waitUntil(t, "rgb reverts", func() bool { return fs.get(1, 52) == 255 && fs.get(1, 50) == 0 })

	r.Execute(Action{Name: "FF_DOF", Args: "E101,-1,E102,250"})
	fd.mu.Lock()
	defer fd.mu.Unlock()
	if len(fd.pulses) != 2 || fd.pulses[0] != "E101" || fd.pulses[1] != "E102" {
		t.Fatalf("pulses %v", fd.pulses)
	}
}

type nopLog struct{}

func (nopLog) Infof(string, ...any) {}
func (nopLog) Warnf(string, ...any) {}

func TestManagerFlow(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("All_Pre.FX", "[COMMANDS]\nFlipper_Left|ON|FF_Dev DV_LF,10000\nFlipper_Left|OFF|FF_Dev DV_LF,0\nBUMPER01|ON|FF_DOF E120,-1\nBUMPER_01|ALIAS|BUMPER01\n")
	write("MyTable.FX", "[STARTUP]\nFF_ROM=fx3_test\nFF_DOF=E369,-1\n[SHUTDOWN]\nFF_DOF=E369,0\n[COMMANDS]\nINLANE_MODE|ON|FF_DOF E310,-1\nINLANE_MODE|OFF|FF_DOF E310,0\n")
	cfg := NewConfig()
	cfg.Dir = dir
	cfg.Set("PATH_FX", dir)
	cfg.addLine("LINK_LF=1017,50,5000,255")
	cfg.Set("FORCE_TABLE", "")
	fs, fd := &fakeSink{}, &fakeDOF{}
	r := NewRunner(cfg, fd, fs, nil, nopLog{})
	m := NewManager(cfg, r, nopLog{})
	m.tableIDs["77"] = "MyTable"

	m.HandleTrigger("E,Flipper_Left,1") // auto attaches FX profile
	waitUntil(t, "flipper", func() bool { return fs.get(1, 17) == 255 })
	m.HandleTrigger("G,77") // selects MyTable.FX
	waitUntil(t, "rom", func() bool { fd.mu.Lock(); defer fd.mu.Unlock(); return len(fd.roms) == 1 && fd.roms[0] == "fx3_test" })
	m.HandleTrigger("E,BUMPER_01,1")
	m.HandleTrigger("S,InLane")
	waitUntil(t, "pulses", func() bool {
		fd.mu.Lock()
		defer fd.mu.Unlock()
		return strings.Join(fd.pulses, ",") == "E369,E120,E310"
	})
	m.HandleTrigger("G,999") // unknown id must not crash
	m.endGame("test")
	fd.mu.Lock()
	last := fd.pulses[len(fd.pulses)-1]
	fd.mu.Unlock()
	if last != "E369" {
		t.Fatalf("shutdown FF_DOF not executed, last pulse %s", last)
	}
}

type fakeMem map[uint32]uint32

func (f fakeMem) ReadU32(a uint32) (uint32, error) { return f[a], nil }

func TestReadTriggerChain(t *testing.T) {
	mem := fakeMem{0x882FC0: 0x1000, 0x1000 + 0x2E4: 0x2000, 0x2000 + 0x04: 0x3000, 0x3000 + 0x20: 0x4000, 0x4000 + 0x08: 0xAB}
	tr := MemTrigger{Address: 0x882FC0, Mask: 0xFF, Offsets: []uint32{0x2E4, 0x04, 0x20, 0x08}}
	v, ok := readTrigger(mem, tr)
	if !ok || v != 0xAB {
		t.Fatalf("v=%x ok=%v", v, ok)
	}
	core := MemTrigger{Address: 0x88781C, Mask: 0xFF00, Core: true}
	mem[0x88781C] = 0x1200
	if v, _ := readTrigger(mem, core); v&core.Mask == 0 {
		t.Fatal("core mask")
	}
}

func TestSeenIDsAssignAndFileReload(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "All_Pre.FX"), []byte("[COMMANDS]\nBUMPER01|ON|FF_DOF E120,-1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "SorcerersLair.FX"), []byte("[STARTUP]\nFF_ROM=x\n"), 0o644)
	cfg := NewConfig()
	cfg.Dir = dir
	cfg.Set("PATH_FX", dir)
	d := &fakeDOF{}
	m := NewManager(cfg, NewRunner(cfg, d, &fakeSink{}, nil, nopLog{}), nopLog{})
	m.HandleTrigger("G,9001")
	m.HandleTrigger("G,9001")
	m.HandleTrigger("G,9002")
	ids := m.SeenIDs()
	if len(ids) != 2 || ids[0].ID != "9002" || ids[1].Count != 2 || ids[1].Table != "" {
		t.Fatalf("seen %+v", ids)
	}
	m.Learn("9001", "SorcerersLair")
	if k, tb := m.Current(); k != "FX" || tb != "SorcerersLair" {
		t.Fatalf("learn must select the table, got %q %q", k, tb)
	}
	if m.SeenIDs()[1].Table != "SorcerersLair" && m.SeenIDs()[0].Table != "SorcerersLair" {
		t.Fatal("mapping not reported")
	}
	// editing tables.ini while running is picked up on the next unknown id
	os.WriteFile(filepath.Join(dir, "tables.ini"), []byte("9002=SorcerersLair\n"), 0o644)
	m.HandleTrigger("G,9002")
	if m.tableIDs["9002"] != "SorcerersLair" {
		t.Fatal("tables.ini edit was not reloaded")
	}
}
