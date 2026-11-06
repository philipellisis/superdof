package linx

import (
	"encoding/binary"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// fpFake is a process with one memory region holding a VBScript array.
type fpFake struct {
	mu   sync.Mutex
	base uint64
	mem  []byte
	arr  uint64 // address of variant (0,0)
}

func newFPFake() *fpFake {
	f := &fpFake{base: 0x400000, mem: make([]byte, 0x20000), arr: 0x400000 + 0x1230}
	// FF_Init
	for c := 0; c < 32; c++ {
		f.setSlot(c, 31249999999997*32+float64(c))
		f.setSlot(c+32, 31249999999998*32+float64(c))
		f.setSlot(c+64, 31249999999996*32+float64(c))
		f.setSlot(c+96, 31249999999995*32+float64(c))
	}
	return f
}

func (f *fpFake) off(i, j int) int { return int(f.arr-f.base) + (j*fpSlotCount+i)*fpVariantLen }

func (f *fpFake) setSlot(i int, v float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o := f.off(i, 0)
	binary.LittleEndian.PutUint16(f.mem[o:], 5)
	binary.LittleEndian.PutUint64(f.mem[o+8:], math.Float64bits(v))
}

func (f *fpFake) slot(i int) float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return math.Float64frombits(binary.LittleEndian.Uint64(f.mem[f.off(i, 0)+8:]))
}

func (f *fpFake) ReadAt(addr uint64, buf []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if addr < f.base || addr+uint64(len(buf)) > f.base+uint64(len(f.mem)) {
		return strings.NewReader("").UnreadByte()
	}
	copy(buf, f.mem[addr-f.base:])
	return nil
}

func (f *fpFake) WriteAt(addr uint64, buf []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copy(f.mem[addr-f.base:], buf)
	return nil
}

func (f *fpFake) Regions(fn func(base, size uint64) bool) { fn(f.base, uint64(len(f.mem))) }

// encString mirrors FF_EncColour of DOFLinx.vbs.
func (f *fpFake) encString(s string, base int) {
	s = strings.ToUpper(s + strings.Repeat(" ", 21))[:21]
	for slot := 0; slot < 3; slot++ {
		var v float64
		for p := 0; p < 7; p++ {
			v += float64(s[slot*7+p]) * math.Pow(100, float64(p))
		}
		f.setSlot(base+slot, v)
	}
}

func TestFPLinkAgainstVBSEncoding(t *testing.T) {
	cfg := NewConfig()
	cfg.addLine("LINK_LF=1017,50,5000,200")
	cfg.addLine("LINK_FLCN=1030")
	cfg.addLine("RGB_OUTPUT=1050")
	fs, fd := &fakeSink{}, &fakeDOF{}
	r := NewRunner(cfg, fd, fs, nil, nopLog{})
	fp := newFPFake()

	if a, ok := fpFindSlots(fp); !ok || a != fp.arr+8 {
		t.Fatalf("fpFindSlots = %#x %v, want %#x", a, ok, fp.arr+8)
	}

	end := make(chan struct{})
	done := make(chan struct{})
	go func() { fpRun(fp, r, nopLog{}, end, 10*time.Millisecond, time.Millisecond); close(done) }()
	defer func() { close(end); <-done }()

	// handshake: slot 0 becomes the device mask (LF=bit 0, RGB=bit 13, FLCN=bit 24)
	waitUntil(t, "handshake", func() bool { return fp.slot(0) < fpActiveBelow })
	m := int64(fp.slot(0))
	for _, bit := range []int{0, 13, 24} {
		if m&(1<<uint(bit)) == 0 {
			t.Errorf("mask %#x lacks bit %d", m, bit)
		}
	}
	if m&(1<<1) != 0 {
		t.Errorf("mask %#x has RF although it is not linked", m)
	}
	time.Sleep(20 * time.Millisecond) // baseline taken

	// FF_Dev DV_LF,100 (one second)  -> (T*100)+Q+counter
	fp.setSlot(1, 100*100+0+1)
	waitUntil(t, "LF on", func() bool { return fs.get(1, 17) == 200 })
	// FF_Dev DV_LF,0
	fp.setSlot(1, 0*100+0+2)
	waitUntil(t, "LF off", func() bool { return fs.get(1, 17) == 0 })
	// FF_Dev DV_LF,-1: the default time of the device
	fp.setSlot(1, 0*100+10+3)
	waitUntil(t, "LF default time", func() bool { return fs.get(1, 17) == 200 })

	// FF_Colour "Blue",RGB_DF,0 : slot 14 = OnTime*1000 + action*10 + counter, colour in 61..63
	fp.encString("Blue", fpColourBase)
	fp.setSlot(14, 0*1000+1*10+1)
	waitUntil(t, "rgb blue", func() bool { return fs.get(1, 52) == 255 })

	// FF_Flasher DV_FLCN,FL_ON,1,1,100,Red: (perc*1e12)+(it*1e7)+(time*100)+(act*10)+counter
	fp.encString("Red", 52)
	fp.setSlot(25, 100*1e12+1*1e7+1*100+1*10+1)
	waitUntil(t, "flasher red", func() bool { return fs.get(1, 30) == 255 && fs.get(1, 31) == 0 })

	// FF_ROM "tom"
	fp.encString("tom", fpROMColour)
	fp.setSlot(64, 12345678910+1)
	waitUntil(t, "rom", func() bool { fd.mu.Lock(); defer fd.mu.Unlock(); return len(fd.roms) == 1 && fd.roms[0] == "TOM" })

	// FF_DOF "E",101,200 -> on=1, type=1, ref=101, time+1
	fp.setSlot(65, 1e12+1*1e11+101*1e7+201*10+1)
	waitUntil(t, "E101", func() bool { fd.mu.Lock(); defer fd.mu.Unlock(); return len(fd.pulses) == 1 && fd.pulses[0] == "E101" })
}

func TestFPDecodeString(t *testing.T) {
	f := newFPFake()
	f.encString("Orange_red", 46)
	got := fpDecodeString([]float64{f.slot(46), f.slot(47), f.slot(48)})
	if got != "ORANGE_RED" {
		t.Errorf("got %q", got)
	}
}
