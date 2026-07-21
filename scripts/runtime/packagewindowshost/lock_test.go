package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

func TestWindowsBuildLockRejectsInvalidRuntimePackages(t *testing.T) {
	tests := []struct {
		name     string
		packages []LockedRuntimePackage
		want     string
	}{
		{name: "empty", want: "runtimePackages must not be empty"},
		{name: "empty name", packages: []LockedRuntimePackage{{Version: "1.0-1"}}, want: "name must not be empty"},
		{name: "empty version", packages: []LockedRuntimePackage{{Name: "package-a"}}, want: "version must not be empty"},
		{
			name: "duplicate",
			packages: []LockedRuntimePackage{
				{Name: "package-a", Version: "1.0-1"},
				{Name: "package-a", Version: "1.0-1"},
			},
			want: "duplicated",
		},
		{
			name: "unsorted",
			packages: []LockedRuntimePackage{
				{Name: "package-b", Version: "1.0-1"},
				{Name: "package-a", Version: "1.0-1"},
			},
			want: "sorted",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lock := validWindowsBuildLock()
			lock.RuntimePackages = test.packages
			err := lock.validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateRuntimePackageEvidenceRequiresExactClosureAndLock(t *testing.T) {
	root := t.TempDir()
	closure := []string{
		filepath.Join(root, "qemu-system-x86_64.exe"),
		filepath.Join(root, "libffi-8.dll"),
		filepath.Join(root, "libglib-2.0-0.dll"),
	}
	locked := []LockedRuntimePackage{
		{Name: "mingw-glib2", Version: "2.88.2-1"},
		{Name: "mingw-libffi", Version: "3.7.1-1"},
	}
	evidence := RuntimePackageEvidence{SchemaVersion: 1, DLLs: []RuntimePackageDLL{
		{Name: "libffi-8.dll", Package: "mingw-libffi", Version: "3.7.1-1"},
		{Name: "libglib-2.0-0.dll", Package: "mingw-glib2", Version: "2.88.2-1"},
	}}
	if err := validateRuntimePackageEvidence(closure, evidence, locked); err != nil {
		t.Fatalf("validateRuntimePackageEvidence() error = %v", err)
	}

	tests := []struct {
		name     string
		evidence RuntimePackageEvidence
		locked   []LockedRuntimePackage
		want     string
	}{
		{
			name: "unmapped DLL",
			evidence: RuntimePackageEvidence{SchemaVersion: 1, DLLs: []RuntimePackageDLL{
				{Name: "libffi-8.dll", Package: "mingw-libffi", Version: "3.7.1-1"},
			}},
			locked: locked,
			want:   "libglib-2.0-0.dll is not mapped",
		},
		{
			name: "package version drift",
			evidence: RuntimePackageEvidence{SchemaVersion: 1, DLLs: []RuntimePackageDLL{
				{Name: "libffi-8.dll", Package: "mingw-libffi", Version: "3.7.2-1"},
				{Name: "libglib-2.0-0.dll", Package: "mingw-glib2", Version: "2.88.2-1"},
			}},
			locked: locked,
			want:   "runtime package mismatch",
		},
		{
			name: "unlocked package",
			evidence: RuntimePackageEvidence{SchemaVersion: 1, DLLs: []RuntimePackageDLL{
				{Name: "libffi-8.dll", Package: "mingw-libffi", Version: "3.7.1-1"},
				{Name: "libglib-2.0-0.dll", Package: "mingw-other", Version: "2.88.2-1"},
			}},
			locked: locked,
			want:   "runtime package mismatch",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateRuntimePackageEvidence(closure, test.evidence, test.locked)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateRuntimePackageEvidence() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRepositoryWindowsBuildLockPinsObservedRuntimePackages(t *testing.T) {
	lockFile, err := os.Open("../../../runtime/host/windows-amd64/build.lock.json")
	if err != nil {
		t.Fatalf("Open(build.lock.json) error = %v", err)
	}
	defer lockFile.Close()
	lock, err := loadWindowsBuildLock(lockFile)
	if err != nil {
		t.Fatalf("loadWindowsBuildLock() error = %v", err)
	}
	want := []LockedRuntimePackage{
		{Name: "mingw-w64-clang-x86_64-bzip2", Version: "1.0.8-3"},
		{Name: "mingw-w64-clang-x86_64-gettext-runtime", Version: "1.0-1"},
		{Name: "mingw-w64-clang-x86_64-glib2", Version: "2.88.2-1"},
		{Name: "mingw-w64-clang-x86_64-libffi", Version: "3.7.1-1"},
		{Name: "mingw-w64-clang-x86_64-libiconv", Version: "1.19-1"},
		{Name: "mingw-w64-clang-x86_64-libslirp", Version: "4.9.3-1"},
		{Name: "mingw-w64-clang-x86_64-libwinpthread", Version: "14.0.0.r190.g96fb1bff7-1"},
		{Name: "mingw-w64-clang-x86_64-ncurses", Version: "6.6-4"},
		{Name: "mingw-w64-clang-x86_64-pcre2", Version: "10.47-1"},
		{Name: "mingw-w64-clang-x86_64-pixman", Version: "0.46.4-3"},
		{Name: "mingw-w64-clang-x86_64-zlib", Version: "1.3.2-2"},
		{Name: "mingw-w64-clang-x86_64-zstd", Version: "1.5.7-2"},
	}
	if !reflect.DeepEqual(lock.RuntimePackages, want) {
		t.Fatalf("RuntimePackages = %#v, want %#v", lock.RuntimePackages, want)
	}
}

func validWindowsBuildLock() WindowsBuildLock {
	return WindowsBuildLock{
		SchemaVersion: 1,
		HostPlatform:  runtimepkg.Platform{OS: "windows", Architecture: "amd64"},
		Components: []runtimepkg.Component{{
			Name: "qemu", Version: "v11.0.2", Source: "https://download.qemu.org/qemu-11.0.2.tar.xz",
			Revision: strings.Repeat("a", 40), SHA256: strings.Repeat("b", 64),
		}},
		RuntimePackages: []LockedRuntimePackage{{Name: "package-a", Version: "1.0-1"}},
		FirmwareFiles:   []string{"bios-256k.bin"},
	}
}
