//go:build windows

package platformfs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PublishFileNoReplace atomically publishes a temporary file without replacing a target.
func PublishFileNoReplace(temporaryPath, finalPath string) error {
	if _, err := os.Lstat(finalPath); err == nil {
		return fmt.Errorf("publish file target exists: %w", os.ErrExist)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect publish file target: %w", err)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		if _, statErr := os.Lstat(finalPath); statErr == nil {
			return fmt.Errorf("publish file target exists: %w", os.ErrExist)
		}
		return fmt.Errorf("rename temporary file: %w", err)
	}
	return nil
}

// PublishDirectoryNoReplace atomically publishes a prepared directory without replacement.
func PublishDirectoryNoReplace(temporaryPath, finalPath string) error {
	if _, err := os.Lstat(finalPath); err == nil {
		return fmt.Errorf("publish directory target exists: %w", os.ErrExist)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect publish directory target: %w", err)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		if _, statErr := os.Lstat(finalPath); statErr == nil {
			return fmt.Errorf("publish directory target exists: %w", os.ErrExist)
		}
		return fmt.Errorf("rename temporary directory: %w", err)
	}
	return nil
}

// SyncDirectory is a no-op because Windows does not expose Unix directory fsync semantics.
func SyncDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect directory for sync: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("sync path must be a directory")
	}
	return nil
}

// ValidatePrivateFile verifies the file shape; access is inherited from the user-private cache tree.
func ValidatePrivateFile(info os.FileInfo) error {
	return validateRegularFile(info)
}

// ValidatePublicFile verifies the file shape on Windows.
func ValidatePublicFile(info os.FileInfo) error {
	return validateRegularFile(info)
}

// ValidateArtifactFile verifies artifact file shape; Windows does not preserve POSIX modes.
func ValidateArtifactFile(info os.FileInfo, _ uint32) error {
	return validateRegularFile(info)
}

// ArtifactMode returns the portable mode recorded in Runtime manifests.
func ArtifactMode(path string, info os.FileInfo) (uint32, error) {
	if err := validateRegularFile(info); err != nil {
		return 0, err
	}
	if strings.EqualFold(filepath.Ext(path), ".exe") {
		return 0o755, nil
	}
	return 0o644, nil
}
