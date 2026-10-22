package dof

import "strings"

// AreaSettings are the matrix / area parameters of a ledcontrol setting
// (used for addressable LED strips).
type AreaSettings struct {
	Left, Top, Width, Height int // percent of the strip
	Speed                    int
	Acceleration             int
	FlickerDensity           int
	FlickerMinMs             int
	FlickerMaxMs             int
	FlickerFadeMs            int
	Direction                byte // 'L','R','U','D' or 0

	IsBitmap      bool
	BitmapTop     int
	BitmapLeft    int
	BitmapWidth   int
	BitmapHeight  int
	BitmapFrame   int
	AnimStepSize  int
	AnimStepCount int
	AnimFrameMs   int
	AnimDirection byte // 'F','R','D'
	AnimBehaviour byte // 'O','L','C'
	ShapeName     string
	IsPlasma      bool
	PlasmaSpeed   int
	PlasmaDensity int
	ColorName2    string
	ColorConfig2  *ColorConfig
}

func newAreaSettings() AreaSettings {
	return AreaSettings{Width: 100, Height: 100, Speed: 100, BitmapWidth: -1, BitmapHeight: -1,
		AnimStepSize: 1, AnimFrameMs: 30, AnimDirection: 'F', AnimBehaviour: 'L', PlasmaSpeed: 100, PlasmaDensity: 100}
}

func isAreaBitmapPrefix(p string) bool {
	switch p {
	case "ABT", "ABL", "ABW", "ABH", "ABF", "AAS", "AAC", "AAF":
		return true
	}
	return false
}

// parseAreaToken consumes area related tokens (port of the AT/AL/AW/AH/AS/
// AD/AF*/AB*/AA*/AP*/SHP parts of TableConfigSetting.ParseSettingData).
func parseAreaToken(s *Setting, p, up string) bool {
	a := &s.Area
	rest := func(n int) string { return p[n:] }
	mark := func() bool { s.IsArea = true; return true }
	switch {
	case len(p) > 3 && up[:3] == "APS" && isInteger(rest(3)):
		a.PlasmaSpeed, a.IsPlasma = toInt(rest(3)), true
		return mark()
	case len(p) > 3 && up[:3] == "APD" && isInteger(rest(3)):
		a.PlasmaDensity, a.IsPlasma = toInt(rest(3)), true
		return mark()
	case len(p) > 3 && up[:3] == "APC":
		a.ColorName2, a.IsPlasma = strings.ToUpper(rest(3)), true
		return mark()
	case len(p) > 3 && up[:3] == "SHP":
		a.ShapeName = strings.ToUpper(strings.TrimSpace(rest(3)))
		return mark()
	case len(p) > 3 && isAreaBitmapPrefix(up[:3]) && isInteger(rest(3)):
		n := toInt(rest(3))
		switch up[:3] {
		case "ABT":
			a.BitmapTop = n
		case "ABL":
			a.BitmapLeft = n
		case "ABW":
			a.BitmapWidth = n
		case "ABH":
			a.BitmapHeight = n
		case "ABF":
			a.BitmapFrame = n
		case "AAS":
			a.AnimStepSize = n
		case "AAC":
			a.AnimStepCount = n
		case "AAF":
			a.AnimFrameMs = 1000 / limit(n, 10, 1<<31-1)
		}
		a.IsBitmap = true
		return mark()
	case len(p) == 4 && up[:3] == "AAD" && strings.IndexByte("FRD", up[3]) >= 0:
		a.AnimDirection, a.IsBitmap = up[3], true
		return mark()
	case len(p) == 4 && up[:3] == "AAB" && strings.IndexByte("OLC", up[3]) >= 0:
		a.AnimBehaviour, a.IsBitmap = up[3], true
		return mark()
	case len(p) > 5 && up[:5] == "AFDEN" && isInteger(rest(5)):
		a.FlickerDensity = toInt(rest(5))
		return mark()
	case len(p) > 5 && up[:5] == "AFMIN" && isInteger(rest(5)):
		a.FlickerMinMs = toInt(rest(5))
		return mark()
	case len(p) > 5 && up[:5] == "AFMAX" && isInteger(rest(5)):
		a.FlickerMaxMs = toInt(rest(5))
		return mark()
	case len(p) > 6 && up[:6] == "AFFADE" && isInteger(rest(6)):
		a.FlickerFadeMs = toInt(rest(6))
		return mark()
	case len(p) > 2 && up[:2] == "AT" && isInteger(rest(2)):
		a.Top = limit(toInt(rest(2)), 0, 100)
		return mark()
	case len(p) > 2 && up[:2] == "AL" && isInteger(rest(2)):
		a.Left = limit(toInt(rest(2)), 0, 100)
		return mark()
	case len(p) > 2 && up[:2] == "AW" && isInteger(rest(2)):
		a.Width = limit(toInt(rest(2)), 0, 100)
		return mark()
	case len(p) > 2 && up[:2] == "AH" && isInteger(rest(2)):
		a.Height = limit(toInt(rest(2)), 0, 100)
		return mark()
	case len(p) > 2 && up[:2] == "AA" && isInteger(rest(2)):
		a.Acceleration = toInt(rest(2))
		return mark()
	case len(p) > 3 && up[:3] == "ASA" && isInteger(rest(3)):
		a.Acceleration = toInt(rest(3))
		return mark()
	case len(p) > 2 && up[:2] == "AS" && isInteger(rest(2)):
		a.Speed = limit(toInt(rest(2)), 1, 10000)
		return mark()
	case len(p) > 3 && up[:3] == "ASS" && isInteger(rest(3)):
		a.Speed = limit(toInt(rest(3)), 1, 10000)
		return mark()
	case len(p) > 5 && up[:3] == "ASS" && strings.HasSuffix(up, "MS") && isInteger(p[3:len(p)-2]):
		ms := toInt(p[3 : len(p)-2])
		if ms != 0 {
			a.Speed = limit(100000/ms, 10, 100000)
		}
		return mark()
	case len(p) == 3 && up[:2] == "AD" && isShiftDirection(up[2]):
		a.Direction = up[2]
		return mark()
	case len(p) == 4 && up[:3] == "ASD" && isShiftDirection(up[3]):
		a.Direction = up[3]
		return mark()
	}
	return false
}
