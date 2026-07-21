package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

type LinuxBuildLock struct {
	SchemaVersion int                    `json:"schemaVersion"`
	HostPlatform  runtimepkg.Platform    `json:"hostPlatform"`
	Components    []runtimepkg.Component `json:"components"`
	FirmwareFiles []string               `json:"firmwareFiles"`
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
