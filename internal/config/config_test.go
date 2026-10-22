package config

import (
	"os"
	"path/filepath"
	"testing"

	"superdof/internal/linx"
)

const sample = `{
  // line comment
  "pinone": "auto",
  "dof": { "iniFilesPath": "ini" /* block comment */ },
  "cabinet": {
    "controllers": [ { "type": "wemosd1", "name": "LED Strips 0", "comPort": "COM4", "ledsPerStrip": [144, 144] } ],
    "toys": [ { "type": "ledstrip", "name": "Right Side", "width": 1, "height": 144, "controller": "LED Strips 0" } ]
  },
  "doflinx": {
    "processes": ["PinballFX-Win64-Shipping", "Pinball FX3"],
    "paths": { "fx": "FX" },
    "pinOneDevice": 1,
    "tables": { "12": "BackToTheFuture" },
    "devices": {
      "LF": { "port": 1017, "defaultMs": 50, "maxMs": 5000, "intensity": 200 },
      "SR": { "port": 1021, "mode": "ON", "flashMs": 0 }
    },
    "flashers": { "FLCN": [1030, 1033] },
    "buttons": { "BUT_LB": { "port": 1040, "colour": "Cyan", "key": 31 } },
    "rgb": [1050],
    "ini": { "SCORE_SCREEN": "0" }
  }
}`

func TestLoadAndConvertToLinxConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "superdof.json")
	if err := os.WriteFile(p, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.Cabinet == nil || len(f.Cabinet.Controllers) != 1 || f.Cabinet.Controllers[0].LedsPerStrip[1] != 144 {
		t.Fatalf("cabinet %+v", f.Cabinet)
	}
	c := f.LinxConfig("dir")
	links := linx.LoadLinks(c)
	if d := links.Dev["LF"]; len(d) != 1 || d[0].Addr != 1017 || d[0].MaxMs != 5000 || d[0].Intensity != 200 {
		t.Fatalf("LF %+v", d)
	}
	if d := links.Dev["SR"]; len(d) != 1 || d[0].Mode != "ON" {
		t.Fatalf("SR %+v", d)
	}
	if fl := links.Flasher["FLCN"]; len(fl) != 2 || fl[1].Addr != 1033 {
		t.Fatalf("FLCN %+v", fl)
	}
	if b := links.Button["BUT_LB"]; len(b) != 1 || b[0].Colour != "Cyan" || b[0].Key != 31 {
		t.Fatalf("BUT_LB %+v", b)
	}
	if len(links.RGB) != 1 || links.RGB[0].Addr != 1050 {
		t.Fatalf("rgb %+v", links.RGB)
	}
	if got := c.List("PROCESSES"); len(got) != 2 || got[1] != "Pinball FX3" {
		t.Fatalf("processes %v", got)
	}
	if c.Get("SCORE_SCREEN") != "0" || c.Get("PATH_FX") != "FX" {
		t.Fatal("passthrough/paths lost")
	}
}

func TestUnknownFieldsAreRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(p, []byte(`{"doflinx": {"proceses": []}}`), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("typos in key names must be reported")
	}
}

func TestINIRoundTrip(t *testing.T) {
	c := linx.NewConfig()
	c.Add("PROCESSES", "PinballFX-Win64-Shipping")
	c.Add("LINK_LF", "2017,50,5000,255")
	c.Add("LINK_FLCN", "3024,3002")
	c.Add("LINK_BUT_ST", "1003,Cyan,31")
	c.Add("RGB_OUTPUT", "2009")
	c.Add("SCORE_SCREEN", "1")
	f := FromINI(c, nil, nil)
	back := f.LinxConfig(".")
	l := linx.LoadLinks(back)
	if d := l.Dev["LF"]; len(d) != 1 || d[0].Addr != 2017 || d[0].DefaultMs != 50 {
		t.Fatalf("LF %+v", d)
	}
	if len(l.Flasher["FLCN"]) != 2 || len(l.RGB) != 1 || back.Get("SCORE_SCREEN") != "1" {
		t.Fatal("round trip lost data")
	}
}

func TestSampleFileLoads(t *testing.T) {
	f, err := Load(filepath.Join("..", "..", "superdof.sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Cabinet == nil || len(f.Cabinet.Toys) != 4 || f.DOFLinx.Devices["LF"].Port != 1017 {
		t.Fatalf("unexpected %+v", f.Cabinet)
	}
}
