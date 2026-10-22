package dof

import (
	"os"
	"path/filepath"
	"strings"
)

// settingColor resolves the active color of a setting (named color or #hex).
func settingColor(cc *ColorConfig, name string) *RGBA {
	var c *ColorConfig
	switch {
	case cc != nil:
		c = cc
	case strings.HasPrefix(name, "#"):
		c = parseHTMLColor(name)
	}
	if c == nil {
		return nil
	}
	return &RGBA{c.R, c.G, c.B, c.A}
}

// findRomBitmap finds <ini folder>\<rom>.<ext> (the bitmap of a table).
func findRomBitmap(dir, rom string) string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.EqualFold(strings.TrimSuffix(n, filepath.Ext(n)), rom) {
			switch strings.ToLower(filepath.Ext(n)) {
			case ".png", ".gif", ".jpg", ".jpeg":
				return filepath.Join(dir, n)
			}
		}
	}
	return ""
}

// buildMatrixEffect creates the leaf effect for an area setting that
// targets a LED strip toy (port of the matrix branch of Configurator.SetupTable).
func buildMatrixEffect(s *Setting, toyName string, layer int, f *LedControlFile, rom string, log Logger) Effect {
	a := &s.Area
	mb := newMatrixBase(toyName, layer, a.Left, a.Top, a.Width, a.Height)
	active := settingColor(s.ColorConfig, s.ColorName)
	inactive := RGBA{}
	if active != nil {
		inactive = *active
		inactive.A = 0
	}
	switch {
	case a.ShapeName != "":
		e := &shapeEffect{matrixBase: mb, shape: a.ShapeName}
		if active != nil {
			e.scaleColors, e.active, e.inactive = true, *active, inactive
		}
		return e
	case a.IsBitmap:
		file := findRomBitmap(filepath.Dir(f.Path), rom)
		if file == "" {
			log.Warnf("%s: no bitmap file named %s.png/.gif/.jpg next to the ini file", rom, rom)
			return nil
		}
		if a.AnimStepCount > 1 {
			return &matrixBitmapAnimEffect{matrixBase: mb, file: file, frame: a.BitmapFrame, bmpTop: a.BitmapTop, bmpLeft: a.BitmapLeft,
				bmpWidth: a.BitmapWidth, bmpHeight: a.BitmapHeight, mode: "BlendPixels", stepDir: a.AnimDirection,
				stepSize: a.AnimStepSize, frameCount: a.AnimStepCount, frameMs: a.AnimFrameMs, behaviour: a.AnimBehaviour}
		}
		return &matrixBitmapEffect{matrixBase: mb, file: file, frame: a.BitmapFrame, bmpTop: a.BitmapTop, bmpLeft: a.BitmapLeft,
			bmpWidth: a.BitmapWidth, bmpHeight: a.BitmapHeight, mode: "BlendPixels"}
	case a.IsPlasma:
		c1 := settingColor(s.ColorConfig, s.ColorName)
		c2 := settingColor(a.ColorConfig2, a.ColorName2)
		var in RGBA
		switch {
		case c1 != nil:
			in = *c1
			in.A = 0
		case c2 != nil:
			in = *c2
			in.A = 0
		}
		col1, col2 := RGBA{255, 0, 0, 255}, RGBA{0, 255, 0, 255}
		if c1 != nil {
			col1 = *c1
		}
		if c2 != nil {
			col2 = *c2
		}
		return &matrixPlasmaEffect{matrixBase: mb, speed: a.PlasmaSpeed, scale: 100, density: a.PlasmaDensity,
			color1: col1, color2: col2, inactive: in}
	}
	if active == nil {
		log.Warnf("%s: invalid color definition %q for area effect; setting skipped", rom, s.ColorName)
		return nil
	}
	switch {
	case a.Direction != 0:
		e := &matrixShiftEffect{matrixBase: mb, direction: a.Direction, speed: 200, accel: float32(a.Acceleration), active: *active, inactive: inactive}
		if a.Speed > 0 {
			e.speed = float32(a.Speed)
		}
		return e
	case a.FlickerDensity > 0:
		e := &matrixFlickerEffect{matrixBase: mb, density: limit(a.FlickerDensity, 1, 99), minMs: 60, maxMs: 150, active: *active, inactive: inactive}
		if a.FlickerMinMs > 0 {
			e.minMs = a.FlickerMinMs
		}
		if a.FlickerMaxMs > 0 {
			e.maxMs = a.FlickerMaxMs
		}
		if a.FlickerFadeMs > 0 {
			e.fadeUpMs, e.fadeDownMs = a.FlickerFadeMs, a.FlickerFadeMs
		}
		return e
	}
	return &matrixColorEffect{matrixBase: mb, active: *active, inactive: inactive}
}
