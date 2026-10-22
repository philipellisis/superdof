package linx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnknownIDsAreNeverGuessed(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "All_Pre.FX"), []byte("[COMMANDS]\nBUMPER01|ON|FF_DOF E120,-1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "MyTable.FX"), []byte("[STARTUP]\nFF_ROM=x\n"), 0o644)
	cfg := NewConfig()
	cfg.Dir = dir
	cfg.Set("PATH_FX", dir)
	m := NewManager(cfg, NewRunner(cfg, &fakeDOF{}, &fakeSink{}, nil, nopLog{}), nopLog{})
	m.Learn("42", "MyTable") // the user assigns one id
	for _, id := range []string{"43", "44", "45"} {
		m.HandleTrigger("G," + id) // menu scrolling while MyTable is active
	}
	for _, id := range []string{"43", "44", "45"} {
		if tb, ok := m.tableIDs[id]; ok {
			t.Fatalf("id %s was guessed as %q", id, tb)
		}
	}
	m.Learn("42", "Other") // reassigning replaces, never duplicates
	b, _ := os.ReadFile(filepath.Join(dir, "tables.learned.ini"))
	if strings.Count(string(b), "42=") != 1 || !strings.Contains(string(b), "42=Other") || strings.Contains(string(b), "43=") {
		t.Fatalf("learned file: %q", b)
	}
	m2 := NewManager(cfg, NewRunner(cfg, &fakeDOF{}, &fakeSink{}, nil, nopLog{}), nopLog{})
	if m2.tableIDs["42"] != "Other" {
		t.Fatal("learned file not reloaded")
	}
}
