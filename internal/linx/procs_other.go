//go:build !windows

package linx

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Proc is a running process.
type Proc struct {
	PID  uint32
	Name string
}

// ListProcesses lists processes from /proc (Linux).
func ListProcesses() ([]Proc, error) {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var out []Proc
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if err != nil {
			continue
		}
		out = append(out, Proc{PID: uint32(pid), Name: strings.TrimSpace(string(b))})
	}
	return out, nil
}

// WindowTitles is not available without a window system binding.
func WindowTitles(pid uint32) []string { return nil }

// ProcessMemory is unsupported off Windows.
type ProcessMemory struct{}

func OpenProcessMemory(pid uint32) (*ProcessMemory, error) {
	return nil, fmt.Errorf("process memory triggers are only supported on Windows")
}
func (p *ProcessMemory) ReadU32(addr uint32) (uint32, error) { return 0, fmt.Errorf("unsupported") }
func (p *ProcessMemory) Close()                              {}
