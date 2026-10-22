// Package linx re-implements the DOFLinx behaviours that are needed to apply
// force-feedback (PinOne) for Pinball FX, Pinball FX2/FX3 and "LINX"
// emulators (Pinball M, Infected Mushroom Pinball), driven by the unmodified
// DOFLinx.INI / .FX / .FX2 / .Linx / Menu.ini files.
package linx

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is a parsed DOFLinx.INI (key=value, '#' comments). A key may be
// repeated; Get returns the last value, All returns every value.
type Config struct {
	Path string
	vals map[string][]string
	Dir  string
}

// ParseINI reads a DOFLinx style ini file.
func ParseINI(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	c := &Config{Path: path, vals: map[string][]string{}, Dir: filepath.Dir(path)}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		c.addLine(sc.Text())
	}
	return c, sc.Err()
}

// NewConfig returns an empty config (tests / defaults).
func NewConfig() *Config { return &Config{vals: map[string][]string{}} }

func (c *Config) addLine(line string) {
	line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
		return
	}
	eq := strings.IndexByte(line, '=')
	if eq <= 0 {
		return
	}
	k := strings.ToUpper(strings.TrimSpace(line[:eq]))
	v := strings.TrimSpace(line[eq+1:])
	c.vals[k] = append(c.vals[k], v)
}

// Add appends a value for key (same as a line KEY=value in the ini).
func (c *Config) Add(key, value string) {
	k := strings.ToUpper(strings.TrimSpace(key))
	c.vals[k] = append(c.vals[k], strings.TrimSpace(value))
}

// Set overrides a value (command line, tests).
func (c *Config) Set(key, value string) { c.vals[strings.ToUpper(key)] = []string{value} }

// Get returns the last value of key.
func (c *Config) Get(key string) string {
	v := c.vals[strings.ToUpper(key)]
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}

// Has reports whether key is defined.
func (c *Config) Has(key string) bool { return len(c.vals[strings.ToUpper(key)]) > 0 }

// All returns every value of key.
func (c *Config) All(key string) []string { return c.vals[strings.ToUpper(key)] }

// Int returns key as an int or def.
func (c *Config) Int(key string, def int) int {
	s := c.Get(key)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}

// Bool returns key as a boolean (1/YES/TRUE/ON).
func (c *Config) Bool(key string, def bool) bool {
	s := strings.ToUpper(c.Get(key))
	switch s {
	case "":
		return def
	case "1", "YES", "TRUE", "ON", "Y":
		return true
	}
	return false
}

// List returns a comma separated key as trimmed items.
func (c *Config) List(key string) []string {
	var out []string
	for _, v := range c.All(key) {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// Keys returns all defined keys with the given prefix.
func (c *Config) Keys(prefix string) []string {
	prefix = strings.ToUpper(prefix)
	var out []string
	for k := range c.vals {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out
}

// Resolve makes a path from the ini relative to the ini folder when needed.
func (c *Config) Resolve(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, `\`) || (len(p) > 1 && p[1] == ':') {
		return filepath.FromSlash(strings.ReplaceAll(p, `\`, "/"))
	}
	return filepath.Join(c.Dir, filepath.FromSlash(strings.ReplaceAll(p, `\`, "/")))
}

// RGB is a color with 8 bit channels.
type RGB struct{ R, G, B uint8 }

// Colours maps colour names (case insensitive) to RGB.
type Colours map[string]RGB

// LoadColours reads a Colours.ini ([Colors DOF] format: Name=#RRGGBB[AA]).
func LoadColours(path string) (Colours, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	col := Colours{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		eq := strings.IndexByte(l, '=')
		if eq <= 0 || strings.HasPrefix(l, "#") {
			continue
		}
		name, val := strings.TrimSpace(l[:eq]), strings.TrimSpace(l[eq+1:])
		if rgb, ok := parseColourValue(val); ok {
			col[strings.ToUpper(name)] = rgb
		}
	}
	return col, sc.Err()
}

func parseColourValue(v string) (RGB, bool) {
	if strings.HasPrefix(v, "#") && (len(v) == 7 || len(v) == 9) {
		n, err := strconv.ParseUint(v[1:7], 16, 32)
		if err != nil {
			return RGB{}, false
		}
		return RGB{uint8(n >> 16), uint8(n >> 8), uint8(n)}, true
	}
	parts := strings.Split(v, ",")
	if len(parts) == 3 {
		var c [3]uint8
		for i, p := range parts {
			n, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				return RGB{}, false
			}
			if n > 48 {
				n = 48
			}
			c[i] = uint8(float64(n) * 5.3125)
		}
		return RGB{c[0], c[1], c[2]}, true
	}
	return RGB{}, false
}

// DefaultColours are used when no Colours.ini is available.
func DefaultColours() Colours {
	return Colours{
		"BLACK": {0, 0, 0}, "WHITE": {255, 255, 255}, "RED": {255, 0, 0}, "LIME": {0, 255, 0},
		"BLUE": {0, 0, 255}, "YELLOW": {255, 255, 0}, "CYAN": {0, 255, 255}, "MAGENTA": {255, 0, 255},
		"GREEN": {0, 128, 0}, "ORANGE": {255, 165, 0}, "PURPLE": {128, 0, 128},
	}
}

// Lookup finds a colour by name; "#RRGGBB" is accepted too.
func (c Colours) Lookup(name string) (RGB, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return RGB{}, false
	}
	if v, ok := c[strings.ToUpper(name)]; ok {
		return v, true
	}
	return parseColourValue(name)
}

// Logger is the logging dependency of the package.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
}

type stdLogger struct{}

func (stdLogger) Infof(f string, a ...any) { fmt.Fprintf(os.Stderr, "INFO  "+f+"\n", a...) }
func (stdLogger) Warnf(f string, a ...any) { fmt.Fprintf(os.Stderr, "WARN  "+f+"\n", a...) }

// StdLogger logs to stderr.
func StdLogger() Logger { return stdLogger{} }
