package dof

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// xnode is a minimal generic XML tree used to read DOF cabinet.xml files
// (which are .NET XmlSerializer output).
type xnode struct {
	Name     string
	Text     string
	Children []*xnode
}

func (n *xnode) child(name string) *xnode {
	for _, c := range n.Children {
		if strings.EqualFold(c.Name, name) {
			return c
		}
	}
	return nil
}

func (n *xnode) text(name string) string {
	if c := n.child(name); c != nil {
		return strings.TrimSpace(c.Text)
	}
	return ""
}

func readXML(path string) (*xnode, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := xml.NewDecoder(f)
	dec.CharsetReader = func(label string, in io.Reader) (io.Reader, error) { return in, nil }
	var stack []*xnode
	var root *xnode
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xnode{Name: t.Name.Local}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.Children = append(p.Children, n)
			} else {
				root = n
			}
			stack = append(stack, n)
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("empty xml file")
	}
	return root, nil
}

// GlobalConfig holds the subset of DOF's GlobalConfig.xml that is used.
type GlobalConfig struct {
	IniFilesPath             string
	CabinetConfigFilePattern string
	MinEffectDurationMs      int
	MinRGBEffectDurationMs   int
	PinOneComPort            string
	PinOneMinCommandInterval int
}

// LoadGlobalConfig reads a DOF GlobalConfig xml file.
func LoadGlobalConfig(path string) (*GlobalConfig, error) {
	root, err := readXML(path)
	if err != nil {
		return nil, err
	}
	g := &GlobalConfig{}
	g.IniFilesPath = root.text("IniFilesPath")
	g.CabinetConfigFilePattern = root.text("CabinetConfigFilePattern")
	g.PinOneComPort = root.text("PinOneComPort")
	atoi := func(k string) int { n, _ := strconv.Atoi(root.text(k)); return n }
	g.MinEffectDurationMs = atoi("LedControlMinimumEffectDurationMs")
	g.MinRGBEffectDurationMs = atoi("LedControlMinimumRGBEffectDurationMs")
	g.PinOneMinCommandInterval = atoi("PinOneDefaultMinCommandIntervalMs")
	return g, nil
}

// autoConfigLWE adds a LedWizEquivalent for every PinOne (number 10+unit,
// 63 outputs) and FTDI chip (number 40+id, 8 outputs) that has none, like
// the DOF auto configurators.
func (c *Cabinet) autoConfigLWE(log Logger) {
	for _, ct := range c.Controllers {
		n, count := 0, 0
		switch p := ct.(type) {
		case *PinOneController:
			n, count = p.Number()+10, PinOneOutputs
		case *FTDIController:
			n, count = p.ID()+40, 8
		default:
			continue
		}
		if c.lwe[n] != nil {
			continue
		}
		l := &LedWizEquivalent{name: ct.Name() + " Equivalent", Number: n}
		for i := 1; i <= count; i++ {
			l.Outputs = append(l.Outputs, LWEOutput{OutputName: fmt.Sprintf("%s\\%s.%02d", ct.Name(), ct.Name(), i), Number: i})
		}
		c.AddToy(l)
		log.Infof("added LedWizEquivalent %d for %s", n, ct.Name())
	}
}

// LoadTableMappings reads DOF's tablemappings.xml (TableName -> RomName),
// keyed by lower case table name.
func LoadTableMappings(path string) (map[string]string, error) {
	root, err := readXML(path)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, mp := range root.Children {
		if !strings.EqualFold(mp.Name, "Mapping") {
			continue
		}
		t, r := mp.text("TableName"), mp.text("RomName")
		if t != "" && r != "" {
			m[strings.ToLower(t)] = r
		}
	}
	return m, nil
}
