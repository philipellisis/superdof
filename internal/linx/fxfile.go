package linx

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Action is a single "FF_xxx args" step of a command.
type Action struct {
	Name string // upper case, e.g. FF_DOF
	Args string // everything after the first space (raw)
}

// Command is the action list for "Name|STATE".
type Command struct {
	Name    string
	State   string // ON / OFF
	Actions []Action
	Alias   string // when set this entry is an alias for another command name
}

// MemTrigger is a FX2/FX3 memory trigger definition.
type MemTrigger struct {
	Name    string
	Address uint32
	Mask    uint32
	Offsets []uint32 // table based chain, empty for core triggers
	Core    bool
}

// FXFile is the parsed content of one .FX/.FX2/.FX3/.Linx file.
type FXFile struct {
	Path     string
	Startup  []Action
	Shutdown []Action
	// Startup parameters (key=value lines that are not FF_ actions), e.g.
	// BUTTON_COLOUR_CHANGE, COLOUR_PALETTE, BUTTON_LIGHT_ACTION, FF_ROM.
	Params   map[string][]string
	Commands []Command
	Triggers []MemTrigger
	Clear    []string
	// EncryptedLines counts opaque base64 lines (FX3 memory triggers).
	EncryptedLines int
}

func parseActions(s string) []Action {
	var out []Action
	for _, part := range strings.Split(s, "|") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, args := part, ""
		if i := strings.IndexAny(part, " \t"); i > 0 {
			name, args = part[:i], strings.TrimSpace(part[i+1:])
		} else if eq := strings.IndexByte(part, '='); eq > 0 && strings.HasPrefix(strings.ToUpper(part), "FF_") {
			name, args = part[:eq], strings.TrimSpace(part[eq+1:])
		}
		out = append(out, Action{Name: strings.ToUpper(name), Args: args})
	}
	return out
}

func looksEncrypted(l string) bool {
	if len(l) < 40 || strings.ContainsAny(l, " |=,[]") {
		return false
	}
	for _, c := range l {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=') {
			return false
		}
	}
	return true
}

// ParseFXFile parses a DOFLinx table/emulator file.
func ParseFXFile(path string) (*FXFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fx := &FXFile{Path: path, Params: map[string][]string{}}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	section := ""
	for sc.Scan() {
		l := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]") {
			section = strings.ToUpper(l[1 : len(l)-1])
			continue
		}
		switch {
		case section == "STARTUP" || section == "SHUTDOWN":
			acts := parseActions(l)
			for _, a := range acts {
				if strings.HasPrefix(a.Name, "FF_") {
					if section == "STARTUP" {
						fx.Startup = append(fx.Startup, a)
					} else {
						fx.Shutdown = append(fx.Shutdown, a)
					}
				}
			}
			if eq := strings.IndexByte(l, '='); eq > 0 && !strings.HasPrefix(strings.ToUpper(l), "FF_") {
				k := strings.ToUpper(strings.TrimSpace(l[:eq]))
				fx.Params[k] = append(fx.Params[k], strings.TrimSpace(l[eq+1:]))
			} else if len(acts) > 0 && strings.HasPrefix(acts[0].Name, "FF_") && section == "STARTUP" {
				if acts[0].Name == "FF_ROM" {
					fx.Params["FF_ROM"] = append(fx.Params["FF_ROM"], acts[0].Args)
				}
			}
		case strings.HasPrefix(section, "MEMORY TRIGGERS"):
			if t, ok := parseMemTrigger(l, section == "MEMORY TRIGGERS - CORE"); ok {
				fx.Triggers = append(fx.Triggers, t)
			}
		case section == "CLEAR COMMANDS":
			fx.Clear = append(fx.Clear, strings.ToUpper(strings.Fields(l)[0]))
		case section == "COMMANDS":
			if looksEncrypted(l) {
				fx.EncryptedLines++
				continue
			}
			parts := strings.SplitN(l, "|", 3)
			if len(parts) < 3 {
				continue
			}
			name, state, rest := strings.TrimSpace(parts[0]), strings.ToUpper(strings.TrimSpace(parts[1])), parts[2]
			if state == "ALIAS" {
				fx.Commands = append(fx.Commands, Command{Name: strings.ToUpper(name), State: "ON", Alias: strings.ToUpper(strings.TrimSpace(rest))})
				fx.Commands = append(fx.Commands, Command{Name: strings.ToUpper(name), State: "OFF", Alias: strings.ToUpper(strings.TrimSpace(rest))})
				continue
			}
			if state != "ON" && state != "OFF" {
				continue
			}
			fx.Commands = append(fx.Commands, Command{Name: strings.ToUpper(name), State: state, Actions: parseActions(rest)})
		default:
			if looksEncrypted(l) {
				fx.EncryptedLines++
			}
		}
	}
	return fx, sc.Err()
}

func parseMemTrigger(l string, core bool) (MemTrigger, bool) {
	eq := strings.IndexByte(l, '=')
	if eq <= 0 {
		return MemTrigger{}, false
	}
	t := MemTrigger{Name: strings.TrimSpace(l[:eq]), Core: core}
	fields := strings.Split(l[eq+1:], ",")
	if len(fields) < 2 {
		return MemTrigger{}, false
	}
	hex := func(s string) (uint32, bool) {
		n, err := strconv.ParseUint(strings.TrimSpace(s), 16, 32)
		return uint32(n), err == nil
	}
	var ok bool
	if t.Address, ok = hex(fields[0]); !ok {
		return MemTrigger{}, false
	}
	if t.Mask, ok = hex(fields[1]); !ok {
		return MemTrigger{}, false
	}
	for _, f := range fields[2:] {
		o, ok := hex(f)
		if !ok {
			return MemTrigger{}, false
		}
		t.Offsets = append(t.Offsets, o)
	}
	return t, true
}

// CommandSet is the merged command table of several files
// (All_Pre, table file, All_Post). Like DOFLinx, the commands of all files
// accumulate and run in file order; [CLEAR COMMANDS] removes the commands
// collected so far. Aliases resolve to the commands of the target name.
type CommandSet struct {
	cmds map[string]map[string][]Action // NAME -> STATE -> actions
	alia map[string]string
}

func NewCommandSet() *CommandSet {
	return &CommandSet{cmds: map[string]map[string][]Action{}, alia: map[string]string{}}
}

// Merge adds a file's commands after the ones already collected.
func (cs *CommandSet) Merge(f *FXFile) {
	for _, n := range f.Clear {
		delete(cs.cmds, n)
		delete(cs.alia, n)
	}
	for _, c := range f.Commands {
		if c.Alias != "" {
			cs.alia[c.Name] = c.Alias
			continue
		}
		m := cs.cmds[c.Name]
		if m == nil {
			m = map[string][]Action{}
			cs.cmds[c.Name] = m
		}
		m[c.State] = append(m[c.State], c.Actions...)
	}
}

// Lookup returns the actions for an event name and state ("ON"/"OFF").
func (cs *CommandSet) Lookup(name, state string) []Action {
	name = strings.ToUpper(name)
	var out []Action
	seen := map[string]bool{}
	for name != "" && !seen[name] {
		seen[name] = true
		if m, ok := cs.cmds[name]; ok {
			out = append(out, m[state]...)
		}
		name = cs.alia[name]
	}
	return out
}

// Len is the number of distinct command names (incl. aliases).
func (cs *CommandSet) Len() int { return len(cs.cmds) + len(cs.alia) }

// FindFile locates name+ext in dirs, case-insensitively.
func FindFile(dirs []string, name, ext string) string {
	want := strings.ToLower(name + ext)
	for _, d := range dirs {
		if d == "" {
			continue
		}
		ents, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !e.IsDir() && strings.ToLower(e.Name()) == want {
				return filepath.Join(d, e.Name())
			}
		}
	}
	return ""
}

func (f *FXFile) String() string {
	return fmt.Sprintf("%s (%d commands, %d triggers)", filepath.Base(f.Path), len(f.Commands), len(f.Triggers))
}

// Names lists the command names (events) with the states they define, e.g.
// "BUMPER01|ON", sorted. Aliases are listed with their own name.
func (cs *CommandSet) Names() []string {
	var out []string
	for n, m := range cs.cmds {
		for st := range m {
			out = append(out, n+"|"+st)
		}
	}
	for n := range cs.alia {
		out = append(out, n+"|ON", n+"|OFF")
	}
	sort.Strings(out)
	return out
}
