package build

import (
	"archive/tar"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	digest "github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestVerifyOCIArchiveAcceptsLinuxAMD64Image(t *testing.T) {
	archivePath := writeOCIArchive(t, nil)
	if err := VerifyOCIArchive(archivePath); err != nil {
		t.Fatalf("VerifyOCIArchive() error = %v", err)
	}
}

func TestVerifyOCIArchiveRejectsInvalidMetadata(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*ociFixture)
		wantError string
	}{
		{name: "multiple manifests", mutate: func(fixture *ociFixture) {
			fixture.index.Manifests = append(fixture.index.Manifests, fixture.index.Manifests[0])
		}, wantError: "exactly one manifest"},
		{name: "ARM index platform", mutate: func(fixture *ociFixture) { fixture.index.Manifests[0].Platform.Architecture = "arm64" }, wantError: "linux/arm64"},
		{name: "platform variant", mutate: func(fixture *ociFixture) { fixture.index.Manifests[0].Platform.Variant = "v2" }, wantError: "variant"},
		{name: "manifest digest mismatch", mutate: func(fixture *ociFixture) {
			fixture.index.Manifests[0].Digest = digest.Digest("sha256:" + strings.Repeat("0", 64))
		}, wantError: "manifest blob"},
		{name: "manifest size mismatch", mutate: func(fixture *ociFixture) { fixture.index.Manifests[0].Size++ }, wantError: "size"},
		{name: "ARM config", mutate: func(fixture *ociFixture) { fixture.config.Architecture = "arm64" }, wantError: "config platform"},
		{name: "missing config blob", mutate: func(fixture *ociFixture) { fixture.omitConfig = true }, wantError: "config blob"},
		{name: "duplicate index", mutate: func(fixture *ociFixture) { fixture.duplicateIndex = true }, wantError: "duplicated"},
		{name: "symlink entry", mutate: func(fixture *ociFixture) { fixture.symlink = true }, wantError: "unsupported type"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := VerifyOCIArchive(writeOCIArchive(t, test.mutate))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.wantError)) {
				t.Fatalf("VerifyOCIArchive() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

type ociFixture struct {
	index          ocispec.Index
	manifest       ocispec.Manifest
	config         ocispec.Image
	omitConfig     bool
	duplicateIndex bool
	symlink        bool
}

func writeOCIArchive(t *testing.T, mutate func(*ociFixture)) string {
	t.Helper()
	fixture := ociFixture{
		config: ocispec.Image{
			Platform: ocispec.Platform{OS: "linux", Architecture: "amd64"},
			RootFS:   ocispec.RootFS{Type: "layers", DiffIDs: []digest.Digest{}},
		},
	}
	configBytes := marshalOCI(t, fixture.config)
	fixture.manifest = ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageConfig, Digest: digest.FromBytes(configBytes), Size: int64(len(configBytes)),
		},
	}
	manifestBytes := marshalOCI(t, fixture.manifest)
	fixture.index = ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: []ocispec.Descriptor{{
			MediaType: ocispec.MediaTypeImageManifest, Digest: digest.FromBytes(manifestBytes), Size: int64(len(manifestBytes)),
			Platform: &ocispec.Platform{OS: "linux", Architecture: "amd64"},
		}},
	}
	if mutate != nil {
		mutate(&fixture)
	}
	configBytes = marshalOCI(t, fixture.config)
	fixture.manifest.Config.Digest = digest.FromBytes(configBytes)
	fixture.manifest.Config.Size = int64(len(configBytes))
	manifestBytes = marshalOCI(t, fixture.manifest)
	if fixture.index.Manifests[0].Digest.Algorithm() != digest.SHA256 || fixture.index.Manifests[0].Digest.Encoded() != strings.Repeat("0", 64) {
		fixture.index.Manifests[0].Digest = digest.FromBytes(manifestBytes)
	}
	if fixture.index.Manifests[0].Size != int64(len(manifestBytes))+1 {
		fixture.index.Manifests[0].Size = int64(len(manifestBytes))
	}
	indexBytes := marshalOCI(t, fixture.index)

	archivePath := filepath.Join(t.TempDir(), "image.oci.tar")
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	writer := tar.NewWriter(archiveFile)
	writeTarDirectory(t, writer, "blobs/")
	writeTarDirectory(t, writer, "blobs/sha256/")
	writeTarFile(t, writer, "index.json", indexBytes)
	if fixture.duplicateIndex {
		writeTarFile(t, writer, "index.json", indexBytes)
	}
	writeTarFile(t, writer, blobPath(digest.FromBytes(manifestBytes)), manifestBytes)
	if !fixture.omitConfig {
		writeTarFile(t, writer, blobPath(digest.FromBytes(configBytes)), configBytes)
	}
	if fixture.symlink {
		if err := writer.WriteHeader(&tar.Header{Name: "unsafe", Typeflag: tar.TypeSymlink, Linkname: "index.json"}); err != nil {
			t.Fatalf("WriteHeader(symlink) error = %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("tar Close() error = %v", err)
	}
	if err := archiveFile.Close(); err != nil {
		t.Fatalf("archive Close() error = %v", err)
	}
	return archivePath
}

func marshalOCI(t *testing.T, value any) []byte {
	t.Helper()
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return contents
}

func blobPath(digest digest.Digest) string {
	return filepath.ToSlash(filepath.Join("blobs", digest.Algorithm().String(), digest.Encoded()))
}

func writeTarDirectory(t *testing.T, writer *tar.Writer, name string) {
	t.Helper()
	if err := writer.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatalf("WriteHeader(%s) error = %v", name, err)
	}
}

func writeTarFile(t *testing.T, writer *tar.Writer, name string, contents []byte) {
	t.Helper()
	if err := writer.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(contents))}); err != nil {
		t.Fatalf("WriteHeader(%s) error = %v", name, err)
	}
	if _, err := writer.Write(contents); err != nil {
		t.Fatalf("Write(%s) error = %v", name, err)
	}
}
