package linx

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Profile describes one supported game family.
type Profile struct {
	Kind    string   // FX, FX2, FX3, LINX_M, LINX_IMP
	Procs   []string // process names (case insensitive, without .exe)
	PathKey string   // PATH_FX ...
	Ext     string   // .FX ...
	Pre     []string // files merged before the table file
	Post    []string
	Default string // fallback table file base name ("default" for FX2)
	Memory  bool   // events come from process memory (FX2/FX3)
	Trigger bool   // events come from DOFLinxTrigger.dll pipe
}

func defaultProfiles(cfg *Config) []*Profile {
	ps := []*Profile{
		{Kind: "FX", Procs: []string{"PinballFX-Win64-Shipping", "PinballFX"}, PathKey: "PATH_FX", Ext: ".FX",
			Pre: []string{"All_Pre"}, Post: []string{"All_Post"}, Trigger: true},
		{Kind: "FX2", Procs: []string{"Pinball FX2"}, PathKey: "PATH_FX2", Ext: ".FX2",
			Pre: []string{"All_Pre"}, Post: []string{"All_Post"}, Default: "default", Memory: true},
		{Kind: "FX3", Procs: []string{"Pinball FX3", "Pinball FX Classic"}, PathKey: "PATH_FX3", Ext: ".FX3",
			Pre: []string{"All_Pre"}, Post: []string{"All_Post"}, Memory: true},
		{Kind: "LINX_M", Procs: []string{"PinballM-Win64-Shipping"}, PathKey: "PATH_LINX", Ext: ".Linx",
			Pre: []string{"All_Pre", "All_Pre_M"}, Post: []string{"All_Post_M", "All_Post"}, Trigger: true},
		{Kind: "LINX_IMP", Procs: []string{"IMPinball", "IMPinball-Win64-Shipping"}, PathKey: "PATH_LINX", Ext: ".Linx",
			Pre: []string{"All_Pre", "All_Pre_IMP"}, Post: []string{"All_Post_IMP", "All_Post"}, Trigger: true},
	}
	// PROCESS_ADD / PROCESSES extend the process names of FX style games.
	for _, p := range append(cfg.List("PROCESSES"), cfg.List("PROCESS_ADD")...) {
		known := false
		for _, pr := range ps {
			for _, n := range pr.Procs {
				if strings.EqualFold(n, p) {
					known = true
				}
			}
		}
		if !known && !strings.EqualFold(p, "mame") && !strings.EqualFold(p, "mame64") && !strings.Contains(strings.ToLower(p), "future pinball") {
			ps[0].Procs = append(ps[0].Procs, p)
		}
	}
	for _, p := range cfg.List("PROCESSES_LINX") {
		known := false
		for _, pr := range ps {
			for _, n := range pr.Procs {
				if strings.EqualFold(n, p) {
					known = true
				}
			}
		}
		if !known {
			ps[3].Procs = append(ps[3].Procs, p)
		}
	}
	return ps
}

type activeGame struct {
	prof   *Profile
	proc   Proc
	table  string
	files  []*FXFile
	memEnd chan struct{}
}

// Manager detects games, selects tables and routes events to the Runner.
type Manager struct {
	Cfg    *Config
	Runner *Runner
	Log    Logger

	ListProcs func() ([]Proc, error)
	Titles    func(pid uint32) []string

	profiles []*Profile
	tableIDs map[string]string
	forced   string

	mu        sync.Mutex
	cur       *activeGame
	stop      chan struct{}
	warned    map[string]bool
	autoDirs  map[string]string
	seenIDs   []SeenID
	dirsOK    bool
}

// NewManager creates a manager.
func NewManager(cfg *Config, r *Runner, log Logger) *Manager {
	if log == nil {
		log = StdLogger()
	}
	m := &Manager{Cfg: cfg, Runner: r, Log: log, ListProcs: ListProcesses, Titles: WindowTitles,
		profiles: defaultProfiles(cfg), tableIDs: map[string]string{}, stop: make(chan struct{}), warned: map[string]bool{}}
	m.forced = cfg.Get("FORCE_TABLE")
	if p := cfg.Get("TABLE_IDS_FILE"); p != "" {
		m.loadTableIDs(cfg.Resolve(p))
	} else {
		m.loadTableIDs(filepath.Join(cfg.Dir, "tables.ini"))
	}
	m.loadTableIDs(m.learnedPath())
	return m
}

func (m *Manager) learnedPath() string { return filepath.Join(m.Cfg.Dir, "tables.learned.ini") }

// learn records that game id belongs to a table file and persists it.
func (m *Manager) learn(id, table string) {
	id = strings.TrimSpace(id)
	if id == "" || table == "" {
		return
	}
	m.mu.Lock()
	if cur, ok := m.tableIDs[id]; ok && strings.EqualFold(cur, table) {
		m.mu.Unlock()
		return
	}
	m.tableIDs[id] = table
	m.mu.Unlock()
	m.Log.Infof("game id %s = %s (saved to %s)", id, table, m.learnedPath())
	m.saveLearned(id, table)
}

// saveLearned rewrites tables.learned.ini with id set to table (one line per id).
func (m *Manager) saveLearned(id, table string) {
	path := m.learnedPath()
	entries := map[string]string{}
	if b, err := os.ReadFile(path); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			l = strings.TrimSpace(l)
			if eq := strings.IndexByte(l, '='); eq > 0 && !strings.HasPrefix(l, "#") {
				entries[strings.TrimSpace(l[:eq])] = strings.TrimSpace(l[eq+1:])
			}
		}
	}
	entries[id] = table
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, e1 := strconv.Atoi(keys[i])
		b, e2 := strconv.Atoi(keys[j])
		if e1 == nil && e2 == nil {
			return a < b
		}
		return keys[i] < keys[j]
	})
	var sb strings.Builder
	sb.WriteString("# game id = table file, assigned on the game id page\n")
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s=%s\n", k, entries[k])
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		m.Log.Warnf("cannot save %s: %v", path, err)
	}
}

// SetTableIDs adds game id -> table file mappings (JSON config).
func (m *Manager) SetTableIDs(ids map[string]string) {
	for k, v := range ids {
		m.tableIDs[strings.TrimSpace(k)] = v
	}
}

func (m *Manager) loadTableIDs(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "[") {
			continue
		}
		if eq := strings.IndexByte(l, '='); eq > 0 {
			m.tableIDs[strings.TrimSpace(l[:eq])] = strings.TrimSpace(l[eq+1:])
		}
	}
	m.Log.Infof("loaded %d game id mappings from %s", len(m.tableIDs), path)
}

func (m *Manager) dirsFor(p *Profile) []string {
	d := m.Cfg.Get(p.PathKey)
	var dirs []string
	if d != "" {
		if r := m.Cfg.Resolve(d); isDir(r) {
			dirs = append(dirs, r)
		}
	}
	if len(dirs) == 0 {
		// The configured folder is missing or unset: look for the shipped
		// command files in the usual places (once) and say which was used.
		m.mu.Lock()
		found, ok := m.autoDirs[p.PathKey]
		m.mu.Unlock()
		if !ok {
			found = findCommandDir(m.Cfg.Dir, p)
			m.mu.Lock()
			if m.autoDirs == nil {
				m.autoDirs = map[string]string{}
			}
			m.autoDirs[p.PathKey] = found
			m.mu.Unlock()
			if found != "" {
				m.Log.Warnf("%s is not set or does not exist (%q); using %s. Set %s in DOFLinx.INI to silence this", p.PathKey, d, found, p.PathKey)
			} else {
				m.Log.Warnf("no folder with %s command files found: set %s in DOFLinx.INI (the FX folder of the DOFLinx download)", p.Ext, p.PathKey)
			}
		}
		if found != "" {
			dirs = append(dirs, found)
		}
	}
	dirs = append(dirs, m.Cfg.Dir)
	return dirs
}

func isDir(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }

// findCommandDir looks for a folder holding the profile's command files.
func findCommandDir(cfgDir string, p *Profile) string {
	sub := strings.TrimPrefix(strings.ToUpper(p.Ext), ".")
	if strings.HasPrefix(strings.ToUpper(p.Kind), "LINX") {
		sub = "Linx"
	}
	var bases []string
	bases = append(bases, cfgDir, filepath.Dir(cfgDir))
	if exe, _ := os.Executable(); exe != "" {
		bases = append(bases, filepath.Dir(exe))
	}
	bases = append(bases, `C:DOFLinx`, `C:DirectOutput`)
	var cands []string
	for _, b := range bases {
		cands = append(cands, filepath.Join(b, sub), filepath.Join(b, "config", sub))
	}
	if home, _ := os.UserHomeDir(); home != "" {
		for _, d := range []string{"Downloads", "Desktop", "Documents"} {
			m, _ := filepath.Glob(filepath.Join(home, d, "DOFLinx*", sub))
			cands = append(cands, m...)
			m, _ = filepath.Glob(filepath.Join(home, d, "DOFLinx*", "*", sub))
			cands = append(cands, m...)
		}
	}
	for _, c := range cands {
		if ents, err := os.ReadDir(c); err == nil {
			for _, e := range ents {
				if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), p.Ext) {
					return c
				}
			}
		}
	}
	return ""
}

func (m *Manager) profileFor(procName string) *Profile {
	for _, p := range m.profiles {
		for _, n := range p.Procs {
			if strings.EqualFold(n, procName) {
				return p
			}
		}
	}
	return nil
}

// ProcessNames lists the process names being watched.
func (m *Manager) ProcessNames() []string {
	var out []string
	for _, p := range m.profiles {
		out = append(out, p.Procs...)
	}
	return out
}

// Run watches for games until Close is called. It blocks.
func (m *Manager) Run() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-tick.C:
			m.poll()
		}
	}
}

// Close stops Run and ends the active game.
func (m *Manager) Close() {
	select {
	case <-m.stop:
	default:
		close(m.stop)
	}
	m.endGame("shutdown")
}

func (m *Manager) poll() {
	procs, err := m.ListProcs()
	if err != nil {
		return
	}
	m.mu.Lock()
	cur := m.cur
	m.mu.Unlock()
	if cur != nil {
		alive := false
		for _, p := range procs {
			if p.PID == cur.proc.PID {
				alive = true
				break
			}
		}
		if !alive {
			m.Log.Infof("%s ended", cur.proc.Name)
			m.endGame("process ended")
			return
		}
		if cur.table == "" || cur.table == cur.prof.Default && cur.prof.Default != "" {
			m.resolveByTitle(cur)
		}
		return
	}
	for _, p := range procs {
		if prof := m.profileFor(p.Name); prof != nil {
			m.Log.Infof("detected %s (pid %d), profile %s", p.Name, p.PID, prof.Kind)
			m.startGame(prof, p, "")
			return
		}
	}
}

func norm(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// tableFiles lists the table file base names available for a profile.
func (m *Manager) tableFiles(p *Profile) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range m.dirsFor(p) {
		ents, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), p.Ext) {
				continue
			}
			b := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			l := strings.ToLower(b)
			if strings.HasPrefix(l, "all_") || l == "default" || seen[l] {
				continue
			}
			seen[l] = true
			out = append(out, b)
		}
	}
	sort.Strings(out)
	return out
}

func (m *Manager) resolveByTitle(g *activeGame) {
	if m.Titles == nil {
		return
	}
	titles := m.Titles(g.proc.PID)
	if len(titles) == 0 {
		return
	}
	best, bestLen := "", 0
	for _, f := range m.tableFiles(g.prof) {
		nf := norm(f)
		if len(nf) < 4 {
			continue
		}
		for _, t := range titles {
			if strings.Contains(norm(t), nf) && len(nf) > bestLen {
				best, bestLen = f, len(nf)
			}
		}
	}
	if best != "" && !strings.EqualFold(best, g.table) {
		m.Log.Infof("table identified from window title: %s", best)
		m.SelectTable(best)
	}
}

// startGame begins a session for the profile.
func (m *Manager) startGame(p *Profile, proc Proc, table string) {
	m.mu.Lock()
	m.cur = &activeGame{prof: p, proc: proc}
	m.mu.Unlock()
	if table == "" && m.forced != "" {
		table = m.forced
	}
	m.applyTable(table)
}

// SelectTable switches the active game to a table file base name.
func (m *Manager) SelectTable(name string) {
	m.mu.Lock()
	cur := m.cur
	m.mu.Unlock()
	if cur == nil {
		return
	}
	if strings.EqualFold(cur.table, name) {
		return
	}
	m.applyTable(name)
}

func (m *Manager) applyTable(table string) {
	m.mu.Lock()
	g := m.cur
	m.mu.Unlock()
	if g == nil {
		return
	}
	if g.memEnd != nil {
		close(g.memEnd)
		g.memEnd = nil
	}
	p := g.prof
	dirs := m.dirsFor(p)
	var files []*FXFile
	load := func(name string) *FXFile {
		path := FindFile(dirs, name, p.Ext)
		if path == "" {
			return nil
		}
		f, err := ParseFXFile(path)
		if err != nil {
			m.Log.Warnf("%s: %v", path, err)
			return nil
		}
		return f
	}
	for _, n := range p.Pre {
		if f := load(n); f != nil {
			files = append(files, f)
		}
	}
	var tf *FXFile
	if table != "" {
		tf = load(table)
		if tf == nil {
			m.Log.Warnf("no %s file for table %q in %v", p.Ext, table, dirs)
		}
	}
	if tf == nil && p.Default != "" {
		tf = load(p.Default)
		if tf != nil {
			m.Log.Infof("using %s%s", p.Default, p.Ext)
		}
	}
	if tf != nil {
		files = append(files, tf)
	}
	for _, n := range p.Post {
		if f := load(n); f != nil {
			files = append(files, f)
		}
	}
	cs := NewCommandSet()
	for _, f := range files {
		cs.Merge(f)
	}
	g.files = files
	g.table = table
	m.Log.Infof("starting %s game %q: %d command names from %d files", p.Kind, table, cs.Len(), len(files))
	m.Runner.Begin(table, cs, files)

	if p.Memory && tf != nil {
		if len(tf.Triggers) == 0 && tf.EncryptedLines > 0 {
			m.warnOnce("enc:"+tf.Path, "%s stores its memory triggers encrypted (%d opaque lines); this build cannot read them, so no events will fire for it. See docs/FX3-triggers.md", filepath.Base(tf.Path), tf.EncryptedLines)
		} else if len(tf.Triggers) > 0 {
			g.memEnd = make(chan struct{})
			go m.pollMemory(g, tf.Triggers, g.memEnd)
		}
	}
}

func (m *Manager) warnOnce(key, f string, a ...any) {
	m.mu.Lock()
	seen := m.warned[key]
	m.warned[key] = true
	m.mu.Unlock()
	if !seen {
		m.Log.Warnf(f, a...)
	}
}

func (m *Manager) endGame(reason string) {
	m.mu.Lock()
	g := m.cur
	m.cur = nil
	m.mu.Unlock()
	if g == nil {
		return
	}
	if g.memEnd != nil {
		close(g.memEnd)
	}
	m.Log.Infof("ending game (%s)", reason)
	for _, f := range g.files {
		for _, a := range f.Shutdown {
			m.Runner.Execute(a)
		}
	}
	m.Runner.End()
}

// pollMemory evaluates FX2/FX3 memory triggers (experimental, see docs).
func (m *Manager) pollMemory(g *activeGame, trig []MemTrigger, end chan struct{}) {
	var pm *ProcessMemory
	for pm == nil {
		var err error
		pm, err = OpenProcessMemory(g.proc.PID)
		if err != nil {
			m.warnOnce("memopen", "%v", err)
			select {
			case <-end:
				return
			case <-time.After(2 * time.Second):
			}
		}
	}
	defer pm.Close()
	state := make([]int, len(trig))
	for i := range state {
		state[i] = -1
	}
	tk := time.NewTicker(5 * time.Millisecond)
	defer tk.Stop()
	for {
		select {
		case <-end:
			return
		case <-tk.C:
		}
		for i, t := range trig {
			v, ok := readTrigger(pm, t)
			if !ok {
				continue
			}
			on := 0
			if v&t.Mask != 0 {
				on = 1
			}
			if state[i] == -1 {
				state[i] = on
				if on == 0 {
					continue
				}
			} else if state[i] == on {
				continue
			}
			state[i] = on
			m.Runner.Event(t.Name, on)
		}
	}
}

type memReader interface {
	ReadU32(addr uint32) (uint32, error)
}

// readTrigger evaluates a trigger. Core triggers read the value at the
// address directly. Table based triggers follow the pointer chain
// base -> [base] -> +off1 -> ... and read the final dword at the last offset.
func readTrigger(pm memReader, t MemTrigger) (uint32, bool) {
	if len(t.Offsets) == 0 {
		v, err := pm.ReadU32(t.Address)
		return v, err == nil
	}
	p, err := pm.ReadU32(t.Address)
	if err != nil || p == 0 {
		return 0, false
	}
	for _, off := range t.Offsets[:len(t.Offsets)-1] {
		p, err = pm.ReadU32(p + off)
		if err != nil || p == 0 {
			return 0, false
		}
	}
	v, err := pm.ReadU32(p + t.Offsets[len(t.Offsets)-1])
	return v, err == nil
}

// ---------------------------------------------------------------- triggers

// HandleTrigger processes one decoded DOFLinxTrigger message.
func (m *Manager) HandleTrigger(text string) {
	msg, ok := ParseTrigger(text)
	if !ok {
		return
	}
	m.mu.Lock()
	has := m.cur != nil
	m.mu.Unlock()
	if !has {
		// The game was started before us: attach to the first trigger profile.
		for _, p := range m.profiles {
			if p.Trigger {
				m.Log.Infof("trigger message received without detected process; starting %s profile", p.Kind)
				m.startGame(p, Proc{Name: p.Procs[0]}, "")
				break
			}
		}
	}
	switch msg.Kind {
	case 'E':
		m.Runner.Event(msg.Arg, msg.Value)
	case 'S':
		m.Runner.Status(msg.Arg)
	case 'N':
		switch strings.ToUpper(msg.Arg) {
		case "LEFT":
			m.Runner.Nudge("Left")
		case "RIGHT":
			m.Runner.Nudge("Right")
		case "FORWARD":
			m.Runner.Nudge("Forward")
		}
	case 'G':
		if m.forced != "" {
			return
		}
		id := strings.TrimSpace(msg.Arg)
		m.noteID(id)
		m.mu.Lock()
		name, ok := m.tableIDs[id]
		cur := m.cur
		m.mu.Unlock()
		if !ok {
			// the mapping files may have been edited while we run
			m.reloadTableIDs()
			m.mu.Lock()
			name, ok = m.tableIDs[id]
			m.mu.Unlock()
		}
		if !ok && cur != nil && cur.prof.Kind == "FX" {
			// built-in list of the official Pinball FX table numbers
			if f, _ := BuiltinTable(id); f != "" {
				name, ok = f, true
			}
		}
		if !ok {
			_ = cur
			// Never guess: the table active before is not the table of a new id.
			m.warnOnce("gid:"+msg.Arg, "game id %s is not mapped to a table file yet: assign it on the game id page or add %s=<TableFileName> to tables.ini", msg.Arg, msg.Arg)
			return
		}
		m.SelectTable(name)
	}
}

// HandleCommand processes a real time command / menu action sent to the
// DOFLINX pipe (DOFLinxMsg.exe), e.g. QUIT, OUTPUT_NOW_TIMER=1010,1500.
// It returns true when the process should quit.
func (m *Manager) HandleCommand(line string) (quit bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	for _, cmd := range strings.Split(line, "#") {
		cmd = strings.TrimSpace(cmd)
		key, val := cmd, ""
		if eq := strings.IndexByte(cmd, '='); eq > 0 {
			key, val = strings.ToUpper(strings.TrimSpace(cmd[:eq])), strings.TrimSpace(cmd[eq+1:])
		} else {
			key = strings.ToUpper(cmd)
		}
		args := splitArgs(val)
		switch key {
		case "QUIT":
			return true
		case "OUTPUT_NOW_TIMER":
			for i := 0; i+1 < len(args); i += 2 {
				if a, ok := parseInt(args[i]); ok {
					ms, _ := parseInt(args[i+1])
					m.Runner.OutputNow(a, ms)
				}
			}
		case "OUTPUT_NOW_COLOUR":
			for i := 0; i+2 < len(args); i += 3 {
				if a, ok := parseInt(args[i]); ok {
					ms, _ := parseInt(args[i+1])
					m.Runner.OutputColourNow(a, ms, args[i+2])
				}
			}
		case "OUTPUTS_OFF":
			m.Runner.OutputsOff()
		case "INFO":
			m.Log.Infof("%s", val)
		case "KILL", "ATTRACT_MODE", "CLEAR_LOG", "SHOW_VARIABLES", "HOLD_ACTIVATION", "GAME_FLAG_FILE",
			"SUP_INI", "SHOW_IN_TASKBAR", "START_DOF2DMD", "STOP_DOF2DMD", "LIGHT_BUTTONS_NOW":
			m.Log.Infof("real time command %s is not applicable in the PinOne build and is ignored", key)
		default:
			// A front end menu action name (Menu.ini [COMMANDS]).
			if m.menuAction(key) {
				continue
			}
			m.Log.Infof("unknown command %q", cmd)
		}
	}
	return false
}

var menuActions struct {
	sync.Mutex
	set  *CommandSet
	path string
}

// LoadMenu loads Menu.ini so front end actions can be triggered by name.
func (m *Manager) LoadMenu(path string) {
	f, err := ParseFXFile(path)
	if err != nil {
		return
	}
	cs := NewCommandSet()
	cs.Merge(f)
	menuActions.Lock()
	menuActions.set, menuActions.path = cs, path
	menuActions.Unlock()
	for _, a := range f.Startup {
		m.Runner.Execute(a)
	}
	m.Log.Infof("loaded %d front end actions from %s", cs.Len(), path)
}

func (m *Manager) menuAction(name string) bool {
	menuActions.Lock()
	cs := menuActions.set
	menuActions.Unlock()
	if cs == nil {
		return false
	}
	acts := cs.Lookup(name, "ON")
	if acts == nil {
		return false
	}
	for _, a := range acts {
		m.Runner.Execute(a)
	}
	return true
}

func (m *Manager) String() string {
	return fmt.Sprintf("manager(%d profiles)", len(m.profiles))
}

// ---------------------------------------------------------------- simulation (tester)

// ProfileKinds lists the supported game families (FX, FX2, FX3, LINX_M, LINX_IMP).
func (m *Manager) ProfileKinds() []string {
	var out []string
	for _, p := range m.profiles {
		out = append(out, p.Kind)
	}
	return out
}

// TablesFor lists the table files available for a game family.
func (m *Manager) TablesFor(kind string) []string {
	for _, p := range m.profiles {
		if strings.EqualFold(p.Kind, kind) {
			return m.tableFiles(p)
		}
	}
	return nil
}

// Simulate starts a game session without a running game: the command files
// of the family and table are loaded and their startup actions run, exactly
// like a detected game. Memory triggers are not polled.
func (m *Manager) Simulate(kind, table string) error {
	for _, p := range m.profiles {
		if strings.EqualFold(p.Kind, kind) {
			m.endGame("simulation restart")
			cp := *p
			cp.Memory = false
			m.startGame(&cp, Proc{Name: p.Procs[0]}, table)
			return nil
		}
	}
	return fmt.Errorf("unknown game family %q", kind)
}

// StopSimulation ends the session (shutdown actions run, outputs released).
func (m *Manager) StopSimulation() { m.endGame("simulation stopped") }

// Current reports the active family and table ("" when none).
func (m *Manager) Current() (kind, table string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cur == nil {
		return "", ""
	}
	return m.cur.prof.Kind, m.cur.table
}

// ---------------------------------------------------------------- game id learning

// SeenID is a game id reported by the game, with its mapping (if any).
type SeenID struct {
	ID    string `json:"id"`
	Last  time.Time `json:"-"`
	AgeMs int64  `json:"ageMs"`
	Table string `json:"table"`
	Name  string `json:"name"`   // official Pinball FX name when the id is a known table
	Built bool   `json:"builtin"` // Table comes from the built-in list, not from you
	Count int    `json:"count"`
}

func (m *Manager) noteID(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.seenIDs {
		if m.seenIDs[i].ID == id {
			m.seenIDs[i].Last = time.Now()
			m.seenIDs[i].Count++
			return
		}
	}
	m.seenIDs = append(m.seenIDs, SeenID{ID: id, Last: time.Now(), Count: 1})
}

// SeenIDs lists every game id received so far, most recent first.
func (m *Manager) SeenIDs() []SeenID {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SeenID, len(m.seenIDs))
	copy(out, m.seenIDs)
	for i := range out {
		out[i].Table = m.tableIDs[out[i].ID]
		f, n := BuiltinTable(out[i].ID)
		out[i].Name = n
		if out[i].Table == "" && f != "" {
			out[i].Table, out[i].Built = f, true
		}
		out[i].AgeMs = time.Since(out[i].Last).Milliseconds()
	}
	// most recently seen first; ties keep the later arrival first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	return out
}

// Learn assigns a game id to a table file, saves it and selects the table.
func (m *Manager) Learn(id, table string) {
	m.learn(id, table)
	m.SelectTable(table)
}

func (m *Manager) reloadTableIDs() {
	if p := m.Cfg.Get("TABLE_IDS_FILE"); p != "" {
		m.loadTableIDsQuiet(m.Cfg.Resolve(p))
	} else {
		m.loadTableIDsQuiet(filepath.Join(m.Cfg.Dir, "tables.ini"))
	}
	m.loadTableIDsQuiet(m.learnedPath())
}

func (m *Manager) loadTableIDsQuiet(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "[") {
			continue
		}
		if eq := strings.IndexByte(l, '='); eq > 0 {
			m.tableIDs[strings.TrimSpace(l[:eq])] = strings.TrimSpace(l[eq+1:])
		}
	}
}

// BuiltinTable returns the DOFLinx file and official name of a Pinball FX table number.
func BuiltinTable(id string) (file, name string) {
	n, err := strconv.Atoi(strings.TrimSpace(id))
	if err != nil {
		return "", ""
	}
	for _, t := range PinballFXTables {
		if t.ID == n {
			return t.File, t.Name
		}
	}
	return "", ""
}
