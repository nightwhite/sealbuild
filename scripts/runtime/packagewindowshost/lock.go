package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

// WindowsBuildLock fixes the source metadata and firmware payload for one build.
type WindowsBuildLock struct {
	SchemaVersion   int                    `json:"schemaVersion"`
	HostPlatform    runtimepkg.Platform    `json:"hostPlatform"`
	Components      []runtimepkg.Component `json:"components"`
	RuntimePackages []LockedRuntimePackage `json:"runtimePackages"`
	FirmwareFiles   []string               `json:"firmwareFiles"`
}

// LockedRuntimePackage fixes one MSYS2 package that owns a packaged DLL.
type LockedRuntimePackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// RuntimePackageEvidence records the package owner of every packaged DLL.
type RuntimePackageEvidence struct {
	SchemaVersion int                 `json:"schemaVersion"`
	DLLs          []RuntimePackageDLL `json:"dlls"`
}

// RuntimePackageDLL maps one DLL basename to its exact MSYS2 package.
type RuntimePackageDLL struct {
	Name    string `json:"name"`
	Package string `json:"package"`
	Version string `json:"version"`
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
	if len(lock.RuntimePackages) == 0 {
		return fmt.Errorf("runtimePackages must not be empty")
	}
	for index, runtimePackage := range lock.RuntimePackages {
		if runtimePackage.Name == "" {
			return fmt.Errorf("runtimePackages[%d].name must not be empty", index)
		}
		if runtimePackage.Version == "" {
			return fmt.Errorf("runtimePackages[%d].version must not be empty", index)
		}
		if index > 0 {
			previous := lock.RuntimePackages[index-1].Name
			if previous == runtimePackage.Name {
				return fmt.Errorf("runtime package %s is duplicated", runtimePackage.Name)
			}
			if previous > runtimePackage.Name {
				return fmt.Errorf("runtimePackages must be sorted by name")
			}
		}
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

func loadRuntimePackageEvidence(reader io.Reader) (RuntimePackageEvidence, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var evidence RuntimePackageEvidence
	if err := decoder.Decode(&evidence); err != nil {
		return RuntimePackageEvidence{}, fmt.Errorf("decode Windows runtime package evidence: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return RuntimePackageEvidence{}, fmt.Errorf("decode Windows runtime package evidence: trailing JSON value")
		}
		return RuntimePackageEvidence{}, fmt.Errorf("decode Windows runtime package evidence: %w", err)
	}
	if err := evidence.validate(); err != nil {
		return RuntimePackageEvidence{}, err
	}
	return evidence, nil
}

func (evidence RuntimePackageEvidence) validate() error {
	if evidence.SchemaVersion != 1 {
		return fmt.Errorf("runtime package evidence schemaVersion must be 1")
	}
	if len(evidence.DLLs) == 0 {
		return fmt.Errorf("runtime package evidence DLLs must not be empty")
	}
	for index, dll := range evidence.DLLs {
		if dll.Name == "" || filepath.Base(dll.Name) != dll.Name || !strings.EqualFold(filepath.Ext(dll.Name), ".dll") {
			return fmt.Errorf("runtime package evidence DLL name must be a basename ending in .dll: %s", dll.Name)
		}
		if dll.Package == "" || dll.Version == "" {
			return fmt.Errorf("runtime package evidence for %s must contain package and version", dll.Name)
		}
		if index > 0 {
			previous := strings.ToLower(evidence.DLLs[index-1].Name)
			current := strings.ToLower(dll.Name)
			if previous == current {
				return fmt.Errorf("runtime package evidence DLL %s is duplicated", dll.Name)
			}
			if previous > current {
				return fmt.Errorf("runtime package evidence DLLs must be sorted by name")
			}
		}
	}
	return nil
}

func validateRuntimePackageEvidence(closure []string, evidence RuntimePackageEvidence, locked []LockedRuntimePackage) error {
	if err := evidence.validate(); err != nil {
		return err
	}
	expectedDLLs := make(map[string]struct{}, len(closure)-1)
	for _, dependency := range closure[1:] {
		expectedDLLs[strings.ToLower(filepath.Base(dependency))] = struct{}{}
	}
	evidenceDLLs := make(map[string]struct{}, len(evidence.DLLs))
	packagesByName := make(map[string]LockedRuntimePackage)
	for _, dll := range evidence.DLLs {
		name := strings.ToLower(dll.Name)
		if _, expected := expectedDLLs[name]; !expected {
			return fmt.Errorf("runtime package evidence contains DLL %s outside the PE closure", dll.Name)
		}
		evidenceDLLs[name] = struct{}{}
		candidate := LockedRuntimePackage{Name: dll.Package, Version: dll.Version}
		if existing, exists := packagesByName[candidate.Name]; exists && existing.Version != candidate.Version {
			return fmt.Errorf("runtime package %s has conflicting versions %s and %s", candidate.Name, existing.Version, candidate.Version)
		}
		packagesByName[candidate.Name] = candidate
	}
	for name := range expectedDLLs {
		if _, mapped := evidenceDLLs[name]; !mapped {
			return fmt.Errorf("DLL %s is not mapped to an MSYS2 package", name)
		}
	}
	actual := make([]LockedRuntimePackage, 0, len(packagesByName))
	for _, runtimePackage := range packagesByName {
		actual = append(actual, runtimePackage)
	}
	sort.Slice(actual, func(first, second int) bool { return actual[first].Name < actual[second].Name })
	if !slices.Equal(actual, locked) {
		return fmt.Errorf("runtime package mismatch: evidence is %v, lock is %v", actual, locked)
	}
	return nil
}
