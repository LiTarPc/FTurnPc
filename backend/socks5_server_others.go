//go:build !windows

package backend

import (
	"fmt"
)

type Socks5Instance struct{}

func StartNetstackSocks5(wgConfig string, listenAddr string, customMTU int) (*Socks5Instance, error) {
	return nil, fmt.Errorf("SOCKS5 mode is currently supported on Windows only")
}

func (s *Socks5Instance) Stop() {}

func (s *Socks5Instance) GetBytes() (rx, tx int64) {
	return 0, 0
}
