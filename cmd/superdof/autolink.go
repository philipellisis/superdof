package main

import (
	"path/filepath"

	"superdof/internal/dof"
	"superdof/internal/linx"
)

// ledWiz numbers: PinOne unit n is 10+n, FTDI chip i is 40+i.
const (
	pinOneBase = 10
	ftdiBase   = 40
)

// linkBoards turns the loaded directoutputconfigNN.ini files into the boards
// whose toy-name comments define the DOFLinx devices. The PinOne unit 1 file
// (11) is the DOFLinx device PINONE_DEVICE; FTDI files take the devices from
// FTDI_DEVICE (default 2) upwards in file order. ids lists the FTDI chip ids
// in that order.
func linkBoards(cfg *linx.Config, files []*dof.LedControlFile) (boards []linx.Board, ftdiIDs []int) {
	pin := cfg.Int("PINONE_DEVICE", 1)
	ft := cfg.Int("FTDI_DEVICE", 2)
	for _, f := range files {
		if len(f.ToyNames) == 0 {
			continue
		}
		b := linx.Board{Label: filepath.Base(f.Path), Names: f.ToyNames}
		switch {
		case f.Number == pinOneBase+1:
			b.Device = pin
		case f.Number > ftdiBase && f.Number < ftdiBase+10, f.Number == ftdiBase:
			b.Device = ft + len(ftdiIDs)
			ftdiIDs = append(ftdiIDs, f.Number-ftdiBase)
		default:
			continue
		}
		boards = append(boards, b)
	}
	return boards, ftdiIDs
}

// applyAutoLinks fills in the LINK_ devices that DOFLinx.INI does not define
// from the toy names of the DOF ini files, and returns what it added.
func applyAutoLinks(lg linx.Logger, cfg *linx.Config, eng *dof.Engine) (added []linx.Derived, ftdiIDs []int) {
	if !cfg.Bool("AUTO_LINKS", true) {
		return nil, nil
	}
	boards, ids := linkBoards(cfg, eng.LoadedFiles())
	if len(boards) == 0 {
		lg.Infof("no toy names found under [Config DOF] in the directoutputconfig ini files; LINK_ devices come from the DOFLinx config only")
		return nil, ids
	}
	added, notes := linx.DeriveLinks(cfg, boards)
	for _, n := range notes {
		lg.Warnf("auto link: %s", n)
	}
	for _, d := range added {
		lg.Infof("auto link: %s=%s  (%q, %s)", d.Key, d.Value, d.Toy, d.Board)
	}
	lg.Infof("DOFLinx devices derived from the toy names in %d ini file(s): %d links", len(boards), len(added))
	return added, ids
}
