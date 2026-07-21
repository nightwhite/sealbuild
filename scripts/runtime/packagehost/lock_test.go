package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

func TestBuildLockAcceptsPinnedDarwinHosts(t *testing.T) {
	for _, architecture := range []string{"arm64", "amd64"} {
		t.Run(architecture, func(t *testing.T) {
			lock := validBuildLock()
			lock.HostPlatform.Architecture = architecture

			if err := lock.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestBuildLockRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*BuildLock)
		wantError string
	}{
		{
			name:      "unknown schema",
			mutate:    func(lock *BuildLock) { lock.SchemaVersion = 2 },
			wantError: "schemaVersion must be 1",
		},
		{
			name:      "wrong host platform",
			mutate:    func(lock *BuildLock) { lock.HostPlatform.OS = "linux" },
			wantError: "hostPlatform must be darwin/arm64 or darwin/amd64",
		},
		{
			name: "wrong component order",
			mutate: func(lock *BuildLock) {
				lock.Components[0], lock.Components[1] = lock.Components[1], lock.Components[0]
			},
			wantError: "component 0 must be qemu",
		},
		{
			name: "duplicate component",
			mutate: func(lock *BuildLock) {
				lock.Components[1].Name = "qemu"
			},
			wantError: "component qemu is duplicated",
		},
		{
			name:      "missing version",
			mutate:    func(lock *BuildLock) { lock.Components[0].Version = "" },
			wantError: "component qemu version is required",
		},
		{
			name:      "missing source",
			mutate:    func(lock *BuildLock) { lock.Components[0].Source = "" },
			wantError: "component qemu source is required",
		},
		{
			name:      "invalid revision",
			mutate:    func(lock *BuildLock) { lock.Components[0].Revision = "main" },
			wantError: "component qemu revision must be 40 lowercase hexadecimal characters",
		},
		{
			name:      "invalid checksum",
			mutate:    func(lock *BuildLock) { lock.Components[0].SHA256 = "ABC" },
			wantError: "component qemu sha256 must be 64 lowercase hexadecimal characters",
		},
		{
			name:      "missing license",
			mutate:    func(lock *BuildLock) { lock.Components[0].License = "" },
			wantError: "component qemu license is required",
		},
		{
			name:      "missing license files",
			mutate:    func(lock *BuildLock) { lock.Components[0].LicenseFiles = nil },
			wantError: "component qemu licenseFiles must not be empty",
		},
		{
			name:      "unsafe license path",
			mutate:    func(lock *BuildLock) { lock.Components[0].LicenseFiles[0] = "../COPYING" },
			wantError: "component qemu license file must be a clean relative slash path",
		},
		{
			name: "duplicate license path",
			mutate: func(lock *BuildLock) {
				lock.Components[0].LicenseFiles = []string{"COPYING", "COPYING"}
			},
			wantError: "component qemu license file COPYING is duplicated",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lock := validBuildLock()
			test.mutate(&lock)

			err := lock.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Validate() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestLoadBuildLockRejectsUnknownFieldAndTrailingJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError string
	}{
		{
			name: "unknown field",
			input: `{"schemaVersion":1,"hostPlatform":{"os":"darwin","architecture":"arm64"},` +
				`"components":[],"unknown":true}`,
			wantError: "decode host build lock",
		},
		{
			name: "trailing JSON",
			input: `{"schemaVersion":1,"hostPlatform":{"os":"darwin","architecture":"arm64"},` +
				`"components":[]} {}`,
			wantError: "trailing JSON value",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadBuildLock(strings.NewReader(test.input))
			if err == nil {
				t.Fatal("LoadBuildLock() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("LoadBuildLock() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestRepositoryBuildLockPinsVerifiedSources(t *testing.T) {
	file, err := os.Open("../../../runtime/host/darwin-arm64/build.lock.json")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer file.Close()

	lock, err := LoadBuildLock(file)
	if err != nil {
		t.Fatalf("LoadBuildLock() error = %v", err)
	}

	expected := []struct {
		name    string
		version string
		sha256  string
	}{
		{"qemu", "v11.0.2", "3745f6ea88e2e87fe0dc838b2b1d4e0a770bf48e01a1d5a186842a1fff76ccf5"},
		{"glib", "2.88.2", "cf3f215a640c8a4257f14317586b8f1fdd25a10a93cb4bdda147c0f9ad88e74f"},
		{"pixman", "0.46.4", "d09c44ebc3bd5bee7021c79f922fe8fb2fb57f7320f55e97ff9914d2346a591c"},
		{"libslirp", "4.9.3", "ee698ca4ce05217ca7d520c7f0b1b1228fd7d32922dd32d1051c347152588417"},
		{"zstd", "1.5.7", "37d7284556b20954e56e1ca85b80226768902e2edabd3b649e9e72c0c9012ee3"},
		{"gettext", "1.0", "85d99b79c981a404874c02e0342176cf75c7698e2b51fe41031cf6526d974f1a"},
		{"pcre2", "10.47", "47fe8c99461250d42f89e6e8fdaeba9da057855d06eb7fc08d9ca03fd08d7bc7"},
	}
	if len(lock.Components) != len(expected) {
		t.Fatalf("len(Components) = %d, want %d", len(lock.Components), len(expected))
	}
	for index, component := range lock.Components {
		if component.Name != expected[index].name || component.Version != expected[index].version || component.SHA256 != expected[index].sha256 {
			t.Errorf("Components[%d] = %s %s %s, want %s %s %s", index, component.Name, component.Version, component.SHA256, expected[index].name, expected[index].version, expected[index].sha256)
		}
	}
}

func TestRepositoryBuildLockPinsGLibLicenseFileInsteadOfSymlink(t *testing.T) {
	file, err := os.Open("../../../runtime/host/darwin-arm64/build.lock.json")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer file.Close()

	lock, err := LoadBuildLock(file)
	if err != nil {
		t.Fatalf("LoadBuildLock() error = %v", err)
	}
	glib := lock.Components[1]
	want := []string{"LICENSES/LGPL-2.1-or-later.txt"}
	if !slices.Equal(glib.LicenseFiles, want) {
		t.Fatalf("glib licenseFiles = %#v, want %#v", glib.LicenseFiles, want)
	}
}

func validBuildLock() BuildLock {
	components := []LockedComponent{
		{
			Name:         "qemu",
			Version:      "v11.0.2",
			Source:       "https://download.qemu.org/qemu-11.0.2.tar.xz",
			Revision:     "e545d8bb9d63e9dd61542b88463183314cff9482",
			SHA256:       "3745f6ea88e2e87fe0dc838b2b1d4e0a770bf48e01a1d5a186842a1fff76ccf5",
			License:      "GPL-2.0-only AND LGPL-2.1-only",
			LicenseFiles: []string{"COPYING", "COPYING.LIB", "LICENSE"},
		},
	}
	for _, name := range []string{"glib", "pixman", "libslirp", "zstd", "gettext", "pcre2"} {
		components = append(components, LockedComponent{
			Name:         name,
			Version:      "1.0.0",
			Source:       "https://example.invalid/" + name,
			SHA256:       strings.Repeat("a", 64),
			License:      "MIT",
			LicenseFiles: []string{"LICENSE"},
		})
	}
	return BuildLock{
		SchemaVersion: 1,
		HostPlatform:  runtimepkg.Platform{OS: "darwin", Architecture: "arm64"},
		Components:    components,
	}
}
