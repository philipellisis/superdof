// Package serial is a minimal blocking serial port with read timeouts.
package serial

import "errors"

// ErrTimeout is returned by Read when no data arrived in time.
var ErrTimeout = errors.New("serial: read timeout")

// Config describes a port.
type Config struct {
	Name      string
	Baud      int
	DataBits  int    // default 8
	Parity    string // "none" (default), "even", "odd"
	StopBits  int    // 1 (default) or 2
	DTR       bool
	TimeoutMs int // read and write timeout, default 200
}

// Port is an open serial port.
type Port interface {
	// Read reads up to len(p) bytes; returns ErrTimeout when none arrive.
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	// Available returns the number of bytes waiting in the receive buffer.
	Available() int
	// Flush discards unread input.
	Flush()
	Close() error
}

func (c *Config) defaults() {
	if c.DataBits == 0 {
		c.DataBits = 8
	}
	if c.StopBits == 0 {
		c.StopBits = 1
	}
	if c.TimeoutMs == 0 {
		c.TimeoutMs = 200
	}
	if c.Baud == 0 {
		c.Baud = 9600
	}
}
