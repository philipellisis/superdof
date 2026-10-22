// Command superdof is the unified DOF + DOFLinx force feedback service for
// the PinOne board (plus Wemos/Teensy addressable LED strips and FTDI relay
// chips). It reads superdof.json (or the legacy DOFLinx.INI + cabinet.xml),
// the DOFLinx .FX/.FX2/.Linx files and the DOF directoutputconfig*.ini files,
// listens for the games and VPX, and drives the hardware.
//
// Sub commands:
//
//	superdof [flags]                    run the service
//	superdof convert [flags] -o FILE    convert DOFLinx.INI + cabinet.xml to JSON
//	superdof install-vpx B2S_FOLDER     install the B2S server plugin for VPX
//	superdof install-trigger [-dll F]   copy DOFLinxTrigger.dll next to Pinball FX / M / IMP
//	superdof devices                    list PinOne / serial / FTDI devices
//	superdof test [flags]               browser based output / toy / ROM / DOFLinx tester
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"superdof/internal/config"
	"superdof/internal/dof"
	"superdof/internal/ftdi"
	"superdof/internal/linx"
	"superdof/internal/pinone"
	"superdof/internal/serial"
)

const version = "0.3.0"

type logger struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *logger) out(level, f string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, "%s %-5s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), level, fmt.Sprintf(f, a...))
}
func (l *logger) Infof(f string, a ...any) { l.out("INFO", f, a...) }
func (l *logger) Warnf(f string, a ...any) { l.out("WARN", f, a...) }

// sink maps DOFLinx DOOO addresses onto the PinOne board.
type sink struct {
	cab    *dof.Cabinet
	device int
	unit   int
	log    linx.Logger
	warned sync.Map
}

func (s *sink) SetOutput(device, port int, key any, v byte) bool {
	if device != s.device {
		if _, seen := s.warned.LoadOrStore(device, true); !seen {
			s.log.Warnf("DOFLinx device %d is not the PinOne board (pinOneDevice=%d); outputs on it are ignored", device, s.device)
		}
		return false
	}
	o := s.cab.Output(fmt.Sprintf("PinOne Controller %02d.%d", s.unit, port))
	if o == nil {
		if _, seen := s.warned.LoadOrStore(device*1000+port, true); !seen {
			s.log.Warnf("output %d on device %d does not exist on a PinOne (1-%d)", port, device, dof.PinOneOutputs)
		}
		return false
	}
	o.SetExternal(key, v)
	return true
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "convert":
			cmdConvert(os.Args[2:])
			return
		case "install-vpx":
			cmdInstallVPX(os.Args[2:])
			return
		case "test", "tester":
			cmdTester(os.Args[2:])
			return
		case "install-trigger":
			cmdInstallTrigger(os.Args[2:])
			return
		case "devices":
			cmdDevices()
			return
		}
	}
	runService(os.Args[1:])
}

// loaded is the resolved configuration.
type loaded struct {
	lcfg     *linx.Config
	dofCfg   dof.Config
	maps     string // tablemappings.xml
	tableIDs map[string]string
	source   string
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func firstExisting(ps ...string) string {
	for _, p := range ps {
		if p != "" && fileExists(p) {
			return p
		}
	}
	return ""
}

func findINI() string {
	cands := []string{"DOFLinx.INI", filepath.Join("config", "DOFLinx.INI")}
	if exe, _ := os.Executable(); exe != "" {
		d := filepath.Dir(exe)
		cands = append(cands, filepath.Join(d, "DOFLinx.INI"), filepath.Join(d, "config", "DOFLinx.INI"))
	}
	return firstExisting(cands...)
}

// loadConfig resolves the configuration from JSON or the legacy files.
func loadConfig(lg *logger, jsonPath, iniPath, dofDir, cabinetFile, port string) (*loaded, error) {
	out := &loaded{tableIDs: map[string]string{}}
	if jsonPath == "" && iniPath == "" {
		jsonPath = config.FindDefault()
	}
	if jsonPath != "" {
		f, err := config.Load(jsonPath)
		if err != nil {
			return nil, err
		}
		dir := filepath.Dir(jsonPath)
		out.source = jsonPath
		out.lcfg = f.LinxConfig(dir)
		out.lcfg.Path = jsonPath
		out.dofCfg.IniFilesPath = f.DOF.IniFilesPath
		out.dofCfg.Cabinet = f.Cabinet
		out.dofCfg.PinOne = f.PinOne
		out.dofCfg.MinCommandIntervalMs = f.DOF.PinOneMinIntervalMs
		out.dofCfg.Options = dof.ConfiguratorOptions{MinDurationMs: f.DOF.MinEffectDurationMs, MinRGBDurationMs: f.DOF.MinRgbEffectDurationMs}
		out.maps = f.DOF.TableMappings
		out.tableIDs = f.DOFLinx.Tables
		if out.dofCfg.IniFilesPath == "" {
			out.dofCfg.IniFilesPath = dir
		}
		if !filepath.IsAbs(out.dofCfg.IniFilesPath) {
			out.dofCfg.IniFilesPath = filepath.Join(dir, out.dofCfg.IniFilesPath)
		}
	} else {
		if iniPath == "" {
			iniPath = findINI()
		}
		cfg := linx.NewConfig()
		if iniPath != "" {
			c, err := linx.ParseINI(iniPath)
			if err != nil {
				return nil, err
			}
			cfg = c
			out.source = iniPath
		} else {
			lg.Warnf("no superdof.json or DOFLinx.INI found; using defaults")
			cfg.Dir, _ = os.Getwd()
		}
		out.lcfg = cfg
		gc := &dof.GlobalConfig{}
		if g := cfg.Get("DIRECTOUTPUTGLOBAL"); g != "" {
			p := firstExisting(cfg.Resolve(g), filepath.Join(filepath.Dir(cfg.Resolve(g)), "GlobalConfig_B2SServer.xml"))
			if v, err := dof.LoadGlobalConfig(p); err == nil {
				gc = v
				lg.Infof("DOF global config: %s", p)
			} else {
				lg.Warnf("DIRECTOUTPUTGLOBAL %s: %v", g, err)
			}
		}
		out.dofCfg.IniFilesPath = firstNonEmpty(gc.IniFilesPath, cfg.Dir)
		cf := firstNonEmpty(cabinetFile, gc.CabinetConfigFilePattern)
		if strings.ContainsAny(cf, "{*") {
			cf = ""
		}
		if cf == "" {
			cf = firstExisting(filepath.Join(out.dofCfg.IniFilesPath, "cabinet.xml"))
		}
		out.dofCfg.CabinetFile = cf
		out.dofCfg.PinOne = gc.PinOneComPort
		out.dofCfg.MinCommandIntervalMs = gc.PinOneMinCommandInterval
		out.dofCfg.Options = dof.ConfiguratorOptions{MinDurationMs: gc.MinEffectDurationMs, MinRGBDurationMs: gc.MinRGBEffectDurationMs}
	}
	if dofDir != "" {
		out.dofCfg.IniFilesPath = dofDir
	}
	if port != "" && port != "auto" || out.dofCfg.PinOne == "" {
		out.dofCfg.PinOne = firstNonEmpty(port, "auto")
	}
	return out, nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func runService(args []string) {
	fs := flag.NewFlagSet("superdof", flag.ExitOnError)
	var (
		jsonPath = fs.String("config", "", "superdof.json (default: ./superdof.json, ./config/superdof.json, next to the exe)")
		iniPath  = fs.String("ini", "", "legacy DOFLinx.INI (used when no JSON config is given)")
		dofDir   = fs.String("dof-config", "", "folder with directoutputconfig*.ini (overrides the config)")
		cabinet  = fs.String("cabinet", "", "legacy DOF cabinet.xml")
		portFlag = fs.String("pinone", "auto", "PinOne connection: auto, hid or a serial port (COM4, /dev/ttyACM0)")
		dryRun   = fs.Bool("dry-run", false, "do not open any hardware; log output changes")
		table    = fs.String("table", "", "force a table file base name")
		rom      = fs.String("rom", "", "load this DOF ROM config at startup (testing)")
		logFile  = fs.String("log", "", "log file (default: stderr)")
		uiAddr   = fs.String("ui", "127.0.0.1:8765", "address of the game id page (loopback), or off")
		check    = fs.Bool("check", false, "load and validate the configuration, then exit")
		testOut  = fs.Int("test-outputs", 0, "light PinOne outputs 1..N one after another, then exit")
		showVer  = fs.Bool("version", false, "print version and exit")
	)
	fs.Parse(args)
	if *showVer {
		fmt.Println("superdof", version)
		return
	}
	lg := &logger{w: os.Stderr}
	if *logFile != "" {
		if f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
			lg.w = io.MultiWriter(os.Stderr, f)
			defer f.Close()
		}
	}
	lg.Infof("SuperDOF %s starting", version)
	if *testOut > 0 {
		runOutputTest(lg, *portFlag, *testOut)
		return
	}

	ld, err := loadConfig(lg, *jsonPath, *iniPath, *dofDir, *cabinet, *portFlag)
	if err != nil {
		lg.Warnf("configuration: %v", err)
		os.Exit(1)
	}
	if ld.source != "" {
		lg.Infof("config: %s", ld.source)
	}
	cfg := ld.lcfg
	if *table != "" {
		cfg.Set("FORCE_TABLE", *table)
	}
	ld.dofCfg.DryRun = *dryRun
	ld.dofCfg.Log = lg
	eng := dof.New(ld.dofCfg)
	if err := eng.Start(); err != nil {
		lg.Warnf("DOF engine: %v", err)
		os.Exit(1)
	}
	defer eng.Stop()

	runner, mgr := newLinx(lg, cfg, eng, ld)

	if !*check && !*dryRun && cfg.Bool("AUTO_INSTALL_TRIGGER", true) {
		installTriggers(cfg.Get("TRIGGER_DLL"), cfg.List("GAME_DIR"), cfg.Dir).log(lg)
	}

	if *check {
		printCheck(lg, cfg, eng, runner, mgr, ld)
		return
	}
	if *rom != "" {
		eng.LoadRom(*rom, nil)
	}

	maps := map[string]string{}
	if mp := firstExisting(ld.maps, filepath.Join(ld.dofCfg.IniFilesPath, "tablemappings.xml"), filepath.Join(cfg.Dir, "tablemappings.xml")); mp != "" {
		if m, err := dof.LoadTableMappings(mp); err == nil {
			maps = m
			lg.Infof("table mappings: %d from %s", len(m), mp)
		}
	}

	quit := make(chan struct{})
	var quitOnce sync.Once
	doQuit := func() { quitOnce.Do(func() { close(quit) }) }
	var stops []func()
	listen := func(name string, h func(c io.ReadWriteCloser)) {
		stop, err := linx.ListenPipe(name, h)
		if err != nil {
			lg.Warnf("pipe %s: %v", name, err)
			return
		}
		lg.Infof("listening on pipe %s", name)
		stops = append(stops, stop)
	}
	listen("DOFLinxTrigger", func(c io.ReadWriteCloser) {
		var carry []byte
		buf := make([]byte, 4096)
		for {
			n, err := c.Read(buf)
			if n > 0 {
				for _, rec := range linx.SplitTriggerRecords(buf[:n], &carry) {
					mgr.HandleTrigger(rec)
				}
			}
			if err != nil {
				return
			}
		}
	})
	listen("DOFLINX", func(c io.ReadWriteCloser) {
		sc := bufio.NewScanner(c)
		for sc.Scan() {
			if mgr.HandleCommand(sc.Text()) {
				doQuit()
			}
		}
	})
	br := &linx.Bridge{DOF: eng, Runner: runner, Log: lg, Version: version, RomFor: func(table, rom string) string {
		if rom != "" {
			return rom
		}
		return maps[strings.ToLower(strings.TrimSuffix(filepath.Base(table), filepath.Ext(table)))]
	}}
	listen("SuperDOF", br.Serve)

	stopUI := serveLearnUI(*uiAddr, mgr, lg)
	defer stopUI()
	go mgr.Run()
	lg.Infof("ready; watching for: %s", strings.Join(mgr.ProcessNames(), ", "))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	select {
	case <-sig:
	case <-quit:
	}
	lg.Infof("shutting down")
	for _, s := range stops {
		s()
	}
	mgr.Close()
}

func printCheck(lg *logger, cfg *linx.Config, eng *dof.Engine, r *linx.Runner, m *linx.Manager, ld *loaded) {
	lg.Infof("DOF ini folder: %s", ld.dofCfg.IniFilesPath)
	for _, f := range eng.LoadedFiles() {
		lg.Infof("  %s: %d table configs, %d colors, %d warnings", filepath.Base(f.Path), len(f.Tables), len(f.Colors), len(f.Warnings))
	}
	for _, c := range eng.Cabinet().Controllers {
		lg.Infof("controller: %s (%d outputs)", c.Name(), len(c.Outputs()))
	}
	l := r.Links()
	lg.Infof("DOFLinx links: %d devices, %d flashers, %d buttons, %d rgb outputs", len(l.Dev), len(l.Flasher), len(l.Button), len(l.RGB))
	lg.Infof("watching processes: %s", strings.Join(m.ProcessNames(), ", "))
	if t, err := pinone.Open("auto"); err == nil {
		lg.Infof("PinOne board: %s", t.Describe())
		t.Close()
	} else {
		lg.Warnf("PinOne board: %v", err)
	}
}

func runOutputTest(lg *logger, port string, n int) {
	t, err := pinone.Open(port)
	if err != nil {
		lg.Warnf("%v", err)
		os.Exit(1)
	}
	defer t.Close()
	lg.Infof("connected via %s; walking outputs 1..%d", t.Describe(), n)
	var vals [dof.PinOneOutputs]byte
	flush := func() {
		for b := 0; b*7 < dof.PinOneOutputs; b++ {
			p := make([]byte, 9)
			p[1] = byte(200 + b)
			end := b*7 + 7
			if end > dof.PinOneOutputs {
				end = dof.PinOneOutputs
			}
			copy(p[2:], vals[b*7:end])
			if err := t.Write(p); err != nil {
				lg.Warnf("write: %v", err)
				os.Exit(1)
			}
		}
	}
	for i := 0; i < n && i < dof.PinOneOutputs; i++ {
		vals[i] = 255
		flush()
		lg.Infof("output %d on", i+1)
		time.Sleep(400 * time.Millisecond)
		vals[i] = 0
		flush()
	}
}

// ---------------------------------------------------------------- convert

func cmdConvert(args []string) {
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	iniPath := fs.String("ini", "", "DOFLinx.INI")
	cabinet := fs.String("cabinet", "", "DOF cabinet.xml (default: next to the DOF ini files)")
	outPath := fs.String("o", "superdof.json", "output file")
	fs.Parse(args)
	lg := &logger{w: os.Stderr}
	ini := firstNonEmpty(*iniPath, findINI())
	if ini == "" {
		lg.Warnf("DOFLinx.INI not found; pass -ini")
		os.Exit(1)
	}
	cfg, err := linx.ParseINI(ini)
	if err != nil {
		lg.Warnf("%v", err)
		os.Exit(1)
	}
	var gc *dof.GlobalConfig
	if g := cfg.Get("DIRECTOUTPUTGLOBAL"); g != "" {
		p := firstExisting(cfg.Resolve(g), filepath.Join(filepath.Dir(cfg.Resolve(g)), "GlobalConfig_B2SServer.xml"))
		if p != "" {
			gc, _ = dof.LoadGlobalConfig(p)
		}
	}
	cabFile := *cabinet
	if cabFile == "" {
		dir := cfg.Dir
		if gc != nil && gc.IniFilesPath != "" {
			dir = gc.IniFilesPath
		}
		cabFile = firstExisting(filepath.Join(dir, "cabinet.xml"))
	}
	var spec *dof.CabinetSpec
	if cabFile != "" {
		spec, err = dof.LoadCabinetXML(cabFile, lg)
		if err != nil {
			lg.Warnf("cabinet.xml: %v", err)
		}
	}
	f := config.FromINI(cfg, spec, gc)
	if err := f.Save(*outPath); err != nil {
		lg.Warnf("%v", err)
		os.Exit(1)
	}
	lg.Infof("wrote %s (from %s%s)", *outPath, ini, map[bool]string{true: " + " + cabFile, false: ""}[cabFile != ""])
}

// ---------------------------------------------------------------- devices

func cmdDevices() {
	lg := &logger{w: os.Stdout}
	if t, err := pinone.Open("auto"); err == nil {
		lg.Infof("PinOne: %s", t.Describe())
		t.Close()
	} else {
		lg.Infof("PinOne: %v", err)
	}
	ports := serial.PortNames()
	lg.Infof("serial ports: %s", strings.Join(ports, ", "))
	devs, err := ftdi.List()
	if err != nil {
		lg.Infof("FTDI: %v", err)
	}
	for i, d := range devs {
		lg.Infof("FTDI chip %d: serial %s, %s", i, d.Serial, d.Description)
	}
}

// ---------------------------------------------------------------- vpx

func cmdInstallVPX(args []string) {
	lg := &logger{w: os.Stderr}
	if len(args) != 1 {
		lg.Warnf("usage: superdof install-vpx <B2S server folder>")
		os.Exit(2)
	}
	if len(bridgeDLL) == 0 {
		lg.Warnf("this build has no embedded bridge plugin")
		os.Exit(1)
	}
	dir := args[0]
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		lg.Warnf("%s is not a folder", dir)
		os.Exit(1)
	}
	dst := filepath.Join(dir, "B2SServerDirectOutputPlugin.dll")
	if fileExists(dst) {
		bak := dst + ".bak"
		if !fileExists(bak) {
			if err := os.Rename(dst, bak); err != nil {
				lg.Warnf("backup: %v", err)
				os.Exit(1)
			}
			lg.Infof("backed up the existing plugin to %s", bak)
		}
	}
	if err := os.WriteFile(dst, bridgeDLL, 0o644); err != nil {
		lg.Warnf("%v", err)
		os.Exit(1)
	}
	lg.Infof("installed %s", dst)
	if !fileExists(filepath.Join(dir, "B2SServerPluginInterface.dll")) {
		lg.Warnf("B2SServerPluginInterface.dll was not found in that folder; is it the B2S server folder?")
	}
	lg.Infof("DirectOutput.dll in that folder is no longer used and can stay or be removed")
}

// newLinx builds the DOFLinx layer (runner + game manager) on top of a DOF engine.
func newLinx(lg linx.Logger, cfg *linx.Config, eng *dof.Engine, ld *loaded) (*linx.Runner, *linx.Manager) {
	snk := &sink{cab: eng.Cabinet(), device: cfg.Int("PINONE_DEVICE", 1), unit: 1, log: lg}
	colours := linx.DefaultColours()
	if cf := cfg.Get("COLOUR_FILE"); cf != "" {
		for _, cand := range []string{cfg.Resolve(cf), filepath.Join(cfg.Dir, filepath.Base(strings.ReplaceAll(cf, "\\", "/"))), filepath.Join(cfg.Dir, "config", "Colours.ini")} {
			if c, err := linx.LoadColours(cand); err == nil {
				colours = c
				lg.Infof("colours: %s (%d)", cand, len(c))
				break
			}
		}
	} else if c, err := linx.LoadColours(filepath.Join(cfg.Dir, "Colours.ini")); err == nil {
		colours = c
	}
	runner := linx.NewRunner(cfg, eng, snk, colours, lg)
	mgr := linx.NewManager(cfg, runner, lg)
	mgr.SetTableIDs(ld.tableIDs)
	if mi := filepath.Join(cfg.Dir, "Menu.ini"); fileExists(mi) {
		mgr.LoadMenu(mi)
	}
	return runner, mgr
}
