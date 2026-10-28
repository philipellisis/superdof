package linx

import (
	"strings"
	"testing"
)

// the comment line the DOF Config Tool wrote into a real directoutputconfig11.ini
const realToyLine = `# "Rom","Flipper Left","Slingshot Left","10 Bumper Middle Left","10 Bumper Middle Center","10 Bumper Middle Right","Flipper Right","Slingshot Right","10 Bumper Back Right","10 Bumper Back Center","10 Bumper Back Left","Gear","Shaker","Knocker","","","RGB Flippers","","","RGB Right Magnasave","","","RGB Left Magnasave","","","Extra Ball","Coin","Launch Button","Start Button","RGB Fire Button","","","5 Flasher Outside Left","","","5 Flasher Left","","","5 Flasher Center","","","5 Flasher Right","","","5 Flasher Outside Right","","","Strobe"`

func names(line string) []string {
	var out []string
	for _, f := range strings.Split(strings.TrimLeft(line, "# "), `","`) {
		out = append(out, strings.Trim(f, `"`))
	}
	return out
}

func TestDeriveLinksFromRealComment(t *testing.T) {
	c := NewConfig()
	added, _ := DeriveLinks(c, []Board{{Device: 1, Label: "11", Names: names(realToyLine)}})
	if len(added) == 0 {
		t.Fatal("nothing derived")
	}
	L := LoadLinks(c)
	dev := func(code string, port int) {
		t.Helper()
		ls := L.Dev[code]
		if len(ls) != 1 || ls[0].Addr != 1000+port {
			t.Errorf("%s = %+v, want address %d", code, ls, 1000+port)
		}
	}
	dev("LF", 1)
	dev("LS", 2)
	dev("ML", 3)
	dev("MC", 4)
	dev("MR", 5)
	dev("RF", 6)
	dev("RS", 7)
	dev("BR", 8)
	dev("BC", 9)
	dev("BL", 10)
	dev("GR", 11)
	dev("SH", 12)
	dev("KN", 13)
	dev("SR", 47)
	if L.Dev["LF"][0].MaxMs != 10000 {
		t.Errorf("flipper max ms %d", L.Dev["LF"][0].MaxMs)
	}
	fl := func(code string, port int) {
		t.Helper()
		ls := L.Flasher[code]
		if len(ls) != 1 || ls[0].Addr != 1000+port {
			t.Errorf("%s = %+v, want %d", code, ls, 1000+port)
		}
	}
	fl("FLOL", 32)
	fl("FLIL", 35)
	fl("FLCN", 38)
	fl("FLIR", 41)
	fl("FLOR", 44)
	but := func(code string, port int, mono bool) {
		t.Helper()
		ls := L.Button[code]
		if len(ls) != 1 || ls[0].Addr != 1000+port || ls[0].Mono != mono {
			t.Errorf("%s = %+v, want %d mono=%v", code, ls, 1000+port, mono)
		}
	}
	but("BUT_EB", 25, true)
	but("BUT_CN", 26, true)
	but("BUT_LB", 27, true)
	but("BUT_ST", 28, true)
	but("BUT_FR", 29, false)
	but("BUT_MR", 19, false)
	but("BUT_ML", 22, false)
	if len(L.RGB) != 1 || L.RGB[0].Addr != 1016 {
		t.Errorf("RGB_OUTPUT = %+v, want 1016", L.RGB)
	}
}

func TestDeriveLinksLayoutFollowsTheComment(t *testing.T) {
	// a different cabinet: flipper on 5, start button on 3, a shaker on 9
	n := []string{"Rom", "", "", "Start Button", "", "Flipper Left", "", "", "", "Shaker"}
	c := NewConfig()
	DeriveLinks(c, []Board{{Device: 2, Names: n}})
	L := LoadLinks(c)
	if L.Dev["LF"][0].Addr != 2005 || L.Dev["SH"][0].Addr != 2009 || L.Button["BUT_ST"][0].Addr != 2003 {
		t.Errorf("got LF %+v SH %+v ST %+v", L.Dev["LF"], L.Dev["SH"], L.Button["BUT_ST"])
	}
}

func TestDeriveLinksExplicitWins(t *testing.T) {
	c := NewConfig()
	c.Add("LINK_LF", "105,50,10000,255")
	c.Add("LINK_ST", "126,MONO")
	c.Add("RGB_OUTPUT", "1040")
	_, notes := DeriveLinks(c, []Board{{Device: 1, Names: names(realToyLine)}})
	L := LoadLinks(c)
	if L.Dev["LF"][0].Addr != 105 || len(L.Dev["LF"]) != 1 {
		t.Errorf("explicit LF replaced: %+v", L.Dev["LF"])
	}
	if L.Button["BUT_ST"][0].Addr != 126 || len(L.Button["BUT_ST"]) != 1 {
		t.Errorf("explicit ST replaced: %+v", L.Button["BUT_ST"])
	}
	if len(L.RGB) != 1 || L.RGB[0].Addr != 1040 {
		t.Errorf("explicit RGB_OUTPUT replaced: %+v", L.RGB)
	}
	if len(notes) < 3 {
		t.Errorf("expected notes about the explicit entries, got %v", notes)
	}
	// the others are still derived
	if L.Dev["RF"][0].Addr != 1006 {
		t.Errorf("RF %+v", L.Dev["RF"])
	}
}

func TestDeriveLinksUnknownNamesAreReported(t *testing.T) {
	c := NewConfig()
	_, notes := DeriveLinks(c, []Board{{Device: 1, Label: "x", Names: []string{"Rom", "Disco Ball", "Flipper Left"}}})
	if len(notes) != 1 || !strings.Contains(notes[0], "Disco Ball") {
		t.Errorf("notes %v", notes)
	}
	if LoadLinks(c).Dev["LF"][0].Addr != 1002 {
		t.Error("LF not derived")
	}
}
