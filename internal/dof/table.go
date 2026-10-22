package dof

import (
	"math"
	"strings"
)

type assignedEffect struct {
	name   string
	effect Effect
}

// TableElement holds the current value of an element and the effects it triggers.
type TableElement struct {
	Type     ElementType
	Number   int
	Name     string
	Value    int
	assigned []*assignedEffect
	// AutoOffMs is the longest self terminating duration of the assigned
	// effects (used for DOFLinx "FF_DOF Ennn,-1" pulses). 0 means "stays on".
	AutoOffMs int
	table     *Table
}

func (e *TableElement) data() TED {
	return TED{Type: e.Type, Number: e.Number, Name: e.Name, Value: e.Value}
}

func (e *TableElement) set(v int) {
	if e.Value == v {
		return
	}
	e.Value = v
	d := e.data()
	for _, a := range e.assigned {
		if a.effect != nil {
			a.effect.Trigger(d)
		}
	}
}

type elemKey struct {
	t ElementType
	n int
}

// Table is the set of elements, effects and static effects for one game.
type Table struct {
	Name        string
	RomName     string
	engine      *Engine
	numbered    map[elemKey]*TableElement
	named       map[string]*TableElement
	Effects     map[string]Effect
	effectOrder []string
	static      []*assignedEffect
	cab         *Cabinet
}

func NewTable() *Table {
	return &Table{
		numbered: map[elemKey]*TableElement{},
		named:    map[string]*TableElement{},
		Effects:  map[string]Effect{},
	}
}

func (t *Table) AddEffect(e Effect) {
	t.Effects[e.Name()] = e
	t.effectOrder = append(t.effectOrder, e.Name())
}

func (t *Table) HasEffect(name string) bool { _, ok := t.Effects[name]; return ok }

// element returns (creating when needed) the element for the data.
func (t *Table) element(d TED) *TableElement {
	if d.Type == ElemNamedElement {
		n := strings.ToUpper(d.Name)
		if e, ok := t.named[n]; ok {
			return e
		}
		e := &TableElement{Type: ElemNamedElement, Name: n, Number: math.MinInt32, Value: math.MinInt32, table: t}
		t.named[n] = e
		return e
	}
	k := elemKey{d.Type, d.Number}
	if e, ok := t.numbered[k]; ok {
		return e
	}
	e := &TableElement{Type: d.Type, Number: d.Number, Value: math.MinInt32, table: t}
	t.numbered[k] = e
	return e
}

// Lookup returns an existing element or nil.
func (t *Table) Lookup(d TED) *TableElement {
	if d.Type == ElemNamedElement {
		return t.named[strings.ToUpper(d.Name)]
	}
	return t.numbered[elemKey{d.Type, d.Number}]
}

// UpdateElement applies new data to an element, triggering its effects on change.
func (t *Table) UpdateElement(d TED) {
	t.element(d).set(d.Value)
}

// assign registers an effect for the element described by descriptor
// (creating the element at value 0 like the DOF configurator does).
func (t *Table) assign(descriptor string, effectName string, autoOffMs int) {
	d := NewTED(descriptor, 0)
	el := t.element(d)
	if el.Value == math.MinInt32 {
		el.Value = 0
	}
	el.assigned = append(el.assigned, &assignedEffect{name: effectName})
	if autoOffMs > el.AutoOffMs {
		el.AutoOffMs = autoOffMs
	}
}

func (t *Table) init(e *Engine, c *Cabinet) {
	t.engine = e
	t.cab = c
	for _, n := range t.effectOrder {
		t.Effects[n].Init(t)
	}
	resolve := func(as []*assignedEffect) {
		for _, a := range as {
			if ef, ok := t.Effects[a.name]; ok {
				a.effect = ef
			}
		}
	}
	for _, el := range t.numbered {
		resolve(el.assigned)
	}
	for _, el := range t.named {
		resolve(el.assigned)
	}
	resolve(t.static)
}

func (t *Table) triggerStatic() {
	d := TED{Type: ElemUnknown, Value: 1}
	for _, a := range t.static {
		if a.effect != nil {
			a.effect.Trigger(d)
		}
	}
}

func (t *Table) finish() {
	for _, n := range t.effectOrder {
		t.Effects[n].Finish()
	}
}

// ElementCount reports the number of known elements.
func (t *Table) ElementCount() int { return len(t.numbered) + len(t.named) }
