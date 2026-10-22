//go:build windows

package serial

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	k32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetCommState    = k32.NewProc("GetCommState")
	procSetCommState    = k32.NewProc("SetCommState")
	procSetCommTimeouts = k32.NewProc("SetCommTimeouts")
	procEscapeComm      = k32.NewProc("EscapeCommFunction")
	procPurgeComm       = k32.NewProc("PurgeComm")
	procClearCommError  = k32.NewProc("ClearCommError")
	procRegEnumValue    = syscall.NewLazyDLL("advapi32.dll").NewProc("RegEnumValueW")
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

type timeouts struct {
	ReadInterval, ReadMult, ReadConst, WriteMult, WriteConst uint32
}

type comstat struct {
	Flags    uint32
	CbInQue  uint32
	CbOutQue uint32
}

type port struct{ h syscall.Handle }

// Open opens a serial port.
func Open(c Config) (Port, error) {
	c.defaults()
	name := c.Name
	if !strings.HasPrefix(name, `\\.\`) {
		name = `\\.\` + name
	}
	h, err := syscall.CreateFile(syscall.StringToUTF16Ptr(name), syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", c.Name, err)
	}
	var d dcb
	d.DCBlength = uint32(unsafe.Sizeof(d))
	if r, _, e := procGetCommState.Call(uintptr(h), uintptr(unsafe.Pointer(&d))); r == 0 {
		syscall.CloseHandle(h)
		return nil, fmt.Errorf("GetCommState: %v", e)
	}
	d.BaudRate = uint32(c.Baud)
	d.ByteSize = byte(c.DataBits)
	switch strings.ToLower(c.Parity) {
	case "odd":
		d.Parity = 1
	case "even":
		d.Parity = 2
	default:
		d.Parity = 0
	}
	if c.StopBits == 2 {
		d.StopBits = 2
	} else {
		d.StopBits = 0
	}
	d.Flags = 1 // fBinary, no flow control
	if c.DTR {
		d.Flags |= 1 << 4
	}
	if r, _, e := procSetCommState.Call(uintptr(h), uintptr(unsafe.Pointer(&d))); r == 0 {
		syscall.CloseHandle(h)
		return nil, fmt.Errorf("SetCommState: %v", e)
	}
	t := timeouts{ReadConst: uint32(c.TimeoutMs), WriteConst: uint32(c.TimeoutMs)}
	procSetCommTimeouts.Call(uintptr(h), uintptr(unsafe.Pointer(&t)))
	if c.DTR {
		procEscapeComm.Call(uintptr(h), 5) // SETDTR
	} else {
		procEscapeComm.Call(uintptr(h), 6) // CLRDTR
	}
	return &port{h: h}, nil
}

func (p *port) Read(b []byte) (int, error) {
	var n uint32
	if err := syscall.ReadFile(p.h, b, &n, nil); err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, ErrTimeout
	}
	return int(n), nil
}

func (p *port) Write(b []byte) (int, error) {
	var n uint32
	err := syscall.WriteFile(p.h, b, &n, nil)
	if err == nil && int(n) != len(b) {
		err = fmt.Errorf("serial: short write %d/%d", n, len(b))
	}
	return int(n), err
}

func (p *port) Available() int {
	var errs uint32
	var st comstat
	procClearCommError.Call(uintptr(p.h), uintptr(unsafe.Pointer(&errs)), uintptr(unsafe.Pointer(&st)))
	return int(st.CbInQue)
}

func (p *port) Flush() { procPurgeComm.Call(uintptr(p.h), 0x0008) } // PURGE_RXCLEAR

func (p *port) Close() error { return syscall.CloseHandle(p.h) }

// PortNames lists COM ports present in the registry (HARDWARE\DEVICEMAP\SERIALCOMM).
func PortNames() []string {
	var hk syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, syscall.StringToUTF16Ptr(`HARDWARE\DEVICEMAP\SERIALCOMM`), 0, syscall.KEY_READ, &hk); err != nil {
		return nil
	}
	defer syscall.RegCloseKey(hk)
	var out []string
	for i := uint32(0); ; i++ {
		var name [256]uint16
		nl := uint32(len(name))
		var typ uint32
		var data [128]uint16
		dl := uint32(len(data) * 2)
		r, _, _ := procRegEnumValue.Call(uintptr(hk), uintptr(i), uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&nl)), 0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&dl)))
		if r != 0 {
			break
		}
		out = append(out, syscall.UTF16ToString(data[:]))
	}
	return out
}
