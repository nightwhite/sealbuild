package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

func TestLoadLinuxBuildLockValidatesSchema(t *testing.T) {
	valid := `{
		"schemaVersion":1,
		"hostPlatform":{"os":"linux","architecture":"amd64"},
		"components":[{"name":"qemu","version":"v11.0.2","source":"https://download.qemu.org/qemu-11.0.2.tar.xz","sha256":"` + strings.Repeat("a", 64) + `"}],
		"runtimePackages":[{"name":"libc6","version":"2.39-1"}],
		"firmwareFiles":["bios-256k.bin"]
	}`
	tests := []struct {
		name      string
		input     string
		wantError string
	}{
		{name: "valid", input: valid},
		{name: "wrong platform", input: strings.Replace(valid, `"os":"linux"`, `"os":"darwin"`, 1), wantError: "hostPlatform must be linux/amd64"},
		{name: "unsafe firmware", input: strings.Replace(valid, `"bios-256k.bin"`, `"../bios.bin"`, 1), wantError: "firmware file must be a clean relative slash path"},
		{name: "unknown field", input: strings.TrimSuffix(valid, "}") + `,"unknown":true}`, wantError: "unknown field"},
		{name: "trailing JSON", input: valid + `{}`, wantError: "trailing JSON value"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lock, err := loadLinuxBuildLock(strings.NewReader(test.input))
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("loadLinuxBuildLock() error = %v", err)
				}
				if lock.HostPlatform != (runtimepkg.Platform{OS: "linux", Architecture: "amd64"}) {
					t.Fatalf("HostPlatform = %#v", lock.HostPlatform)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("loadLinuxBuildLock() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestRepositoryLinuxBuildLockPinsObservedRuntimePackages(t *testing.T) {
	file, err := os.Open("../../../runtime/host/linux-amd64/build.lock.json")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer file.Close()
	lock, err := loadLinuxBuildLock(file)
	if err != nil {
		t.Fatalf("loadLinuxBuildLock() error = %v", err)
	}
	want := []LockedRuntimePackage{
		{Name: "libaio1t64", Version: "0.3.113-6build1.1"},
		{Name: "libblkid1", Version: "2.39.3-9ubuntu6.5"},
		{Name: "libc6", Version: "2.39-0ubuntu8.7"},
		{Name: "libfdt1", Version: "1.7.0-2build1"},
		{Name: "libffi8", Version: "3.4.6-1build1"},
		{Name: "libglib2.0-0t64", Version: "2.80.0-6ubuntu3.8"},
		{Name: "libmount1", Version: "2.39.3-9ubuntu6.5"},
		{Name: "libpcre2-8-0", Version: "10.42-4ubuntu2.1"},
		{Name: "libpixman-1-0", Version: "0.42.2-1build1"},
		{Name: "libseccomp2", Version: "2.5.5-1ubuntu3.1"},
		{Name: "libselinux1", Version: "3.5-2ubuntu2.1"},
		{Name: "libslirp0", Version: "4.7.0-1ubuntu3.1"},
		{Name: "liburing2", Version: "2.5-1build1"},
		{Name: "libzstd1", Version: "1.5.5+dfsg2-2build1.1"},
		{Name: "zlib1g", Version: "1:1.3.dfsg-3.1ubuntu2.1"},
	}
	if !reflect.DeepEqual(lock.RuntimePackages, want) {
		t.Fatalf("RuntimePackages = %#v, want %#v", lock.RuntimePackages, want)
	}
}

func TestLoadLinuxRuntimePackageEvidenceRequiresSortedExactPackages(t *testing.T) {
	packages, err := loadLinuxRuntimePackageEvidence(strings.NewReader("libc6\t2.39-1\nlibzstd1\t1.5.5-1\n"))
	if err != nil {
		t.Fatalf("loadLinuxRuntimePackageEvidence() error = %v", err)
	}
	want := []LockedRuntimePackage{{Name: "libc6", Version: "2.39-1"}, {Name: "libzstd1", Version: "1.5.5-1"}}
	if !reflect.DeepEqual(packages, want) {
		t.Fatalf("packages = %#v, want %#v", packages, want)
	}
	for _, input := range []string{"", "libc6 2.39-1\n", "libzstd1\t1\nlibc6\t1\n", "libc6\t1\nlibc6\t1\n"} {
		if _, err := loadLinuxRuntimePackageEvidence(strings.NewReader(input)); err == nil {
			t.Errorf("loadLinuxRuntimePackageEvidence(%q) error = nil", input)
		}
	}
}

func TestRepositoryLinuxBuildLockPinsQEMUAndFirmware(t *testing.T) {
	file, err := os.Open("../../../runtime/host/linux-amd64/build.lock.json")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	lock, loadErr := loadLinuxBuildLock(file)
	closeErr := file.Close()
	if loadErr != nil {
		t.Fatalf("loadLinuxBuildLock() error = %v", loadErr)
	}
	if closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
	if len(lock.Components) != 1 || lock.Components[0].Name != "qemu" || lock.Components[0].Version != "v11.0.2" {
		t.Fatalf("Components = %#v, want pinned QEMU", lock.Components)
	}
	if len(lock.FirmwareFiles) != 4 {
		t.Fatalf("FirmwareFiles = %#v, want four files", lock.FirmwareFiles)
	}
}
