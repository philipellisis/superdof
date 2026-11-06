package linx

import (
	"bytes"
	"os"
	"path/filepath"
)

// PinUP Popper (through PinUPDOF.DOFLink -> PinUpDOF.exe) and some table
// scripts create the COM object "DirectOutput.ComObject" to send DOF events.
// SuperDOF ships a replacement with the same CLSID and methods that forwards
// them to the SuperDOF pipe. It is registered for the current user only
// (HKCU), which overrides a DOF registration without admin rights, and
// RestoreComObject removes it again.

const (
	ComObjectDLL  = "DirectOutputComObject.dll"
	popperMarkers = "PinUpDOF.exe"
)

// ComObjectDir is where the COM object assembly is kept.
func ComObjectDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, "SuperDOF", "ComObject")
}

// FindPopper lists the PinUP Popper folders (they contain PinUpDOF.exe).
func FindPopper(extra ...string) []string {
	var cands []string
	for _, e := range extra {
		if e != "" {
			cands = append(cands, e)
		}
	}
	for _, r := range roots() {
		for _, pat := range []string{"popper", "PinUP*", "*opper*", "*/popper", "*/PinUP*", "*/*opper*", "*/*/PinUP*"} {
			m, _ := filepath.Glob(filepath.Join(r, pat))
			cands = append(cands, m...)
		}
	}
	var out []string
	for _, c := range uniq(cands) {
		if fileThere(filepath.Join(c, popperMarkers)) {
			out = append(out, c)
		}
	}
	return out
}

// InstallComObject writes the assembly to ComObjectDir and registers it for the
// current user. It returns "installed", "updated" or "up to date" (the
// registration is always refreshed).
func InstallComObject(data []byte) (string, error) {
	dir := ComObjectDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", permHint(err, dir)
	}
	dst := filepath.Join(dir, ComObjectDLL)
	status := "installed"
	if old, err := os.ReadFile(dst); err == nil {
		if bytes.Equal(old, data) {
			status = "up to date"
		} else {
			status = "updated"
		}
	}
	if status != "up to date" {
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return "", permHint(err, dir)
		}
	}
	return status, registerComObject(dst)
}

// RestoreComObject removes the per user registration so a DirectOutput
// installation (HKLM) is used again.
func RestoreComObject() error { return unregisterComObject() }
