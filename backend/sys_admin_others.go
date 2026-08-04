//go:build !windows

package backend

import "os"

// IsAdmin returns true on non-windows if root
func IsAdmin() bool {
	return os.Geteuid() == 0
}

// RelaunchAsAdmin stub for non-windows
func RelaunchAsAdmin() error {
	return nil
}
