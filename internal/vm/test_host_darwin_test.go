//go:build darwin

package vm

import (
	"path/filepath"
	"testing"
)

func writeTestQEMU(t *testing.T, directory string) string {
	t.Helper()
	return writeConfigFile(t, filepath.Join(directory, "qemu-system-x86_64"), "qemu", 0o755)
}

func expectedTestLaunchPath(config Config) string {
	return config.QEMUPath
}
