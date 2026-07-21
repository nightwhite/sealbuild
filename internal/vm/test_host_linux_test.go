//go:build linux

package vm

import (
	"path/filepath"
	"testing"
)

func writeTestQEMU(t *testing.T, directory string) string {
	t.Helper()
	binDirectory := writeConfigDirectory(t, filepath.Join(directory, "host", "bin"))
	libraryDirectory := writeConfigDirectory(t, filepath.Join(directory, "host", "lib"))
	writeConfigFile(t, filepath.Join(libraryDirectory, "ld-linux-x86-64.so.2"), "loader", 0o755)
	return writeConfigFile(t, filepath.Join(binDirectory, "qemu-system-x86_64"), "qemu", 0o755)
}

func expectedTestLaunchPath(config Config) string {
	return filepath.Join(filepath.Dir(filepath.Dir(config.QEMUPath)), "lib", "ld-linux-x86-64.so.2")
}
