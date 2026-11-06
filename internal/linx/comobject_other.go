//go:build !windows

package linx

import "fmt"

func registerComObject(dll string) error {
	return fmt.Errorf("COM registration is only supported on Windows")
}
func unregisterComObject() error { return fmt.Errorf("COM registration is only supported on Windows") }
