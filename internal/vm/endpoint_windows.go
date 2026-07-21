//go:build windows

package vm

import (
	"fmt"
	"strconv"
)

// PrepareShutdownPath returns the empty path because Windows uses loopback TCP.
func PrepareShutdownPath() (string, error) {
	return "", nil
}

func reserveShutdownEndpoint(ports PortAllocator, _ string) (uint16, string, func() error, error) {
	port, release, err := ports.ReserveLoopback()
	if err != nil {
		return 0, "", nil, err
	}
	return port, "127.0.0.1:" + strconv.FormatUint(uint64(port), 10), release, nil
}

func cleanupShutdownPath(path string) error {
	if path != "" {
		return fmt.Errorf("unexpected Windows shutdown path %q", path)
	}
	return nil
}
