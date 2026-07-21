package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

type LinuxBuildLock struct {
	SchemaVersion   int                    `json:"schemaVersion"`
	HostPlatform    runtimepkg.Platform    `json:"hostPlatform"`
	Components      []runtimepkg.Component `json:"components"`
	RuntimePackages []LockedRuntimePackage `json:"runtimePackages"`
	FirmwareFiles   []string               `json:"firmwareFiles"`
}

type LockedRuntimePackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func loadLinuxBuildLock(reader io.Reader) (LinuxBuildLock, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var lock LinuxBuildLock
	if err := decoder.Decode(&lock); err != nil {
		return LinuxBuildLock{}, fmt.Errorf("decode Linux Host Build Lock: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return LinuxBuildLock{}, fmt.Errorf("decode Linux Host Build Lock: trailing JSON value")
		}
		return LinuxBuildLock{}, fmt.Errorf("decode Linux Host Build Lock: %w", err)
	}
	if err := lock.validate(); err != nil {
		return LinuxBuildLock{}, err
	}
	return lock, nil
}

func (lock LinuxBuildLock) validate() error {
	if lock.SchemaVersion != 1 {
		return fmt.Errorf("schemaVersion must be 1")
	}
	if lock.HostPlatform != (runtimepkg.Platform{OS: "linux", Architecture: "amd64"}) {
		return fmt.Errorf("hostPlatform must be linux/amd64")
	}
	manifest := runtimepkg.ArtifactManifest{
		SchemaVersion: 1,
		Kind:          runtimepkg.ArtifactKindHost,
		Platform:      lock.HostPlatform,
		Components:    lock.Components,
		Files: []runtimepkg.ArtifactFile{{
			Path: "placeholder", SHA256: strings.Repeat("0", 64), Size: 1, Mode: 0o644,
		}},
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("validate Linux Host Build Lock components: %w", err)
	}
	if err := validateLockedRuntimePackages(lock.RuntimePackages); err != nil {
		return err
	}
	if len(lock.FirmwareFiles) == 0 {
		return fmt.Errorf("firmwareFiles must not be empty")
	}
	seen := make(map[string]struct{}, len(lock.FirmwareFiles))
	for _, name := range lock.FirmwareFiles {
		if name == "" || name == "." || path.IsAbs(name) || path.Clean(name) != name ||
			name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, `\`) {
			return fmt.Errorf("firmware file must be a clean relative slash path: %s", name)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("firmware file %s is duplicated", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func loadLinuxRuntimePackageEvidence(reader io.Reader) ([]LockedRuntimePackage, error) {
	packages := make([]LockedRuntimePackage, 0)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 2 {
			return nil, fmt.Errorf("Linux runtime package evidence line must contain package and version separated by one tab")
		}
		packages = append(packages, LockedRuntimePackage{Name: fields[0], Version: fields[1]})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read Linux runtime package evidence: %w", err)
	}
	if err := validateLockedRuntimePackages(packages); err != nil {
		return nil, fmt.Errorf("validate Linux runtime package evidence: %w", err)
	}
	return packages, nil
}

func validateLockedRuntimePackages(packages []LockedRuntimePackage) error {
	if len(packages) == 0 {
		return fmt.Errorf("runtimePackages must not be empty")
	}
	for index, runtimePackage := range packages {
		if runtimePackage.Name == "" || runtimePackage.Version == "" {
			return fmt.Errorf("runtimePackages[%d] must contain name and version", index)
		}
		if index == 0 {
			continue
		}
		previous := packages[index-1].Name
		if previous == runtimePackage.Name {
			return fmt.Errorf("runtime package %s is duplicated", runtimePackage.Name)
		}
		if previous > runtimePackage.Name {
			return fmt.Errorf("runtimePackages must be sorted by name")
		}
	}
	return nil
}

func validateLinuxRuntimePackageEvidence(actual, locked []LockedRuntimePackage) error {
	if !slices.Equal(actual, locked) {
		return fmt.Errorf("Linux runtime package mismatch: evidence is %v, lock is %v", actual, locked)
	}
	return nil
}
