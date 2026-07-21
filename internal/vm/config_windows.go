//go:build windows

package vm

import (
	"fmt"
	"strconv"
)

func validateShutdownInput(config Config) error {
	if config.ShutdownPath != "" {
		return fmt.Errorf("shutdown path is not supported on Windows")
	}
	return nil
}

func validateShutdownReady(config Config) error {
	if config.ShutdownPort == 0 {
		return fmt.Errorf("shutdown port must not be zero")
	}
	return validateShutdownInput(config)
}

func shutdownChardev(config Config) (string, error) {
	return "socket,id=shutdown,host=127.0.0.1,port=" + strconv.FormatUint(uint64(config.ShutdownPort), 10) + ",server=on,wait=off", nil
}
