//go:build !windows

package backend

// SetSystemProxy stub for non-windows
func SetSystemProxy(enable bool, proxyAddr string, bypassRu bool) error {
	return nil
}
