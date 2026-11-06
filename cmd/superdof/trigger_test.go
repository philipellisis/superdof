package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedTriggerDLL(t *testing.T) {
	if len(triggerDLL) < 64*1024 || triggerDLL[0] != 'M' || triggerDLL[1] != 'Z' {
		t.Fatalf("embedded trigger DLL is not a PE (%d bytes)", len(triggerDLL))
	}
	for _, n := range []string{"OnEvent", "OnGameChange", "OnStatusChange"} {
		if !bytes.Contains(triggerDLL, []byte(n)) {
			t.Errorf("embedded DLL lacks export %s", n)
		}
	}
}

func TestInstallTriggersUsesEmbeddedDLL(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "PinballFX-Win64-Shipping.exe")
	if err := os.WriteFile(exe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := installTriggers("", []string{dir})
	var got *triggerGame
	for i := range rep.Games {
		if filepath.Dir(rep.Games[i].Exe) == dir {
			got = &rep.Games[i]
		}
	}
	if got == nil || !got.OK || got.Status != "installed" {
		t.Fatalf("report %+v", rep)
	}
	b, err := os.ReadFile(filepath.Join(dir, "DOFLinxTrigger.dll"))
	if err != nil || !bytes.Equal(b, triggerDLL) {
		t.Fatalf("installed file differs: %v", err)
	}
	if rep2 := installTriggers("", []string{dir}); statusFor(rep2, dir) != "up to date" {
		t.Errorf("second run: %+v", rep2)
	}
}

func statusFor(r triggerReport, dir string) string {
	for _, g := range r.Games {
		if filepath.Dir(g.Exe) == dir {
			return g.Status
		}
	}
	return ""
}
