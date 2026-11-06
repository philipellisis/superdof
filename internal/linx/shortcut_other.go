//go:build !windows

package linx

import "fmt"

func ShortcutTarget(lnk string) (string, error) {
	return "", fmt.Errorf("shortcuts are only supported on Windows")
}
func CreateShortcut(lnk, target string) error {
	return fmt.Errorf("shortcuts are only supported on Windows")
}
