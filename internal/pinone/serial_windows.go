//go:build windows

package pinone

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	procGetCommState    = modKernel32.NewProc("GetCommState")
	procSetCommState    = modKernel32.NewProc("SetCommState")
	procSetCommTimeouts = modKernel32.NewProc("SetCommTimeouts")
	procEscapeCommFunc  = modKernel32.NewProc("EscapeCommFunction")
	procPurgeComm       = modKernel32.NewProc("PurgeComm")
)

type dcb struct {
	DCBlength  uint32
	BaudRate   uint32
	Flags      uint32
	WReserved  uint16
	XonLim     uint16
	XoffLim    uint16
	ByteSize   byte
	Parity     byte
	StopBits   byte
	XonChar    byte
	XoffChar   byte
	ErrorChar  byte
	EofChar    byte
	EvtChar    byte
	WReserved1 uint16
}

type commTimeouts struct {
	ReadIntervalTimeout         uint32
	ReadTotalTimeoutMultiplier  uint32
	ReadTotalTimeoutConstant    uint32
	WriteTotalTimeoutMultiplier uint32
	WriteTotalTimeoutConstant   uint32
}

type serialPort struct {
	h    syscall.Handle
	name string
}

func (s *serialPort) Describe() string { return "serial " + s.name }

func (s *serialPort) Close() error {
	if s.h != 0 && s.h != syscall.InvalidHandle {
		err := syscall.CloseHandle(s.h)
		s.h = syscall.InvalidHandle
		return err
	}
	return nil
}

func (s *serialPort) Write(packet []byte) error {
	var n uint32
	if err := syscall.WriteFile(s.h, packet, &n, nil); err != nil {
		return fmt.Errorf("serial write: %w", err)
	}
	if int(n) != len(packet) {
		return fmt.Errorf("short serial write: %d of %d", n, len(packet))
	}
	return nil
}

func openSerial(name string) (Transport, error) {
	full := name
	if !strings.HasPrefix(full, `\\.\`) {
		full = `\\.\` + name
	}
	h, err := syscall.CreateFile(syscall.StringToUTF16Ptr(full), syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	var d dcb
	d.DCBlength = uint32(unsafe.Sizeof(d))
	if r, _, e := procGetCommState.Call(uintptr(h), uintptr(unsafe.Pointer(&d))); r == 0 {
		syscall.CloseHandle(h)
		return nil, fmt.Errorf("GetCommState %s: %v", name, e)
	}
	d.BaudRate = SerialBaud
	d.ByteSize = 8
	d.Parity = 0
	d.StopBits = 0
	// fBinary=1, fDtrControl=ENABLE(1), everything else off.
	d.Flags = 1 | (1 << 4)
	if r, _, e := procSetCommState.Call(uintptr(h), uintptr(unsafe.Pointer(&d))); r == 0 {
		syscall.CloseHandle(h)
		return nil, fmt.Errorf("SetCommState %s: %v", name, e)
	}
	to := commTimeouts{ReadIntervalTimeout: 0xFFFFFFFF, WriteTotalTimeoutConstant: 500}
	procSetCommTimeouts.Call(uintptr(h), uintptr(unsafe.Pointer(&to)))
	procEscapeCommFunc.Call(uintptr(h), 5) // SETDTR
	return &serialPort{h: h, name: name}, nil
}

// findLegacyPort looks up the COM port of a legacy-firmware PinOne board in
// the registry (HKLM\SYSTEM\CurrentControlSet\Enum\USB\VID_0E8F&PID_9207).
func findLegacyPort() string {
	base := fmt.Sprintf(`SYSTEM\CurrentControlSet\Enum\USB\VID_%04X&PID_%04X`, LegacyVendorID, LegacyProduct)
	var hk syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, syscall.StringToUTF16Ptr(base), 0, syscall.KEY_READ, &hk); err != nil {
		return ""
	}
	defer syscall.RegCloseKey(hk)
	for i := uint32(0); ; i++ {
		var name [256]uint16
		n := uint32(len(name))
		if err := syscall.RegEnumKeyEx(hk, i, &name[0], &n, nil, nil, nil, nil); err != nil {
			return ""
		}
		sub := base + `\` + syscall.UTF16ToString(name[:n]) + `\Device Parameters`
		var hk2 syscall.Handle
		if err := syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, syscall.StringToUTF16Ptr(sub), 0, syscall.KEY_READ, &hk2); err != nil {
			continue
		}
		var typ, size uint32 = 0, 128
		buf := make([]uint16, 64)
		err := syscall.RegQueryValueEx(hk2, syscall.StringToUTF16Ptr("PortName"), nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &size)
		syscall.RegCloseKey(hk2)
		if err == nil {
			if p := syscall.UTF16ToString(buf); strings.HasPrefix(strings.ToUpper(p), "COM") {
				return p
			}
		}
	}
}
