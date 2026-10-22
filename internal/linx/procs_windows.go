//go:build windows

package linx

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	user32                 = syscall.NewLazyDLL("user32.dll")
	procEnumWindows        = user32.NewProc("EnumWindows")
	procGetWindowThreadPID = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowTextW     = user32.NewProc("GetWindowTextW")
	procIsWindowVisible    = user32.NewProc("IsWindowVisible")
	procOpenProcess        = kernel32.NewProc("OpenProcess")
	procReadProcessMemory  = kernel32.NewProc("ReadProcessMemory")
	procIsWow64Process     = kernel32.NewProc("IsWow64Process")
)

// Proc is a running process.
type Proc struct {
	PID  uint32
	Name string // executable name without ".exe"
}

// ListProcesses returns the running processes.
func ListProcesses() ([]Proc, error) {
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(snap)
	var pe syscall.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err := syscall.Process32First(snap, &pe); err != nil {
		return nil, err
	}
	var out []Proc
	for {
		n := syscall.UTF16ToString(pe.ExeFile[:])
		out = append(out, Proc{PID: pe.ProcessID, Name: strings.TrimSuffix(n, ".exe")})
		if err := syscall.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	return out, nil
}

// WindowTitles returns the titles of the visible top level windows of pid.
func WindowTitles(pid uint32) []string {
	var titles []string
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var p uint32
		procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&p)))
		if p != pid {
			return 1
		}
		if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
			return 1
		}
		buf := make([]uint16, 256)
		n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
		if n > 0 {
			titles = append(titles, syscall.UTF16ToString(buf[:n]))
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return titles
}

// ProcessMemory reads another process' memory.
type ProcessMemory struct{ h syscall.Handle }

// OpenProcessMemory opens pid for reading.
func OpenProcessMemory(pid uint32) (*ProcessMemory, error) {
	const vmRead, queryInfo = 0x0010, 0x0400
	h, _, e := procOpenProcess.Call(vmRead|queryInfo, 0, uintptr(pid))
	if h == 0 {
		return nil, fmt.Errorf("OpenProcess(%d): %v (is DOFLinx running with the same privileges as the game?)", pid, e)
	}
	return &ProcessMemory{h: syscall.Handle(h)}, nil
}

// ReadU32 reads a little endian 32 bit value.
func (p *ProcessMemory) ReadU32(addr uint32) (uint32, error) {
	var v uint32
	var n uintptr
	r, _, e := procReadProcessMemory.Call(uintptr(p.h), uintptr(addr), uintptr(unsafe.Pointer(&v)), 4, uintptr(unsafe.Pointer(&n)))
	if r == 0 || n != 4 {
		return 0, fmt.Errorf("ReadProcessMemory(0x%X): %v", addr, e)
	}
	return v, nil
}

func (p *ProcessMemory) Close() { syscall.CloseHandle(p.h) }
