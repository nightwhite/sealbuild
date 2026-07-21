package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

func buildHostManifest(lock BuildLock) runtimepkg.ArtifactManifest {
	components := make([]runtimepkg.Component, 0, len(lock.Components))
	for _, locked := range lock.Components {
		components = append(components, runtimepkg.Component{
			Name:     locked.Name,
			Version:  locked.Version,
			Source:   locked.Source,
			Revision: locked.Revision,
			SHA256:   locked.SHA256,
		})
	}
	return runtimepkg.ArtifactManifest{
		SchemaVersion: 1,
		Kind:          runtimepkg.ArtifactKindHost,
		Platform:      lock.HostPlatform,
		Components:    components,
	}
}

func validateDependencyComponents(graph DependencyGraph, homebrewRoot string, lock BuildLock) error {
	lockedComponents := make(map[string]LockedComponent, len(lock.Components)-1)
	for _, component := range lock.Components {
		if component.Name != "qemu" {
			lockedComponents[component.Name] = component
		}
	}

	cellarRoot := filepath.Join(homebrewRoot, "Cellar")
	foundComponents := make(map[string]struct{}, len(lockedComponents))
	for _, library := range graph.Libraries {
		relativePath, err := filepath.Rel(cellarRoot, library.SourcePath)
		if err != nil {
			return fmt.Errorf("resolve Homebrew dependency path %s: %w", library.SourcePath, err)
		}
		parts := strings.Split(filepath.ToSlash(relativePath), "/")
		if len(parts) < 3 || parts[0] == ".." {
			return fmt.Errorf("dependency %s does not use a Homebrew Cellar path", library.SourcePath)
		}

		componentName := parts[0]
		installedVersion := parts[1]
		locked, exists := lockedComponents[componentName]
		if !exists {
			return fmt.Errorf("dependency %s is not present in Host Build Lock", componentName)
		}
		if !matchesBottleVersion(installedVersion, locked.Version) {
			return fmt.Errorf("dependency %s version is %s, expected %s", componentName, installedVersion, locked.Version)
		}
		foundComponents[componentName] = struct{}{}
	}

	for _, component := range lock.Components {
		if component.Name == "qemu" {
			continue
		}
		if _, exists := foundComponents[component.Name]; !exists {
			return fmt.Errorf("locked dependency %s is missing from QEMU closure", component.Name)
		}
	}
	return nil
}

func copyLockedLicenses(lock BuildLock, qemuLicenseDirectory, dependencyLicenseDirectory, payloadDirectory string) error {
	for _, component := range lock.Components {
		sourceRoot := filepath.Join(dependencyLicenseDirectory, component.Name)
		if component.Name == "qemu" {
			sourceRoot = qemuLicenseDirectory
		}
		for _, licensePath := range component.LicenseFiles {
			sourcePath := filepath.Join(sourceRoot, filepath.FromSlash(licensePath))
			destinationPath := filepath.Join(
				payloadDirectory,
				"licenses",
				component.Name,
				filepath.FromSlash(licensePath),
			)
			if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
				return fmt.Errorf("create %s license directory: %w", component.Name, err)
			}
			if err := copyRegularFile(sourcePath, destinationPath, 0o644); err != nil {
				return fmt.Errorf("copy %s license %s: %w", component.Name, licensePath, err)
			}
			actualSHA256, err := regularFileSHA256(destinationPath)
			if err != nil {
				return fmt.Errorf("hash %s license %s: %w", component.Name, licensePath, err)
			}
			expectedSHA256 := component.LicenseFileSHA256[licensePath]
			if actualSHA256 != expectedSHA256 {
				return fmt.Errorf(
					"%s license %s SHA-256 mismatch: got %s, expected %s",
					component.Name,
					licensePath,
					actualSHA256,
					expectedSHA256,
				)
			}
		}
	}
	return nil
}

func regularFileSHA256(filePath string) (returnSHA256 string, returnErr error) {
	info, err := os.Lstat(filePath)
	if err != nil {
		return "", fmt.Errorf("inspect file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("file must be regular")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && returnErr == nil {
			returnErr = fmt.Errorf("close file: %w", closeErr)
		}
	}()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func matchesBottleVersion(installed, locked string) bool {
	if installed == locked {
		return true
	}
	revision := strings.TrimPrefix(installed, locked+"_")
	if revision == installed || revision == "" {
		return false
	}
	for _, character := range revision {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
