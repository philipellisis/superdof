// Package config loads the single SuperDOF JSON configuration file, which
// replaces DOFLinx.INI, the DOF GlobalConfig.xml and cabinet.xml. The old
// files are still accepted and can be converted with `superdof convert`.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"superdof/internal/dof"
	"superdof/internal/linx"
)

// File is the root of superdof.json.
type File struct {
	// PinOne selects the board connection ("auto", "hid", "COM4").
	PinOne string `json:"pinone,omitempty"`
	// DOF holds the DirectOutput framework settings.
	DOF DOFSection `json:"dof"`
	// Cabinet describes controllers and toys. When it has no PinOne a
	// PinOne is auto detected.
	Cabinet *dof.CabinetSpec `json:"cabinet,omitempty"`
	// DOFLinx holds the settings that used to live in DOFLinx.INI.
	DOFLinx Linx `json:"doflinx"`
}

// DOFSection are the DOF framework settings.
type DOFSection struct {
	// IniFilesPath is the folder with directoutputconfig*.ini and
	// DirectOutputShapes.xml.
	IniFilesPath string `json:"iniFilesPath,omitempty"`
	// TableMappings is the tablemappings.xml (table name -> rom name).
	TableMappings          string `json:"tableMappings,omitempty"`
	MinEffectDurationMs    int    `json:"minEffectDurationMs,omitempty"`
	MinRgbEffectDurationMs int    `json:"minRgbEffectDurationMs,omitempty"`
	PinOneMinIntervalMs    int    `json:"pinOneMinCommandIntervalMs,omitempty"`
}

// Linx are the DOFLinx settings.
type Linx struct {
	// Processes are the process names (without .exe) of Pinball FX style games.
	Processes []string `json:"processes,omitempty"`
	// ProcessesLinx are the process names of LINX games (Pinball M, IMP).
	ProcessesLinx []string `json:"processesLinx,omitempty"`
	// Paths of the .FX / .FX2 / .FX3 / .Linx files.
	Paths struct {
		FX   string `json:"fx,omitempty"`
		FX2  string `json:"fx2,omitempty"`
		FX3  string `json:"fx3,omitempty"`
		Linx string `json:"linx,omitempty"`
	} `json:"paths"`
	// PinOneDevice is the DOFLinx device number (first digit of DOOO) that
	// addresses the PinOne in the ports below. Default 1.
	PinOneDevice int `json:"pinOneDevice,omitempty"`
	// ForceTable forces a table file base name.
	ForceTable string `json:"forceTable,omitempty"`
	// ColourFile is the Colours.ini.
	ColourFile string `json:"colourFile,omitempty"`
	// Tables maps the game id reported by the trigger DLL to a table file.
	Tables map[string]string `json:"tables,omitempty"`

	// Devices are the force feedback devices LF RF LS RS ML MC MR BL BC BR SH GR
	// KN FN BE C1 C2 C3 SR BK, keyed by code.
	Devices map[string]Device `json:"devices,omitempty"`
	// Flashers FLOL FLIL FLCN FLOR FLIR: one or more RGB flasher ports (red).
	Flashers map[string][]int `json:"flashers,omitempty"`
	// Buttons BUT_ST BUT_LF ... : LED output port (red for RGB buttons).
	Buttons map[string]Button `json:"buttons,omitempty"`
	// RGB are the red ports of the RGB undercab devices.
	RGB []int `json:"rgb,omitempty"`
	// FixedOutputs stay on while a game runs.
	FixedOutputs []int `json:"fixedOutputs,omitempty"`
	// FixedOutputsColour is port -> colour for RGB fixed outputs.
	FixedOutputsColour map[string]string `json:"fixedOutputsColour,omitempty"`
	// ColourOrder overrides the RGB channel order, e.g. {"BUT_B3": "BGR"}.
	ColourOrder map[string]string `json:"colourOrder,omitempty"`
	// Ini passes any other DOFLinx.INI key through unchanged.
	Ini map[string]string `json:"ini,omitempty"`
}

// Device is one LINK_xx device.
type Device struct {
	Port      int    `json:"port"`
	DefaultMs int    `json:"defaultMs,omitempty"`
	MaxMs     int    `json:"maxMs,omitempty"`
	Intensity int    `json:"intensity,omitempty"` // 0-255, default 255
	Mode      string `json:"mode,omitempty"`      // "", "ON" or "FLASH" (SR/BK)
	FlashMs   int    `json:"flashMs,omitempty"`
	// Extra additional ports driven together with the same settings.
	Extra []int `json:"extraPorts,omitempty"`
}

// Button is one LINK_BUT_xx button light.
type Button struct {
	Port   int    `json:"port"`
	Colour string `json:"colour,omitempty"` // default colour name, or "MONO"
	Key    int    `json:"key,omitempty"`
}

// StripComments removes // and /* */ comments (outside of strings) so the
// JSON file can be annotated.
func StripComments(b []byte) []byte {
	var out bytes.Buffer
	inStr, esc := false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inStr {
			out.WriteByte(c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out.WriteByte(c)
		case c == '/' && i+1 < len(b) && b[i+1] == '/':
			for i < len(b) && b[i] != '\n' {
				i++
			}
			out.WriteByte('\n')
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			i += 2
			for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
				i++
			}
			i++
		default:
			out.WriteByte(c)
		}
	}
	return out.Bytes()
}

// Load reads a superdof.json.
func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	dec := json.NewDecoder(bytes.NewReader(StripComments(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &f, nil
}

// Save writes the file as indented JSON.
func (f *File) Save(path string) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// LinxConfig converts the DOFLinx section into the key/value form used by the
// DOFLinx layer. dir is the folder relative paths are resolved against.
func (f *File) LinxConfig(dir string) *linx.Config {
	c := linx.NewConfig()
	c.Dir = dir
	l := &f.DOFLinx
	if len(l.Processes) > 0 {
		c.Add("PROCESSES", strings.Join(l.Processes, ","))
	}
	if len(l.ProcessesLinx) > 0 {
		c.Add("PROCESSES_LINX", strings.Join(l.ProcessesLinx, ","))
	}
	for k, v := range map[string]string{"PATH_FX": l.Paths.FX, "PATH_FX2": l.Paths.FX2, "PATH_FX3": l.Paths.FX3,
		"PATH_LINX": l.Paths.Linx, "FORCE_TABLE": l.ForceTable, "COLOUR_FILE": l.ColourFile} {
		if v != "" {
			c.Add(k, v)
		}
	}
	if l.PinOneDevice > 0 {
		c.Add("PINONE_DEVICE", strconv.Itoa(l.PinOneDevice))
	}
	codes := sortedKeys(l.Devices)
	for _, code := range codes {
		d := l.Devices[code]
		ports := append([]int{d.Port}, d.Extra...)
		in := d.Intensity
		if in == 0 {
			in = 255
		}
		var parts []string
		for _, p := range ports {
			if d.Mode != "" {
				parts = append(parts, fmt.Sprintf("%d,%s,%d,%d", p, d.Mode, d.FlashMs, in))
			} else {
				parts = append(parts, fmt.Sprintf("%d,%d,%d,%d", p, d.DefaultMs, d.MaxMs, in))
			}
		}
		c.Add("LINK_"+strings.ToUpper(code), strings.Join(parts, ","))
	}
	for _, code := range sortedKeys(l.Flashers) {
		var s []string
		for _, p := range l.Flashers[code] {
			s = append(s, strconv.Itoa(p))
		}
		c.Add("LINK_"+strings.ToUpper(code), strings.Join(s, ","))
	}
	for _, code := range sortedKeys(l.Buttons) {
		b := l.Buttons[code]
		col := b.Colour
		if col == "" {
			col = "MONO"
		}
		name := strings.ToUpper(code)
		if !strings.HasPrefix(name, "BUT_") {
			name = "BUT_" + name
		}
		c.Add("LINK_"+name, fmt.Sprintf("%d,%s,%d", b.Port, col, b.Key))
	}
	if len(l.RGB) > 0 {
		c.Add("RGB_OUTPUT", joinInts(l.RGB))
	}
	if len(l.FixedOutputs) > 0 {
		c.Add("FIXED_OUTPUTS", joinInts(l.FixedOutputs))
	}
	var fc []string
	for _, p := range sortedKeys(l.FixedOutputsColour) {
		fc = append(fc, p+","+l.FixedOutputsColour[p])
	}
	if len(fc) > 0 {
		c.Add("FIXED_OUTPUTS_COLOUR", strings.Join(fc, ","))
	}
	var lo []string
	for _, k := range sortedKeys(l.ColourOrder) {
		lo = append(lo, k+","+l.ColourOrder[k])
	}
	if len(lo) > 0 {
		c.Add("LED_COLOUR_ORDER", strings.Join(lo, ","))
	}
	for _, k := range sortedKeys(l.Ini) {
		c.Add(k, l.Ini[k])
	}
	return c
}

// FromINI builds a JSON file model from a DOFLinx.INI (+ optional cabinet).
func FromINI(c *linx.Config, cab *dof.CabinetSpec, gc *dof.GlobalConfig) *File {
	f := &File{Cabinet: cab}
	if gc != nil {
		f.DOF.IniFilesPath = gc.IniFilesPath
		f.DOF.MinEffectDurationMs = gc.MinEffectDurationMs
		f.DOF.MinRgbEffectDurationMs = gc.MinRGBEffectDurationMs
		f.DOF.PinOneMinIntervalMs = gc.PinOneMinCommandInterval
		f.PinOne = gc.PinOneComPort
	}
	l := &f.DOFLinx
	l.Processes = c.List("PROCESSES")
	l.ProcessesLinx = c.List("PROCESSES_LINX")
	l.Paths.FX, l.Paths.FX2, l.Paths.FX3, l.Paths.Linx = c.Get("PATH_FX"), c.Get("PATH_FX2"), c.Get("PATH_FX3"), c.Get("PATH_LINX")
	l.PinOneDevice = c.Int("PINONE_DEVICE", 0)
	l.ForceTable = c.Get("FORCE_TABLE")
	l.ColourFile = c.Get("COLOUR_FILE")
	links := linx.LoadLinks(c)
	if len(links.Dev) > 0 {
		l.Devices = map[string]Device{}
	}
	for code, ls := range links.Dev {
		if len(ls) == 0 {
			continue
		}
		d := Device{Port: ls[0].Addr, DefaultMs: ls[0].DefaultMs, MaxMs: ls[0].MaxMs, Intensity: ls[0].Intensity, Mode: ls[0].Mode, FlashMs: ls[0].FlashMs}
		for _, x := range ls[1:] {
			d.Extra = append(d.Extra, x.Addr)
		}
		l.Devices[code] = d
	}
	if len(links.Flasher) > 0 {
		l.Flashers = map[string][]int{}
	}
	for code, ls := range links.Flasher {
		for _, x := range ls {
			l.Flashers[code] = append(l.Flashers[code], x.Addr)
		}
	}
	if len(links.Button) > 0 {
		l.Buttons = map[string]Button{}
	}
	for code, ls := range links.Button {
		if len(ls) == 0 {
			continue
		}
		col := ls[0].Colour
		if ls[0].Mono {
			col = "MONO"
		}
		l.Buttons[code] = Button{Port: ls[0].Addr, Colour: col, Key: ls[0].Key}
	}
	for _, r := range links.RGB {
		l.RGB = append(l.RGB, r.Addr)
	}
	l.FixedOutputs = links.Fixed
	if len(links.FixedC) > 0 {
		l.FixedOutputsColour = map[string]string{}
		for p, col := range links.FixedC {
			l.FixedOutputsColour[strconv.Itoa(p)] = col
		}
	}
	if len(links.Order) > 0 {
		l.ColourOrder = links.Order
	}
	// pass every other simple key through
	handled := map[string]bool{"PROCESSES": true, "PROCESSES_LINX": true, "PATH_FX": true, "PATH_FX2": true, "PATH_FX3": true,
		"PATH_LINX": true, "PINONE_DEVICE": true, "FORCE_TABLE": true, "COLOUR_FILE": true, "RGB_OUTPUT": true,
		"FIXED_OUTPUTS": true, "BUTTONS_ON": true, "FIXED_OUTPUTS_COLOUR": true, "BUTTONS_ON_COLOUR": true,
		"LED_COLOUR_ORDER": true, "DIRECTOUTPUTGLOBAL": true}
	for _, k := range c.Keys("") {
		if handled[k] || strings.HasPrefix(k, "LINK_") {
			continue
		}
		if l.Ini == nil {
			l.Ini = map[string]string{}
		}
		l.Ini[k] = c.Get(k)
	}
	return f
}

// FindDefault looks for superdof.json in the usual places.
func FindDefault() string {
	cands := []string{"superdof.json", filepath.Join("config", "superdof.json")}
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		cands = append(cands, filepath.Join(d, "superdof.json"), filepath.Join(d, "config", "superdof.json"))
	}
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func sortedKeys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

func joinInts(v []int) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ",")
}
