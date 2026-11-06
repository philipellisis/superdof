//go:build windows

package main

import (
	"os"
	"syscall"
)

// superdof.exe is built for the Windows GUI subsystem (-H=windowsgui), so starting
// it from the Startup folder or Explorer opens no console window. When it is run
// from a command prompt it attaches to that prompt's console instead, so the
// output is shown there as usual.

var (
	kernel32Con         = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole   = kernel32Con.NewProc("AttachConsole")
	procGetConsoleWin   = kernel32Con.NewProc("GetConsoleWindow")
	procGetFileType     = kernel32Con.NewProc("GetFileType")
	fileTypeDisk        = uintptr(1)
	fileTypePipe        = uintptr(3)
	attachParentProcess = ^uintptr(0) // (DWORD)-1
)

// attachConsole reports whether output has somewhere to go: a console, a pipe or a file.
func attachConsole() bool {
	if w, _, _ := procGetConsoleWin.Call(); w != 0 {
		return true // the process already has a console (console build)
	}
	// output redirected to a file or a pipe (superdof ... > log.txt, | more)
	if h, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE); err == nil && h != 0 && h != syscall.InvalidHandle {
		if ft, _, _ := procGetFileType.Call(uintptr(h)); ft == fileTypeDisk || ft == fileTypePipe {
			return true
		}
	}
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		return false // started without a console (Startup folder, Explorer, service)
	}
	if out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout, os.Stderr = out, out
	}
	if in, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = in
	}
	return true
}
