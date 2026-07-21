package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

const buildLockSchemaVersion = 1

var (
	buildLockSHA256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	buildLockRevisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	expectedComponentNames   = []string{"qemu", "glib", "pixman", "libslirp", "zstd", "gettext", "pcre2"}
)

type BuildLock struct {
	SchemaVersion int                 `json:"schemaVersion"`
	HostPlatform  runtimepkg.Platform `json:"hostPlatform"`
	Components    []LockedComponent   `json:"components"`
}

type LockedComponent struct {
	Name              string            `json:"name"`
	Version           string            `json:"version"`
	Source            string            `json:"source"`
	Revision          string            `json:"revision,omitempty"`
	SHA256            string            `json:"sha256"`
	License           string            `json:"license"`
	LicenseFiles      []string          `json:"licenseFiles"`
	LicenseFileSHA256 map[string]string `json:"licenseFileSHA256"`
}

func LoadBuildLock(reader io.Reader) (BuildLock, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()

	var lock BuildLock
	if err := decoder.Decode(&lock); err != nil {
		return BuildLock{}, fmt.Errorf("decode host build lock: %w", err)
	}
	var trailingValue any
	if err := decoder.Decode(&trailingValue); err != io.EOF {
		if err == nil {
			return BuildLock{}, fmt.Errorf("decode host build lock: trailing JSON value")
		}
		return BuildLock{}, fmt.Errorf("decode host build lock: %w", err)
	}
	if err := lock.Validate(); err != nil {
		return BuildLock{}, err
	}
	return lock, nil
}

func (lock BuildLock) Validate() error {
	if lock.SchemaVersion != buildLockSchemaVersion {
		return fmt.Errorf("schemaVersion must be %d", buildLockSchemaVersion)
	}
	if lock.HostPlatform.OS != "darwin" ||
		(lock.HostPlatform.Architecture != "arm64" && lock.HostPlatform.Architecture != "amd64") {
		return fmt.Errorf("hostPlatform must be darwin/arm64 or darwin/amd64")
	}
	if len(lock.Components) != len(expectedComponentNames) {
		return fmt.Errorf("components must contain exactly %d entries", len(expectedComponentNames))
	}

	componentNames := make(map[string]struct{}, len(lock.Components))
	for _, component := range lock.Components {
		if _, exists := componentNames[component.Name]; exists {
			return fmt.Errorf("component %s is duplicated", component.Name)
		}
		componentNames[component.Name] = struct{}{}
	}

	for index, component := range lock.Components {
		if component.Name != expectedComponentNames[index] {
			return fmt.Errorf("component %d must be %s", index, expectedComponentNames[index])
		}
		if component.Version == "" {
			return fmt.Errorf("component %s version is required", component.Name)
		}
		if component.Source == "" {
			return fmt.Errorf("component %s source is required", component.Name)
		}
		if component.Name == "qemu" && component.Revision == "" {
			return fmt.Errorf("component qemu revision is required")
		}
		if component.Revision != "" && !buildLockRevisionPattern.MatchString(component.Revision) {
			return fmt.Errorf("component %s revision must be 40 lowercase hexadecimal characters", component.Name)
		}
		if !buildLockSHA256Pattern.MatchString(component.SHA256) {
			return fmt.Errorf("component %s sha256 must be 64 lowercase hexadecimal characters", component.Name)
		}
		if component.License == "" {
			return fmt.Errorf("component %s license is required", component.Name)
		}
		if len(component.LicenseFiles) == 0 {
			return fmt.Errorf("component %s licenseFiles must not be empty", component.Name)
		}
		if len(component.LicenseFileSHA256) != len(component.LicenseFiles) {
			return fmt.Errorf(
				"component %s licenseFileSHA256 must contain exactly %d entries",
				component.Name,
				len(component.LicenseFiles),
			)
		}

		licensePaths := make(map[string]struct{}, len(component.LicenseFiles))
		for _, licensePath := range component.LicenseFiles {
			if !isCleanLockPath(licensePath) {
				return fmt.Errorf("component %s license file must be a clean relative slash path: %s", component.Name, licensePath)
			}
			if _, exists := licensePaths[licensePath]; exists {
				return fmt.Errorf("component %s license file %s is duplicated", component.Name, licensePath)
			}
			licensePaths[licensePath] = struct{}{}
			licenseSHA256, exists := component.LicenseFileSHA256[licensePath]
			if !exists {
				return fmt.Errorf("component %s license file %s sha256 is required", component.Name, licensePath)
			}
			if !buildLockSHA256Pattern.MatchString(licenseSHA256) {
				return fmt.Errorf(
					"component %s license file %s sha256 must be 64 lowercase hexadecimal characters",
					component.Name,
					licensePath,
				)
			}
		}
	}
	return nil
}

func isCleanLockPath(filePath string) bool {
	if filePath == "" || filePath == "." || path.IsAbs(filePath) || strings.Contains(filePath, `\`) {
		return false
	}
	if filePath == ".." || strings.HasPrefix(filePath, "../") {
		return false
	}
	return path.Clean(filePath) == filePath
}
