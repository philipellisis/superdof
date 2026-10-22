package linx

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func fakeDLL(t *testing.T, p string) {
	t.Helper()
	b := append([]byte("MZ"), bytes.Repeat([]byte{0}, 70*1024)...)
	b = append(b, []byte("OnEvent\x00OnGameChange\x00OnStatusChange\x00")...)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstallTrigger(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src", TriggerDLL)
	os.MkdirAll(filepath.Dir(src), 0o755)
	fakeDLL(t, src)
	if got := FindTriggerDLL(filepath.Dir(src)); got != src {
		t.Fatalf("FindTriggerDLL = %q", got)
	}
	bad := filepath.Join(root, "bad", TriggerDLL)
	os.MkdirAll(filepath.Dir(bad), 0o755)
	os.WriteFile(bad, []byte("MZ not a trigger"), 0o644)
	if looksLikeTriggerDLL(bad) {
		t.Fatal("a file without the exports must not be accepted")
	}

	win := filepath.Join(root, "lib", "Pinball FX", "PinballFX", "Binaries", "Win64")
	os.MkdirAll(win, 0o755)
	os.WriteFile(filepath.Join(win, "PinballFX-Win64-Shipping.exe"), []byte("x"), 0o644)
	var games []GameInstall
	for _, g := range FindGames(filepath.Join(root, "lib")) {
		if g.Dir == win { // the machine running the test may also have a real install
			games = append(games, g)
		}
	}
	if len(games) != 1 {
		t.Fatalf("games %+v", games)
	}
	for i, want := range []string{"installed", "up to date"} {
		if st, err := InstallTrigger(src, games[0]); err != nil || st != want {
			t.Fatalf("install %d: %q %v, want %q", i, st, err, want)
		}
	}
	// a changed source updates the copy
	os.WriteFile(src, append(mustRead(t, src), 'x'), 0o644)
	if st, _ := InstallTrigger(src, games[0]); st != "updated" {
		t.Fatalf("expected update, got %q", st)
	}
	if _, err := os.Stat(filepath.Join(win, TriggerDLL+".superdof.tmp")); err == nil {
		t.Fatal("temp file left behind")
	}
}

func mustRead(t *testing.T, p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
