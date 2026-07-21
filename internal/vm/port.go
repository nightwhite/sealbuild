package vm

import (
	"errors"
	"fmt"
	"net"
	"sync"
)

// LoopbackPorts reserves ephemeral IPv4 ports on the host loopback interface.
type LoopbackPorts struct{}

// ReserveLoopback holds one port until the returned release function runs.
func (LoopbackPorts) ReserveLoopback() (uint16, func() error, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, nil, fmt.Errorf("listen on host loopback: %w", err)
	}
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || address.Port <= 0 || address.Port > 65535 {
		return 0, nil, errors.Join(fmt.Errorf("host loopback returned an invalid TCP address"), listener.Close())
	}
	var releaseOnce sync.Once
	var releaseErr error
	release := func() error {
		releaseOnce.Do(func() {
			if err := listener.Close(); err != nil {
				releaseErr = fmt.Errorf("release host loopback port: %w", err)
			}
		})
		return releaseErr
	}
	return uint16(address.Port), release, nil
}
