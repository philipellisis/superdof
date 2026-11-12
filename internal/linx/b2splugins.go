package linx

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The B2S server (2025 builds) does not load B2SServerDirectOutputPlugin.dll
// from its own folder. It loads every plugin folder that a shortcut (*.lnk)
// in its Plugins (32 bit VPX) and Plugins64 (64 bit VPX) folders points to; DOF
// is activated with a shortcut DirectOutput.lnk -> C:\DirectOutput\x86 / x64.
// SuperDOF therefore keeps the bridge in a folder of its own and adds a
// SuperDOF.lnk shortcut to both folders, and switches the DOF shortcuts off.

const (
	b2sShortcut   = "SuperDOF.lnk"
	disabledSufix = ".superdof-off"
)

// B2SPluginDir is where the bridge plugin is kept.
func B2SPluginDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, "SuperDOF", "B2SPlugin")
}

// b2sPluginFolders returns the Plugins / Plugins64 folders of a B2S server.
func b2sPluginFolders(server string) []string {
	var out []string
	for _, n := range []string{"Plugins", "Plugins64"} {
		p := filepath.Join(server, n)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

func isDOFFolder(dir string) bool {
	for _, f := range []string{B2SPlugin, "DirectOutput.dll"} {
		if fileThere(filepath.Join(dir, f)) {
			return true
		}
	}
	return false
}

// B2SResult describes what InstallB2S did for one server folder.
type B2SResult struct {
	Server  string
	Actions []string
}

// InstallB2S makes the B2S server at dir load the SuperDOF bridge: the bridge
// is written to B2SPluginDir, a SuperDOF.lnk shortcut is added to every
// Plugins folder, and shortcuts to DirectOutput are renamed (*.lnk.superdof-off)
// so DOF does not drive the PinOne at the same time. Older servers without
// Plugins folders get the plugin copied into their own folder instead.
func InstallB2S(data []byte, dir string) (B2SResult, error) {
	res := B2SResult{Server: dir}
	folders := b2sPluginFolders(dir)
	if len(folders) == 0 {
		st, err := InstallB2SPlugin(data, dir)
		if err == nil {
			res.Actions = append(res.Actions, fmt.Sprintf("%s %s (no Plugins folders: old style install)", B2SPlugin, st))
		}
		return res, err
	}
	// an earlier version copied the bridge over the plugin in the server folder;
	// the server never loads it there, so put the original back
	if root := filepath.Join(dir, B2SPlugin); fileThere(root) {
		if old, err := os.ReadFile(root); err == nil && bytes.Equal(old, data) {
			os.Remove(root)
			if bak := root + ".bak"; fileThere(bak) && os.Rename(bak, root) == nil {
				res.Actions = append(res.Actions, "restored the original "+root)
			} else {
				res.Actions = append(res.Actions, "removed the bridge copy "+root)
			}
		}
	}
	pdir := B2SPluginDir()
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		return res, permHint(err, pdir)
	}
	st, err := InstallB2SPlugin(data, pdir)
	if err != nil {
		return res, err
	}
	// the bridge is not DirectOutput: no backup of a previous bridge is needed
	os.Remove(filepath.Join(pdir, B2SPlugin+".bak"))
	if st != "up to date" {
		res.Actions = append(res.Actions, fmt.Sprintf("bridge plugin %s in %s", st, pdir))
	}
	for _, pf := range folders {
		ents, _ := os.ReadDir(pf)
		have := false
		for _, e := range ents {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".lnk") {
				continue
			}
			lnk := filepath.Join(pf, e.Name())
			target, err := ShortcutTarget(lnk)
			if err != nil {
				continue
			}
			switch {
			case samePath(target, pdir):
				have = true
			case isDOFFolder(target):
				if err := os.Rename(lnk, lnk+disabledSufix); err != nil {
					return res, permHint(err, pf)
				}
				res.Actions = append(res.Actions, fmt.Sprintf("%s switched off (renamed to %s%s)", lnk, e.Name(), disabledSufix))
			}
		}
		if !have {
			if err := CreateShortcut(filepath.Join(pf, b2sShortcut), pdir); err != nil {
				return res, permHint(err, pf)
			}
			res.Actions = append(res.Actions, fmt.Sprintf("%s -> %s added", filepath.Join(pf, b2sShortcut), pdir))
		}
	}
	return res, nil
}

// RestoreB2S undoes InstallB2S for a server folder: the SuperDOF shortcuts are
// removed and the DirectOutput shortcuts come back.
func RestoreB2S(dir string) []string {
	var out []string
	for _, pf := range b2sPluginFolders(dir) {
		if err := os.Remove(filepath.Join(pf, b2sShortcut)); err == nil {
			out = append(out, "removed "+filepath.Join(pf, b2sShortcut))
		}
		m, _ := filepath.Glob(filepath.Join(pf, "*.lnk"+disabledSufix))
		for _, off := range m {
			on := strings.TrimSuffix(off, disabledSufix)
			if !fileThere(on) && os.Rename(off, on) == nil {
				out = append(out, "restored "+on)
			}
		}
	}
	return out
}

// samePath reports whether a and b name the same file or folder. The text is
// compared first; if that differs, the file system decides, so a short 8.3 name
// (C:\Users\RUNNER~1\...) matches the long one a shortcut stores.
func samePath(a, b string) bool {
	if strings.EqualFold(strings.TrimRight(filepath.Clean(a), `\/`), strings.TrimRight(filepath.Clean(b), `\/`)) {
		return true
	}
	sa, err := os.Stat(a)
	if err != nil {
		return false
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(sa, sb)
}
