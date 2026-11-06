//go:build windows

package linx

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func powershell(script string, env ...string) (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("powershell: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// ShortcutTarget returns the target of a .lnk file.
func ShortcutTarget(lnk string) (string, error) {
	out, err := powershell(`(New-Object -ComObject WScript.Shell).CreateShortcut($env:SD_LNK).TargetPath`, "SD_LNK="+lnk)
	if err != nil || out == "" {
		return "", fmt.Errorf("cannot read shortcut %s: %v", lnk, err)
	}
	return out, nil
}

// CreateShortcut writes a .lnk file pointing at target.
func CreateShortcut(lnk, target string) error {
	_, err := powershell(`$l=(New-Object -ComObject WScript.Shell).CreateShortcut($env:SD_LNK); $l.TargetPath=$env:SD_TARGET; $l.Save()`, "SD_LNK="+lnk, "SD_TARGET="+target)
	return err
}
