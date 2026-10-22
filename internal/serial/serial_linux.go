//go:build linux

package serial

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

type port struct {
	f       *os.File
	timeout time.Duration
}

var baudConst = map[int]uint32{
	9600: 0x0D, 19200: 0x0E, 38400: 0x0F, 57600: 0x1001, 115200: 0x1002, 230400: 0x1003,
	460800: 0x1004, 921600: 0x1007, 1000000: 0x1008, 2000000: 0x100F, 3000000: 0x1010,
}

// Open opens a serial port.
func Open(c Config) (Port, error) {
	c.defaults()
	f, err := os.OpenFile(c.Name, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	b, ok := baudConst[c.Baud]
	if !ok {
		f.Close()
		return nil, fmt.Errorf("unsupported baud rate %d", c.Baud)
	}
	var t syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&t))); e != 0 {
		f.Close()
		return nil, e
	}
	t.Iflag, t.Oflag, t.Lflag = 0, 0, 0
	cs := uint32(syscall.CS8)
	switch c.DataBits {
	case 5:
		cs = syscall.CS5
	case 6:
		cs = syscall.CS6
	case 7:
		cs = syscall.CS7
	}
	t.Cflag = b | cs | syscall.CLOCAL | syscall.CREAD
	switch c.Parity {
	case "odd":
		t.Cflag |= syscall.PARENB | syscall.PARODD
	case "even":
		t.Cflag |= syscall.PARENB
	}
	if c.StopBits == 2 {
		t.Cflag |= syscall.CSTOPB
	}
	t.Ispeed, t.Ospeed = b, b
	t.Cc[syscall.VMIN] = 0
	t.Cc[syscall.VTIME] = 1
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(&t))); e != 0 {
		f.Close()
		return nil, e
	}
	return &port{f: f, timeout: time.Duration(c.TimeoutMs) * time.Millisecond}, nil
}

func (p *port) Read(b []byte) (int, error) {
	deadline := time.Now().Add(p.timeout)
	for {
		n, err := p.f.Read(b)
		if n > 0 {
			return n, nil
		}
		if err != nil && err != io.EOF {
			return 0, err
		}
		if time.Now().After(deadline) {
			return 0, ErrTimeout
		}
	}
}

func (p *port) Write(b []byte) (int, error) { return p.f.Write(b) }

func (p *port) Available() int {
	var n int32
	syscall.Syscall(syscall.SYS_IOCTL, p.f.Fd(), syscall.TIOCINQ, uintptr(unsafe.Pointer(&n)))
	return int(n)
}

func (p *port) Flush() {
	const tciflush = 0x540B // TCFLSH
	syscall.Syscall(syscall.SYS_IOCTL, p.f.Fd(), tciflush, 0)
}

func (p *port) Close() error { return p.f.Close() }

// PortNames lists /dev/ttyACM* and /dev/ttyUSB*.
func PortNames() []string {
	var out []string
	for _, pat := range []string{"/dev/ttyACM*", "/dev/ttyUSB*"} {
		m, _ := filepath.Glob(pat)
		out = append(out, m...)
	}
	return out
}
