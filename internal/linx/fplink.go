package linx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"
)

// Future Pinball link.
//
// Tables that include DOFLinx.vbs keep an array DDH_Slots(127,1) of VBScript
// variants in the memory of Future Pinball. FF_Init fills slot c of column 0
// with marker values (999999999999904 + c ...), every FF_* call writes an
// encoded number into the slot of its device, and the last digit of that
// number is a counter so repeated calls are noticed. The link finds the array
// by its markers, tells the table which devices exist (FF_Active is true as
// soon as slot 0 drops below 999999999999900; the other bits of slot 0 are the
// FF_IsDevice flags), and then polls the slots.

const (
	fpSlotCount   = 128
	fpVariantLen  = 16
	fpMarker0     = 999999999999904.0 // slot 0 right after FF_Init
	fpActiveBelow = 999999999999900.0
)

// fpMem is the part of a process that the link needs.
type fpMem interface {
	ReadAt(addr uint64, buf []byte) error
	WriteAt(addr uint64, buf []byte) error
	Regions(fn func(base, size uint64) bool)
}

var fpDeviceCodes = map[int]string{
	1: "LF", 2: "RF", 3: "LS", 4: "RS", 5: "ML", 6: "MC", 7: "MR", 8: "BL", 9: "BC", 10: "BR",
	11: "SH", 12: "GR", 13: "KN", 20: "FN", 21: "SR", 22: "BK", 29: "BE", 30: "C1", 31: "C2", 32: "C3",
}

// flasher device -> first of the three colour slots
var fpFlashers = map[int]struct {
	code string
	base int
}{23: {"FLOL", 46}, 24: {"FLIL", 49}, 25: {"FLCN", 52}, 26: {"FLOR", 55}, 27: {"FLIR", 58}}

var fpButtons = map[int]string{15: "BUT_ST", 16: "BUT_EB", 17: "BUT_EX", 18: "BUT_CN", 19: "BUT_LB", 28: "BUT_FR"}

const (
	fpSlotRGB     = 14 // FF_Colour, colour in slots 61-63
	fpColourBase  = 61
	fpSlotPUP0    = 37 // FF_PUPlayer, 37-39 (ignored)
	fpROMColour   = 43 // FF_ROM name in slots 43-45
	fpSlotROM     = 64
	fpSlotDOF0    = 65 // FF_DOF rotates over 65-67
	fpSlotDOFLast = 67
)

func variantDouble(b []byte) (float64, bool) {
	if binary.LittleEndian.Uint16(b[0:2]) != 5 { // VT_R8
		return 0, false
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(b[8:16])), true
}

func putVariantDouble(v float64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, math.Float64bits(v))
	return b
}

// fpFindSlots looks for DDH_Slots in the memory of the game and returns the
// address of element (0,0).
func fpFindSlots(pm fpMem) (uint64, bool) {
	pat := putVariantDouble(fpMarker0)
	var found uint64
	const chunk = 1 << 20
	check := func(addr uint64) bool {
		// elements 0, 1, 2 and 32 must be intact markers (or already linked)
		buf := make([]byte, 33*fpVariantLen)
		if pm.ReadAt(addr-8, buf[:fpVariantLen]) != nil || binary.LittleEndian.Uint16(buf[0:2]) != 5 {
			return false
		}
		if pm.ReadAt(addr-8, buf) != nil {
			return false
		}
		for _, i := range []int{1, 2, 32} {
			v, ok := variantDouble(buf[i*fpVariantLen:])
			if !ok || v != fpMarker0+float64(i) {
				return false
			}
		}
		return true
	}
	pm.Regions(func(base, size uint64) bool {
		for off := uint64(0); off < size; off += chunk - 64 {
			n := uint64(chunk)
			if off+n > size {
				n = size - off
			}
			buf := make([]byte, n)
			if pm.ReadAt(base+off, buf) != nil {
				continue
			}
			from := 0
			for {
				i := bytes.Index(buf[from:], pat)
				if i < 0 {
					break
				}
				i += from
				from = i + 1
				if i < 8 || i%8 != 0 {
					continue
				}
				if check(base + off + uint64(i)) {
					found = base + off + uint64(i)
					return false
				}
			}
			if off+n >= size {
				break
			}
		}
		return true
	})
	return found, found != 0
}

// fpReadSlots reads column 0 of the array.
func fpReadSlots(pm fpMem, base uint64) ([]float64, error) {
	buf := make([]byte, fpSlotCount*fpVariantLen)
	if err := pm.ReadAt(base-8, buf); err != nil {
		return nil, err
	}
	out := make([]float64, fpSlotCount)
	for i := range out {
		v, ok := variantDouble(buf[i*fpVariantLen:])
		if !ok {
			return nil, fmt.Errorf("slot %d is not a double", i)
		}
		out[i] = v
	}
	return out, nil
}

// fpDecodeString turns three slots into the 21 character string FF_EncColour
// stored: seven characters per slot, two decimal digits each, first character
// in the lowest digits.
func fpDecodeString(s []float64) string {
	var b strings.Builder
	for _, v := range s {
		n := int64(v)
		for i := 0; i < 7; i++ {
			c := byte(n % 100)
			n /= 100
			if c >= 32 && c < 127 {
				b.WriteByte(c)
			} else if c != 0 {
				b.WriteByte('?')
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// fpDispatcher turns changes in the slots into DOFLinx actions.
type fpDispatcher struct {
	r    *Runner
	last []float64
	log  Logger
}

func (d *fpDispatcher) colourOf(slots []float64, base int) string {
	return fpDecodeString(slots[base : base+3])
}

// handle processes the slots that changed since the previous call.
func (d *fpDispatcher) handle(slots []float64) {
	if d.last == nil {
		d.last = append([]float64(nil), slots...)
		return
	}
	for i := 1; i < fpSlotCount; i++ {
		if slots[i] == d.last[i] {
			continue
		}
		d.last[i] = slots[i]
		v := int64(slots[i])
		switch {
		case i == fpSlotROM:
			if name := d.colourOf(slots, fpROMColour); name != "" {
				d.r.Execute(Action{Name: "FF_ROM", Args: name})
			}
		case i >= fpSlotDOF0 && i <= fpSlotDOFLast:
			d.dof(v)
		case i == fpSlotRGB:
			act := [...]string{"", "RGB_DF", "RGB_TT", "RGB_CH", "RGB_FL"}
			a := int(v / 10 % 100)
			if a >= 1 && a < len(act) {
				d.r.colour(d.colourOf(slots, fpColourBase), act[a], int(v/1000))
			}
		case fpDeviceCodes[i] != "":
			ms := int(v/100) * 10
			if v/10%10 == 1 { // negative time: the default of the device
				ms = -1
			}
			d.r.dev(fpDeviceCodes[i], ms)
		case fpFlashers[i].code != "":
			f := fpFlashers[i]
			act := int(v / 10 % 10)
			if names := [...]string{"", "ON", "OFF", "TT", "FL", "FD"}; act >= 1 && act < len(names) {
				d.r.flasher(f.code, "FL_"+names[act], int(v/10000000%10000), int(v/100%100000), int(v/1000000000000), d.colourOf(slots, f.base))
			}
		case fpButtons[i] != "":
			act := int(v / 10 % 10)
			if names := [...]string{"", "ON", "OFF", "TT", "FL", "FD"}; act >= 1 && act < len(names) {
				d.r.button(fpButtons[i], "BA_"+names[act], int(v/10000000%10000), int(v/100%100000))
			}
		}
	}
}

// dof decodes FF_DOF(TheEvent,TheRef,TheTime).
func (d *fpDispatcher) dof(v int64) {
	on := v / 1000000000000
	typ := v / 100000000000 % 10
	ref := v / 10000000 % 10000
	t := v / 10 % 1000000
	letters := "?EWSLGMNDD"
	if typ < 1 || int(typ) >= len(letters) {
		return
	}
	ms := int64(0)
	if on != 0 {
		ms = t - 1 // FF_DOF stores TheTime+1; 0 means the default duration
	}
	d.r.Execute(Action{Name: "FF_DOF", Args: fmt.Sprintf("%c%d,%d", letters[typ], ref, ms)})
}

// deviceMask is the value for slot 0: bit n-1 is set for every device the
// cabinet can drive, so the table plays its normal sound for the others.
func (r *Runner) deviceMask() float64 {
	var mask int64
	set := func(n int) {
		if n >= 1 && n <= 31 {
			mask |= 1 << uint(n-1)
		}
	}
	for n, c := range fpDeviceCodes {
		if len(r.links.Dev[c]) > 0 {
			set(n)
		}
	}
	for n, f := range fpFlashers {
		if len(r.links.Flasher[f.code]) > 0 {
			set(n)
		}
	}
	for n, c := range fpButtons {
		if len(r.links.Button[c]) > 0 {
			set(n)
		}
	}
	if len(r.links.RGB) > 0 {
		set(fpSlotRGB)
	}
	return float64(mask)
}

// FPLink runs the Future Pinball link for the process until end is closed.
// scanWait is how long it searches quickly before it backs off.
func FPLink(pid uint32, r *Runner, log Logger, end <-chan struct{}) {
	var pm *ProcessMemory
	for pm == nil {
		var err error
		if pm, err = OpenProcessMemoryRW(pid); err != nil {
			log.Warnf("Future Pinball link: %v", err)
			if !fpSleep(end, 5*time.Second) {
				return
			}
		}
	}
	defer pm.Close()
	fpRun(pm, r, log, end, 1500*time.Millisecond, 5*time.Millisecond)
}

func fpSleep(end <-chan struct{}, d time.Duration) bool {
	select {
	case <-end:
		return false
	case <-time.After(d):
		return true
	}
}

// fpRun is the link loop; separated from FPLink so it can run on a fake process.
func fpRun(pm fpMem, r *Runner, log Logger, end <-chan struct{}, rescan, poll time.Duration) {
	mask := r.deviceMask()
	disp := &fpDispatcher{r: r, log: log}
	var base uint64
	linked := false
	warned := false
	for {
		if base == 0 {
			if a, ok := fpFindSlots(pm); ok {
				base = a
				disp.last = nil
				log.Infof("Future Pinball link: DOFLinx.vbs slots found at 0x%X", base)
			} else {
				if !warned {
					log.Infof("Future Pinball link: waiting for a table that uses DOFLinx.vbs (FF_Init)")
					warned = true
				}
				if !fpSleep(end, rescan) {
					return
				}
				continue
			}
		}
		slots, err := fpReadSlots(pm, base)
		if err != nil {
			log.Infof("Future Pinball link: lost the slots (%v); searching again", err)
			base, linked, warned = 0, false, false
			continue
		}
		if slots[0] >= fpActiveBelow { // FF_Init has just (re)filled the array
			if err := pm.WriteAt(base, putVariantDouble(mask)); err != nil {
				log.Warnf("Future Pinball link: %v", err)
				base = 0
				continue
			}
			if !linked {
				log.Infof("Future Pinball link established (device mask 0x%X)", int64(mask))
			}
			linked, disp.last = true, nil
			slots[0] = mask
		}
		disp.handle(slots)
		select {
		case <-end:
			return
		case <-time.After(poll):
		}
	}
}
