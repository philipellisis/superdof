package dof

import (
	"fmt"
	"strconv"
	"strings"

	"superdof/internal/ftdi"
	"superdof/internal/serial"
)

// CabinetSpec is the declarative description of the cabinet hardware and
// toys. It can be loaded from a DOF cabinet.xml or from the JSON config.
type CabinetSpec struct {
	// AutoConfig adds a PinOne (and FTDI chips) when none is configured.
	AutoConfig  *bool            `json:"autoConfig,omitempty"`
	Controllers []ControllerSpec `json:"controllers,omitempty"`
	Toys        []ToySpec        `json:"toys,omitempty"`
}

// ControllerSpec describes one output controller.
type ControllerSpec struct {
	// Type is pinone, wemosd1, teensy or ftdi.
	Type string `json:"type"`
	Name string `json:"name,omitempty"`

	// pinone: unit number (1-8) and connection ("auto", "hid", "COM4")
	Number               int    `json:"number,omitempty"`
	Port                 string `json:"port,omitempty"`
	MinCommandIntervalMs int    `json:"minCommandIntervalMs,omitempty"`

	// ftdi
	Serial string `json:"serial,omitempty"`

	// wemosd1 / teensy
	ComPort               string `json:"comPort,omitempty"`
	BaudRate              int    `json:"baudRate,omitempty"`
	LedsPerStrip          []int  `json:"ledsPerStrip,omitempty"` // up to 10 channels
	TimeoutMs             int    `json:"timeoutMs,omitempty"`
	OpenWaitMs            int    `json:"openWaitMs,omitempty"`
	HandshakeStartWaitMs  int    `json:"handshakeStartWaitMs,omitempty"`
	HandshakeEndWaitMs    int    `json:"handshakeEndWaitMs,omitempty"`
	DTR                   bool   `json:"dtr,omitempty"`
	SendPerLedstripLength bool   `json:"sendPerLedstripLength,omitempty"`
	UseCompression        bool   `json:"useCompression,omitempty"`
	TestOnConnect         bool   `json:"testOnConnect,omitempty"`
}

// OutputRef maps a LedWizEquivalent output number to an output/toy name.
type OutputRef struct {
	Output string `json:"output"`
	Number int    `json:"number"`
}

// ToySpec describes one toy.
type ToySpec struct {
	// Type is ledwizequivalent, lamp (any single output toy), rgb or ledstrip.
	Type string `json:"type"`
	Name string `json:"name"`

	// ledwizequivalent
	Number  int         `json:"number,omitempty"`
	Outputs []OutputRef `json:"outputs,omitempty"`

	// lamp
	Output string `json:"output,omitempty"`
	// rgb
	Red   string `json:"red,omitempty"`
	Green string `json:"green,omitempty"`
	Blue  string `json:"blue,omitempty"`

	Curve string `json:"curve,omitempty"`

	// ledstrip
	Width       int     `json:"width,omitempty"`
	Height      int     `json:"height,omitempty"`
	Arrangement string  `json:"arrangement,omitempty"`
	ColorOrder  string  `json:"colorOrder,omitempty"`
	FirstLed    int     `json:"firstLed,omitempty"`
	Brightness  int     `json:"brightness,omitempty"`
	Gamma       float32 `json:"gamma,omitempty"`
	Controller  string  `json:"controller,omitempty"`
}

func (s *CabinetSpec) autoConfig() bool { return s.AutoConfig == nil || *s.AutoConfig }

// specFromXML converts a DOF cabinet.xml tree into a spec.
func specFromXML(root *xnode, log Logger) *CabinetSpec {
	spec := &CabinetSpec{}
	if v := root.text("AutoConfigEnabled"); v != "" {
		b := strings.EqualFold(v, "true")
		spec.AutoConfig = &b
	}
	atoi := func(n *xnode, k string) int { v, _ := strconv.Atoi(n.text(k)); return v }
	if ocs := root.child("OutputControllers"); ocs != nil {
		for _, c := range ocs.Children {
			cs := ControllerSpec{Name: c.text("Name")}
			switch c.Name {
			case "PinOne":
				cs.Type, cs.Number, cs.Port = "pinone", atoi(c, "Number"), c.text("ComPort")
				cs.MinCommandIntervalMs = atoi(c, "MinCommandIntervalMs")
			case "WemosD1MPStripController", "WemosD1StripController", "TeensyStripController":
				cs.Type = "wemosd1"
				if c.Name == "TeensyStripController" {
					cs.Type = "teensy"
				}
				cs.ComPort = c.text("ComPortName")
				cs.BaudRate = atoi(c, "ComPortBaudRate")
				cs.TimeoutMs = atoi(c, "ComPortTimeOutMs")
				cs.OpenWaitMs = atoi(c, "ComPortOpenWaitMs")
				cs.HandshakeStartWaitMs = atoi(c, "ComPortHandshakeStartWaitMs")
				cs.HandshakeEndWaitMs = atoi(c, "ComPortHandshakeEndWaitMs")
				cs.DTR = strings.EqualFold(c.text("ComPortDtrEnable"), "true")
				cs.SendPerLedstripLength = strings.EqualFold(c.text("SendPerLedstripLength"), "true")
				cs.UseCompression = strings.EqualFold(c.text("UseCompression"), "true")
				cs.TestOnConnect = strings.EqualFold(c.text("TestOnConnect"), "true")
				last := 0
				leds := make([]int, 10)
				for i := 1; i <= 10; i++ {
					leds[i-1] = atoi(c, fmt.Sprintf("NumberOfLedsStrip%d", i))
					if leds[i-1] > 0 {
						last = i
					}
				}
				cs.LedsPerStrip = leds[:last]
			case "FT245RBitbangController":
				cs.Type, cs.Serial = "ftdi", c.text("SerialNumber")
			default:
				log.Infof("cabinet.xml: ignoring unsupported output controller %s", c.Name)
				continue
			}
			spec.Controllers = append(spec.Controllers, cs)
		}
	}
	if toys := root.child("Toys"); toys != nil {
		for _, t := range toys.Children {
			ts := ToySpec{Name: t.text("Name"), Curve: t.text("FadingCurveName")}
			switch t.Name {
			case "LedWizEquivalent":
				ts.Type, ts.Number = "ledwizequivalent", atoi(t, "LedWizNumber")
				if outs := t.child("Outputs"); outs != nil {
					for _, o := range outs.Children {
						ts.Outputs = append(ts.Outputs, OutputRef{Output: o.text("OutputName"), Number: atoi(o, "LedWizEquivalentOutputNumber")})
					}
				}
			case "Lamp", "Motor", "Shaker", "GearMotor", "Hardware", "AnalogAlphaToy":
				ts.Type, ts.Output = "lamp", t.text("OutputName")
			case "RGBLed", "RGBAToy":
				ts.Type, ts.Red, ts.Green, ts.Blue = "rgb", t.text("OutputNameRed"), t.text("OutputNameGreen"), t.text("OutputNameBlue")
			case "LedStrip":
				ts.Type = "ledstrip"
				ts.Width, ts.Height = atoi(t, "Width"), atoi(t, "Height")
				ts.Arrangement, ts.ColorOrder = t.text("LedStripArrangement"), t.text("ColorOrder")
				ts.FirstLed, ts.Brightness = atoi(t, "FirstLedNumber"), atoi(t, "Brightness")
				ts.Controller = t.text("OutputControllerName")
				if g, err := strconv.ParseFloat(t.text("BrightnessGammaCorrection"), 32); err == nil {
					ts.Gamma = float32(g)
				}
			default:
				log.Infof("cabinet.xml: ignoring unsupported %s toy %q", t.Name, ts.Name)
				continue
			}
			spec.Toys = append(spec.Toys, ts)
		}
	}
	return spec
}

// LoadCabinetXML reads a DOF cabinet.xml into a spec.
func LoadCabinetXML(path string, log Logger) (*CabinetSpec, error) {
	if log == nil {
		log = nopLogger{}
	}
	root, err := readXML(path)
	if err != nil {
		return nil, err
	}
	return specFromXML(root, log), nil
}

// buildController creates the controller of a spec entry.
func buildController(cs ControllerSpec, engine Config, log Logger) (Controller, error) {
	switch strings.ToLower(cs.Type) {
	case "pinone":
		n := cs.Number
		if n == 0 {
			n = 1
		}
		port := cs.Port
		if port == "" {
			port = engine.PinOne
		}
		iv := cs.MinCommandIntervalMs
		if iv == 0 {
			iv = engine.MinCommandIntervalMs
		}
		c := NewPinOneController(n, port, iv, log)
		if cs.Name != "" {
			c.name = cs.Name
			for _, o := range c.outputs {
				o.Name = fmt.Sprintf("%s.%d", c.name, o.Number)
			}
		}
		return c, nil
	case "wemosd1", "wemosd1mp", "teensy":
		cfg := StripConfig{Name: cs.Name, ComPort: cs.ComPort, Baud: cs.BaudRate, TimeoutMs: cs.TimeoutMs, OpenWaitMs: cs.OpenWaitMs,
			HandshakeMs: cs.HandshakeStartWaitMs, HandshakeEnd: cs.HandshakeEndWaitMs, DTR: cs.DTR}
		if cfg.Name == "" {
			cfg.Name = "LED Strips 0"
		}
		if strings.ToLower(cs.Type) != "teensy" {
			cfg.PerStripSize, cfg.Compression, cfg.TestOnConnect = cs.SendPerLedstripLength, cs.UseCompression, cs.TestOnConnect
		}
		if len(cs.LedsPerStrip) > 10 {
			return nil, fmt.Errorf("%s: at most 10 LED strips per controller", cfg.Name)
		}
		copy(cfg.LedsPerStrip[:], cs.LedsPerStrip)
		sc := NewStripController(cfg, log)
		if engine.DryRun {
			sc.Open = func(serial.Config) (serial.Port, error) { return &dryPort{}, nil }
		}
		return sc, nil
	case "ftdi":
		c := NewFTDIController(0, cs.Serial, log)
		if engine.DryRun {
			c.Open = func(string) (ftdi.Handle, error) { return dryFTDI{}, nil }
		}
		if cs.Name != "" {
			c.name = cs.Name
			for _, o := range c.outputs {
				o.Name = fmt.Sprintf("%s.%02d", c.name, o.Number)
			}
		}
		return c, nil
	}
	return nil, fmt.Errorf("unknown controller type %q", cs.Type)
}

// applyToys adds the toys of a spec to the cabinet (after controllers exist).
func (c *Cabinet) applyToys(toys []ToySpec, log Logger) {
	for _, t := range toys {
		switch strings.ToLower(t.Type) {
		case "ledwizequivalent", "lwe":
			l := &LedWizEquivalent{name: t.Name, Number: t.Number}
			if l.name == "" {
				l.name = fmt.Sprintf("LedWizEquivalent %d", t.Number)
			}
			for _, o := range t.Outputs {
				l.Outputs = append(l.Outputs, LWEOutput{OutputName: o.Output, Number: o.Number})
			}
			c.AddToy(l)
		case "lamp", "analog", "shaker", "motor":
			c.AddToy(c.NewAnalogToy(t.Name, t.Output, t.Curve))
		case "rgb", "rgbled":
			c.AddToy(c.NewRGBToy(t.Name, t.Red, t.Green, t.Blue, t.Curve))
		case "ledstrip", "strip":
			s := NewLedStrip(t.Name)
			if t.Width > 0 {
				s.Width = t.Width
			}
			if t.Height > 0 {
				s.Height = t.Height
			}
			if t.Arrangement != "" {
				s.Arrangement = t.Arrangement
			}
			if t.ColorOrder != "" {
				s.ColorOrder = t.ColorOrder
			}
			if t.FirstLed > 0 {
				s.FirstLed = t.FirstLed
			}
			if t.Brightness > 0 {
				s.Brightness = t.Brightness
			}
			if t.Gamma > 0 {
				s.Gamma = t.Gamma
			}
			s.CurveName, s.Controller = t.Curve, t.Controller
			c.AddToy(s)
		default:
			log.Warnf("unknown toy type %q (%s)", t.Type, t.Name)
		}
	}
}

// dryPort is a serial port that acts like a permanently ready LED controller.
type dryPort struct{ q []byte }

func (d *dryPort) Write(p []byte) (int, error) {
	// answer every command with an ACK / the 3 byte capability answer
	if len(p) == 1 && p[0] == 'M' {
		d.q = append(d.q, 0xFF, 0xFF, 'A')
	} else if len(p) > 0 {
		d.q = append(d.q, 'A')
	}
	return len(p), nil
}
func (d *dryPort) Read(p []byte) (int, error) {
	if len(d.q) == 0 {
		return 0, serial.ErrTimeout
	}
	n := copy(p, d.q)
	d.q = d.q[n:]
	return n, nil
}
func (d *dryPort) Available() int { return len(d.q) }
func (d *dryPort) Flush()         { d.q = nil }
func (d *dryPort) Close() error   { return nil }

type dryFTDI struct{}

func (dryFTDI) Write(byte) error { return nil }
func (dryFTDI) Close() error     { return nil }
