//go:build !windows && !linux

package pinone

import "fmt"

func openHID() (Transport, error) {
	return nil, fmt.Errorf("PinOne USB HID is not implemented on this platform yet")
}

func openSerial(name string) (Transport, error) {
	return nil, fmt.Errorf("PinOne serial is not implemented on this platform yet")
}

func findLegacyPort() string { return "" }
