package linx

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInstallB2SWithPluginShortcuts(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("shortcuts need Windows")
	}
	tmp := t.TempDir()
	t.Setenv("ProgramData", filepath.Join(tmp, "pd"))
	server := filepath.Join(tmp, "B2SServer")
	dofX86 := filepath.Join(tmp, "DirectOutput", "x86")
	dofX64 := filepath.Join(tmp, "DirectOutput", "x64")
	for _, d := range []string{filepath.Join(server, "Plugins"), filepath.Join(server, "Plugins64"), dofX86, dofX64} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dofX86, B2SPlugin), []byte("dof"), 0o644)
	os.WriteFile(filepath.Join(dofX64, B2SPlugin), []byte("dof"), 0o644)
	if err := CreateShortcut(filepath.Join(server, "Plugins", "DirectOutput.lnk"), dofX86); err != nil {
		t.Fatal(err)
	}
	if err := CreateShortcut(filepath.Join(server, "Plugins64", "DirectOutput.lnk"), dofX64); err != nil {
		t.Fatal(err)
	}
	// another plugin (PinUP) must stay untouched
	other := filepath.Join(tmp, "PinUP")
	os.MkdirAll(other, 0o755)
	os.WriteFile(filepath.Join(other, "PinUpPlugin.dll"), []byte("x"), 0o644)
	CreateShortcut(filepath.Join(server, "Plugins", "PinUP.lnk"), other)

	res, err := InstallB2S([]byte("bridge"), server)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) < 5 {
		t.Errorf("actions: %v", res.Actions)
	}
	pdir := B2SPluginDir()
	if b, _ := os.ReadFile(filepath.Join(pdir, B2SPlugin)); string(b) != "bridge" {
		t.Errorf("bridge not written: %q", b)
	}
	for _, p := range []string{"Plugins", "Plugins64"} {
		if fileThere(filepath.Join(server, p, "DirectOutput.lnk")) || !fileThere(filepath.Join(server, p, "DirectOutput.lnk"+disabledSufix)) {
			t.Errorf("%s: DirectOutput shortcut not switched off", p)
		}
		tg, err := ShortcutTarget(filepath.Join(server, p, b2sShortcut))
		if err != nil || !samePath(tg, pdir) {
			t.Errorf("%s: SuperDOF shortcut -> %q (%v), want %s", p, tg, err, pdir)
		}
	}
	if !fileThere(filepath.Join(server, "Plugins", "PinUP.lnk")) {
		t.Error("the PinUP shortcut was touched")
	}
	if res2, _ := InstallB2S([]byte("bridge"), server); len(res2.Actions) != 0 {
		t.Errorf("second run should do nothing: %v", res2.Actions)
	}
	RestoreB2S(server)
	if !fileThere(filepath.Join(server, "Plugins", "DirectOutput.lnk")) || fileThere(filepath.Join(server, "Plugins", b2sShortcut)) {
		t.Error("restore did not bring DOF back")
	}
}
