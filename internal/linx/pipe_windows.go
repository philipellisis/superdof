//go:build windows

package linx

import (
	"fmt"
	"io"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procCreateNamedPipe     = kernel32.NewProc("CreateNamedPipeW")
	procConnectNamedPipe    = kernel32.NewProc("ConnectNamedPipe")
	procDisconnectNamedPipe = kernel32.NewProc("DisconnectNamedPipe")
	advapi32                = syscall.NewLazyDLL("advapi32.dll")
	procConvertSDDL         = advapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
)

type securityAttributes struct {
	Length             uint32
	SecurityDescriptor uintptr
	InheritHandle      int32
}

// pipeSDDL lets every local interactive/authenticated user read and write the
// pipe (so a game running without elevation can talk to an elevated superdof,
// and the other way round) and refuses network clients.
const pipeSDDL = "D:(D;;GA;;;NU)(A;;GRGW;;;AU)(A;;GA;;;SY)(A;;GA;;;BA)"

func pipeSecurity() *securityAttributes {
	var sd uintptr
	p, _ := syscall.UTF16PtrFromString(pipeSDDL)
	r, _, _ := procConvertSDDL.Call(uintptr(unsafe.Pointer(p)), 1, uintptr(unsafe.Pointer(&sd)), 0)
	if r == 0 {
		return nil // fall back to the default descriptor
	}
	return &securityAttributes{Length: uint32(unsafe.Sizeof(securityAttributes{})), SecurityDescriptor: sd}
}

type pipeConn struct{ h syscall.Handle }

func (p *pipeConn) Read(b []byte) (int, error) {
	var n uint32
	if err := syscall.ReadFile(p.h, b, &n, nil); err != nil {
		if n > 0 {
			return int(n), nil
		}
		return 0, io.EOF
	}
	if n == 0 {
		return 0, io.EOF
	}
	return int(n), nil
}

func (p *pipeConn) Write(b []byte) (int, error) {
	var n uint32
	err := syscall.WriteFile(p.h, b, &n, nil)
	return int(n), err
}

func (p *pipeConn) Close() error {
	procDisconnectNamedPipe.Call(uintptr(p.h))
	return syscall.CloseHandle(p.h)
}

// ListenPipe serves the named pipe \\.\pipe\<name>. onConn is called in its
// own goroutine for every client and must return when the client is done.
func ListenPipe(name string, onConn func(c io.ReadWriteCloser)) (stop func(), err error) {
	full := `\\.\pipe\` + name
	var stopped atomic.Bool
	// validate by creating the first instance synchronously
	create := func() (syscall.Handle, error) {
		const (
			pipeAccessDuplex = 0x3
			pipeTypeByte     = 0x0
			pipeWait         = 0x0
			unlimited        = 255
			rejectRemote     = 0x8
		)
		var sa uintptr
		attrs := pipeSecurity()
		if attrs != nil {
			sa = uintptr(unsafe.Pointer(attrs))
		}
		h, _, e := procCreateNamedPipe.Call(uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(full))),
			pipeAccessDuplex, pipeTypeByte|pipeWait|rejectRemote, unlimited, 65536, 65536, 0, sa)
		runtime.KeepAlive(attrs)
		if syscall.Handle(h) == syscall.InvalidHandle {
			return 0, fmt.Errorf("CreateNamedPipe %s: %v", full, e)
		}
		return syscall.Handle(h), nil
	}
	first, err := create()
	if err != nil {
		return nil, err
	}
	go func() {
		h := first
		for !stopped.Load() {
			r, _, e := procConnectNamedPipe.Call(uintptr(h), 0)
			if r == 0 && e != syscall.Errno(535) { // ERROR_PIPE_CONNECTED
				syscall.CloseHandle(h)
			} else if !stopped.Load() {
				go func(c *pipeConn) {
					defer c.Close()
					onConn(c)
				}(&pipeConn{h})
			} else {
				syscall.CloseHandle(h)
			}
			var err error
			h, err = create()
			if err != nil {
				return
			}
		}
		syscall.CloseHandle(h)
	}()
	return func() {
		stopped.Store(true)
		// unblock ConnectNamedPipe with a throw away client
		if h, err := syscall.CreateFile(syscall.StringToUTF16Ptr(full), syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, 0, 0); err == nil {
			syscall.CloseHandle(h)
		}
	}, nil
}
