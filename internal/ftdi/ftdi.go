// Package ftdi drives FTDI FT245R bit-bang chips (8 digital outputs, e.g.
// SainSmart relay boards) through the FTDI D2XX driver (ftd2xx.dll).
package ftdi

import "errors"

// ErrUnavailable is returned when the D2XX driver is not installed.
var ErrUnavailable = errors.New("ftdi: ftd2xx driver not available")

// Device describes an attached FTDI chip.
type Device struct {
	Serial      string
	Description string
}

// Handle is an open chip in synchronous bit-bang mode.
type Handle interface {
	// Write sets the 8 output pins (bit n = output n+1).
	Write(value byte) error
	Close() error
}
