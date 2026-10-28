package dof

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLedControlToyNames(t *testing.T) {
	ini := "[Colors DOF]\nRed=#ff0000\n\n[Config DOF]\n" +
		`# "Rom","Flipper Left","Slingshot Left","","RGB Flippers","",""` + "\n" +
		"abc,E101,E103,0,ON Red,0,0\n"
	p := filepath.Join(t.TempDir(), "directoutputconfig11.ini")
	if err := os.WriteFile(p, []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := ParseLedControl(p, 11)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Rom", "Flipper Left", "Slingshot Left", "", "RGB Flippers", "", ""}
	if len(f.ToyNames) != len(want) {
		t.Fatalf("ToyNames %q", f.ToyNames)
	}
	for i, w := range want {
		if f.ToyNames[i] != w {
			t.Errorf("name %d = %q, want %q", i, f.ToyNames[i], w)
		}
	}
	if len(f.Tables) != 1 {
		t.Errorf("tables %d", len(f.Tables))
	}
}

func TestLedControlNoToyNames(t *testing.T) {
	ini := "[Colors DOF]\nRed=#ff0000\n\n[Config DOF]\n# some other comment\nabc,E101\n"
	p := filepath.Join(t.TempDir(), "directoutputconfig30.ini")
	os.WriteFile(p, []byte(ini), 0o644)
	f, err := ParseLedControl(p, 30)
	if err != nil || f.ToyNames != nil {
		t.Fatalf("err %v names %q", err, f.ToyNames)
	}
}
