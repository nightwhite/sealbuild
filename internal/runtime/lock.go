// Package runtime validates metadata for the embedded guest runtime.
package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
)

const lockSchemaVersion = 1

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Lock describes the immutable inputs used to build a guest runtime.
type Lock struct {
	SchemaVersion int         `json:"schemaVersion"`
	GuestPlatform Platform    `json:"guestPlatform"`
	Components    []Component `json:"components"`
}

// Platform identifies the only image platform Sealbuild produces.
type Platform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

// Component identifies one pinned runtime input.
type Component struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Source   string `json:"source"`
	Revision string `json:"revision,omitempty"`
	SHA256   string `json:"sha256"`
}

// LoadLock decodes and validates one Runtime Lock Manifest.
func LoadLock(reader io.Reader) (Lock, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()

	var lock Lock
	if err := decoder.Decode(&lock); err != nil {
		return Lock{}, fmt.Errorf("decode runtime lock: %w", err)
	}

	var trailingValue any
	if err := decoder.Decode(&trailingValue); err != io.EOF {
		if err == nil {
			return Lock{}, fmt.Errorf("decode runtime lock: trailing JSON value")
		}
		return Lock{}, fmt.Errorf("decode runtime lock: %w", err)
	}

	if err := lock.Validate(); err != nil {
		return Lock{}, err
	}
	return lock, nil
}

// Validate checks the immutable product constraints represented by the lock.
func (lock Lock) Validate() error {
	if lock.SchemaVersion != lockSchemaVersion {
		return fmt.Errorf("schemaVersion must be %d", lockSchemaVersion)
	}
	if lock.GuestPlatform.OS != "linux" || lock.GuestPlatform.Architecture != "amd64" {
		return fmt.Errorf("guestPlatform must be linux/amd64")
	}
	if len(lock.Components) == 0 {
		return fmt.Errorf("components must not be empty")
	}

	componentNames := make(map[string]struct{}, len(lock.Components))
	for _, component := range lock.Components {
		if component.Name == "" {
			return fmt.Errorf("component name is required")
		}
		if _, exists := componentNames[component.Name]; exists {
			return fmt.Errorf("component %s is duplicated", component.Name)
		}
		componentNames[component.Name] = struct{}{}

		if component.Version == "" {
			return fmt.Errorf("component %s version is required", component.Name)
		}
		if component.Source == "" {
			return fmt.Errorf("component %s source is required", component.Name)
		}
		if !sha256Pattern.MatchString(component.SHA256) {
			return fmt.Errorf("component %s sha256 must be 64 lowercase hexadecimal characters", component.Name)
		}
	}
	return nil
}
