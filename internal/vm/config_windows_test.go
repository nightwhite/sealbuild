//go:build windows

package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labring/sealbuild/internal/tlsmaterial"
)

func TestWindowsShutdownChardevUsesLoopbackTCP(t *testing.T) {
	config := validWindowsConfig(t)
	config.HostPort = 49152
	config.ShutdownPort = 49153
	args, err := config.Args()
	if err != nil {
		t.Fatalf("Args() error = %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "socket,id=shutdown,host=127.0.0.1,port=49153,server=on,wait=off") {
		t.Fatalf("Args() = %q, want loopback TCP shutdown chardev", joined)
	}
	for _, forbidden := range []string{"0.0.0.0", "whpx", "hvf", "kvm"} {
		if strings.Contains(strings.ToLower(joined), forbidden) {
			t.Fatalf("Args() contains forbidden value %q", forbidden)
		}
	}
}

func validWindowsConfig(t *testing.T) Config {
	t.Helper()
	directory := t.TempDir()
	tls, err := tlsmaterial.Generate(filepath.Join(directory, "tls"), time.Now())
	if err != nil {
		t.Fatalf("Generate(TLS) error = %v", err)
	}
	return Config{
		QEMUPath:     writeWindowsConfigFile(t, filepath.Join(directory, "qemu-system-x86_64.exe"), "qemu"),
		FirmwarePath: writeWindowsConfigDirectory(t, filepath.Join(directory, "share", "qemu")),
		KernelPath:   writeWindowsConfigFile(t, filepath.Join(directory, "bzImage"), "kernel"),
		RootFSPath:   writeWindowsConfigFile(t, filepath.Join(directory, "rootfs.ext4"), "rootfs"),
		StatePath:    writeWindowsConfigFile(t, filepath.Join(directory, "state.qcow2"), "state"),
		SerialPath:   writeWindowsConfigFile(t, filepath.Join(directory, "serial.log"), ""),
		TLS:          tls,
	}
}

func writeWindowsConfigFile(t *testing.T, path, contents string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	return path
}

func writeWindowsConfigDirectory(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
	return path
}
