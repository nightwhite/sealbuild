package artifact

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

func TestScanPayloadReturnsSortedRegularFiles(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "lib/data", []byte("runtime-data"), 0o644)
	writeTestFile(t, root, "bin/tool", []byte("runtime-tool"), 0o755)

	files, err := ScanPayload(root)
	if err != nil {
		t.Fatalf("ScanPayload() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}

	wantPaths := []string{"bin/tool", "lib/data"}
	wantContents := [][]byte{[]byte("runtime-tool"), []byte("runtime-data")}
	wantModes := []uint32{0o755, 0o644}
	for index, file := range files {
		if file.Path != wantPaths[index] {
			t.Errorf("files[%d].Path = %q, want %q", index, file.Path, wantPaths[index])
		}
		checksum := sha256.Sum256(wantContents[index])
		if file.SHA256 != fmt.Sprintf("%x", checksum) {
			t.Errorf("files[%d].SHA256 = %q, want %x", index, file.SHA256, checksum)
		}
		if file.Size != int64(len(wantContents[index])) {
			t.Errorf("files[%d].Size = %d, want %d", index, file.Size, len(wantContents[index]))
		}
		if file.Mode != wantModes[index] {
			t.Errorf("files[%d].Mode = %#o, want %#o", index, file.Mode, wantModes[index])
		}
	}
}

func TestScanPayloadRejectsUnsafePayload(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(*testing.T, string)
		wantError string
	}{
		{
			name:      "empty payload",
			prepare:   func(*testing.T, string) {},
			wantError: "payload must contain at least one file",
		},
		{
			name: "reserved manifest",
			prepare: func(t *testing.T, root string) {
				writeTestFile(t, root, "manifest.json", []byte("reserved"), 0o644)
			},
			wantError: "payload path manifest.json is reserved",
		},
		{
			name: "reserved checksums",
			prepare: func(t *testing.T, root string) {
				writeTestFile(t, root, "checksums.txt", []byte("reserved"), 0o644)
			},
			wantError: "payload path checksums.txt is reserved",
		},
		{
			name: "symbolic link",
			prepare: func(t *testing.T, root string) {
				writeTestFile(t, root, "target", []byte("target"), 0o644)
				if err := os.Symlink("target", filepath.Join(root, "link")); err != nil {
					t.Fatalf("Symlink() error = %v", err)
				}
			},
			wantError: "payload entry link must be a regular file or directory",
		},
		{
			name: "unsupported permission",
			prepare: func(t *testing.T, root string) {
				writeTestFile(t, root, "secret", []byte("secret"), 0o640)
			},
			wantError: "payload file secret mode must be 0600, 0644, or 0755",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			test.prepare(t, root)

			_, err := ScanPayload(root)
			if err == nil {
				t.Fatal("ScanPayload() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("ScanPayload() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestBuildCreatesDeterministicVerifiedArchive(t *testing.T) {
	payloadDir := t.TempDir()
	writeTestFile(t, payloadDir, "lib/data", []byte("runtime-data"), 0o644)
	writeTestFile(t, payloadDir, "bin/tool", []byte("runtime-tool"), 0o755)

	manifest := runtimepkg.ArtifactManifest{
		SchemaVersion: 1,
		Kind:          runtimepkg.ArtifactKindHost,
		Platform:      runtimepkg.Platform{OS: "darwin", Architecture: "arm64"},
		Components: []runtimepkg.Component{{
			Name:    "qemu",
			Version: "v11.0.2",
			Source:  "https://example.invalid/qemu",
			SHA256:  strings.Repeat("a", 64),
		}},
		Files: []runtimepkg.ArtifactFile{{
			Path:   "forged",
			SHA256: strings.Repeat("b", 64),
			Size:   1,
			Mode:   0o644,
		}},
	}

	firstArchive := filepath.Join(t.TempDir(), "first.tar.zst")
	firstResult, err := Build(BuildConfig{
		PayloadDir: payloadDir,
		OutputPath: firstArchive,
		Manifest:   manifest,
	})
	if err != nil {
		t.Fatalf("Build(first) error = %v", err)
	}
	secondArchive := filepath.Join(t.TempDir(), "second.tar.zst")
	secondResult, err := Build(BuildConfig{
		PayloadDir: payloadDir,
		OutputPath: secondArchive,
		Manifest:   manifest,
	})
	if err != nil {
		t.Fatalf("Build(second) error = %v", err)
	}

	firstBytes, err := os.ReadFile(firstArchive)
	if err != nil {
		t.Fatalf("ReadFile(first) error = %v", err)
	}
	secondBytes, err := os.ReadFile(secondArchive)
	if err != nil {
		t.Fatalf("ReadFile(second) error = %v", err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("Build() produced different bytes for identical inputs")
	}
	archiveHash := sha256.Sum256(firstBytes)
	if firstResult.ArchiveSHA256 != fmt.Sprintf("%x", archiveHash) {
		t.Fatalf("ArchiveSHA256 = %q, want %x", firstResult.ArchiveSHA256, archiveHash)
	}
	if firstResult.ArchiveSize != int64(len(firstBytes)) {
		t.Fatalf("ArchiveSize = %d, want %d", firstResult.ArchiveSize, len(firstBytes))
	}
	if !reflect.DeepEqual(firstResult, secondResult) {
		t.Fatalf("BuildResult differs: first = %#v, second = %#v", firstResult, secondResult)
	}

	entries := readArchive(t, firstArchive)
	wantNames := []string{"bin/tool", "lib/data", "manifest.json", "checksums.txt"}
	if len(entries) != len(wantNames) {
		t.Fatalf("len(entries) = %d, want %d", len(entries), len(wantNames))
	}
	for index, name := range wantNames {
		if entries[index].header.Name != name {
			t.Errorf("entries[%d].Name = %q, want %q", index, entries[index].header.Name, name)
		}
		assertDeterministicHeader(t, entries[index].header)
	}

	loadedManifest, err := runtimepkg.LoadArtifactManifest(bytes.NewReader(entries[2].contents))
	if err != nil {
		t.Fatalf("LoadArtifactManifest() error = %v", err)
	}
	if len(loadedManifest.Files) != 2 {
		t.Fatalf("len(manifest.Files) = %d, want 2", len(loadedManifest.Files))
	}
	if loadedManifest.Files[0].Path != "bin/tool" || loadedManifest.Files[1].Path != "lib/data" {
		t.Fatalf("manifest files = %#v, want scanned payload", loadedManifest.Files)
	}
	if firstResult.Manifest.Files[0].Path != "bin/tool" {
		t.Fatalf("BuildResult.Manifest contains caller-provided file list: %#v", firstResult.Manifest.Files)
	}

	manifestHash := sha256.Sum256(entries[2].contents)
	wantChecksums := fmt.Sprintf(
		"%s  bin/tool\n%s  lib/data\n%x  manifest.json\n",
		loadedManifest.Files[0].SHA256,
		loadedManifest.Files[1].SHA256,
		manifestHash,
	)
	if string(entries[3].contents) != wantChecksums {
		t.Fatalf("checksums.txt = %q, want %q", entries[3].contents, wantChecksums)
	}
}

func TestBuildRejectsExistingOutputWithoutChangingIt(t *testing.T) {
	payloadDir := t.TempDir()
	writeTestFile(t, payloadDir, "payload", []byte("payload"), 0o644)
	outputPath := filepath.Join(t.TempDir(), "runtime.tar.zst")
	if err := os.WriteFile(outputPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("WriteFile(output) error = %v", err)
	}

	_, err := Build(BuildConfig{
		PayloadDir: payloadDir,
		OutputPath: outputPath,
		Manifest:   validBuildManifest(),
	})
	if err == nil {
		t.Fatal("Build() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "artifact output already exists") {
		t.Fatalf("Build() error = %q, want existing output error", err)
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(output) error = %v", err)
	}
	if string(contents) != "existing" {
		t.Fatalf("output contents = %q, want existing", contents)
	}
}

func TestBuildFailureLeavesNoFinalOutput(t *testing.T) {
	payloadDir := t.TempDir()
	writeTestFile(t, payloadDir, "payload", []byte("payload"), 0o644)
	outputPath := filepath.Join(t.TempDir(), "runtime.tar.zst")
	manifest := validBuildManifest()
	manifest.Platform.Architecture = "amd64"

	_, err := Build(BuildConfig{
		PayloadDir: payloadDir,
		OutputPath: outputPath,
		Manifest:   manifest,
	})
	if err == nil {
		t.Fatal("Build() error = nil, want error")
	}
	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("Stat(output) error = %v, want not exist", statErr)
	}
}

func writeTestFile(t *testing.T, root, relativePath string, contents []byte, mode os.FileMode) {
	t.Helper()

	filePath := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filePath, contents, mode); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chmod(filePath, mode); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
}

type archiveEntry struct {
	header   *tar.Header
	contents []byte
}

func readArchive(t *testing.T, archivePath string) []archiveEntry {
	t.Helper()

	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("Open(archive) error = %v", err)
	}
	defer file.Close()
	decoder, err := zstd.NewReader(file)
	if err != nil {
		t.Fatalf("zstd.NewReader() error = %v", err)
	}
	defer decoder.Close()

	reader := tar.NewReader(decoder)
	var entries []archiveEntry
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return entries
		}
		if err != nil {
			t.Fatalf("tar.Next() error = %v", err)
		}
		contents, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("ReadAll(%s) error = %v", header.Name, err)
		}
		headerCopy := *header
		entries = append(entries, archiveEntry{header: &headerCopy, contents: contents})
	}
}

func assertDeterministicHeader(t *testing.T, header *tar.Header) {
	t.Helper()

	if header.Uid != 0 || header.Gid != 0 || header.Uname != "root" || header.Gname != "root" {
		t.Errorf("header %s ownership = %d:%d %q:%q, want root", header.Name, header.Uid, header.Gid, header.Uname, header.Gname)
	}
	if !header.ModTime.Equal(time.Unix(0, 0).UTC()) {
		t.Errorf("header %s ModTime = %s, want Unix epoch", header.Name, header.ModTime)
	}
	if !header.AccessTime.IsZero() || !header.ChangeTime.IsZero() {
		t.Errorf("header %s has non-zero access/change time", header.Name)
	}
}

func validBuildManifest() runtimepkg.ArtifactManifest {
	return runtimepkg.ArtifactManifest{
		SchemaVersion: 1,
		Kind:          runtimepkg.ArtifactKindHost,
		Platform:      runtimepkg.Platform{OS: "darwin", Architecture: "arm64"},
		Components: []runtimepkg.Component{{
			Name:    "qemu",
			Version: "v11.0.2",
			Source:  "https://example.invalid/qemu",
			SHA256:  strings.Repeat("a", 64),
		}},
	}
}
