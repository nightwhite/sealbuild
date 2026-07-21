// Package cache defines paths owned by Sealbuild.
package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var compatibilityIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Layout identifies the cache root owned by Sealbuild.
type Layout struct {
	Root string
}

// DefaultLayout returns the current user's Sealbuild cache layout.
func DefaultLayout() (Layout, error) {
	userCacheDirectory, err := os.UserCacheDir()
	if err != nil {
		return Layout{}, fmt.Errorf("resolve user cache directory: %w", err)
	}
	layout := Layout{Root: filepath.Join(userCacheDirectory, "sealbuild")}
	if err := layout.Validate(); err != nil {
		return Layout{}, err
	}
	return layout, nil
}

// Validate checks that Root is a safe owned cache directory.
func (layout Layout) Validate() error {
	if layout.Root == "" || !filepath.IsAbs(layout.Root) || filepath.Clean(layout.Root) != layout.Root {
		return fmt.Errorf("cache root must be an absolute clean path")
	}
	if filepath.Dir(layout.Root) == layout.Root {
		return fmt.Errorf("cache root must not be the filesystem root")
	}
	return nil
}

// RuntimeDir returns the immutable Runtime content directory.
func (layout Layout) RuntimeDir(compatibilityID string) (string, error) {
	if err := layout.validateCompatibilityID(compatibilityID); err != nil {
		return "", err
	}
	return filepath.Join(layout.Root, "runtime", compatibilityID), nil
}

// StateDir returns the persistent state directory for one Runtime compatibility ID.
func (layout Layout) StateDir(compatibilityID string) (string, error) {
	if err := layout.validateCompatibilityID(compatibilityID); err != nil {
		return "", err
	}
	return filepath.Join(layout.Root, "state", compatibilityID), nil
}

// RuntimeLockPath returns the content installation lock path.
func (layout Layout) RuntimeLockPath(compatibilityID string) (string, error) {
	if err := layout.validateCompatibilityID(compatibilityID); err != nil {
		return "", err
	}
	return filepath.Join(layout.Root, "locks", "runtime-"+compatibilityID+".lock"), nil
}

// StateLockPath returns the persistent state initialization lock path.
func (layout Layout) StateLockPath(compatibilityID string) (string, error) {
	if err := layout.validateCompatibilityID(compatibilityID); err != nil {
		return "", err
	}
	return filepath.Join(layout.Root, "locks", "state-"+compatibilityID+".lock"), nil
}

// BuildLockPath returns the single active-build lock path.
func (layout Layout) BuildLockPath() string {
	return filepath.Join(layout.Root, "locks", "build.lock")
}

// LogDir returns the directory for Sealbuild-owned VM logs.
func (layout Layout) LogDir() string {
	return filepath.Join(layout.Root, "logs")
}

func (layout Layout) validateCompatibilityID(compatibilityID string) error {
	if err := layout.Validate(); err != nil {
		return err
	}
	if !compatibilityIDPattern.MatchString(compatibilityID) {
		return fmt.Errorf("compatibility ID must be 64 lowercase hexadecimal characters")
	}
	return nil
}
