package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

func TestPackageLinuxHostBuildsCompleteArtifact(t *testing.T) {
	workspace := t.TempDir()
	qemu := writeLinuxPackageFile(t, filepath.Join(workspace, "source", "qemu"), "qemu")
	loader := writeLinuxPackageFile(t, filepath.Join(workspace, "source", "loader"), "loader")
	glib := writeLinuxPackageFile(t, filepath.Join(workspace, "source", "libglib-2.0.so.0"), "glib")
	writeLinuxPackageFile(t, filepath.Join(workspace, "firmware", "bios-256k.bin"), "bios")
	writeLinuxPackageFile(t, filepath.Join(workspace, "licenses", "qemu", "COPYING"), "license")
	lock := LinuxBuildLock{
		SchemaVersion: 1,
		HostPlatform:  runtimepkg.Platform{OS: "linux", Architecture: "amd64"},
		Components: []runtimepkg.Component{{
			Name: "qemu", Version: "v11.0.2", Source: "https://download.qemu.org/qemu-11.0.2.tar.xz", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
		FirmwareFiles: []string{"bios-256k.bin"},
	}
	lockBytes, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	lockPath := filepath.Join(workspace, "build.lock.json")
	if err := os.WriteFile(lockPath, lockBytes, 0o644); err != nil {
		t.Fatalf("WriteFile(lock) error = %v", err)
	}
	outputPath := filepath.Join(workspace, "sealbuild-host-runtime-linux-amd64.tar.zst")
	result, err := packageLinuxHost(linuxPackageConfig{
		QEMUPath: qemu, QEMUDataDirectory: filepath.Join(workspace, "firmware"),
		LicenseDirectory: filepath.Join(workspace, "licenses"), LockPath: lockPath, OutputPath: outputPath,
	}, func(string, []string) (ELFClosure, error) {
		return ELFClosure{Executable: qemu, Loader: loader, Libraries: []ELFLibrary{{Name: "libglib-2.0.so.0", SourcePath: glib}}}, nil
	})
	if err != nil {
		t.Fatalf("packageLinuxHost() error = %v", err)
	}
	wantPaths := []string{
		"bin/qemu-system-x86_64",
		"lib/ld-linux-x86-64.so.2",
		"lib/libglib-2.0.so.0",
		"licenses/qemu/COPYING",
		"share/qemu/bios-256k.bin",
	}
	if len(result.Manifest.Files) != len(wantPaths) {
		t.Fatalf("manifest files = %#v, want paths %v", result.Manifest.Files, wantPaths)
	}
	for index, wantPath := range wantPaths {
		if result.Manifest.Files[index].Path != wantPath {
			t.Errorf("Files[%d].Path = %q, want %q", index, result.Manifest.Files[index].Path, wantPath)
		}
	}
	if result.Manifest.Platform != lock.HostPlatform {
		t.Fatalf("manifest platform = %#v, want %#v", result.Manifest.Platform, lock.HostPlatform)
	}
}

func writeLinuxPackageFile(t *testing.T, path, contents string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	return resolved
}
