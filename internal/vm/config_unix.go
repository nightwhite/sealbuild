//go:build !windows

package vm

import (
	"fmt"
	"os"
	"path/filepath"
)

func validateShutdownInput(config Config) error {
	if !filepath.IsAbs(config.ShutdownPath) {
		return fmt.Errorf("shutdown path must be absolute")
	}
	if _, err := os.Lstat(config.ShutdownPath); err == nil {
		return fmt.Errorf("shutdown path already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect shutdown path: %w", err)
	}
	return validateDirectory("shutdown parent", filepath.Dir(config.ShutdownPath))
}

func validateShutdownReady(config Config) error {
	return validateShutdownInput(config)
}

func shutdownChardev(config Config) (string, error) {
	return "socket,id=shutdown,path=" + escapeQEMUOptionValue(config.ShutdownPath) + ",server=on,wait=off", nil
}
