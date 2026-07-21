package main

import (
	"os"
	"strings"
	"testing"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

func TestLoadLinuxBuildLockValidatesSchema(t *testing.T) {
	valid := `{
		"schemaVersion":1,
		"hostPlatform":{"os":"linux","architecture":"amd64"},
		"components":[{"name":"qemu","version":"v11.0.2","source":"https://download.qemu.org/qemu-11.0.2.tar.xz","sha256":"` + strings.Repeat("a", 64) + `"}],
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
