//go:build windows

package ftdi

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	dll                = syscall.NewLazyDLL("ftd2xx.dll")
	procCreateInfoList = dll.NewProc("FT_CreateDeviceInfoList")
	procGetInfoList    = dll.NewProc("FT_GetDeviceInfoList")
	procOpenEx         = dll.NewProc("FT_OpenEx")
	procSetBitMode     = dll.NewProc("FT_SetBitMode")
	procWrite          = dll.NewProc("FT_Write")
	procClose          = dll.NewProc("FT_Close")
)

const (
	openBySerialNumber = 1
	bitModeSyncBitbang = 0x04
	ftOK               = 0
)

type infoNode struct {
	Flags        uint32
	Type         uint32
	ID           uint32
	LocID        uint32
	SerialNumber [16]byte
	Description  [64]byte
	Handle       uintptr
}

func cstr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// List returns the attached FTDI chips.
func List() ([]Device, error) {
	if err := dll.Load(); err != nil {
		return nil, ErrUnavailable
	}
	var n uint32
	if r, _, _ := procCreateInfoList.Call(uintptr(unsafe.Pointer(&n))); r != ftOK {
		return nil, fmt.Errorf("FT_CreateDeviceInfoList failed (%d)", r)
	}
	if n == 0 {
		return nil, nil
	}
	nodes := make([]infoNode, n)
	if r, _, _ := procGetInfoList.Call(uintptr(unsafe.Pointer(&nodes[0])), uintptr(unsafe.Pointer(&n))); r != ftOK {
		return nil, fmt.Errorf("FT_GetDeviceInfoList failed (%d)", r)
	}
	var out []Device
	for _, nd := range nodes[:n] {
		out = append(out, Device{Serial: cstr(nd.SerialNumber[:]), Description: cstr(nd.Description[:])})
	}
	return out, nil
}

type handle struct{ h uintptr }

// Open opens the chip with the given serial number in sync bit-bang mode
// with all 8 pins as outputs.
func Open(serial string) (Handle, error) {
	if err := dll.Load(); err != nil {
		return nil, ErrUnavailable
	}
	b := append([]byte(serial), 0)
	var h uintptr
	if r, _, _ := procOpenEx.Call(uintptr(unsafe.Pointer(&b[0])), openBySerialNumber, uintptr(unsafe.Pointer(&h))); r != ftOK {
		return nil, fmt.Errorf("FT_OpenEx(%s) failed (status %d)", serial, r)
	}
	if r, _, _ := procSetBitMode.Call(h, 0xFF, bitModeSyncBitbang); r != ftOK {
		procClose.Call(h)
		return nil, fmt.Errorf("FT_SetBitMode failed (status %d)", r)
	}
	return &handle{h: h}, nil
}

func (d *handle) Write(v byte) error {
	buf := []byte{v}
	var n uint32
	r, _, _ := procWrite.Call(d.h, uintptr(unsafe.Pointer(&buf[0])), 1, uintptr(unsafe.Pointer(&n)))
	if r != ftOK {
		return fmt.Errorf("FT_Write failed (status %d)", r)
	}
	if n != 1 {
		return fmt.Errorf("FT_Write wrote %d bytes instead of 1", n)
	}
	return nil
}

func (d *handle) Close() error {
	r, _, _ := procClose.Call(d.h)
	if r != ftOK {
		return fmt.Errorf("FT_Close failed (status %d)", r)
	}
	return nil
}
