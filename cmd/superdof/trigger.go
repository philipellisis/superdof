package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"superdof/internal/linx"
)

// triggerReport describes what installTriggers found and did.
type triggerReport struct {
	Source string
	Games  []triggerGame
	Err    string
}

type triggerGame struct {
	Exe    string
	Status string // installed, updated, up to date, or an error text
	OK     bool
}

// installTriggers copies DOFLinxTrigger.dll next to every installed Pinball
// FX / M / IMP executable (Steam libraries plus extra folders).
func installTriggers(dllHint string, extraGames []string, hints ...string) triggerReport {
	var rep triggerReport
	var embedded bool
	games := linx.FindGames(extraGames...)
	// an explicit file wins; otherwise the DLL built into superdof is used, and
	// only a build without it searches the usual folders
	switch {
	case dllHint != "":
		rep.Source = linx.FindTriggerDLL(dllHint)
	case len(triggerDLL) > 0:
		rep.Source, embedded = "built into superdof", true
	default:
		rep.Source = linx.FindTriggerDLL(hints...)
	}
	if len(games) == 0 {
		rep.Err = "no Pinball FX / Pinball M / Infected Mushroom Pinball installation found in the Steam libraries"
		return rep
	}
	for _, g := range games {
		tg := triggerGame{Exe: g.Exe}
		if rep.Source == "" {
			if _, err := os.Stat(filepath.Join(g.Dir, linx.TriggerDLL)); err == nil {
				tg.Status, tg.OK = "already present", true
			} else {
				tg.Status = "missing"
			}
		} else if st, err := install(rep.Source, embedded, g); err != nil {
			tg.Status = err.Error()
		} else {
			tg.Status, tg.OK = st, true
		}
		rep.Games = append(rep.Games, tg)
	}
	if rep.Source == "" {
		rep.Err = linx.TriggerDLL + " was not found. Put it next to superdof.exe (it is in the DOFLinx download) or pass -dll"
	}
	return rep
}

func install(src string, embedded bool, g linx.GameInstall) (string, error) {
	if embedded {
		return linx.InstallTriggerData(triggerDLL, g)
	}
	return linx.InstallTrigger(src, g)
}

func (r triggerReport) log(lg interface {
	Infof(string, ...any)
	Warnf(string, ...any)
}) {
	if r.Source != "" {
		lg.Infof("trigger DLL source: %s", r.Source)
	}
	for _, g := range r.Games {
		if g.OK {
			lg.Infof("%s: trigger DLL %s", g.Exe, g.Status)
		} else {
			lg.Warnf("%s: trigger DLL not installed: %s", g.Exe, g.Status)
		}
	}
	if r.Err != "" {
		lg.Warnf("%s", r.Err)
	}
}

func cmdInstallTrigger(args []string) {
	fs := flag.NewFlagSet("install-trigger", flag.ExitOnError)
	dll := fs.String("dll", "", "DOFLinxTrigger.dll (default: searched next to superdof.exe, in the DOFLinx folders and in Downloads)")
	game := fs.String("game", "", "extra game folder or *-Win64-Shipping.exe (Steam libraries are searched automatically)")
	fs.Parse(args)
	lg := &logger{w: os.Stdout}
	var extra []string
	if *game != "" {
		extra = append(extra, *game)
	}
	rep := installTriggers(*dll, extra)
	rep.log(lg)
	for _, g := range rep.Games {
		if !g.OK {
			os.Exit(1)
		}
	}
	if rep.Err != "" {
		os.Exit(1)
	}
	fmt.Println("Now start the game: Settings > Cabinet > turn cabinet mode on > DOFLinx tab > enable, with superdof running.")
}

// defaultLogPath is the log file used when superdof runs without a console. The
// previous log is kept as superdof.log.1.
func defaultLogPath() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "SuperDOF")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		dir = os.TempDir()
	}
	p := filepath.Join(dir, "superdof.log")
	if _, err := os.Stat(p); err == nil {
		os.Remove(p + ".1")
		os.Rename(p, p+".1")
	}
	return p
}
