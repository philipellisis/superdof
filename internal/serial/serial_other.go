//go:build !windows && !linux

package serial

import "fmt"

// Open is not implemented on this platform.
func Open(c Config) (Port, error) {
	return nil, fmt.Errorf("serial ports are not implemented on this platform")
}

// PortNames returns nothing.
func PortNames() []string { return nil }
