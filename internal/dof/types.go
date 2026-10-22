// Package dof is a focused Go port of the parts of the DirectOutput Framework
// (DOF) that are needed to drive a PinOne board from LedControl/DirectOutputConfig
// ini files: table elements, the effect chain, toys/layers, outputs and the
// PinOne output controller.
package dof

import (
	"math"
	"strconv"
	"strings"
)

// ElementType is the single character table element type used in ini files.
type ElementType byte

const (
	ElemUnknown      ElementType = '?'
	ElemLamp         ElementType = 'L'
	ElemSwitch       ElementType = 'W'
	ElemSolenoid     ElementType = 'S'
	ElemGIString     ElementType = 'G'
	ElemMech         ElementType = 'M'
	ElemGetMech      ElementType = 'N'
	ElemEMTable      ElementType = 'E'
	ElemLED          ElementType = 'D'
	ElemScore        ElementType = 'C'
	ElemScoreDigit   ElementType = 'B'
	ElemNamedElement ElementType = '$'
)

func isElementType(c byte) bool {
	switch ElementType(c) {
	case ElemUnknown, ElemLamp, ElemSwitch, ElemSolenoid, ElemGIString, ElemMech,
		ElemGetMech, ElemEMTable, ElemLED, ElemScore, ElemScoreDigit, ElemNamedElement:
		return true
	}
	return false
}

// TED is the data sent to a table element (TableElementData in DOF).
type TED struct {
	Type   ElementType
	Number int
	Name   string
	Value  int
}

// NewTED builds element data from a descriptor such as "E101" or "$MyName".
func NewTED(descriptor string, value int) TED {
	if len(descriptor) > 1 && isElementType(descriptor[0]) && ElementType(descriptor[0]) != ElemNamedElement && isInteger(descriptor[1:]) {
		n, _ := strconv.Atoi(descriptor[1:])
		return TED{Type: ElementType(descriptor[0]), Number: n, Value: value}
	}
	name := descriptor
	if len(name) > 0 && name[0] == '$' {
		name = name[1:]
	}
	return TED{Type: ElemNamedElement, Name: strings.ToUpper(name), Number: math.MinInt32, Value: value}
}

func isInteger(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	_, err := strconv.ParseInt(strings.TrimSpace(s), 10, 32)
	return err == nil
}

func toInt(s string) int {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 32)
	if err != nil {
		return 0
	}
	return int(n)
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func hexToInt(s string) int {
	n, _ := strconv.ParseInt(s, 16, 32)
	return int(n)
}

func limit(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Logger receives engine diagnostics.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
}

type nopLogger struct{}

func (nopLogger) Infof(string, ...any) {}
func (nopLogger) Warnf(string, ...any) {}
