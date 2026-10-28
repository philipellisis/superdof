package dof

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// OutputControl is how a setting is driven.
type OutputControl int

const (
	ControlFixedOff OutputControl = iota
	ControlFixedOn
	ControlControlled
	ControlCondition
)

// ColorConfig is a named color from the [Colors DOF] section.
type ColorConfig struct {
	Name       string
	R, G, B, A int
}

// Setting is a single parsed ledcontrol column setting.
type Setting struct {
	Control      OutputControl
	ColorName    string
	ColorConfig  *ColorConfig
	TableElement string
	Condition    string

	DurationMs, MinDurationMs, MaxDurationMs, ExtDurationMs int
	intensity                                               int
	FadeUpMs, FadeDownMs                                    int
	Blink, BlinkIntervalMs, BlinkIntervalMsNested           int
	BlinkPulseWidth, BlinkPulseWidthNested, BlinkLow        int
	Invert, NoBool                                          bool
	WaitMs                                                  int
	Layer                                                   *int
	IsArea                                                  bool
	Area                                                    AreaSettings
}

// Intensity mirrors DOF: -1 when the setting carries a color.
func (s *Setting) Intensity() int {
	if s.ColorName != "" {
		return -1
	}
	return s.intensity
}

var (
	reHTMLColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6,8}$`)
	reWord      = regexp.MustCompile(`^[A-Za-z_]+$`)
)

func hasPrefixFold(s, p string) bool {
	return len(s) >= len(p) && strings.EqualFold(s[:len(p)], p)
}

// ParseSetting parses one setting (the text between '/' separators).
func ParseSetting(data string) (*Setting, error) {
	s := &Setting{intensity: 255, DurationMs: -1, BlinkPulseWidth: 50, BlinkPulseWidthNested: 50, Area: newAreaSettings()}
	str := strings.TrimSpace(data)
	if strings.HasPrefix(str, "(") {
		depth, closing := 1, -1
		for i := 1; i < len(str); i++ {
			if str[i] == '(' {
				depth++
			} else if str[i] == ')' {
				depth--
			}
			if depth == 0 {
				closing = i
				break
			}
		}
		if closing <= 0 {
			return nil, fmt.Errorf("no closing bracket found for condition in setting %q", str)
		}
		s.Condition = strings.ToUpper(str[:closing+1])
		s.Control = ControlCondition
		str = strings.TrimSpace(str[len(s.Condition):])
	} else {
		if len(str) == 0 {
			return nil, fmt.Errorf("no data to parse")
		}
		end := -1
		var last byte
		for i := 0; i < len(str)-1; i++ {
			if str[i] == ' ' && last != '|' && last != 0 {
				end = i
				break
			}
			if str[i] != ' ' {
				last = str[i]
			}
		}
		if end == -1 {
			end = len(str)
		}
		trigger := strings.ToUpper(strings.TrimSpace(str[:end]))
		ok := true
		switch trigger {
		case "ON", "1":
			s.Control = ControlFixedOn
		case "OFF", "0":
			s.Control = ControlFixedOff
		case "B", "BLINK":
			s.Control = ControlFixedOn
			s.Blink = -1
			s.BlinkIntervalMs = 1000
		default:
			for _, e := range strings.Split(trigger, "|") {
				e = strings.TrimSpace(e)
				if e == "" {
					continue
				}
				if len(e) <= 1 {
					ok = false
					break
				}
				if ElementType(e[0]) == ElemNamedElement && isNamedChars(e[1:]) {
					continue
				}
				if isElementType(e[0]) && isInteger(e[1:]) {
					continue
				}
				ok = false
				break
			}
			if ok {
				s.Control = ControlControlled
				s.TableElement = trigger
			}
		}
		if !ok {
			return nil, fmt.Errorf("can't parse trigger part %q of ledcontrol setting %q", trigger, data)
		}
		str = strings.TrimSpace(str[len(trigger):])
	}

	parts := strings.Fields(str)
	intCnt := 0
	for _, p := range parts {
		up := strings.ToUpper(p)
		rest := func(n int) string { return p[n:] }
		switch {
		case up == "BLINK":
			s.Blink = -1
			s.BlinkIntervalMs = 1000
		case up == "INVERT":
			s.Invert = true
		case up == "NOBOOL":
			s.NoBool = true
		case parseAreaToken(s, p, up):
		case len(p) > 3 && up[:3] == "MAX" && isInteger(rest(3)):
			s.MaxDurationMs = limit(toInt(rest(3)), 0, 1<<31-1)
		case len(p) > 4 && up[:4] == "BNPW" && isInteger(rest(4)):
			s.BlinkPulseWidthNested = limit(toInt(rest(4)), 1, 99)
		case len(p) > 3 && up[:3] == "BNP" && isInteger(rest(3)):
			s.BlinkIntervalMsNested = limit(toInt(rest(3)), 0, 1<<31-1)
		case len(p) > 3 && up[:3] == "BPW" && isInteger(rest(3)):
			s.BlinkPulseWidth = limit(toInt(rest(3)), 1, 99)
		case len(p) > 3 && up[:3] == "BL#" && isHex(rest(3)):
			s.BlinkLow = limit(hexToInt(rest(3)), 0, 255)
		case len(p) > 1 && up[0] == 'E' && isInteger(rest(1)):
			s.ExtDurationMs = limit(toInt(rest(1)), 0, 1<<31-1)
		case len(p) > 2 && up[:2] == "I#" && isHex(rest(2)):
			s.intensity = limit(hexToInt(rest(2)), 0, 255)
		case len(p) > 1 && up[0] == 'I' && isInteger(rest(1)):
			s.intensity = int(float64(limit(toInt(rest(1)), 0, 48)) * 5.3125)
		case len(p) > 1 && up[0] == 'L' && isInteger(rest(1)):
			l := toInt(rest(1))
			s.Layer = &l
		case len(p) > 1 && up[0] == 'W' && isInteger(rest(1)):
			s.WaitMs = limit(toInt(rest(1)), 0, 1<<31-1)
		case len(p) > 1 && up[0] == 'M' && isInteger(rest(1)):
			s.MinDurationMs = limit(toInt(rest(1)), 0, 1<<31-1)
		case len(p) > 1 && up[0] == 'F' && isInteger(rest(1)):
			s.FadeUpMs = limit(toInt(rest(1)), 0, 1<<31-1)
			s.FadeDownMs = s.FadeUpMs
		case len(p) > 2 && up[:2] == "FU" && isInteger(rest(2)):
			s.FadeUpMs = limit(toInt(rest(2)), 0, 1<<31-1)
		case len(p) > 2 && up[:2] == "FD" && isInteger(rest(2)):
			s.FadeDownMs = limit(toInt(rest(2)), 0, 1<<31-1)
		case isInteger(p):
			switch intCnt {
			case 0:
				if s.Blink == -1 {
					s.BlinkIntervalMs = limit(toInt(p), 1, 1<<31-1)
					s.DurationMs = 0
				} else {
					s.DurationMs = limit(toInt(p), 1, 1<<31-1)
				}
			case 1:
				if s.Blink != -1 {
					s.Blink = limit(toInt(p), 1, 1<<31-1)
					if s.DurationMs > 0 && s.Blink >= 1 {
						s.BlinkIntervalMs = limit(s.DurationMs/s.Blink, 1, 1<<31-1)
						s.DurationMs = 0
					}
				}
			default:
				return nil, fmt.Errorf("ledcontrol setting %q contains more than 2 numeric values without a type definition", data)
			}
			intCnt++
		case strings.HasPrefix(p, "#"):
			if !reHTMLColor.MatchString(p) {
				return nil, fmt.Errorf("invalid '#' HTML-style color code %q", p)
			}
			s.ColorName = up
		case len(p) > 2 && reWord.MatchString(p):
			s.ColorName = up
		default:
			return nil, fmt.Errorf("can't parse the part %q of ledcontrol setting %q", p, data)
		}
	}
	return s, nil
}

func isShiftDirection(c byte) bool {
	return c == 'L' || c == 'R' || c == 'U' || c == 'D'
}

func isNamedChars(s string) bool {
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// Column is one table column (output or RGB triple) of a table config line.
type Column struct {
	Number      int
	FirstOutput int
	Settings    []*Setting
}

// RequiredOutputs is 3 when any setting carries a color, otherwise 1.
func (c *Column) RequiredOutputs() int {
	for _, s := range c.Settings {
		if s.ColorName != "" {
			return 3
		}
	}
	return 1
}

// IsArea is true if any setting is a matrix area setting.
func (c *Column) IsArea() bool {
	for _, s := range c.Settings {
		if s.IsArea {
			return true
		}
	}
	return false
}

// TableConfig is one ROM line of a ledcontrol file.
type TableConfig struct {
	ShortRomName string
	Columns      []*Column
}

// LedControlFile is a parsed directoutputconfigNN.ini file.
type LedControlFile struct {
	Path      string
	Number    int // LedWiz(Equivalent) number
	Colors    map[string]*ColorConfig
	Tables    []*TableConfig
	Version   string
	Warnings  []string
	// ToyNames holds the names from the comment line under [Config DOF]
	// ("Rom","Flipper Left",...). Index n is the name of column n (1 based);
	// index 0 is unused. Empty when the file has no such comment.
	ToyNames  []string
	byRomName map[string]*TableConfig
}

func splitTop(s string, sep byte) []string {
	var out []string
	depth, last := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if s[i] == sep && depth <= 0 {
			out = append(out, s[last:i])
			last = i + 1
			depth = 0
		}
	}
	if last < len(s) {
		out = append(out, s[last:])
	}
	return out
}

func parseColors(lines []string, f *LedControlFile) {
	for _, l := range lines {
		eq := strings.Index(l, "=")
		if eq <= 0 {
			continue
		}
		name, val := l[:eq], strings.TrimSpace(l[eq+1:])
		cc := &ColorConfig{Name: name}
		switch {
		case strings.HasPrefix(val, "#") && (len(val) == 7 || len(val) == 9) && isHex(val[1:]):
			cc.R, cc.G, cc.B = hexToInt(val[1:3]), hexToInt(val[3:5]), hexToInt(val[5:7])
			cc.A = 255
			if len(val) == 9 {
				cc.A = hexToInt(val[7:9])
			}
		default:
			vs := strings.Split(val, ",")
			if (len(vs) == 3 || len(vs) == 4) && allInts(vs) {
				cc.R = int(float64(limit(toInt(vs[0]), 0, 48)) * 5.3125)
				cc.G = int(float64(limit(toInt(vs[1]), 0, 48)) * 5.3125)
				cc.B = int(float64(limit(toInt(vs[2]), 0, 48)) * 5.3125)
				cc.A = 255
				if cc.R+cc.G+cc.B == 0 {
					cc.A = 0
				}
				if len(vs) == 4 {
					cc.A = int(float64(limit(toInt(vs[2]), 0, 48)) * 5.3125)
				}
			} else {
				f.Warnings = append(f.Warnings, fmt.Sprintf("color line %q has an unknown structure", l))
				continue
			}
		}
		f.Colors[strings.ToUpper(name)] = cc
	}
}

func allInts(vs []string) bool {
	for _, v := range vs {
		if !isInteger(v) {
			return false
		}
	}
	return true
}

// ParseLedControl loads a directoutputconfig/ledcontrol ini file.
func ParseLedControl(path string, number int) (*LedControlFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f := &LedControlFile{Path: path, Number: number, Colors: map[string]*ColorConfig{}, byRomName: map[string]*TableConfig{}}
	sections := map[string][]string{}
	header := ""
	var cur []string
	flush := func() {
		if header == "" {
			return
		}
		h := header
		for n := 2; ; n++ {
			if _, dup := sections[h]; !dup {
				break
			}
			h = fmt.Sprintf("%s %d", header, n)
		}
		sections[h] = cur
		cur = nil
	}
	text := strings.ReplaceAll(string(raw), "\r", "\n")
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "#") {
			if strings.EqualFold(header, "[Config DOF]") && f.ToyNames == nil {
				f.ToyNames = parseToyNames(l)
			}
			continue
		}
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]") && len(l) > 2 {
			flush()
			header = l
			cur = nil
			continue
		}
		cur = append(cur, l)
	}
	flush()

	get := func(names ...string) []string {
		for _, n := range names {
			for k, v := range sections {
				if strings.EqualFold(k, n) {
					return v
				}
			}
		}
		return nil
	}
	colorData := get("[Colors DOF]", "[Colors LedWiz]")
	outData := get("[Config DOF]", "[Config outs]")
	varData := get("[Variables DOF]")
	tableVarData := get("[TableVariables]")
	if v := get("[version]"); v != nil {
		for _, l := range v {
			if hasPrefixFold(l, "mindofversion=") {
				f.Version = l[len("mindofversion="):]
			}
		}
	}
	if colorData == nil || outData == nil {
		return nil, fmt.Errorf("%s: missing color or config section", filepath.Base(path))
	}
	if tableVarData != nil {
		resolveTableVariables(outData, tableVarData)
	}
	if varData != nil {
		resolveVariables(outData, varData)
	}
	parseColors(colorData, f)

	for _, line := range outData {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cols := splitTop(line, ',')
		if len(cols) < 2 || strings.TrimSpace(cols[0]) == "" {
			continue
		}
		tc := &TableConfig{ShortRomName: cols[0]}
		lastData := -1
		for i := 1; i < len(cols); i++ {
			if strings.TrimSpace(cols[i]) != "" {
				lastData = i
			}
		}
		first := 1
		for i := 1; i <= lastData; i++ {
			c := &Column{Number: i, FirstOutput: first}
			for _, sd := range splitTop(cols[i], '/') {
				if strings.TrimSpace(sd) == "" {
					continue
				}
				st, err := ParseSetting(sd)
				if err != nil {
					f.Warnings = append(f.Warnings, fmt.Sprintf("%s: %v", tc.ShortRomName, err))
					continue
				}
				if st.Control == ControlFixedOff {
					continue
				}
				if cc, ok := f.Colors[strings.ToUpper(st.ColorName)]; ok && st.ColorName != "" {
					st.ColorConfig = cc
				}
				if cc, ok := f.Colors[st.Area.ColorName2]; ok && st.Area.ColorName2 != "" {
					st.Area.ColorConfig2 = cc
				}
				c.Settings = append(c.Settings, st)
			}
			tc.Columns = append(tc.Columns, c)
			first += c.RequiredOutputs()
		}
		f.Tables = append(f.Tables, tc)
		key := strings.ToUpper(tc.ShortRomName)
		if _, dup := f.byRomName[key]; !dup {
			f.byRomName[key] = tc
		}
	}
	return f, nil
}

// find returns the table config for a ROM name using DOF's three step match.
func (f *LedControlFile) find(rom string) *TableConfig {
	up := strings.ToUpper(rom)
	for _, t := range f.Tables {
		if strings.ToUpper(t.ShortRomName) == up {
			return t
		}
	}
	for _, t := range f.Tables {
		if strings.HasPrefix(up, strings.ToUpper(t.ShortRomName)+"_") {
			return t
		}
	}
	for _, t := range f.Tables {
		if strings.HasPrefix(rom, t.ShortRomName) {
			return t
		}
	}
	return nil
}

// HasRom reports whether the file has a config for rom.
func (f *LedControlFile) HasRom(rom string) bool { return f.find(rom) != nil }

func resolveVariables(data, vars []string) {
	for _, v := range vars {
		p := strings.Index(v, "=")
		if p <= 0 {
			continue
		}
		n := strings.TrimSpace(v[:p])
		val := strings.TrimSpace(v[p+1:])
		if !strings.HasPrefix(n, "@") {
			n = "@" + n
		}
		if !strings.HasSuffix(n, "@") {
			n += "@"
		}
		for i := range data {
			data[i] = strings.ReplaceAll(data[i], n, val)
		}
	}
}

func resolveTableVariables(data, vars []string) {
	tv := map[string][][2]string{}
	for _, d := range vars {
		tp := strings.Index(d, ",")
		if tp <= 0 {
			continue
		}
		table := strings.TrimSpace(d[:tp])
		vd := strings.TrimSpace(d[tp+1:])
		pos, last := 0, 0
		for pos < len(vd) {
			pos = strings.IndexByte(vd[last:], '=')
			if pos < 0 {
				break
			}
			pos += last
			if pos-last < 1 {
				break
			}
			name := strings.TrimSpace(vd[last:pos])
			val := ""
			pos++
			if pos < len(vd) {
				last = pos
				if vd[pos] == '{' {
					e := strings.IndexByte(vd[last:], '}')
					if e < 0 {
						break
					}
					e += last
					val = vd[last+1 : e]
					pos = e + 1
					for pos < len(vd) && vd[pos] == ' ' {
						pos++
					}
					if pos < len(vd) && vd[pos] != '/' {
						break
					}
				} else {
					e := strings.IndexByte(vd[last:], '/')
					if e < 0 {
						pos = len(vd)
					} else {
						pos = last + e
					}
					val = strings.TrimSpace(vd[last:pos])
				}
			}
			tv[table] = append(tv[table], [2]string{name, val})
			last = pos + 1
		}
	}
	for i, d := range data {
		tp := strings.Index(d, ",")
		if tp <= 0 {
			continue
		}
		table := strings.TrimSpace(d[:tp])
		kv, ok := tv[table]
		if !ok {
			continue
		}
		seen := map[string]bool{}
		for _, p := range kv {
			if seen[p[0]] {
				continue
			}
			seen[p[0]] = true
			n := p[0]
			if !strings.HasPrefix(n, "@") {
				n = "@" + n
			}
			if !strings.HasSuffix(n, "@") {
				n += "@"
			}
			d = strings.ReplaceAll(d, n, p[1])
		}
		data[i] = d
	}
}

// LedControlFileNumber extracts the number from directoutputconfig11.ini.
func LedControlFileNumber(name string) (int, bool) {
	l := strings.ToLower(name)
	for _, pre := range []string{"directoutputconfig", "ledcontrol"} {
		if strings.HasPrefix(l, pre) && strings.HasSuffix(l, ".ini") {
			mid := l[len(pre) : len(l)-4]
			if mid == "" {
				return 1, true
			}
			if n, err := strconv.Atoi(mid); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

// parseToyNames reads the comment line the DOF Config Tool writes under
// [Config DOF]: # "Rom","Flipper Left","Slingshot Left",... It returns nil for
// any other comment. The result is indexed by column number (index 0 = "Rom").
func parseToyNames(line string) []string {
	s := strings.TrimSpace(strings.TrimLeft(line, "# \t"))
	if !hasPrefixFold(s, `"rom"`) {
		return nil
	}
	var out []string
	for _, f := range splitTop(s, ',') {
		out = append(out, strings.TrimSpace(strings.Trim(strings.TrimSpace(f), `"`)))
	}
	return out
}
