//go:build !windows

package vm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// PrepareShutdownPath reserves a unique Unix socket pathname for one VM.
func PrepareShutdownPath() (string, error) {
	tempDir, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return "", fmt.Errorf("resolve VM shutdown temp directory: %w", err)
	}
	file, err := os.CreateTemp(tempDir, ".sealbuild-shutdown-*.sock")
	if err != nil {
		return "", fmt.Errorf("reserve VM shutdown socket path: %w", err)
	}
	path := file.Name()
	if err := errors.Join(file.Close(), os.Remove(path)); err != nil {
		return "", fmt.Errorf("prepare VM shutdown socket path: %w", err)
	}
	return path, nil
}

func reserveShutdownEndpoint(_ PortAllocator, path string) (uint16, string, func() error, error) {
	return 0, path, func() error { return nil }, nil
}

func cleanupShutdownPath(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove Guest shutdown socket: %w", err)
	}
	return nil
}
