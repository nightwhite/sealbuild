//go:build !windows

package vm

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/labring/sealbuild/internal/tlsmaterial"
)

func TestConfigArgsConstructsFixedTCGCommand(t *testing.T) {
	config := validConfig(t)
	args, err := config.Args()
	if err != nil {
		t.Fatalf("Args() error = %v", err)
	}

	want := []string{
		"-L", config.FirmwarePath,
		"-accel", "tcg,thread=multi",
		"-machine", "q35",
		"-cpu", "max",
		"-smp", "4",
		"-m", "4096",
		"-nodefaults",
		"-no-reboot",
		"-nographic",
		"-kernel", config.KernelPath,
		"-append", "root=/dev/vda ro console=ttyS0,115200 panic=1",
		"-drive", "file=" + config.RootFSPath + ",format=raw,if=virtio,readonly=on",
		"-drive", "file=" + config.StatePath + ",format=qcow2,if=virtio",
		"-netdev", "user,id=net0,hostfwd=tcp:127.0.0.1:49152-:1234",
		"-device", "virtio-net-pci,netdev=net0",
		"-chardev", "socket,id=shutdown,path=" + config.ShutdownPath + ",server=on,wait=off",
		"-device", "virtio-serial-pci",
		"-device", "virtserialport,chardev=shutdown,name=org.sealbuild.shutdown",
		"-fw_cfg", "name=opt/sealbuild/tls/ca.crt,file=" + config.TLS.CA,
		"-fw_cfg", "name=opt/sealbuild/tls/server.crt,file=" + config.TLS.ServerCert,
		"-fw_cfg", "name=opt/sealbuild/tls/server.key,file=" + config.TLS.ServerKey,
		"-serial", "file:" + config.SerialPath,
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("Args() = %#v, want %#v", args, want)
	}

	args[0] = "modified"
	second, err := config.Args()
	if err != nil {
		t.Fatalf("second Args() error = %v", err)
	}
	if second[0] != "-L" {
		t.Fatalf("second Args()[0] = %q, want independent result", second[0])
	}

	joined := strings.ToLower(strings.Join(second, " "))
	for _, forbidden := range []string{"hvf", "kvm", "whpx", "0.0.0.0"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("Args() contains forbidden value %q", forbidden)
		}
	}
}

func TestConfigArgsAddsProxyByFileWithoutExposingContents(t *testing.T) {
	config := validConfig(t)
	config.ProxyFile = writeConfigFile(t, filepath.Join(t.TempDir(), "proxy"), "http://10.0.2.2:7890", 0o600)

	args, err := config.Args()
	if err != nil {
		t.Fatalf("Args() error = %v", err)
	}
	wantProxy := "name=opt/sealbuild/proxy/url,file=" + config.ProxyFile
	if !slices.Contains(args, wantProxy) {
		t.Fatalf("Args() = %#v, want proxy fw_cfg %q", args, wantProxy)
	}
	if strings.Contains(strings.Join(args, " "), "http://10.0.2.2:7890") {
		t.Fatal("Args() exposes proxy URL contents")
	}
}

func TestConfigValidateRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*testing.T, *Config)
		wantError string
	}{
		{name: "zero port", mutate: func(_ *testing.T, config *Config) { config.HostPort = 0 }, wantError: "host port"},
		{name: "relative QEMU", mutate: func(_ *testing.T, config *Config) { config.QEMUPath = "qemu" }, wantError: "QEMU path must be absolute"},
		{name: "relative kernel", mutate: func(_ *testing.T, config *Config) { config.KernelPath = "bzImage" }, wantError: "kernel path must be absolute"},
		{name: "relative rootfs", mutate: func(_ *testing.T, config *Config) { config.RootFSPath = "rootfs.ext4" }, wantError: "rootfs path must be absolute"},
		{name: "relative firmware", mutate: func(_ *testing.T, config *Config) { config.FirmwarePath = "share/qemu" }, wantError: "firmware path must be absolute"},
		{name: "relative state", mutate: func(_ *testing.T, config *Config) { config.StatePath = "state.qcow2" }, wantError: "state path must be absolute"},
		{name: "relative serial", mutate: func(_ *testing.T, config *Config) { config.SerialPath = "serial.log" }, wantError: "serial path must be absolute"},
		{name: "relative shutdown", mutate: func(_ *testing.T, config *Config) { config.ShutdownPath = "shutdown.sock" }, wantError: "shutdown path must be absolute"},
		{name: "existing shutdown", mutate: func(t *testing.T, config *Config) {
			config.ShutdownPath = writeConfigFile(t, filepath.Join(t.TempDir(), "shutdown.sock"), "stale", 0o600)
		}, wantError: "shutdown path already exists"},
		{name: "missing QEMU", mutate: func(t *testing.T, config *Config) { config.QEMUPath = filepath.Join(t.TempDir(), "missing") }, wantError: "inspect QEMU file"},
		{name: "QEMU directory", mutate: func(t *testing.T, config *Config) { config.QEMUPath = t.TempDir() }, wantError: "QEMU path must be a regular file"},
		{name: "invalid TLS", mutate: func(_ *testing.T, config *Config) {
			if err := os.Chmod(config.TLS.ServerKey, 0o644); err != nil {
				t.Fatalf("Chmod() error = %v", err)
			}
		}, wantError: "validate TLS material"},
		{name: "relative proxy", mutate: func(_ *testing.T, config *Config) { config.ProxyFile = "proxy" }, wantError: "proxy path must be absolute"},
		{name: "proxy wrong mode", mutate: func(t *testing.T, config *Config) {
			config.ProxyFile = writeConfigFile(t, filepath.Join(t.TempDir(), "proxy"), "http://10.0.2.2:7890", 0o644)
		}, wantError: "proxy file mode"},
		{name: "proxy directory", mutate: func(t *testing.T, config *Config) { config.ProxyFile = t.TempDir() }, wantError: "proxy path must be a regular file"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validConfig(t)
			test.mutate(t, &config)
			err := config.Validate()
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Validate() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func validConfig(t *testing.T) Config {
	t.Helper()
	directory := t.TempDir()
	tls, err := tlsmaterial.Generate(filepath.Join(directory, "tls"), time.Now())
	if err != nil {
		t.Fatalf("Generate(TLS) error = %v", err)
	}
	return Config{
		QEMUPath:     writeTestQEMU(t, directory),
		FirmwarePath: writeConfigDirectory(t, filepath.Join(directory, "share", "qemu")),
		KernelPath:   writeConfigFile(t, filepath.Join(directory, "bzImage"), "kernel", 0o644),
		RootFSPath:   writeConfigFile(t, filepath.Join(directory, "rootfs.ext4"), "rootfs", 0o644),
		StatePath:    writeConfigFile(t, filepath.Join(directory, "state.qcow2"), "state", 0o600),
		SerialPath:   writeConfigFile(t, filepath.Join(directory, "serial.log"), "", 0o600),
		ShutdownPath: filepath.Join(directory, "shutdown.sock"),
		TLS:          tls,
		HostPort:     49152,
	}
}

func writeConfigFile(t *testing.T, path, contents string, mode os.FileMode) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("Chmod(%s) error = %v", path, err)
	}
	return path
}

func writeConfigDirectory(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
	return path
}
