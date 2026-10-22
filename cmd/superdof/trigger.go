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
	games := linx.FindGames(extraGames...)
	rep.Source = linx.FindTriggerDLL(append([]string{dllHint}, hints...)...)
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
		} else if st, err := linx.InstallTrigger(rep.Source, g); err != nil {
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
