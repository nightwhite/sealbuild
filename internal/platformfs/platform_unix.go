//go:build !windows

package platformfs

import (
	"errors"
	"fmt"
	"os"
)

// PublishFileNoReplace atomically publishes a temporary file without replacing a target.
func PublishFileNoReplace(temporaryPath, finalPath string) error {
	if err := os.Link(temporaryPath, finalPath); err != nil {
		return fmt.Errorf("link temporary file to final path: %w", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		return errors.Join(fmt.Errorf("remove temporary file link: %w", err), os.Remove(finalPath))
	}
	return nil
}

// PublishDirectoryNoReplace publishes a prepared directory after checking the target is absent.
func PublishDirectoryNoReplace(temporaryPath, finalPath string) error {
	if _, err := os.Lstat(finalPath); err == nil {
		return fmt.Errorf("publish directory target exists: %w", os.ErrExist)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect publish directory target: %w", err)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return fmt.Errorf("rename temporary directory: %w", err)
	}
	return nil
}

// SyncDirectory persists directory metadata on Unix filesystems.
func SyncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	return errors.Join(directory.Sync(), directory.Close())
}

// ValidatePrivateFile requires a Unix private regular file.
func ValidatePrivateFile(info os.FileInfo) error {
	if err := validateRegularFile(info); err != nil {
		return err
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("private file mode is %#o, expected 0600", info.Mode().Perm())
	}
	return nil
}

// ValidatePublicFile requires a Unix public regular file.
func ValidatePublicFile(info os.FileInfo) error {
	if err := validateRegularFile(info); err != nil {
		return err
	}
	if info.Mode().Perm() != 0o644 {
		return fmt.Errorf("public file mode is %#o, expected 0644", info.Mode().Perm())
	}
	return nil
}

// ValidateArtifactFile requires the exact manifest mode on Unix.
func ValidateArtifactFile(info os.FileInfo, expectedMode uint32) error {
	if err := validateRegularFile(info); err != nil {
		return err
	}
	if uint32(info.Mode().Perm()) != expectedMode {
		return fmt.Errorf("artifact file mode is %#o, expected %#o", info.Mode().Perm(), expectedMode)
	}
	return nil
}

// ArtifactMode returns the actual Unix payload mode.
func ArtifactMode(_ string, info os.FileInfo) (uint32, error) {
	if err := validateRegularFile(info); err != nil {
		return 0, err
	}
	return uint32(info.Mode().Perm()), nil
}
