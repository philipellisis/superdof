package linx

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// TriggerDLL is the file name Pinball FX / Pinball M / Infected Mushroom
// Pinball look for next to their executable. The game only offers its
// "DOFLinx" cabinet option when this file is present.
const TriggerDLL = "DOFLinxTrigger.dll"

// triggerGames are the shipping executables that load the trigger DLL.
var triggerGames = []string{"PinballFX-Win64-Shipping.exe", "PinballM-Win64-Shipping.exe", "IMPinball-Win64-Shipping.exe"}

// GameInstall is an installed game that can use the trigger DLL.
type GameInstall struct {
	Exe string // full path of the *-Win64-Shipping.exe
	Dir string // folder the DLL belongs in (same as the exe)
}

// SteamLibraries lists the Steam library folders ("...\steamapps\common" parents).
func SteamLibraries() []string {
	var roots []string
	add := func(p string) {
		p = filepath.Clean(strings.TrimSpace(p))
		if p == "." || p == "" {
			return
		}
		for _, r := range roots {
			if strings.EqualFold(r, p) {
				return
			}
		}
		roots = append(roots, p)
	}
	if runtime.GOOS == "windows" {
		for _, key := range []string{`HKCU\Software\Valve\Steam`, `HKLM\SOFTWARE\WOW6432Node\Valve\Steam`} {
			for _, v := range []string{"SteamPath", "InstallPath"} {
				out, err := exec.Command("reg", "query", key, "/v", v).Output()
				if err != nil {
					continue
				}
				if m := regexp.MustCompile(`(?m)REG_SZ\s+(.+?)\s*$`).FindSubmatch(out); m != nil {
					add(string(m[1]))
				}
			}
		}
		add(`C:\Program Files (x86)\Steam`)
		add(`C:\Program Files\Steam`)
	} else if home, _ := os.UserHomeDir(); home != "" {
		add(filepath.Join(home, ".steam", "steam"))
		add(filepath.Join(home, ".local", "share", "Steam"))
	}
	// additional libraries from libraryfolders.vdf
	re := regexp.MustCompile(`"path"\s+"([^"]+)"`)
	for _, r := range append([]string(nil), roots...) {
		b, err := os.ReadFile(filepath.Join(r, "steamapps", "libraryfolders.vdf"))
		if err != nil {
			continue
		}
		for _, m := range re.FindAllSubmatch(b, -1) {
			add(strings.ReplaceAll(string(m[1]), `\`, `\`))
		}
	}
	var out []string
	for _, r := range roots {
		if st, err := os.Stat(filepath.Join(r, "steamapps", "common")); err == nil && st.IsDir() {
			out = append(out, r)
		}
	}
	return out
}

// FindGames finds installed Pinball FX / M / IMP executables in the Steam
// libraries (and in extra folders given by the caller).
func FindGames(extra ...string) []GameInstall {
	var out []GameInstall
	seen := map[string]bool{}
	add := func(exe string) {
		k := strings.ToLower(exe)
		if !seen[k] {
			seen[k] = true
			out = append(out, GameInstall{Exe: exe, Dir: filepath.Dir(exe)})
		}
	}
	for _, e := range extra {
		if st, err := os.Stat(e); err == nil && !st.IsDir() {
			add(e)
			continue
		}
		for _, g := range triggerGames {
			for _, pat := range []string{"*/Binaries/Win64", "*/*/Binaries/Win64"} {
				if m, _ := filepath.Glob(filepath.Join(e, filepath.FromSlash(pat), g)); len(m) > 0 {
					add(m[0])
				}
			}
			if _, err := os.Stat(filepath.Join(e, g)); err == nil {
				add(filepath.Join(e, g))
			}
			if _, err := os.Stat(filepath.Join(e, "Binaries", "Win64", g)); err == nil {
				add(filepath.Join(e, "Binaries", "Win64", g))
			}
		}
	}
	for _, lib := range SteamLibraries() {
		for _, g := range triggerGames {
			m, _ := filepath.Glob(filepath.Join(lib, "steamapps", "common", "*", "*", "Binaries", "Win64", g))
			for _, p := range m {
				add(p)
			}
		}
	}
	return out
}

// looksLikeTriggerDLL checks that the file is a PE with the expected exports.
func looksLikeTriggerDLL(p string) bool {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() || st.Size() < 64*1024 || st.Size() > 64<<20 {
		return false
	}
	b, err := os.ReadFile(p)
	if err != nil || len(b) < 2 || b[0] != 'M' || b[1] != 'Z' {
		return false
	}
	for _, name := range []string{"OnEvent", "OnGameChange", "OnStatusChange"} {
		if !bytes.Contains(b, []byte(name)) {
			return false
		}
	}
	return true
}

// FindTriggerDLL looks for DOFLinxTrigger.dll: the given hints first (files or
// folders), then next to the executable, in the config folder, the usual
// DOFLinx folders and the Downloads folder. It returns "" when none is found.
func FindTriggerDLL(hints ...string) string {
	var cands []string
	for _, h := range hints {
		if h == "" {
			continue
		}
		if st, err := os.Stat(h); err == nil && st.IsDir() {
			cands = append(cands, filepath.Join(h, TriggerDLL))
		} else {
			cands = append(cands, h)
		}
	}
	if exe, _ := os.Executable(); exe != "" {
		d := filepath.Dir(exe)
		cands = append(cands, filepath.Join(d, TriggerDLL), filepath.Join(d, "config", TriggerDLL))
	}
	cands = append(cands, TriggerDLL, filepath.Join("config", TriggerDLL),
		`C:\DOFLinx\`+TriggerDLL, `C:\DOFLinx\x86\`+TriggerDLL, `C:\DirectOutput\x86\`+TriggerDLL, `C:\DirectOutput\`+TriggerDLL)
	if home, _ := os.UserHomeDir(); home != "" {
		for _, base := range []string{"Downloads", "Desktop", "Documents"} {
			for _, pat := range []string{"DOFLinx*", "DOFLinx*/*", "DOFLinx*/*/*"} {
				m, _ := filepath.Glob(filepath.Join(home, base, pat, TriggerDLL))
				cands = append(cands, m...)
			}
		}
	}
	for _, c := range cands {
		if looksLikeTriggerDLL(c) {
			return c
		}
	}
	return ""
}

func fileHash(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// InstallTrigger copies src next to the game executable. It returns
// "installed", "updated" or "up to date".
func InstallTrigger(src string, g GameInstall) (string, error) {
	dst := filepath.Join(g.Dir, TriggerDLL)
	status := "installed"
	if _, err := os.Stat(dst); err == nil {
		a, e1 := fileHash(src)
		b, e2 := fileHash(dst)
		if e1 == nil && e2 == nil && a == b {
			return "up to date", nil
		}
		status = "updated"
	}
	in, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	tmp := dst + ".superdof.tmp"
	if err := os.WriteFile(tmp, in, 0o644); err != nil {
		return "", permHint(err, g.Dir)
	}
	if err := os.Rename(tmp, dst); err != nil {
		// the game may hold the old DLL open
		os.Remove(tmp)
		return "", permHint(err, g.Dir)
	}
	return status, nil
}

func permHint(err error, dir string) error {
	if os.IsPermission(err) {
		return fmt.Errorf("%w (no write access to %s: run superdof once as administrator, or close the game if it is running)", err, dir)
	}
	return err
}
