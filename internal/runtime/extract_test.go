package runtime_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	runtimepkg "github.com/labring/sealbuild/internal/runtime"
	"github.com/labring/sealbuild/scripts/runtime/artifact"
)

func TestExtractAssetInstallsVerifiedHostArtifact(t *testing.T) {
	workspace := t.TempDir()
	payloadDirectory := filepath.Join(workspace, "payload")
	writeExtractFixture(t, payloadDirectory, "bin/qemu-system-x86_64", "qemu", 0o755)
	writeExtractFixture(t, payloadDirectory, "lib/libglib.dylib", "glib", 0o644)
	archivePath := filepath.Join(workspace, "host.tar.zst")
	buildResult, err := artifact.Build(artifact.BuildConfig{
		PayloadDir: payloadDirectory,
		OutputPath: archivePath,
		Manifest: runtimepkg.ArtifactManifest{
			SchemaVersion: 1,
			Kind:          runtimepkg.ArtifactKindHost,
			Platform:      runtimepkg.Platform{OS: "darwin", Architecture: "arm64"},
			Components: []runtimepkg.Component{{
				Name: "qemu", Version: "v11.0.2", Source: "https://example.invalid/qemu",
				SHA256: strings.Repeat("a", 64),
			}},
		},
	})
	if err != nil {
		t.Fatalf("artifact.Build() error = %v", err)
	}
	destination := filepath.Join(workspace, "installed")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatalf("Mkdir(destination) error = %v", err)
	}

	result, err := runtimepkg.ExtractAsset(
		t.Context(),
		fileAsset(t, archivePath, buildResult.ArchiveSHA256, buildResult.ArchiveSize),
		runtimepkg.ArtifactKindHost,
		destination,
	)
	if err != nil {
		t.Fatalf("ExtractAsset() error = %v", err)
	}
	if result.SHA256 != buildResult.ArchiveSHA256 || result.Size != buildResult.ArchiveSize {
		t.Fatalf("ExtractResult = %#v, want archive SHA and size", result)
	}
	if result.Manifest.Kind != runtimepkg.ArtifactKindHost || len(result.Manifest.Files) != 2 {
		t.Fatalf("ExtractResult.Manifest = %#v", result.Manifest)
	}

	wantFiles := map[string]struct {
		contents string
		mode     os.FileMode
	}{
		"bin/qemu-system-x86_64": {contents: "qemu", mode: 0o755},
		"lib/libglib.dylib":      {contents: "glib", mode: 0o644},
		"manifest.json":          {mode: 0o644},
		"checksums.txt":          {mode: 0o644},
	}
	for relativePath, want := range wantFiles {
		filePath := filepath.Join(destination, filepath.FromSlash(relativePath))
		contents, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", relativePath, err)
		}
		if want.contents != "" && string(contents) != want.contents {
			t.Errorf("%s contents = %q, want %q", relativePath, contents, want.contents)
		}
		info, err := os.Stat(filePath)
		if err != nil {
			t.Fatalf("Stat(%s) error = %v", relativePath, err)
		}
		if info.Mode().Perm() != want.mode {
			t.Errorf("%s mode = %#o, want %#o", relativePath, info.Mode().Perm(), want.mode)
		}
	}
}

func TestExtractAssetRejectsUnsafeTarEntries(t *testing.T) {
	tests := []struct {
		name      string
		entries   []rawTarEntry
		wantError string
	}{
		{name: "absolute path", entries: []rawTarEntry{{name: "/escape", contents: "x"}}, wantError: "unsafe archive path"},
		{name: "parent path", entries: []rawTarEntry{{name: "../escape", contents: "x"}}, wantError: "unsafe archive path"},
		{name: "backslash path", entries: []rawTarEntry{{name: `bin\\qemu`, contents: "x"}}, wantError: "unsafe archive path"},
		{name: "symlink", entries: []rawTarEntry{{name: "link", typeFlag: tar.TypeSymlink, linkName: "target"}}, wantError: "unsupported archive entry type"},
		{name: "hardlink", entries: []rawTarEntry{{name: "link", typeFlag: tar.TypeLink, linkName: "target"}}, wantError: "unsupported archive entry type"},
		{name: "fifo", entries: []rawTarEntry{{name: "pipe", typeFlag: tar.TypeFifo}}, wantError: "unsupported archive entry type"},
		{name: "duplicate file", entries: []rawTarEntry{{name: "bin/qemu", contents: "one"}, {name: "bin/qemu", contents: "two"}}, wantError: "archive entry bin/qemu is duplicated"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			asset := rawAsset(t, test.entries)
			destination := filepath.Join(t.TempDir(), "destination")
			if err := os.Mkdir(destination, 0o755); err != nil {
				t.Fatalf("Mkdir() error = %v", err)
			}
			_, err := runtimepkg.ExtractAsset(t.Context(), asset, runtimepkg.ArtifactKindHost, destination)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("ExtractAsset() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestExtractAssetRejectsDescriptorAndDestinationErrors(t *testing.T) {
	asset := rawAsset(t, []rawTarEntry{{name: "bin/qemu", contents: "qemu"}})

	t.Run("checksum mismatch", func(t *testing.T) {
		invalid := asset
		invalid.SHA256 = strings.Repeat("0", 64)
		destination := filepath.Join(t.TempDir(), "destination")
		if err := os.Mkdir(destination, 0o755); err != nil {
			t.Fatalf("Mkdir() error = %v", err)
		}
		_, err := runtimepkg.ExtractAsset(t.Context(), invalid, runtimepkg.ArtifactKindHost, destination)
		if err == nil || !strings.Contains(err.Error(), "asset SHA-256") {
			t.Fatalf("ExtractAsset() error = %v, want SHA mismatch", err)
		}
	})

	t.Run("size mismatch", func(t *testing.T) {
		invalid := asset
		invalid.Size++
		destination := filepath.Join(t.TempDir(), "destination")
		if err := os.Mkdir(destination, 0o755); err != nil {
			t.Fatalf("Mkdir() error = %v", err)
		}
		_, err := runtimepkg.ExtractAsset(t.Context(), invalid, runtimepkg.ArtifactKindHost, destination)
		if err == nil || !strings.Contains(err.Error(), "asset size") {
			t.Fatalf("ExtractAsset() error = %v, want size mismatch", err)
		}
	})

	t.Run("nonempty destination", func(t *testing.T) {
		destination := t.TempDir()
		if err := os.WriteFile(filepath.Join(destination, "existing"), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		_, err := runtimepkg.ExtractAsset(t.Context(), asset, runtimepkg.ArtifactKindHost, destination)
		if err == nil || !strings.Contains(err.Error(), "destination must be empty") {
			t.Fatalf("ExtractAsset() error = %v, want nonempty destination", err)
		}
	})

	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		destination := filepath.Join(t.TempDir(), "destination")
		if err := os.Mkdir(destination, 0o755); err != nil {
			t.Fatalf("Mkdir() error = %v", err)
		}
		_, err := runtimepkg.ExtractAsset(ctx, asset, runtimepkg.ArtifactKindHost, destination)
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("ExtractAsset() error = %v, want context cancellation", err)
		}
	})
}

func TestExtractAssetRejectsInvalidArtifactMetadata(t *testing.T) {
	tests := []struct {
		name      string
		asset     func(*testing.T) runtimepkg.Asset
		wantError string
	}{
		{
			name: "missing manifest",
			asset: func(t *testing.T) runtimepkg.Asset {
				return rawAsset(t, []rawTarEntry{{name: "bin/qemu", contents: "qemu"}})
			},
			wantError: "manifest.json is missing",
		},
		{
			name: "missing checksums",
			asset: func(t *testing.T) runtimepkg.Asset {
				manifestBytes, _ := fixtureManifest(t, runtimepkg.ArtifactKindHost, "qemu")
				return rawAsset(t, []rawTarEntry{
					{name: "bin/qemu", contents: "qemu"},
					{name: "manifest.json", contents: string(manifestBytes)},
				})
			},
			wantError: "checksums.txt is missing",
		},
		{
			name: "wrong kind",
			asset: func(t *testing.T) runtimepkg.Asset {
				return metadataAsset(t, runtimepkg.ArtifactKindGuest, "qemu", "")
			},
			wantError: "artifact kind is guest, expected host",
		},
		{
			name: "payload mismatch",
			asset: func(t *testing.T) runtimepkg.Asset {
				return metadataAsset(t, runtimepkg.ArtifactKindHost, "changed", "qemu")
			},
			wantError: "metadata does not match manifest",
		},
		{
			name: "checksums mismatch",
			asset: func(t *testing.T) runtimepkg.Asset {
				return metadataAsset(t, runtimepkg.ArtifactKindHost, "qemu", "invalid checksums\n")
			},
			wantError: "checksums.txt does not match",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "destination")
			if err := os.Mkdir(destination, 0o755); err != nil {
				t.Fatalf("Mkdir() error = %v", err)
			}
			_, err := runtimepkg.ExtractAsset(t.Context(), test.asset(t), runtimepkg.ArtifactKindHost, destination)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("ExtractAsset() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestExtractAssetRejectsInterruptedCompressedStream(t *testing.T) {
	asset := runtimepkg.Asset{
		Name: "interrupted.tar.zst", SHA256: strings.Repeat("a", 64), Size: 1024,
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(io.MultiReader(strings.NewReader("partial"), failingReader{})), nil
		},
	}
	destination := filepath.Join(t.TempDir(), "destination")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	_, err := runtimepkg.ExtractAsset(t.Context(), asset, runtimepkg.ArtifactKindHost, destination)
	if err == nil || !strings.Contains(err.Error(), "copy Runtime asset") {
		t.Fatalf("ExtractAsset() error = %v, want interrupted copy", err)
	}
}

func fileAsset(t *testing.T, archivePath, checksum string, size int64) runtimepkg.Asset {
	t.Helper()
	return runtimepkg.Asset{
		Name: "fixture.tar.zst", SHA256: checksum, Size: size,
		Open: func() (io.ReadCloser, error) { return os.Open(archivePath) },
	}
}

func writeExtractFixture(t *testing.T, root, relativePath, contents string, mode os.FileMode) {
	t.Helper()
	filePath := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filePath, []byte(contents), mode); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

type rawTarEntry struct {
	name     string
	contents string
	typeFlag byte
	linkName string
	mode     int64
}

func rawAsset(t *testing.T, entries []rawTarEntry) runtimepkg.Asset {
	t.Helper()
	var compressed bytes.Buffer
	encoder, err := zstd.NewWriter(&compressed, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}
	tarWriter := tar.NewWriter(encoder)
	for _, entry := range entries {
		typeFlag := entry.typeFlag
		if typeFlag == 0 {
			typeFlag = tar.TypeReg
		}
		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		header := &tar.Header{
			Name: entry.name, Mode: mode, Size: int64(len(entry.contents)),
			Typeflag: typeFlag, Linkname: entry.linkName,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("WriteHeader(%s) error = %v", entry.name, err)
		}
		if typeFlag == tar.TypeReg {
			if _, err := tarWriter.Write([]byte(entry.contents)); err != nil {
				t.Fatalf("Write(%s) error = %v", entry.name, err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("Close(tar) error = %v", err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatalf("Close(zstd) error = %v", err)
	}
	contents := append([]byte(nil), compressed.Bytes()...)
	checksum := sha256.Sum256(contents)
	return runtimepkg.Asset{
		Name: "raw.tar.zst", SHA256: fmt.Sprintf("%x", checksum), Size: int64(len(contents)),
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(contents)), nil
		},
	}
}

func metadataAsset(t *testing.T, kind runtimepkg.ArtifactKind, payloadContents, checksumsOverride string) runtimepkg.Asset {
	t.Helper()
	manifestBytes, manifest := fixtureManifest(t, kind, "qemu")
	manifestHash := sha256.Sum256(manifestBytes)
	checksums := fmt.Sprintf("%s  bin/qemu\n%x  manifest.json\n", manifest.Files[0].SHA256, manifestHash)
	if checksumsOverride != "" {
		checksums = checksumsOverride
	}
	return rawAsset(t, []rawTarEntry{
		{name: "bin/qemu", contents: payloadContents},
		{name: "manifest.json", contents: string(manifestBytes)},
		{name: "checksums.txt", contents: checksums},
	})
}

func fixtureManifest(t *testing.T, kind runtimepkg.ArtifactKind, payloadContents string) ([]byte, runtimepkg.ArtifactManifest) {
	t.Helper()
	payloadHash := sha256.Sum256([]byte(payloadContents))
	platform := runtimepkg.Platform{OS: "darwin", Architecture: "arm64"}
	if kind == runtimepkg.ArtifactKindGuest {
		platform = runtimepkg.Platform{OS: "linux", Architecture: "amd64"}
	}
	manifest := runtimepkg.ArtifactManifest{
		SchemaVersion: 1,
		Kind:          kind,
		Platform:      platform,
		Components: []runtimepkg.Component{{
			Name: "component", Version: "v1", Source: "https://example.invalid/source", SHA256: strings.Repeat("a", 64),
		}},
		Files: []runtimepkg.ArtifactFile{{
			Path: "bin/qemu", SHA256: fmt.Sprintf("%x", payloadHash), Size: int64(len(payloadContents)), Mode: 0o644,
		}},
	}
	contents, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	return append(contents, '\n'), manifest
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("fixture stream failed")
}
