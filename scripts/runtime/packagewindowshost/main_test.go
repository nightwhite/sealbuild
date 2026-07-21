package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

func TestPackageWindowsHostBuildsVerifiedArtifact(t *testing.T) {
	workspace := t.TempDir()
	qemu := writePackageFile(t, filepath.Join(workspace, "qemu-system-x86_64.exe"), "qemu")
	dll := writePackageFile(t, filepath.Join(workspace, "libglib-2.0-0.dll"), "glib")
	firmwareDirectory := filepath.Join(workspace, "firmware")
	writePackageFile(t, filepath.Join(firmwareDirectory, "bios-256k.bin"), "bios")
	licenseDirectory := filepath.Join(workspace, "licenses")
	writePackageFile(t, filepath.Join(licenseDirectory, "qemu", "COPYING"), "license")
	lock := WindowsBuildLock{
		SchemaVersion: 1,
		HostPlatform:  runtimepkg.Platform{OS: "windows", Architecture: "amd64"},
		Components: []runtimepkg.Component{{
			Name: "qemu", Version: "v11.0.2", Source: "https://download.qemu.org/qemu-11.0.2.tar.xz",
			Revision: strings.Repeat("a", 40), SHA256: strings.Repeat("b", 64),
		}},
		FirmwareFiles: []string{"bios-256k.bin"},
	}
	lockBytes, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("Marshal(lock) error = %v", err)
	}
	lockPath := writePackageFile(t, filepath.Join(workspace, "build.lock.json"), string(lockBytes))
	outputPath := filepath.Join(workspace, "windows-host.tar.zst")

	result, err := packageWindowsHost(windowsPackageConfig{
		QEMUPath: qemu, QEMUDataDirectory: firmwareDirectory, LicenseDirectory: licenseDirectory,
		LockPath: lockPath, OutputPath: outputPath,
	}, func(string, []string) ([]string, error) { return []string{qemu, dll}, nil })
	if err != nil {
		t.Fatalf("packageWindowsHost() error = %v", err)
	}
	if result.Manifest.Platform != lock.HostPlatform || result.ArchiveSize == 0 || result.ArchiveSHA256 == "" {
		t.Fatalf("result = %#v", result)
	}
	paths := make([]string, 0, len(result.Manifest.Files))
	for _, file := range result.Manifest.Files {
		paths = append(paths, file.Path)
	}
	for _, want := range []string{"bin/libglib-2.0-0.dll", "bin/qemu-system-x86_64.exe", "licenses/qemu/COPYING", "share/qemu/bios-256k.bin"} {
		if !containsString(paths, want) {
			t.Errorf("manifest paths = %#v, missing %q", paths, want)
		}
	}
}

func writePackageFile(t *testing.T, path, contents string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
