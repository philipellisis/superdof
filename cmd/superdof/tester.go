package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"superdof/internal/dof"
	"superdof/internal/linx"
)

//go:embed tester.html
var testerHTML []byte

// ---------------------------------------------------------------- log ring

type logLine struct {
	Seq   int    `json:"seq"`
	Time  string `json:"t"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

type ringLog struct {
	mu    sync.Mutex
	lines []logLine
	seq   int
	echo  bool
}

func (r *ringLog) add(level, f string, a ...any) {
	r.mu.Lock()
	r.seq++
	l := logLine{Seq: r.seq, Time: time.Now().Format("15:04:05.000"), Level: level, Msg: fmt.Sprintf(f, a...)}
	r.lines = append(r.lines, l)
	if len(r.lines) > 1000 {
		r.lines = r.lines[len(r.lines)-800:]
	}
	r.mu.Unlock()
	if r.echo {
		fmt.Fprintf(os.Stderr, "%s %-5s %s\n", l.Time, level, l.Msg)
	}
}
func (r *ringLog) Infof(f string, a ...any) { r.add("INFO", f, a...) }
func (r *ringLog) Warnf(f string, a ...any) { r.add("WARN", f, a...) }

func (r *ringLog) since(seq int) (out []logLine, last int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range r.lines {
		if l.Seq > seq {
			out = append(out, l)
		}
	}
	return out, r.seq
}

// ---------------------------------------------------------------- tester

type testerApp struct {
	ld     *loaded
	eng    *dof.Engine
	runner *linx.Runner
	mgr    *linx.Manager
	dry    bool
	device int
}

type tester struct {
	mu   sync.Mutex
	app  *testerApp
	log  *ringLog
	stop chan struct{} // cancels the running walk / chase
}

type loadReq struct {
	Config    string `json:"config"`
	INI       string `json:"ini"`
	Cabinet   string `json:"cabinet"`
	AutoCab   bool   `json:"autoCabinet"`
	IniDir    string `json:"iniDir"`
	PinOne    string `json:"pinone"`
	Dry       bool   `json:"dry"`
	TableFile string `json:"tableFile"`
}

func cmdTester(args []string) {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	addr := fs.String("listen", "127.0.0.1:0", "address of the tester web page (loopback only)")
	noOpen := fs.Bool("no-browser", false, "do not open the browser")
	cfgPath := fs.String("config", "", "preload superdof.json")
	iniPath := fs.String("ini", "", "preload DOFLinx.INI")
	cabinet := fs.String("cabinet", "", "preload DOF cabinet.xml")
	dofDir := fs.String("dof-config", "", "preload folder with directoutputconfig*.ini")
	pinone := fs.String("pinone", "auto", "PinOne connection: auto, hid or a serial port")
	dry := fs.Bool("dry-run", false, "start in simulation mode (no hardware)")
	fs.Parse(args)

	host, _, err := net.SplitHostPort(*addr)
	if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		fmt.Fprintln(os.Stderr, "the tester only listens on loopback (127.0.0.1)")
		os.Exit(2)
	}
	t := &tester{log: &ringLog{echo: true}}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mux := http.NewServeMux()
	t.routes(mux)
	url := "http://" + ln.Addr().String() + "/"
	fmt.Fprintf(os.Stderr, "SuperDOF tester: %s  (Ctrl+C to quit)\n", url)

	if *cfgPath != "" || *iniPath != "" || *dofDir != "" {
		req := loadReq{Config: *cfgPath, INI: *iniPath, Cabinet: *cabinet, IniDir: *dofDir, PinOne: *pinone, Dry: *dry}
		if err := t.load(req); err != nil {
			t.log.Warnf("load: %v", err)
		}
	}
	if !*noOpen {
		go openBrowser(url)
	}
	srv := &http.Server{Handler: guard(mux), ReadHeaderTimeout: 5 * time.Second}
	err = srv.Serve(ln)
	t.unload()
	if err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
	}
}

// guard rejects requests whose Host or Origin is not the loopback tester,
// so that a web page in another browser tab cannot drive the hardware.
func guard(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.Host)
		if host != "127.0.0.1" && host != "localhost" && host != "::1" && host != "[::1]" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !strings.HasPrefix(o, "http://"+r.Host) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		c = exec.Command("open", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	c.Start()
}

func (t *tester) unload() {
	t.cancelRun()
	t.mu.Lock()
	a := t.app
	t.app = nil
	t.mu.Unlock()
	if a != nil {
		a.mgr.StopSimulation()
		a.runner.OutputsOff()
		a.eng.Stop()
	}
}

func (t *tester) cancelRun() {
	t.mu.Lock()
	if t.stop != nil {
		close(t.stop)
		t.stop = nil
	}
	t.mu.Unlock()
}

// startRun cancels any running walk/chase and returns a fresh cancel channel.
func (t *tester) startRun() chan struct{} {
	t.cancelRun()
	c := make(chan struct{})
	t.mu.Lock()
	t.stop = c
	t.mu.Unlock()
	return c
}

func (t *tester) load(req loadReq) error {
	t.unload()
	lg := &loggerAdapter{t.log}
	ld, err := loadConfig(&logger{w: discard{}}, req.Config, req.INI, req.IniDir, req.Cabinet, firstNonEmpty(req.PinOne, "auto"))
	if err != nil {
		return err
	}
	if ld.source == "" && req.IniDir != "" {
		ld.lcfg.Dir = req.IniDir
	}
	if req.AutoCab {
		ld.dofCfg.CabinetFile, ld.dofCfg.Cabinet = "", nil
	}
	if req.TableFile != "" {
		ld.lcfg.Set("FORCE_TABLE", req.TableFile)
	}
	ld.dofCfg.DryRun = req.Dry
	ld.dofCfg.Log = lg
	eng := dof.New(ld.dofCfg)
	if err := eng.Start(); err != nil {
		return err
	}
	runner, mgr := newLinx(lg, ld.lcfg, eng, ld)
	t.mu.Lock()
	t.app = &testerApp{ld: ld, eng: eng, runner: runner, mgr: mgr, dry: req.Dry, device: ld.lcfg.Int("PINONE_DEVICE", 1)}
	t.mu.Unlock()
	t.log.Infof("loaded %s (%s)", firstNonEmpty(ld.source, "no DOFLinx config"), map[bool]string{true: "SIMULATION, no hardware", false: "LIVE hardware"}[req.Dry])
	return nil
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

type loggerAdapter struct{ r *ringLog }

func (l *loggerAdapter) Infof(f string, a ...any) { l.r.Infof(f, a...) }
func (l *loggerAdapter) Warnf(f string, a ...any) { l.r.Warnf(f, a...) }

func (t *tester) cur() *testerApp {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.app
}

// ---------------------------------------------------------------- http

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, f string, a ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"error": fmt.Sprintf(f, a...)})
}

func (t *tester) routes(mux *http.ServeMux) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(testerHTML)
	})
	// needApp wraps a handler that needs a loaded configuration and a JSON body.
	post := func(path string, h func(a *testerApp, body map[string]any, w http.ResponseWriter)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				fail(w, 405, "POST only")
				return
			}
			a := t.cur()
			if a == nil {
				fail(w, 409, "nothing loaded yet: use the Setup tab")
				return
			}
			body := map[string]any{}
			json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body)
			h(a, body, w)
		})
	}

	mux.HandleFunc("/api/trigger", func(w http.ResponseWriter, r *http.Request) {
		// GET lists what an install would do; POST performs it.
		hints := []string{}
		if a := t.cur(); a != nil {
			hints = append(hints, a.ld.lcfg.Dir)
		}
		if r.Method == http.MethodPost {
			rep := installTriggers("", nil, hints...)
			rep.log(t.log)
			writeJSON(w, map[string]any{"source": rep.Source, "games": rep.Games, "error": rep.Err})
			return
		}
		games := linx.FindGames()
		rows := []triggerGame{}
		for _, g := range games {
			st := "missing"
			ok := false
			if _, err := os.Stat(filepath.Join(g.Dir, linx.TriggerDLL)); err == nil {
				st, ok = "installed", true
			}
			rows = append(rows, triggerGame{Exe: g.Exe, Status: st, OK: ok})
		}
		writeJSON(w, map[string]any{"source": linx.FindTriggerDLL(hints...), "games": rows})
	})
	mux.HandleFunc("/api/detect", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, detect()) })
	mux.HandleFunc("/api/load", func(w http.ResponseWriter, r *http.Request) {
		var req loadReq
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			fail(w, 400, "%v", err)
			return
		}
		if err := t.load(req); err != nil {
			t.log.Warnf("load failed: %v", err)
			fail(w, 400, "%v", err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/info", func(w http.ResponseWriter, r *http.Request) {
		a := t.cur()
		if a == nil {
			writeJSON(w, map[string]any{"loaded": false})
			return
		}
		writeJSON(w, t.info(a))
	})
	mux.HandleFunc("/api/live", func(w http.ResponseWriter, r *http.Request) {
		a := t.cur()
		since, _ := strconv.Atoi(r.URL.Query().Get("since"))
		lines, last := t.log.since(since)
		out := map[string]any{"log": lines, "seq": last, "loaded": a != nil}
		if a != nil {
			var cs []map[string]any
			for _, c := range a.eng.Cabinet().Controllers {
				vals, leds := a.eng.Values(c.Name())
				m := map[string]any{"name": c.Name()}
				if leds != nil {
					m["leds"] = leds // base64 in JSON
				} else {
					iv := make([]int, len(vals))
					for i, v := range vals {
						iv[i] = int(v)
					}
					m["values"] = iv
				}
				cs = append(cs, m)
			}
			out["controllers"] = cs
			k, tb := a.mgr.Current()
			out["game"] = map[string]string{"kind": k, "table": tb}
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("/api/roms", func(w http.ResponseWriter, r *http.Request) {
		a := t.cur()
		if a == nil {
			writeJSON(w, map[string]any{"roms": []string{}})
			return
		}
		q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		out := []string{}
		all := a.eng.Roms()
		for _, n := range all {
			if q == "" || strings.Contains(strings.ToLower(n), q) {
				out = append(out, n)
				if len(out) >= 200 {
					break
				}
			}
		}
		writeJSON(w, map[string]any{"roms": out, "total": len(all)})
	})
	mux.HandleFunc("/api/tables", func(w http.ResponseWriter, r *http.Request) {
		a := t.cur()
		if a == nil {
			writeJSON(w, map[string]any{"tables": []string{}})
			return
		}
		writeJSON(w, map[string]any{"tables": a.mgr.TablesFor(r.URL.Query().Get("kind"))})
	})

	post("/api/output", func(a *testerApp, b map[string]any, w http.ResponseWriter) {
		ctl, n, v := str(b, "controller"), num(b, "number"), num(b, "value")
		if !a.eng.ForceOutput(ctl, n, byte(clamp(v, 0, 255))) {
			fail(w, 404, "no output %d on %q", n, ctl)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	post("/api/alloff", func(a *testerApp, b map[string]any, w http.ResponseWriter) {
		t.cancelRun()
		a.eng.ReleaseTestOutputs()
		a.eng.ClearTestToys()
		a.runner.OutputsOff()
		a.mgr.StopSimulation()
		a.eng.Clear()
		t.log.Infof("all off: test outputs, toy tests, DOFLinx outputs, game and ROM released")
		writeJSON(w, map[string]any{"ok": true})
	})
	post("/api/release", func(a *testerApp, b map[string]any, w http.ResponseWriter) {
		t.cancelRun()
		a.eng.ReleaseTestOutputs()
		a.eng.ClearTestToys()
		writeJSON(w, map[string]any{"ok": true})
	})
	post("/api/walk", func(a *testerApp, b map[string]any, w http.ResponseWriter) {
		ctl, from, to, ms, v := str(b, "controller"), num(b, "from"), num(b, "to"), num(b, "ms"), num(b, "value")
		if ms < 50 {
			ms = 400
		}
		stop := t.startRun()
		t.log.Infof("walking outputs %d..%d of %s", from, to, ctl)
		go func() {
			defer a.eng.ReleaseTestOutputs()
			for n := from; n <= to; n++ {
				if !a.eng.ForceOutput(ctl, n, byte(clamp(v, 1, 255))) {
					continue
				}
				select {
				case <-stop:
					return
				case <-time.After(time.Duration(ms) * time.Millisecond):
				}
				a.eng.ForceOutput(ctl, n, 0)
			}
			t.log.Infof("walk finished")
		}()
		writeJSON(w, map[string]any{"ok": true})
	})
	post("/api/stop", func(a *testerApp, b map[string]any, w http.ResponseWriter) {
		t.cancelRun()
		writeJSON(w, map[string]any{"ok": true})
	})
	post("/api/toy", func(a *testerApp, b map[string]any, w http.ResponseWriter) {
		name, pat := str(b, "name"), str(b, "pattern")
		t.cancelRun()
		if pat == "chase" {
			n := a.eng.StripSize(name)
			if n == 0 {
				fail(w, 400, "%q is not a LED strip toy", name)
				return
			}
			stop := t.startRun()
			r, g, bl, ms := num(b, "r"), num(b, "g"), num(b, "b"), clampMin(num(b, "ms"), 15, 40)
			go func() {
				for i := 0; ; i = (i + 1) % n {
					a.eng.TestToy(name, "chase", r, g, bl, i)
					select {
					case <-stop:
						a.eng.TestToy(name, "off", 0, 0, 0, 0)
						return
					case <-time.After(time.Duration(ms) * time.Millisecond):
					}
				}
			}()
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		if msg := a.eng.TestToy(name, pat, num(b, "r"), num(b, "g"), num(b, "b"), num(b, "index")); msg != "" {
			fail(w, 400, "%s", msg)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	post("/api/rom", func(a *testerApp, b map[string]any, w http.ResponseWriter) {
		rom := str(b, "rom")
		ch := make(chan bool, 1)
		a.eng.LoadRom(rom, func(ok bool) { ch <- ok })
		select {
		case ok := <-ch:
			writeJSON(w, map[string]any{"found": ok, "elements": t.elements(a)})
		case <-time.After(5 * time.Second):
			fail(w, 504, "engine did not answer")
		}
	})
	post("/api/clear", func(a *testerApp, b map[string]any, w http.ResponseWriter) {
		a.eng.Clear()
		writeJSON(w, map[string]any{"ok": true})
	})
	post("/api/elements", func(a *testerApp, b map[string]any, w http.ResponseWriter) {
		writeJSON(w, map[string]any{"elements": t.elements(a)})
	})
	post("/api/element", func(a *testerApp, b map[string]any, w http.ResponseWriter) {
		d, mode := strings.TrimSpace(str(b, "desc")), str(b, "mode")
		if d == "" {
			fail(w, 400, "no element")
			return
		}
		switch mode {
		case "pulse": // DOFLinx FF_DOF Exxx,-1 semantics
			a.eng.Pulse(d, -1)
		case "ms":
			a.eng.Pulse(d, clampMin(num(b, "ms"), 1, 500))
		case "on":
			a.eng.SendNamed(d, 255)
		case "off":
			a.eng.SendNamed(d, 0)
		case "value":
			a.eng.SendNamed(d, num(b, "value"))
		default:
			fail(w, 400, "unknown mode %q", mode)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	post("/api/linx", func(a *testerApp, b map[string]any, w http.ResponseWriter) {
		mode, text := str(b, "mode"), strings.TrimSpace(str(b, "text"))
		switch mode {
		case "action": // FF_DOF E101,200 | FF_DEV LF,500 ...
			for _, part := range strings.Split(text, "|") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				name, args := part, ""
				if i := strings.IndexAny(part, " \t"); i > 0 {
					name, args = part[:i], strings.TrimSpace(part[i+1:])
				}
				a.runner.Execute(linx.Action{Name: strings.ToUpper(name), Args: args})
			}
		case "trigger": // E,BUMPER01,1 | S,Play | G,12
			a.mgr.HandleTrigger(text)
		case "command": // OUTPUT_NOW_TIMER=1010,1500
			a.mgr.HandleCommand(text)
		default:
			fail(w, 400, "unknown mode %q", mode)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	post("/api/game", func(a *testerApp, b map[string]any, w http.ResponseWriter) {
		if str(b, "stop") != "" {
			a.mgr.StopSimulation()
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		if err := a.mgr.Simulate(str(b, "kind"), str(b, "table")); err != nil {
			fail(w, 400, "%v", err)
			return
		}
		time.Sleep(150 * time.Millisecond) // let FF_ROM load
		writeJSON(w, map[string]any{"ok": true, "events": a.runner.EventNames(), "elements": t.elements(a)})
	})
	post("/api/event", func(a *testerApp, b map[string]any, w http.ResponseWriter) {
		name, val := str(b, "name"), num(b, "value")
		a.runner.Event(name, val)
		writeJSON(w, map[string]any{"ok": true})
	})
	post("/api/events", func(a *testerApp, b map[string]any, w http.ResponseWriter) {
		writeJSON(w, map[string]any{"events": a.runner.EventNames()})
	})
}

func (t *tester) elements(a *testerApp) []dof.ElementInfo {
	var els []dof.ElementInfo
	a.eng.Sync(func() { els = a.eng.Elements() })
	if els == nil {
		els = []dof.ElementInfo{}
	}
	return els
}

func str(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case bool:
		if v {
			return "1"
		}
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func num(m map[string]any, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	}
	return 0
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// clampMin returns def when v < min.
func clampMin(v, min, def int) int {
	if v < min {
		return def
	}
	return v
}

// ---------------------------------------------------------------- info

type linkRow struct {
	Kind   string `json:"kind"` // dev, flasher, button, rgb, fixed
	Code   string `json:"code"`
	Addr   int    `json:"addr"`
	Device int    `json:"device"`
	Port   int    `json:"port"`
	Status string `json:"status"` // ok | other device | out of range
	Colour string `json:"colour,omitempty"`
	Mode   string `json:"mode,omitempty"`
}

func (t *tester) info(a *testerApp) map[string]any {
	cs, ts := a.eng.Inventory()
	var files []map[string]any
	for _, f := range a.eng.LoadedFiles() {
		files = append(files, map[string]any{"name": filepath.Base(f.Path), "ledwiz": f.Number, "tables": len(f.Tables), "warnings": len(f.Warnings), "firstWarnings": firstN(f.Warnings, 5)})
	}
	status := func(addr int) string {
		d, p := linx.Addr(addr)
		switch {
		case d != a.device:
			return fmt.Sprintf("device %d is not the PinOne (device %d): ignored", d, a.device)
		case p < 1 || p > dof.PinOneOutputs:
			return fmt.Sprintf("output %d does not exist (1-%d)", p, dof.PinOneOutputs)
		}
		return "ok"
	}
	rows := []linkRow{}
	l := a.runner.Links()
	add := func(kind string, m map[string][]linx.Link) {
		var keys []string
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			for _, ln := range m[k] {
				d, p := linx.Addr(ln.Addr)
				rows = append(rows, linkRow{Kind: kind, Code: k, Addr: ln.Addr, Device: d, Port: p, Status: status(ln.Addr), Colour: ln.Colour, Mode: ln.Mode})
			}
		}
	}
	add("dev", l.Dev)
	add("flasher", l.Flasher)
	add("button", l.Button)
	for _, ln := range l.RGB {
		d, p := linx.Addr(ln.Addr)
		rows = append(rows, linkRow{Kind: "rgb", Code: "RGB_OUTPUT", Addr: ln.Addr, Device: d, Port: p, Status: status(ln.Addr)})
	}
	k, tb := a.mgr.Current()
	return map[string]any{
		"loaded": true, "source": a.ld.source, "dry": a.dry, "iniDir": a.ld.dofCfg.IniFilesPath,
		"files": files, "controllers": cs, "toys": ts, "links": rows,
		"families": a.mgr.ProfileKinds(), "pinOneDevice": a.device,
		"romCount": len(a.eng.Roms()), "game": map[string]string{"kind": k, "table": tb},
		"processes": a.mgr.ProcessNames(),
	}
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ---------------------------------------------------------------- detect

// detect looks for configuration files in the usual places.
func detect() map[string]any {
	dirs := []string{"", "config"}
	if exe, _ := os.Executable(); exe != "" {
		d := filepath.Dir(exe)
		dirs = append(dirs, d, filepath.Join(d, "config"))
	}
	if cwd, _ := os.Getwd(); cwd != "" {
		dirs = append(dirs, cwd)
	}
	dirs = append(dirs, `C:\DirectOutput\Config`, `C:\DirectOutput`, `C:\DOFLinx\config`, `C:\DOFLinx`, `C:\DirectOutput\x86`)
	if v := os.Getenv("DOF_CONFIG"); v != "" {
		dirs = append(dirs, v)
	}
	seen := map[string]bool{}
	res := map[string][]string{"superdofJson": {}, "doflinxIni": {}, "cabinetXml": {}, "iniDirs": {}}
	for _, d := range dirs {
		abs, err := filepath.Abs(d)
		if err != nil || seen[strings.ToLower(abs)] {
			continue
		}
		seen[strings.ToLower(abs)] = true
		ents, err := os.ReadDir(abs)
		if err != nil {
			continue
		}
		hasDOF := false
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			l := strings.ToLower(e.Name())
			p := filepath.Join(abs, e.Name())
			switch {
			case l == "superdof.json":
				res["superdofJson"] = append(res["superdofJson"], p)
			case l == "doflinx.ini":
				res["doflinxIni"] = append(res["doflinxIni"], p)
			case l == "cabinet.xml":
				res["cabinetXml"] = append(res["cabinetXml"], p)
			case strings.HasPrefix(l, "directoutputconfig") && strings.HasSuffix(l, ".ini"), strings.HasPrefix(l, "ledcontrol") && strings.HasSuffix(l, ".ini"):
				hasDOF = true
			}
		}
		if hasDOF {
			res["iniDirs"] = append(res["iniDirs"], abs)
		}
	}
	return map[string]any{"found": res}
}
