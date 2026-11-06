package linx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindAndInstallFPScript(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FPExe), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range FindFuturePinball(dir) {
		found = found || d == filepath.Clean(dir)
	}
	if !found {
		t.Fatal("Future Pinball folder not found")
	}
	data := []byte("' script v1")
	if st, err := InstallFPScript(data, dir); err != nil || st != "installed" {
		t.Fatalf("%q %v", st, err)
	}
	if st, _ := InstallFPScript(data, dir); st != "up to date" {
		t.Errorf("second run %q", st)
	}
	if st, _ := InstallFPScript([]byte("' v2"), dir); st != "updated" {
		t.Errorf("update %q", st)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "Scripts", FPScript+".bak")); string(b) != "' script v1" {
		t.Errorf("backup %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "Scripts", FPScript)); string(b) != "' v2" {
		t.Errorf("file %q", b)
	}
}

func TestInstallB2SPluginKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, B2SPluginI), []byte("i"), 0o644)
	os.WriteFile(filepath.Join(dir, B2SPlugin), []byte("original DirectOutput plugin"), 0o644)
	if got := FindB2SServers(dir); len(got) == 0 || got[0] != filepath.Clean(dir) {
		t.Fatalf("not found: %v", got)
	}
	if st, err := InstallB2SPlugin([]byte("bridge"), dir); err != nil || st != "updated" {
		t.Fatalf("%q %v", st, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, B2SPlugin+".bak")); string(b) != "original DirectOutput plugin" {
		t.Errorf("backup %q", b)
	}
	if st, _ := InstallB2SPlugin([]byte("bridge"), dir); st != "up to date" {
		t.Errorf("second run %q", st)
	}
	// a later bridge build must not overwrite the first backup
	InstallB2SPlugin([]byte("bridge2"), dir)
	if b, _ := os.ReadFile(filepath.Join(dir, B2SPlugin+".bak")); string(b) != "original DirectOutput plugin" {
		t.Errorf("backup replaced: %q", b)
	}
}

func TestFindFuturePinballOddFolderName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vPinball", "FuturePinball")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, FPExe), []byte("x"), 0o644)
	// an explicit folder always works; the globs are exercised through a root
	has := false
	for _, d := range FindFuturePinball(dir) {
		has = has || d == filepath.Clean(dir)
	}
	if !has {
		t.Fatalf("explicit folder %s not found", dir)
	}
	m, _ := filepath.Glob(filepath.Join(filepath.Dir(filepath.Dir(dir)), "*/Future*Pinball*"))
	if len(m) != 1 {
		t.Fatalf("glob does not match %s: %v", dir, m)
	}
}
