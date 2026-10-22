package linx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinPinballFXTables(t *testing.T) {
	seen := map[int]bool{}
	files := map[string]int{}
	for _, tb := range PinballFXTables {
		if seen[tb.ID] {
			t.Fatalf("duplicate id %d", tb.ID)
		}
		seen[tb.ID] = true
		if tb.File != "" {
			if prev, dup := files[tb.File]; dup {
				t.Fatalf("file %s used by %d and %d", tb.File, prev, tb.ID)
			}
			files[tb.File] = tb.ID
		}
	}
	// ids confirmed in the game by the user
	for id, want := range map[string]string{"33": "SorcerersLair", "75": "WildWestRampage", "108": "WMSFishTales"} {
		if f, _ := BuiltinTable(id); f != want {
			t.Fatalf("id %s = %q, want %q", id, f, want)
		}
	}
	if f, n := BuiltinTable("183"); f != "" || n == "" {
		t.Fatalf("183 should be a known table without an FX file, got %q %q", f, n)
	}
}

func TestBuiltinIDSelectsTableAndUserWins(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "All_Pre.FX"), []byte("[COMMANDS]\nBUMPER01|ON|FF_DOF E120,-1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "SorcerersLair.FX"), []byte("[STARTUP]\nFF_ROM=a\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "Mine.FX"), []byte("[STARTUP]\nFF_ROM=b\n"), 0o644)
	cfg := NewConfig()
	cfg.Dir = dir
	cfg.Set("PATH_FX", dir)
	m := NewManager(cfg, NewRunner(cfg, &fakeDOF{}, &fakeSink{}, nil, nopLog{}), nopLog{})
	m.HandleTrigger("G,33")
	if _, tb := m.Current(); tb != "SorcerersLair" {
		t.Fatalf("built-in id 33 not applied, table %q", tb)
	}
	m.Learn("33", "Mine") // an explicit user mapping overrides the built-in one
	m.HandleTrigger("G,75")
	m.HandleTrigger("G,33")
	if _, tb := m.Current(); tb != "Mine" {
		t.Fatalf("user mapping must win, table %q", tb)
	}
	for _, s := range m.SeenIDs() {
		if s.ID == "75" && (s.Name != "Wild West Rampage" || !s.Built) {
			t.Fatalf("seen id info %+v", s)
		}
	}
}
