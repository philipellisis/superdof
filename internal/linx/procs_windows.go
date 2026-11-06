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

var (
	procWriteProcessMemory = kernel32.NewProc("WriteProcessMemory")
	procVirtualQueryEx     = kernel32.NewProc("VirtualQueryEx")
)

// OpenProcessMemoryRW opens pid for reading, writing and querying regions.
func OpenProcessMemoryRW(pid uint32) (*ProcessMemory, error) {
	const vmOperation, vmRead, vmWrite, queryInfo = 0x0008, 0x0010, 0x0020, 0x0400
	h, _, e := procOpenProcess.Call(vmOperation|vmRead|vmWrite|queryInfo, 0, uintptr(pid))
	if h == 0 {
		return nil, fmt.Errorf("OpenProcess(%d): %v (is superdof running with the same privileges as the game?)", pid, e)
	}
	return &ProcessMemory{h: syscall.Handle(h)}, nil
}

// ReadAt fills buf from the address space of the process.
func (p *ProcessMemory) ReadAt(addr uint64, buf []byte) error {
	if len(buf) == 0 {
		return nil
	}
	var n uintptr
	r, _, e := procReadProcessMemory.Call(uintptr(p.h), uintptr(addr), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)))
	if r == 0 || int(n) != len(buf) {
		return fmt.Errorf("ReadProcessMemory(0x%X, %d): %v", addr, len(buf), e)
	}
	return nil
}

// WriteAt writes buf into the address space of the process.
func (p *ProcessMemory) WriteAt(addr uint64, buf []byte) error {
	if len(buf) == 0 {
		return nil
	}
	var n uintptr
	r, _, e := procWriteProcessMemory.Call(uintptr(p.h), uintptr(addr), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n)))
	if r == 0 || int(n) != len(buf) {
		return fmt.Errorf("WriteProcessMemory(0x%X, %d): %v", addr, len(buf), e)
	}
	return nil
}

type memoryBasicInfo struct {
	BaseAddress       uintptr
	AllocationBase    uintptr
	AllocationProtect uint32
	PartitionID       uint16
	_                 uint16
	RegionSize        uintptr
	State             uint32
	Protect           uint32
	Type              uint32
	_                 uint32
}

// Regions calls fn for every committed, readable and writable region below
// 4 GB (the address range of a 32 bit game) until fn returns false.
func (p *ProcessMemory) Regions(fn func(base, size uint64) bool) {
	const (
		memCommit  = 0x1000
		pageGuard  = 0x100
		pageNoAcc  = 0x1
		writeFlags = 0x04 | 0x08 | 0x40 | 0x80 // RW, WC, ERW, ECW
	)
	var addr uint64
	for addr < 1<<32 {
		var mi memoryBasicInfo
		r, _, _ := procVirtualQueryEx.Call(uintptr(p.h), uintptr(addr), uintptr(unsafe.Pointer(&mi)), unsafe.Sizeof(mi))
		if r == 0 || mi.RegionSize == 0 {
			return
		}
		if mi.State == memCommit && mi.Protect&(pageGuard|pageNoAcc) == 0 && mi.Protect&writeFlags != 0 {
			if !fn(uint64(mi.BaseAddress), uint64(mi.RegionSize)) {
				return
			}
		}
		next := uint64(mi.BaseAddress) + uint64(mi.RegionSize)
		if next <= addr {
			return
		}
		addr = next
	}
}
