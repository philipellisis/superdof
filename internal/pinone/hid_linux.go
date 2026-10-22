//go:build linux

package pinone

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

type linuxDev struct {
	f    *os.File
	desc string
	hid  bool
}

func (d *linuxDev) Describe() string { return d.desc }
func (d *linuxDev) Close() error     { return d.f.Close() }

func (d *linuxDev) Write(packet []byte) error {
	buf := packet
	if d.hid {
		buf = HIDReport(packet)
	}
	n, err := d.f.Write(buf)
	if err != nil {
		return err
	}
	if n != len(buf) {
		return fmt.Errorf("short write: %d of %d", n, len(buf))
	}
	return nil
}

// openHID finds the hidraw node exposing the PinOne vendor channel.
func openHID() (Transport, error) {
	nodes, _ := filepath.Glob("/sys/class/hidraw/hidraw*")
	wantID := fmt.Sprintf("HID_ID=0003:%08X:%08X", HIDVendorID, HIDProductID)
	var lastErr error
	for _, n := range nodes {
		uev, err := os.ReadFile(filepath.Join(n, "device", "uevent"))
		if err != nil || !strings.Contains(strings.ToUpper(string(uev)), wantID) {
			continue
		}
		desc, err := os.ReadFile(filepath.Join(n, "device", "report_descriptor"))
		if err != nil {
			continue
		}
		// usage page 0xFF00 (06 00 FF) followed by usage 1 (09 01)
		if !bytes.Contains(desc, []byte{0x06, 0x00, 0xFF, 0x09, 0x01}) {
			continue
		}
		path := "/dev/" + filepath.Base(n)
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			lastErr = err
			continue
		}
		return &linuxDev{f: f, desc: "USB HID " + path, hid: true}, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrNotFound
}

func openSerial(name string) (Transport, error) {
	f, err := os.OpenFile(name, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	var t syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&t))); e != 0 {
		f.Close()
		return nil, fmt.Errorf("TCGETS %s: %v", name, e)
	}
	const b2000000 = 0x100f // B2000000 on Linux
	t.Iflag = 0
	t.Oflag = 0
	t.Lflag = 0
	t.Cflag = b2000000 | syscall.CS8 | syscall.CLOCAL | syscall.CREAD
	t.Ispeed, t.Ospeed = b2000000, b2000000
	t.Cc[syscall.VMIN] = 0
	t.Cc[syscall.VTIME] = 1
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(&t))); e != 0 {
		f.Close()
		return nil, fmt.Errorf("TCSETS %s: %v", name, e)
	}
	return &linuxDev{f: f, desc: "serial " + name}, nil
}

// findLegacyPort finds /dev/ttyACM* of a legacy firmware board via sysfs.
func findLegacyPort() string {
	ttys, _ := filepath.Glob("/sys/class/tty/ttyACM*")
	for _, t := range ttys {
		dev, err := filepath.EvalSymlinks(filepath.Join(t, "device"))
		if err != nil {
			continue
		}
		for d := dev; d != "/" && d != "."; d = filepath.Dir(d) {
			v, e1 := os.ReadFile(filepath.Join(d, "idVendor"))
			p, e2 := os.ReadFile(filepath.Join(d, "idProduct"))
			if e1 == nil && e2 == nil {
				if strings.EqualFold(strings.TrimSpace(string(v)), fmt.Sprintf("%04x", LegacyVendorID)) &&
					strings.EqualFold(strings.TrimSpace(string(p)), fmt.Sprintf("%04x", LegacyProduct)) {
					return "/dev/" + filepath.Base(t)
				}
				break
			}
		}
	}
	return ""
}
