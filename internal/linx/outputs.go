package linx

import (
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sink receives raw output values. device/port come from the DOFLinx
// "DOOO" addressing (D = device 1-9, OOO = output 1-999).
type Sink interface {
	// SetOutput sets the value of an output on behalf of key. 0 releases it.
	SetOutput(device, port int, key any, value byte) bool
}

// Addr splits a DOFLinx DOOO address.
func Addr(a int) (device, port int) { return a / 1000, a % 1000 }

// Link is one LINK_xx destination.
type Link struct {
	Addr      int
	DefaultMs int
	MaxMs     int
	Intensity int
	Mode      string // "" (timed), "ON", "FLASH"
	FlashMs   int
	Colour    string
	Mono      bool
	Key       int // keyboard code (buttons)
}

// Links holds all LINK_* definitions of the cabinet.
type Links struct {
	Dev     map[string][]Link // LF, RF, LS, ...
	Flasher map[string][]Link // FLOL, FLIL, FLCN, FLOR, FLIR
	Button  map[string][]Link // BUT_ST ...
	RGB     []Link            // RGB_OUTPUT
	Order   map[string]string // LED_COLOUR_ORDER per BUT_xx / DV_xx
	Fixed   []int             // FIXED_OUTPUTS / BUTTONS_ON
	FixedC  map[int]string    // FIXED_OUTPUTS_COLOUR
}

var flasherCodes = map[string]bool{"FLOL": true, "FLIL": true, "FLCN": true, "FLOR": true, "FLIR": true}
var legacyButtons = map[string]bool{"ST": true, "EB": true, "EX": true, "CN": true, "LB": true, "FR": true}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func isNum(s string) bool {
	_, err := strconv.Atoi(strings.TrimSpace(s))
	return err == nil
}

// parseLink parses the value of a LINK_xx key into links.
func parseLinkValue(v string) []Link {
	f := strings.Split(v, ",")
	for i := range f {
		f[i] = strings.TrimSpace(f[i])
	}
	var out []Link
	for i := 0; i < len(f); {
		if !isNum(f[i]) {
			i++
			continue
		}
		l := Link{Addr: atoi(f[i]), Intensity: 255}
		switch {
		case i+1 < len(f) && (strings.EqualFold(f[i+1], "ON") || strings.EqualFold(f[i+1], "FLASH")):
			l.Mode = strings.ToUpper(f[i+1])
			if i+2 < len(f) {
				l.FlashMs = atoi(f[i+2])
			}
			if i+3 < len(f) {
				l.Intensity = atoi(f[i+3])
			}
			i += 4
		case i+3 < len(f) && isNum(f[i+1]) && isNum(f[i+2]) && isNum(f[i+3]):
			l.DefaultMs, l.MaxMs, l.Intensity = atoi(f[i+1]), atoi(f[i+2]), atoi(f[i+3])
			i += 4
		case len(f)-i == 3 && isNum(f[i+1]) && isNum(f[i+2]):
			l.DefaultMs, l.MaxMs = atoi(f[i+1]), atoi(f[i+2])
			i += 3
		case i+1 < len(f) && !isNum(f[i+1]):
			if strings.EqualFold(f[i+1], "MONO") {
				l.Mono = true
			} else {
				l.Colour = f[i+1]
			}
			if i+2 < len(f) && isNum(f[i+2]) {
				l.Key = atoi(f[i+2])
				i += 3
			} else {
				i += 2
			}
		default:
			i++
		}
		if l.Intensity <= 0 || l.Intensity > 255 {
			if l.Intensity > 255 {
				l.Intensity = 255
			}
		}
		out = append(out, l)
	}
	return out
}

// LoadLinks reads LINK_*, RGB_OUTPUT and related keys from the config.
func LoadLinks(c *Config) *Links {
	L := &Links{Dev: map[string][]Link{}, Flasher: map[string][]Link{}, Button: map[string][]Link{},
		Order: map[string]string{}, FixedC: map[int]string{}}
	for _, k := range c.Keys("LINK_") {
		code := strings.TrimPrefix(k, "LINK_")
		links := parseLinkValue(c.Get(k))
		if len(links) == 0 {
			continue
		}
		switch {
		case strings.HasPrefix(code, "BUT_"):
			L.Button[code] = links
		case legacyButtons[code]:
			L.Button["BUT_"+code] = links
		case flasherCodes[code]:
			// flashers are listed as bare ports: LINK_FLCN=3024,3002
			var ports []Link
			for _, p := range strings.Split(c.Get(k), ",") {
				if isNum(p) {
					ports = append(ports, Link{Addr: atoi(p), Intensity: 255})
				}
			}
			L.Flasher[code] = ports
		default:
			L.Dev[code] = links
		}
	}
	for _, p := range c.List("RGB_OUTPUT") {
		if isNum(p) {
			L.RGB = append(L.RGB, Link{Addr: atoi(p), Intensity: 255})
		}
	}
	for _, k := range []string{"FIXED_OUTPUTS", "BUTTONS_ON"} {
		for _, p := range c.List(k) {
			if isNum(p) {
				L.Fixed = append(L.Fixed, atoi(p))
			}
		}
	}
	for _, k := range []string{"FIXED_OUTPUTS_COLOUR", "BUTTONS_ON_COLOUR"} {
		l := c.List(k)
		for i := 0; i+1 < len(l); i += 2 {
			if isNum(l[i]) {
				L.FixedC[atoi(l[i])] = l[i+1]
			}
		}
	}
	for _, v := range c.All("LED_COLOUR_ORDER") {
		p := strings.Split(v, ",")
		for i := 0; i+1 < len(p); i += 2 {
			L.Order[strings.ToUpper(strings.TrimSpace(p[i]))] = strings.ToUpper(strings.TrimSpace(p[i+1]))
		}
	}
	return L
}

// animator runs cancellable timed output animations.
type animator struct {
	mu      sync.Mutex
	cancels map[string]chan struct{}
}

func newAnimator() *animator { return &animator{cancels: map[string]chan struct{}{}} }

// run cancels any running animation with the same key and starts fn.
func (a *animator) run(key string, fn func(stop <-chan struct{})) {
	a.mu.Lock()
	if c, ok := a.cancels[key]; ok {
		close(c)
	}
	stop := make(chan struct{})
	a.cancels[key] = stop
	a.mu.Unlock()
	go fn(stop)
}

func (a *animator) cancel(key string) {
	a.mu.Lock()
	if c, ok := a.cancels[key]; ok {
		close(c)
		delete(a.cancels, key)
	}
	a.mu.Unlock()
}

func (a *animator) cancelAll() {
	a.mu.Lock()
	for k, c := range a.cancels {
		close(c)
		delete(a.cancels, k)
	}
	a.mu.Unlock()
}

// sleep waits d or returns false when stopped.
func sleep(stop <-chan struct{}, d time.Duration) bool {
	if d <= 0 {
		select {
		case <-stop:
			return false
		default:
			return true
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-stop:
		return false
	case <-t.C:
		return true
	}
}

func scale(v uint8, pct int) uint8 {
	if pct >= 100 {
		return v
	}
	if pct <= 0 {
		return 0
	}
	return uint8(int(v) * pct / 100)
}

// colourOrder returns the channel permutation for a code (default RGB).
func colourOrder(order string) [3]int {
	m := map[byte]int{'R': 0, 'G': 1, 'B': 2}
	o := [3]int{0, 1, 2}
	if len(order) == 3 {
		for i := 0; i < 3; i++ {
			if c, ok := m[order[i]]; ok {
				o[i] = c
			}
		}
	}
	return o
}

var rng = rand.New(rand.NewSource(time.Now().UnixNano()))
var rngMu sync.Mutex

func randInt(n int) int {
	rngMu.Lock()
	defer rngMu.Unlock()
	return rng.Intn(n)
}
