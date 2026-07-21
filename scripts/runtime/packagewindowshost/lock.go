package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

// WindowsBuildLock fixes the source metadata and firmware payload for one build.
type WindowsBuildLock struct {
	SchemaVersion int                    `json:"schemaVersion"`
	HostPlatform  runtimepkg.Platform    `json:"hostPlatform"`
	Components    []runtimepkg.Component `json:"components"`
	FirmwareFiles []string               `json:"firmwareFiles"`
}

func loadWindowsBuildLock(reader io.Reader) (WindowsBuildLock, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var lock WindowsBuildLock
	if err := decoder.Decode(&lock); err != nil {
		return WindowsBuildLock{}, fmt.Errorf("decode Windows Host Build Lock: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return WindowsBuildLock{}, fmt.Errorf("decode Windows Host Build Lock: trailing JSON value")
		}
		return WindowsBuildLock{}, fmt.Errorf("decode Windows Host Build Lock: %w", err)
	}
	if err := lock.validate(); err != nil {
		return WindowsBuildLock{}, err
	}
	return lock, nil
}

func (lock WindowsBuildLock) validate() error {
	if lock.SchemaVersion != 1 {
		return fmt.Errorf("schemaVersion must be 1")
	}
	if lock.HostPlatform != (runtimepkg.Platform{OS: "windows", Architecture: "amd64"}) {
		return fmt.Errorf("hostPlatform must be windows/amd64")
	}
	if len(lock.Components) == 0 {
		return fmt.Errorf("components must not be empty")
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
		return fmt.Errorf("validate Windows Host Build Lock components: %w", err)
	}
	if len(lock.FirmwareFiles) == 0 {
		return fmt.Errorf("firmwareFiles must not be empty")
	}
	seen := make(map[string]struct{}, len(lock.FirmwareFiles))
	for _, name := range lock.FirmwareFiles {
		if name == "" || name == "." || path.IsAbs(name) || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, `\`) {
			return fmt.Errorf("firmware file must be a clean relative slash path: %s", name)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("firmware file %s is duplicated", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}
