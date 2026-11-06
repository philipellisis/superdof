package linx

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
)

const (
	FPExe      = "Future Pinball.exe"
	FPScript   = "DOFLinx.vbs"
	B2SPlugin  = "B2SServerDirectOutputPlugin.dll"
	B2SPluginI = "B2SServerPluginInterface.dll"
)

// roots returns the places installs usually live in: Program Files and every
// drive root.
func roots() []string {
	var out []string
	for _, e := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
		if p := os.Getenv(e); p != "" {
			out = append(out, p)
		}
	}
	if runtime.GOOS == "windows" {
		for c := 'C'; c <= 'Z'; c++ {
			r := string(c) + `:\`
			if _, err := os.Stat(r); err == nil {
				out = append(out, r)
			}
		}
	}
	return out
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range in {
		k := filepath.Clean(p)
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// FindFuturePinball lists the folders that contain Future Pinball.exe. The
// extra paths may be the folder or the exe itself.
func FindFuturePinball(extra ...string) []string {
	var cands []string
	for _, e := range extra {
		if e == "" {
			continue
		}
		if filepath.Base(e) == FPExe {
			e = filepath.Dir(e)
		}
		cands = append(cands, e)
	}
	for _, r := range roots() {
		for _, sub := range []string{"Future Pinball", `Games\Future Pinball`, `Pinball\Future Pinball`, `Visual Pinball\Future Pinball`, `vPinball\Future Pinball`} {
			cands = append(cands, filepath.Join(r, sub))
		}
		// the folder name varies: "Future Pinball", "FuturePinball", "Future_Pinball" ...
		for _, pat := range []string{"Future*Pinball*", "*/Future*Pinball*", "*/*/Future*Pinball*"} {
			m, _ := filepath.Glob(filepath.Join(r, pat))
			cands = append(cands, m...)
		}
	}
	var out []string
	for _, c := range uniq(cands) {
		if _, err := os.Stat(filepath.Join(c, FPExe)); err == nil {
			out = append(out, c)
		}
	}
	return out
}

// InstallFPScript writes DOFLinx.vbs to <fpDir>\Scripts, where tables load it
// with LoadExternalScript. An older different file is kept once as .bak. It
// returns "installed", "updated" or "up to date".
func InstallFPScript(data []byte, fpDir string) (string, error) {
	dir := filepath.Join(fpDir, "Scripts")
	dst := filepath.Join(dir, FPScript)
	status := "installed"
	if old, err := os.ReadFile(dst); err == nil {
		if bytes.Equal(old, data) {
			return "up to date", nil
		}
		status = "updated"
		if bak := dst + ".bak"; !fileThere(bak) {
			os.WriteFile(bak, old, 0o644)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", permHint(err, dir)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", permHint(err, dir)
	}
	return status, nil
}

func fileThere(p string) bool { _, err := os.Stat(p); return err == nil }

// FindB2SServers lists the folders with the B2S server (they hold
// B2SServerPluginInterface.dll, next to which the DirectOutput plugin lives).
func FindB2SServers(extra ...string) []string {
	var cands []string
	for _, e := range extra {
		if e != "" {
			cands = append(cands, e)
		}
	}
	for _, r := range roots() {
		for _, sub := range []string{`Visual Pinball\Tables`, `Visual Pinball\B2SServer`, `Visual Pinball`, `vPinball\VisualPinball\Tables`, `vPinball\VisualPinball`, `VisualPinball\Tables`, `Pinball\Visual Pinball\Tables`, `Pinball\VisualPinball\Tables`, `B2SServer`} {
			cands = append(cands, filepath.Join(r, sub))
		}
		for _, pat := range []string{"*/Tables", "*/*/Tables", "*/B2SServer", "*/*/B2SServer"} {
			m, _ := filepath.Glob(filepath.Join(r, pat))
			cands = append(cands, m...)
		}
	}
	var out []string
	for _, c := range uniq(cands) {
		if fileThere(filepath.Join(c, B2SPluginI)) {
			out = append(out, c)
		}
	}
	return out
}

// InstallB2SPlugin copies the bridge plugin into a B2S server folder, keeping
// the existing DirectOutput plugin once as .bak. It returns "installed",
// "updated" or "up to date".
func InstallB2SPlugin(data []byte, dir string) (string, error) {
	dst := filepath.Join(dir, B2SPlugin)
	status := "installed"
	if old, err := os.ReadFile(dst); err == nil {
		if bytes.Equal(old, data) {
			return "up to date", nil
		}
		status = "updated"
		if bak := dst + ".bak"; !fileThere(bak) {
			if err := os.WriteFile(bak, old, 0o644); err != nil {
				return "", permHint(err, dir)
			}
		}
	}
	tmp := dst + ".superdof.tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", permHint(err, dir)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", permHint(err, dir)
	}
	return status, nil
}
