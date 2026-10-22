// Package pinone talks to the Cleveland Software Design PinOne board.
//
// Two firmware generations exist, both are supported:
//
//   - current firmware: USB HID vendor channel (VID 0x39F5, PID 0x9208,
//     usage page 0xFF00 / usage 1). Output reports are 64 bytes: report ID 6
//     followed by the 9 byte protocol packet, zero padded.
//   - legacy firmware: USB CDC serial port (VID 0x0E8F, PID 0x9207). The 9
//     byte protocol packet is written directly to the port.
//
// A protocol packet is [0, 200+bank, v1..v7]: it sets the brightness (0-255)
// of the 7 outputs of one bank (bank 0 = outputs 1-7, bank 1 = 8-14, ...).
package pinone

import (
	"errors"
	"fmt"
	"strings"
)

const (
	HIDVendorID    = 0x39F5
	HIDProductID   = 0x9208
	HIDUsagePage   = 0xFF00
	HIDUsage       = 0x01
	HIDReportID    = 6
	HIDReportSize  = 64
	LegacyVendorID = 0x0E8F
	LegacyProduct  = 0x9207
	SerialBaud     = 2000000
	PacketSize     = 9
)

// Transport sends 9 byte protocol packets to the board.
type Transport interface {
	Write(packet []byte) error
	Close() error
	Describe() string
}

// ErrNotFound is returned when no board is connected.
var ErrNotFound = errors.New("no PinOne board found")

// Open connects to a PinOne board. port is "" / "auto" (HID first, then a
// legacy serial board), "hid", or a serial port name such as COM4 or
// /dev/ttyACM0.
func Open(port string) (Transport, error) {
	p := strings.TrimSpace(port)
	switch strings.ToLower(p) {
	case "", "auto":
		if t, err := openHID(); err == nil {
			return t, nil
		}
		if sp := findLegacyPort(); sp != "" {
			return openSerial(sp)
		}
		return nil, ErrNotFound
	case "hid":
		return openHID()
	default:
		return openSerial(p)
	}
}

// HIDReport wraps a 9 byte protocol packet in a full HID output report.
func HIDReport(packet []byte) []byte {
	buf := make([]byte, HIDReportSize)
	buf[0] = HIDReportID
	n := len(packet)
	if n > HIDReportSize-1 {
		n = HIDReportSize - 1
	}
	copy(buf[1:], packet[:n])
	return buf
}

// MemoryTransport records packets (tests and dry runs).
type MemoryTransport struct {
	Packets [][]byte
	Fail    error
}

func (m *MemoryTransport) Write(p []byte) error {
	if m.Fail != nil {
		return m.Fail
	}
	m.Packets = append(m.Packets, append([]byte(nil), p...))
	return nil
}
func (m *MemoryTransport) Close() error { return nil }
func (m *MemoryTransport) Describe() string {
	return fmt.Sprintf("memory (%d packets)", len(m.Packets))
}
