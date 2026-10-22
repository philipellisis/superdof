package dof

import (
	"fmt"
	"strings"
)

// ConfiguratorOptions are the global minimum effect durations.
type ConfiguratorOptions struct {
	MinDurationMs    int // single output effects (default 60)
	MinRGBDurationMs int // rgb effects (default 120)
}

func (o ConfiguratorOptions) withDefaults() ConfiguratorOptions {
	if o.MinDurationMs == 0 {
		o.MinDurationMs = 60
	}
	if o.MinRGBDurationMs == 0 {
		o.MinRGBDurationMs = 120
	}
	return o
}

// Configure sets up toys on the cabinet and effects on the table from the
// ini files for the given ROM name (port of LedControl.Setup.Configurator).
// It returns false when none of the files has a config for the ROM.
func Configure(files []*LedControlFile, rom string, table *Table, cab *Cabinet, opt ConfiguratorOptions, log Logger) bool {
	opt = opt.withDefaults()
	if log == nil {
		log = nopLogger{}
	}
	type assigned struct {
		tc       *TableConfig
		file     *LedControlFile
		toys     map[int]Toy // column number -> toy
		isRGB    map[int]bool
		isMatrix map[int]bool
	}
	found := map[int]*assigned{}
	// DOF picks one table config per file; first file match per LedWiz number wins.
	for _, f := range files {
		if _, dup := found[f.Number]; dup {
			continue
		}
		if tc := f.find(rom); tc != nil {
			found[f.Number] = &assigned{tc: tc, file: f, toys: map[int]Toy{}, isRGB: map[int]bool{}, isMatrix: map[int]bool{}}
		}
	}
	if len(found) == 0 {
		return false
	}

	// ---- SetupCabinet: assign a toy to every column.
	for nr, a := range found {
		lwe := cab.LedWizEquivalent(nr)
		if lwe == nil {
			log.Warnf("no LedWizEquivalent number %d found in cabinet; ini file for this number is ignored", nr)
			continue
		}
		for _, col := range a.tc.Columns {
			if col.IsArea() {
				if name, ok := lwe.outputName(col.FirstOutput); ok {
					if t, ok := cab.toys[name].(*LedStripToy); ok {
						a.toys[col.Number], a.isMatrix[col.Number] = t, true
					} else {
						log.Warnf("unknown matrix toy %q for column %d of LedWizEquivalent %d", name, col.FirstOutput, nr)
					}
				}
				continue
			}
			switch col.RequiredOutputs() {
			case 3:
				if name, ok := lwe.outputName(col.FirstOutput); ok {
					if t, ok := cab.toys[name].(*RGBAToy); ok {
						a.toys[col.Number], a.isRGB[col.Number] = t, true
						continue
					}
				}
				r, ok1 := lwe.outputName(col.FirstOutput)
				g, ok2 := lwe.outputName(col.FirstOutput + 1)
				b, ok3 := lwe.outputName(col.FirstOutput + 2)
				if !(ok1 && ok2 && ok3) {
					continue
				}
				var found Toy
				for _, n := range cab.toyOrder {
					if t, ok := cab.toys[n].(*RGBAToy); ok && t.r == cab.outputs[r] && t.g == cab.outputs[g] && t.b == cab.outputs[b] && t.r != nil {
						found = t
						break
					}
				}
				if found == nil {
					found = cab.NewRGBToy(uniqueToyName(cab, fmt.Sprintf("LedWiz %02d Column %02d", nr, col.Number)), r, g, b, "")
					cab.AddToy(found)
				}
				a.toys[col.Number], a.isRGB[col.Number] = found, true
			case 1:
				name, ok := lwe.outputName(col.FirstOutput)
				if !ok {
					continue
				}
				if t, ok := cab.toys[name].(*AnalogAlphaToy); ok {
					a.toys[col.Number] = t
					continue
				}
				var found Toy
				for _, n := range cab.toyOrder {
					if t, ok := cab.toys[n].(*AnalogAlphaToy); ok && t.OutRef == name {
						found = t
						break
					}
				}
				if found == nil {
					found = cab.NewAnalogToy(uniqueToyName(cab, fmt.Sprintf("LedWiz %02d Column %02d", nr, col.Number)), name, "")
					cab.AddToy(found)
				}
				a.toys[col.Number] = found
			}
		}
	}

	// ---- SetupTable: build effect chains.
	for nr, a := range found {
		for _, col := range a.tc.Columns {
			toy, ok := a.toys[col.Number]
			if !ok {
				continue
			}
			for si, s := range col.Settings {
				settingNr := si + 1
				layer := settingNr
				if s.Layer != nil {
					layer = *s.Layer
				}
				prefix := fmt.Sprintf("Ledwiz %02d Column %02d Setting %02d", nr, col.Number, settingNr)
				var leaf Effect
				isRGB := a.isRGB[col.Number]
				if a.isMatrix[col.Number] {
					leaf = buildMatrixEffect(s, toy.Name(), layer, a.file, a.tc.ShortRomName, log)
					if leaf == nil {
						continue
					}
					leaf.SetName(prefix + " " + strings.TrimPrefix(fmt.Sprintf("%T", leaf), "*dof."))
				} else if isRGB {
					var c *ColorConfig
					switch {
					case s.ColorConfig != nil:
						c = s.ColorConfig
					case strings.HasPrefix(s.ColorName, "#"):
						c = parseHTMLColor(s.ColorName)
					}
					if c == nil {
						log.Warnf("skipped setting %d in column %d for LedWizEquivalent %d: %q is not a valid color", settingNr, col.Number, nr, s.ColorName)
						continue
					}
					act := RGBA{c.R, c.G, c.B, c.A}
					in := act
					in.A = 0
					e := &rgbaEffect{toyName: toy.Name(), layerNr: layer, active: act, inactive: in}
					e.name = prefix + " RGBAColorEffect"
					leaf = e
				} else {
					v := limit(s.Intensity(), 0, 255)
					e := &analogEffect{toyName: toy.Name(), layerNr: layer, active: AnalogAlpha{v, 255}, inactive: AnalogAlpha{v, 0}}
					e.name = prefix + " AnalogToyValueEffect"
					leaf = e
				}
				last := leaf
				add := func(e Effect, suffix string) {
					e.SetName(prefix + " " + suffix)
					for table.HasEffect(e.Name()) {
						e.SetName(e.Name() + " 1")
					}
					table.AddEffect(e)
					last = e
				}
				// keep the first (leaf) name unique too
				for table.HasEffect(leaf.Name()) {
					leaf.SetName(leaf.Name() + " 1")
				}
				table.AddEffect(leaf)

				chain := func() chainEffect { return chainEffect{targetName: last.Name()} }
				autoOff := 0

				if s.FadeUpMs > 0 || s.FadeDownMs > 0 {
					f := newFade("", last.Name(), s.FadeUpMs, s.FadeDownMs)
					add(f, "FadeEffect")
				}
				if s.Blink != 0 && s.BlinkIntervalMsNested > 0 {
					b := &blinkEffect{chainEffect: chain(), high: -1, low: s.BlinkLow,
						activeMs:   int(float64(s.BlinkIntervalMsNested) * float64(s.BlinkPulseWidthNested) / 100),
						inactiveMs: int(float64(s.BlinkIntervalMsNested) * (100 - float64(s.BlinkPulseWidthNested)) / 100)}
					b.activeMs, b.inactiveMs = max1(b.activeMs), max1(b.inactiveMs)
					add(b, "BlinkEffect Inner")
				}
				if s.Blink != 0 {
					b := &blinkEffect{chainEffect: chain(), high: -1,
						activeMs:   int(float64(s.BlinkIntervalMs) * float64(s.BlinkPulseWidth) / 100),
						inactiveMs: int(float64(s.BlinkIntervalMs) * (100 - float64(s.BlinkPulseWidth)) / 100)}
					if s.BlinkIntervalMsNested == 0 {
						b.low = s.BlinkLow
					}
					b.activeMs, b.inactiveMs = max1(b.activeMs), max1(b.inactiveMs)
					add(b, "BlinkEffect")
				}
				if s.DurationMs > 0 || s.Blink > 0 {
					dur := s.DurationMs
					if dur <= 0 {
						dur = (s.Blink*2-1)*s.BlinkIntervalMs/2 + 1
					}
					add(&durationEffect{chainEffect: chain(), ms: dur}, "DurationEffect")
					autoOff = dur
				}
				if s.MaxDurationMs > 0 {
					add(&maxDurationEffect{chainEffect: chain(), ms: s.MaxDurationMs}, "MaxDurationEffect")
					if autoOff == 0 || s.MaxDurationMs < autoOff {
						autoOff = s.MaxDurationMs
					}
				}
				minDur := s.MinDurationMs
				defName := "MinDuratonEffect"
				if minDur <= 0 {
					defName = "DefaultMinDurationEffect"
					if isRGB {
						minDur = opt.MinRGBDurationMs
					} else {
						minDur = opt.MinDurationMs
					}
				}
				if minDur > 0 {
					add(&minDurationEffect{chainEffect: chain(), ms: minDur}, defName)
				}
				if s.ExtDurationMs > 0 {
					add(&extendDurationEffect{chainEffect: chain(), ms: s.ExtDurationMs}, "ExtDurationEffect")
				}
				if s.WaitMs > 0 {
					add(&delayEffect{chainEffect: chain(), ms: s.WaitMs}, "DelayEffect")
				}
				if s.Invert {
					add(&invertEffect{chainEffect: chain()}, "ValueInvertEffect")
				}
				if !s.NoBool {
					add(&fullRangeEffect{chainEffect: chain(), prev: map[elemKeyName]int{}}, "FullRangeEffect")
				}
				switch s.Control {
				case ControlCondition:
					ce := &conditionEffect{chainEffect: chain(), condition: s.Condition}
					add(ce, "TableElementConditionEffect")
					if expr, err := compileCondition(s.Condition); err == nil {
						for _, v := range expr.vars {
							table.assign(v, ce.Name(), 0)
						}
					} else {
						log.Warnf("invalid condition %s: %v", s.Condition, err)
					}
				case ControlFixedOn:
					table.static = append(table.static, &assignedEffect{name: last.Name()})
				case ControlControlled:
					for _, d := range strings.Split(s.TableElement, "|") {
						d = strings.TrimSpace(d)
						if d != "" {
							table.assign(d, last.Name(), autoOff)
						}
					}
				}
			}
		}
	}
	table.RomName = rom
	return true
}

func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

func uniqueToyName(c *Cabinet, base string) string {
	n := base
	for i := 1; c.HasToy(n); i++ {
		n = fmt.Sprintf("%s %d", base, i)
	}
	return n
}

func parseHTMLColor(s string) *ColorConfig {
	if len(s) != 7 && len(s) != 9 || !isHex(s[1:]) {
		return nil
	}
	c := &ColorConfig{Name: s, A: 255}
	c.R, c.G, c.B = hexToInt(s[1:3]), hexToInt(s[3:5]), hexToInt(s[5:7])
	if len(s) == 9 {
		c.A = hexToInt(s[7:9])
	}
	return c
}
