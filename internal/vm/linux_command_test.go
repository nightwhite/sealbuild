package vm

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLinuxQEMUCommandUsesEmbeddedLoaderAndLibraries(t *testing.T) {
	hostRoot := t.TempDir()
	qemuPath := writeLinuxCommandFile(t, filepath.Join(hostRoot, "bin", "qemu-system-x86_64"))
	loaderPath := writeLinuxCommandFile(t, filepath.Join(hostRoot, "lib", "ld-linux-x86-64.so.2"))
	libraryPath := filepath.Join(hostRoot, "lib")
	qemuArguments := []string{"-accel", "tcg,thread=multi"}

	launchPath, launchArguments, err := linuxQEMUCommand(qemuPath, qemuArguments)
	if err != nil {
		t.Fatalf("linuxQEMUCommand() error = %v", err)
	}
	if launchPath != loaderPath {
		t.Fatalf("launch path = %q, want %q", launchPath, loaderPath)
	}
	wantArguments := []string{
		"--inhibit-cache",
		"--library-path", libraryPath,
		qemuPath,
		"-accel", "tcg,thread=multi",
	}
	if !slices.Equal(launchArguments, wantArguments) {
		t.Fatalf("launch arguments = %#v, want %#v", launchArguments, wantArguments)
	}
}

func TestLinuxQEMUCommandRejectsIncompleteHostRuntime(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(*testing.T, string) string
		wantError string
	}{
		{
			name: "missing loader",
			prepare: func(t *testing.T, hostRoot string) string {
				qemuPath := writeLinuxCommandFile(t, filepath.Join(hostRoot, "bin", "qemu-system-x86_64"))
				if err := os.MkdirAll(filepath.Join(hostRoot, "lib"), 0o755); err != nil {
					t.Fatalf("MkdirAll(lib) error = %v", err)
				}
				return qemuPath
			},
			wantError: "inspect Linux QEMU loader file",
		},
		{
			name: "library path is not directory",
			prepare: func(t *testing.T, hostRoot string) string {
				qemuPath := writeLinuxCommandFile(t, filepath.Join(hostRoot, "bin", "qemu-system-x86_64"))
				writeLinuxCommandFile(t, filepath.Join(hostRoot, "lib"))
				return qemuPath
			},
			wantError: "Linux QEMU library path must be a directory",
		},
		{
			name: "unexpected QEMU layout",
			prepare: func(t *testing.T, hostRoot string) string {
				return writeLinuxCommandFile(t, filepath.Join(hostRoot, "qemu-system-x86_64"))
			},
			wantError: "Linux QEMU path must end with bin/qemu-system-x86_64",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			qemuPath := test.prepare(t, t.TempDir())
			_, _, err := linuxQEMUCommand(qemuPath, nil)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("linuxQEMUCommand() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func writeLinuxCommandFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("runtime"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
