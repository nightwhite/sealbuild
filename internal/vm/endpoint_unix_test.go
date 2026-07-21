//go:build !windows

package vm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareShutdownPathResolvesSymlinkedTempDirectory(t *testing.T) {
	realTemp := filepath.Join(t.TempDir(), "real-temp")
	if err := os.Mkdir(realTemp, 0o700); err != nil {
		t.Fatalf("Mkdir(real temp) error = %v", err)
	}
	linkedTemp := filepath.Join(t.TempDir(), "linked-temp")
	if err := os.Symlink(realTemp, linkedTemp); err != nil {
		t.Fatalf("Symlink(temp) error = %v", err)
	}
	t.Setenv("TMPDIR", linkedTemp)

	shutdownPath, err := PrepareShutdownPath()
	if err != nil {
		t.Fatalf("PrepareShutdownPath() error = %v", err)
	}
	config := validConfig(t)
	config.ShutdownPath = shutdownPath
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
