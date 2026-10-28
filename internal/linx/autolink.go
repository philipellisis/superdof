package linx

import (
	"fmt"
	"strconv"
	"strings"
)

// Board is one controller whose outputs are described by the toy names the
// DOF Config Tool writes as a comment under [Config DOF] in its
// directoutputconfigNN.ini. Device is the DOFLinx device digit (the first digit
// of a DOOO address), Names is indexed by port (1 based, index 0 unused).
type Board struct {
	Device int
	Label  string // for logs, e.g. "directoutputconfig11.ini"
	Names  []string
}

// Derived describes one LINK that was built from a toy name.
type Derived struct {
	Key   string // INI key that was added, e.g. LINK_LF, RGB_OUTPUT
	Value string
	Toy   string // toy name from the ini comment
	Addr  int    // DOOO address of its first output
	Board string
}

// toyKind says how a toy is linked.
type toyKind int

const (
	kindNone toyKind = iota
	kindDevice
	kindFlasher
	kindButton
	kindRGB
)

type toyInfo struct {
	kind   toyKind
	code   string // LF, FLOL, BUT_ST ...
	rgb    bool   // occupies three consecutive ports
	params string // text after the address in the LINK_ value
}

func has(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

// side returns L, C, R for "left", "center/centre/middle", "right".
func side(s string) string {
	switch {
	case has(s, "left"):
		return "L"
	case has(s, "center", "centre"):
		return "C"
	case has(s, "right"):
		return "R"
	}
	return ""
}

// ClassifyToy maps a DOF toy name to the DOFLinx device it represents. The
// second result is false when the name is not one DOFLinx knows.
func classifyToy(name string) (toyInfo, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return toyInfo{}, false
	}
	rgb := strings.HasPrefix(n, "rgb ")
	switch {
	case has(n, "flasher"):
		code := ""
		switch {
		case has(n, "outside left", "outer left"):
			code = "FLOL"
		case has(n, "outside right", "outer right"):
			code = "FLOR"
		case has(n, "inner left", "inside left"), side(n) == "L":
			code = "FLIL"
		case has(n, "inner right", "inside right"), side(n) == "R":
			code = "FLIR"
		case side(n) == "C":
			code = "FLCN"
		}
		if code == "" {
			return toyInfo{}, false
		}
		return toyInfo{kind: kindFlasher, code: code, rgb: true}, true
	case has(n, "magnasave"):
		s := side(n)
		if s != "L" && s != "R" {
			return toyInfo{}, false
		}
		return toyInfo{kind: kindButton, code: "BUT_M" + s, rgb: true, params: "White"}, true
	case has(n, "bumper"):
		s := side(n)
		if s == "" {
			return toyInfo{}, false
		}
		row := "M"
		if has(n, "back") {
			row = "B"
		}
		return toyInfo{kind: kindDevice, code: row + s, params: "50,500,255"}, true
	case has(n, "slingshot"):
		s := side(n)
		if s != "L" && s != "R" {
			return toyInfo{}, false
		}
		code := "LS"
		if s == "R" {
			code = "RS"
		}
		return toyInfo{kind: kindDevice, code: code, params: "50,500,255"}, true
	case n == "rgb flippers" || (rgb && has(n, "flipper")):
		return toyInfo{kind: kindRGB, rgb: true}, true
	case has(n, "flipper"):
		switch side(n) {
		case "L":
			return toyInfo{kind: kindDevice, code: "LF", params: "50,10000,255"}, true
		case "R":
			return toyInfo{kind: kindDevice, code: "RF", params: "50,10000,255"}, true
		}
	case has(n, "strobe"):
		return toyInfo{kind: kindDevice, code: "SR", params: "ON,0,255"}, true
	case has(n, "shaker"):
		return toyInfo{kind: kindDevice, code: "SH", params: "1000,5000,255"}, true
	case has(n, "gear"):
		return toyInfo{kind: kindDevice, code: "GR", params: "750,10000,255"}, true
	case has(n, "knocker"):
		return toyInfo{kind: kindDevice, code: "KN", params: "120,500,255"}, true
	case has(n, "fan"):
		return toyInfo{kind: kindDevice, code: "FN", params: "1000,10000,255"}, true
	case has(n, "beacon"):
		return toyInfo{kind: kindDevice, code: "BK", params: "FLASH,200,255"}, true
	case has(n, "extra ball"):
		return button("BUT_EB", rgb), true
	case has(n, "coin"):
		return button("BUT_CN", rgb), true
	case has(n, "launch"):
		return button("BUT_LB", rgb), true
	case has(n, "start"):
		return button("BUT_ST", rgb), true
	case has(n, "exit"):
		return button("BUT_EX", rgb), true
	case has(n, "fire"):
		return button("BUT_FR", rgb), true
	}
	return toyInfo{}, false
}

func button(code string, rgb bool) toyInfo {
	if rgb {
		return toyInfo{kind: kindButton, code: code, rgb: true, params: "White"}
	}
	return toyInfo{kind: kindButton, code: code, params: "MONO"}
}

// configured reports whether the config already defines a link for code (an
// explicit DOFLinx.INI / superdof.json entry always wins over a derived one).
func configured(c *Config, code string) bool {
	if strings.HasPrefix(code, "BUT_") {
		return c.Has("LINK_"+code) || c.Has("LINK_"+strings.TrimPrefix(code, "BUT_")) && legacyButtons[strings.TrimPrefix(code, "BUT_")]
	}
	return c.Has("LINK_" + code)
}

// DeriveLinks adds the LINK_xx, flasher, button and RGB_OUTPUT entries that
// the toy names of the boards imply, for every code the config does not
// define itself. Repeated toys (the same code on several outputs or boards)
// become repeated LINK_ lines, which DOFLinx treats as extra outputs. Names it
// does not know are reported in the returned notes.
func DeriveLinks(c *Config, boards []Board) (added []Derived, notes []string) {
	skip := map[string]bool{} // code -> defined explicitly, decided on first sight
	for _, b := range boards {
		for port := 1; port < len(b.Names); port++ {
			name := b.Names[port]
			if strings.TrimSpace(name) == "" {
				continue
			}
			t, ok := classifyToy(name)
			if !ok {
				notes = append(notes, fmt.Sprintf("%s: output %d %q is not a DOFLinx device; ignored", b.Label, port, name))
				continue
			}
			addr := b.Device*1000 + port
			key := "LINK_" + t.code
			if t.kind == kindRGB {
				key = "RGB_OUTPUT"
			}
			if _, done := skip[key]; !done {
				if key == "RGB_OUTPUT" {
					skip[key] = c.Has(key)
				} else {
					skip[key] = configured(c, t.code)
				}
				if skip[key] {
					notes = append(notes, fmt.Sprintf("%s is set in the DOFLinx config; the toys named for it in the ini files are not used", key))
				}
			}
			if skip[key] {
				if key != "RGB_OUTPUT" && !explicitAddr(c, t.code, addr) {
					notes = append(notes, fmt.Sprintf("%s: output %d is %q (DOFLinx device %s) but %s in the DOFLinx config says %s; the config wins", b.Label, port, name, t.code, key, strings.Join(explicitValues(c, t.code), " | ")))
				}
				continue
			}
			val := strconv.Itoa(addr)
			if t.params != "" && key != "RGB_OUTPUT" {
				val += "," + t.params
			}
			if key == "RGB_OUTPUT" {
				// RGB_OUTPUT is one comma separated list
				if prev := c.All(key); len(prev) > 0 {
					c.Set(key, prev[0]+","+val)
				} else {
					c.Add(key, val)
				}
			} else {
				c.Add(key, val)
			}
			added = append(added, Derived{Key: key, Value: val, Toy: name, Addr: addr, Board: b.Label})
		}
	}
	return added, notes
}

func linkKeys(code string) []string {
	keys := []string{"LINK_" + code}
	if strings.HasPrefix(code, "BUT_") {
		keys = append(keys, "LINK_"+strings.TrimPrefix(code, "BUT_"))
	}
	return keys
}

func explicitValues(c *Config, code string) []string {
	var out []string
	for _, k := range linkKeys(code) {
		out = append(out, c.All(k)...)
	}
	return out
}

// explicitAddr reports whether the config links code to addr itself.
func explicitAddr(c *Config, code string, addr int) bool {
	for _, v := range explicitValues(c, code) {
		for _, p := range strings.Split(v, ",") {
			if isNum(p) && atoi(p) == addr {
				return true
			}
		}
	}
	return false
}
