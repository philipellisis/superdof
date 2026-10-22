//go:build !windows

package ftdi

// List is not available off Windows (the D2XX driver binding is Windows only).
func List() ([]Device, error) { return nil, ErrUnavailable }

// Open is not available off Windows.
func Open(serial string) (Handle, error) { return nil, ErrUnavailable }
