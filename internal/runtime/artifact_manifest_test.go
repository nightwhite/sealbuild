package runtime

import (
	"strings"
	"testing"
)

func TestArtifactManifestAcceptsDarwinARMHost(t *testing.T) {
	manifest := validArtifactManifest()

	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestArtifactManifestAcceptsWindowsAMD64Host(t *testing.T) {
	manifest := validArtifactManifest()
	manifest.Platform = Platform{OS: "windows", Architecture: "amd64"}

	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestArtifactManifestAcceptsLinuxAMD64Guest(t *testing.T) {
	manifest := validArtifactManifest()
	manifest.Kind = ArtifactKindGuest
	manifest.Platform = Platform{OS: "linux", Architecture: "amd64"}

	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestArtifactManifestRejectsInvalidMetadata(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*ArtifactManifest)
		wantError string
	}{
		{
			name:      "unknown schema",
			mutate:    func(manifest *ArtifactManifest) { manifest.SchemaVersion = 2 },
			wantError: "schemaVersion must be 1",
		},
		{
			name:      "unknown kind",
			mutate:    func(manifest *ArtifactManifest) { manifest.Kind = "cache" },
			wantError: "kind must be host or guest",
		},
		{
			name:      "wrong host platform",
			mutate:    func(manifest *ArtifactManifest) { manifest.Platform = Platform{OS: "linux", Architecture: "amd64"} },
			wantError: "host platform must be darwin/arm64 or windows/amd64",
		},
		{
			name: "wrong guest platform",
			mutate: func(manifest *ArtifactManifest) {
				manifest.Kind = ArtifactKindGuest
				manifest.Platform = Platform{OS: "linux", Architecture: "arm64"}
			},
			wantError: "guest platform must be linux/amd64",
		},
		{
			name:      "missing components",
			mutate:    func(manifest *ArtifactManifest) { manifest.Components = nil },
			wantError: "components must not be empty",
		},
		{
			name:      "missing files",
			mutate:    func(manifest *ArtifactManifest) { manifest.Files = nil },
			wantError: "files must not be empty",
		},
		{
			name:      "absolute path",
			mutate:    func(manifest *ArtifactManifest) { manifest.Files[0].Path = "/bin/qemu" },
			wantError: "file path must be a clean relative slash path",
		},
		{
			name:      "parent path",
			mutate:    func(manifest *ArtifactManifest) { manifest.Files[0].Path = "bin/../qemu" },
			wantError: "file path must be a clean relative slash path",
		},
		{
			name:      "backslash path",
			mutate:    func(manifest *ArtifactManifest) { manifest.Files[0].Path = `bin\qemu` },
			wantError: "file path must be a clean relative slash path",
		},
		{
			name:      "manifest in payload",
			mutate:    func(manifest *ArtifactManifest) { manifest.Files[0].Path = "manifest.json" },
			wantError: "file path manifest.json is reserved",
		},
		{
			name: "duplicate path",
			mutate: func(manifest *ArtifactManifest) {
				manifest.Files = append(manifest.Files, manifest.Files[0])
			},
			wantError: "file path bin/qemu-system-x86_64 is duplicated",
		},
		{
			name: "unsorted paths",
			mutate: func(manifest *ArtifactManifest) {
				manifest.Files = []ArtifactFile{
					{Path: "lib/libz.dylib", SHA256: validSHA256, Size: 20, Mode: 0o644},
					{Path: "bin/qemu", SHA256: validSHA256, Size: 10, Mode: 0o755},
				}
			},
			wantError: "files must be sorted by path",
		},
		{
			name:      "invalid checksum",
			mutate:    func(manifest *ArtifactManifest) { manifest.Files[0].SHA256 = "ABC" },
			wantError: "sha256 must be 64 lowercase hexadecimal characters",
		},
		{
			name:      "empty file",
			mutate:    func(manifest *ArtifactManifest) { manifest.Files[0].Size = 0 },
			wantError: "size must be greater than zero",
		},
		{
			name:      "unsupported mode",
			mutate:    func(manifest *ArtifactManifest) { manifest.Files[0].Mode = 0o4755 },
			wantError: "mode must be 0600, 0644, or 0755",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := validArtifactManifest()
			test.mutate(&manifest)

			err := manifest.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Validate() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestLoadArtifactManifestRejectsUnknownFieldAndTrailingJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError string
	}{
		{
			name: "unknown field",
			input: `{"schemaVersion":1,"kind":"host","platform":{"os":"darwin","architecture":"arm64"},` +
				`"components":[{"name":"qemu","version":"v11.0.2","source":"https://example.invalid/qemu","sha256":"` + validSHA256 + `"}],` +
				`"files":[{"path":"bin/qemu","sha256":"` + validSHA256 + `","size":1,"mode":493}],"unknown":true}`,
			wantError: "decode artifact manifest",
		},
		{
			name: "trailing JSON",
			input: `{"schemaVersion":1,"kind":"host","platform":{"os":"darwin","architecture":"arm64"},` +
				`"components":[{"name":"qemu","version":"v11.0.2","source":"https://example.invalid/qemu","sha256":"` + validSHA256 + `"}],` +
				`"files":[{"path":"bin/qemu","sha256":"` + validSHA256 + `","size":1,"mode":493}]} {}`,
			wantError: "trailing JSON value",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadArtifactManifest(strings.NewReader(test.input))
			if err == nil {
				t.Fatal("LoadArtifactManifest() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("LoadArtifactManifest() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func validArtifactManifest() ArtifactManifest {
	return ArtifactManifest{
		SchemaVersion: 1,
		Kind:          ArtifactKindHost,
		Platform:      Platform{OS: "darwin", Architecture: "arm64"},
		Components: []Component{{
			Name:    "qemu",
			Version: "v11.0.2",
			Source:  "https://example.invalid/qemu",
			SHA256:  validSHA256,
		}},
		Files: []ArtifactFile{{
			Path:   "bin/qemu-system-x86_64",
			SHA256: validSHA256,
			Size:   1024,
			Mode:   0o755,
		}},
	}
}
