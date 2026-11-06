package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"superdof/internal/linx"
)

type logf interface {
	Infof(string, ...any)
	Warnf(string, ...any)
}

// installFuturePinball puts DOFLinx.vbs into every Future Pinball folder found.
// It reports whether at least one Future Pinball was found and all installs worked.
func installFuturePinball(lg logf, extra []string) (found, ok bool) {
	dirs := linx.FindFuturePinball(extra...)
	ok = true
	for _, d := range dirs {
		st, err := linx.InstallFPScript(fpScript, d)
		if err != nil {
			lg.Warnf("Future Pinball %s: DOFLinx.vbs not installed: %v", d, err)
			ok = false
			continue
		}
		lg.Infof("Future Pinball %s: Scripts/DOFLinx.vbs %s", d, st)
	}
	return len(dirs) > 0, ok
}

// installVPXBridge puts the B2S bridge plugin into every B2S server folder found.
func installVPXBridge(lg logf, extra []string) (found, ok bool) {
	if len(bridgeDLL) == 0 {
		return false, false
	}
	dirs := linx.FindB2SServers(extra...)
	ok = true
	for _, d := range dirs {
		res, err := linx.InstallB2S(bridgeDLL, d)
		for _, a := range res.Actions {
			lg.Infof("B2S server %s: %s", d, a)
		}
		if err != nil {
			lg.Warnf("B2S server %s: bridge not installed: %v", d, err)
			ok = false
			continue
		}
		if len(res.Actions) == 0 {
			lg.Infof("B2S server %s: already set up", d)
		}
	}
	return len(dirs) > 0, ok
}

func cmdRestoreVPX(args []string) {
	lg := &logger{w: os.Stdout}
	dirs := linx.FindB2SServers(args...)
	if len(dirs) == 0 {
		lg.Warnf("no B2S server found; pass its folder")
		os.Exit(1)
	}
	for _, d := range dirs {
		acts := linx.RestoreB2S(d)
		for _, a := range acts {
			lg.Infof("B2S server %s: %s", d, a)
		}
		if len(acts) == 0 {
			lg.Infof("B2S server %s: nothing to restore", d)
		}
	}
	lg.Infof("DirectOutput is active again for VPX; stop superdof so both do not drive the PinOne")
}

func listOrNil(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return []string{s}
}

func cmdInstallFP(args []string) {
	fs := flag.NewFlagSet("install-fp", flag.ExitOnError)
	dir := fs.String("dir", "", "Future Pinball folder (found automatically when omitted)")
	fs.Parse(args)
	lg := &logger{w: os.Stdout}
	found, ok := installFuturePinball(lg, listOrNil(*dir))
	if !found {
		lg.Warnf("Future Pinball.exe was not found; pass -dir \"D:/Games/Future Pinball\"")
		os.Exit(1)
	}
	if !ok {
		os.Exit(1)
	}
	fmt.Println("Tables that use DOFLinx.vbs (LoadExternalScript(\"DOFLinx.vbs\")) now link to superdof while it is running.")
}

// cmdSetup installs everything the games need: the Pinball FX trigger DLL, the
// Future Pinball script and the VPX bridge.
func cmdSetup(args []string) {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	fpDir := fs.String("fp", "", "Future Pinball folder (found automatically when omitted)")
	b2s := fs.String("b2s", "", "B2S server folder (found automatically when omitted)")
	fs.Parse(args)
	lg := &logger{w: os.Stdout}
	failed := false

	lg.Infof("-- Pinball FX / Pinball M / Infected Mushroom")
	rep := installTriggers("", nil)
	rep.log(lg)
	if len(rep.Games) == 0 {
		lg.Infof("(none installed)")
	}
	for _, g := range rep.Games {
		failed = failed || !g.OK
	}

	lg.Infof("-- Future Pinball")
	if f, ok := installFuturePinball(lg, listOrNil(*fpDir)); !f {
		lg.Infof("Future Pinball not found (use -fp)")
	} else {
		failed = failed || !ok
	}

	lg.Infof("-- Visual Pinball (B2S server)")
	if f, ok := installVPXBridge(lg, listOrNil(*b2s)); !f {
		lg.Infof("no B2S server found (use -b2s)")
	} else {
		failed = failed || !ok
	}
	lg.Infof("-- PinUP Popper")
	if f, ok := installPopper(lg, nil, true); !f {
		lg.Infof("PinUP Popper not found (use install-popper to register the DOF COM object anyway)")
	} else {
		failed = failed || !ok
	}
	if failed {
		lg.Warnf("some installs failed (see above); writing into Program Files may need an administrator shell once")
		os.Exit(1)
	}
	lg.Infof("done: start superdof and leave it running")
}

// installPopper registers the DirectOutput.ComObject replacement that PinUP
// Popper uses for DOF. With onlyIfFound it does nothing unless Popper is found.
func installPopper(lg logf, extra []string, onlyIfFound bool) (found, ok bool) {
	dirs := linx.FindPopper(extra...)
	if onlyIfFound && len(dirs) == 0 {
		return false, true
	}
	st, err := linx.InstallComObject(comDLL)
	if err != nil {
		lg.Warnf("PinUP Popper: DOF COM object not installed: %v", err)
		return len(dirs) > 0, false
	}
	for _, d := range dirs {
		lg.Infof("PinUP Popper found in %s", d)
	}
	lg.Infof("DirectOutput.ComObject (used by Popper's DOF) now forwards to superdof: %s, registered for the current user (undo: superdof restore-popper)", st)
	if len(dirs) > 0 {
		lg.Infof("In Popper set useDOF = true (PUPMenuScriptSysOptions.txt / Popper setup) so the menu sends DOF events")
	}
	return len(dirs) > 0, true
}

func cmdInstallPopper(args []string) {
	fs := flag.NewFlagSet("install-popper", flag.ExitOnError)
	dir := fs.String("dir", "", "PinUP Popper folder (only used for the hint)")
	fs.Parse(args)
	lg := &logger{w: os.Stdout}
	if _, ok := installPopper(lg, listOrNil(*dir), false); !ok {
		os.Exit(1)
	}
}

func cmdRestorePopper() {
	lg := &logger{w: os.Stdout}
	if err := linx.RestoreComObject(); err != nil {
		lg.Warnf("%v", err)
		os.Exit(1)
	}
	lg.Infof("the per-user DirectOutput.ComObject registration was removed; a DirectOutput installation is used again")
}
