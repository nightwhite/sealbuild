package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

const artifactManifestSchemaVersion = 1

// ArtifactKind identifies whether an artifact runs on the host or in the guest.
type ArtifactKind string

const (
	// ArtifactKindHost identifies a host Runtime artifact.
	ArtifactKindHost ArtifactKind = "host"
	// ArtifactKindGuest identifies a guest Runtime artifact.
	ArtifactKindGuest ArtifactKind = "guest"
)

// ArtifactManifest describes one immutable Runtime artifact payload.
type ArtifactManifest struct {
	SchemaVersion int            `json:"schemaVersion"`
	Kind          ArtifactKind   `json:"kind"`
	Platform      Platform       `json:"platform"`
	Components    []Component    `json:"components"`
	Files         []ArtifactFile `json:"files"`
}

// ArtifactFile describes one regular payload file.
type ArtifactFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
}

// LoadArtifactManifest decodes and validates one Runtime artifact manifest.
func LoadArtifactManifest(reader io.Reader) (ArtifactManifest, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()

	var manifest ArtifactManifest
	if err := decoder.Decode(&manifest); err != nil {
		return ArtifactManifest{}, fmt.Errorf("decode artifact manifest: %w", err)
	}

	var trailingValue any
	if err := decoder.Decode(&trailingValue); err != io.EOF {
		if err == nil {
			return ArtifactManifest{}, fmt.Errorf("decode artifact manifest: trailing JSON value")
		}
		return ArtifactManifest{}, fmt.Errorf("decode artifact manifest: %w", err)
	}

	if err := manifest.Validate(); err != nil {
		return ArtifactManifest{}, err
	}
	return manifest, nil
}

// Validate checks the immutable constraints represented by the manifest.
func (manifest ArtifactManifest) Validate() error {
	if manifest.SchemaVersion != artifactManifestSchemaVersion {
		return fmt.Errorf("schemaVersion must be %d", artifactManifestSchemaVersion)
	}

	switch manifest.Kind {
	case ArtifactKindHost:
		if !manifest.Platform.isSupportedHost() {
			return fmt.Errorf("host platform must be darwin/arm64 or windows/amd64")
		}
	case ArtifactKindGuest:
		if manifest.Platform.OS != "linux" || manifest.Platform.Architecture != "amd64" {
			return fmt.Errorf("guest platform must be linux/amd64")
		}
	default:
		return fmt.Errorf("kind must be host or guest")
	}

	if err := validateComponents(manifest.Components); err != nil {
		return err
	}
	if len(manifest.Files) == 0 {
		return fmt.Errorf("files must not be empty")
	}

	filePaths := make(map[string]struct{}, len(manifest.Files))
	previousPath := ""
	for _, file := range manifest.Files {
		if !isCleanArtifactPath(file.Path) {
			return fmt.Errorf("file path must be a clean relative slash path: %s", file.Path)
		}
		if file.Path == "manifest.json" || file.Path == "checksums.txt" {
			return fmt.Errorf("file path %s is reserved", file.Path)
		}
		if _, exists := filePaths[file.Path]; exists {
			return fmt.Errorf("file path %s is duplicated", file.Path)
		}
		filePaths[file.Path] = struct{}{}
		if previousPath != "" && file.Path < previousPath {
			return fmt.Errorf("files must be sorted by path")
		}
		previousPath = file.Path

		if !sha256Pattern.MatchString(file.SHA256) {
			return fmt.Errorf("file %s sha256 must be 64 lowercase hexadecimal characters", file.Path)
		}
		if file.Size <= 0 {
			return fmt.Errorf("file %s size must be greater than zero", file.Path)
		}
		switch file.Mode {
		case 0o600, 0o644, 0o755:
		default:
			return fmt.Errorf("file %s mode must be 0600, 0644, or 0755", file.Path)
		}
	}
	return nil
}

func (platform Platform) isSupportedHost() bool {
	return platform == (Platform{OS: "darwin", Architecture: "arm64"}) ||
		platform == (Platform{OS: "windows", Architecture: "amd64"})
}

func isCleanArtifactPath(filePath string) bool {
	if filePath == "" || filePath == "." || path.IsAbs(filePath) || strings.Contains(filePath, `\`) {
		return false
	}
	if filePath == ".." || strings.HasPrefix(filePath, "../") {
		return false
	}
	return path.Clean(filePath) == filePath
}
