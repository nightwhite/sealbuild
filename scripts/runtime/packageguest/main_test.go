package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

func TestPackageGuestBuildsFixedLinuxAMD64Payload(t *testing.T) {
	outputDirectory := createGuestInputs(t)

	result, err := packageGuest(guestPackageConfig{
		OutputDirectory: outputDirectory,
		LockPath:        filepath.Join(outputDirectory, "manifest.lock.json"),
	})
	if err != nil {
		t.Fatalf("packageGuest() error = %v", err)
	}
	if result.Manifest.Kind != runtimepkg.ArtifactKindGuest {
		t.Fatalf("manifest kind = %q, want guest", result.Manifest.Kind)
	}
	if result.Manifest.Platform != (runtimepkg.Platform{OS: "linux", Architecture: "amd64"}) {
		t.Fatalf("manifest platform = %#v, want linux/amd64", result.Manifest.Platform)
	}

	wantFiles := []string{
		"buildkit-state.qcow2",
		"bzImage",
		"licenses/buildkit/LICENSE",
		"licenses/buildroot/busybox/COPYING",
		"manifest.lock.json",
		"rootfs.ext4",
	}
	if len(result.Manifest.Files) != len(wantFiles) {
		t.Fatalf("manifest files = %#v, want %d files", result.Manifest.Files, len(wantFiles))
	}
	for index, file := range result.Manifest.Files {
		if file.Path != wantFiles[index] {
			t.Errorf("manifest file %d = %q, want %q", index, file.Path, wantFiles[index])
		}
		if strings.Contains(file.Path, "tls/") || file.Path == "buildkit-state.ext4" {
			t.Errorf("manifest contains forbidden payload %q", file.Path)
		}
	}
	for _, outputPath := range []string{
		filepath.Join(outputDirectory, "artifact", "buildkit-state.qcow2"),
		filepath.Join(outputDirectory, "artifact", "licenses", "buildkit", "LICENSE"),
		filepath.Join(outputDirectory, "artifact", "manifest.json"),
		filepath.Join(outputDirectory, "artifact", "checksums.txt"),
		filepath.Join(outputDirectory, guestArchiveName),
	} {
		if _, err := os.Stat(outputPath); err != nil {
			t.Errorf("Stat(%s) error = %v", outputPath, err)
		}
	}
}

func TestPackageGuestRejectsLicenseSymlinkWithoutPublishing(t *testing.T) {
	outputDirectory := createGuestInputs(t)
	licensePath := filepath.Join(outputDirectory, "guest-licenses", "buildkit", "LICENSE")
	if err := os.Remove(licensePath); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Symlink("../buildroot/busybox/COPYING", licensePath); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	_, err := packageGuest(guestPackageConfig{
		OutputDirectory: outputDirectory,
		LockPath:        filepath.Join(outputDirectory, "manifest.lock.json"),
	})
	if err == nil {
		t.Fatal("packageGuest() error = nil, want symlink rejection")
	}
	if !strings.Contains(err.Error(), "must be a regular file or directory") {
		t.Fatalf("packageGuest() error = %q, want file type error", err)
	}
	for _, outputPath := range []string{
		filepath.Join(outputDirectory, "artifact"),
		filepath.Join(outputDirectory, guestArchiveName),
	} {
		if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
			t.Fatalf("Stat(%s) error = %v, want not exist", outputPath, err)
		}
	}
}

func createGuestInputs(t *testing.T) string {
	t.Helper()
	outputDirectory := t.TempDir()
	writeGuestFile(t, outputDirectory, "buildroot/images/bzImage", "kernel", 0o644)
	writeGuestFile(t, outputDirectory, "buildroot/images/rootfs.ext2", "rootfs", 0o644)
	if err := os.Symlink("rootfs.ext2", filepath.Join(outputDirectory, "buildroot", "images", "rootfs.ext4")); err != nil {
		t.Fatalf("Symlink(rootfs.ext4) error = %v", err)
	}
	writeGuestFile(t, outputDirectory, "buildkit-state.qcow2", "qcow2", 0o600)
	writeGuestFile(t, outputDirectory, "guest-licenses/buildroot/busybox/COPYING", "busybox", 0o644)
	writeGuestFile(t, outputDirectory, "guest-licenses/buildkit/LICENSE", "buildkit", 0o644)
	writeGuestFile(t, outputDirectory, "manifest.lock.json", `{
  "schemaVersion": 1,
  "guestPlatform": {"os": "linux", "architecture": "amd64"},
  "components": [{
    "name": "buildkit",
    "version": "v0.31.1",
    "source": "https://example.invalid/buildkit",
    "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }]
}`, 0o644)
	return outputDirectory
}

func writeGuestFile(t *testing.T, root, relativePath, contents string, mode os.FileMode) {
	t.Helper()
	filePath := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", relativePath, err)
	}
	if err := os.WriteFile(filePath, []byte(contents), mode); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", relativePath, err)
	}
}
