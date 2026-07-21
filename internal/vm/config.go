// Package vm manages the local QEMU runtime lifecycle.
package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/labring/sealbuild/internal/platformfs"
	"github.com/labring/sealbuild/internal/tlsmaterial"
)

// Config contains the fixed QEMU runtime inputs for one VM process.
type Config struct {
	QEMUPath     string
	FirmwarePath string
	KernelPath   string
	RootFSPath   string
	StatePath    string
	SerialPath   string
	ShutdownPath string
	ShutdownPort uint16
	TLS          tlsmaterial.Paths
	ProxyFile    string
	HostPort     uint16
}

// Validate verifies every QEMU input without starting a process.
func (config Config) Validate() error {
	if config.HostPort == 0 {
		return fmt.Errorf("host port must not be zero")
	}
	if err := validateShutdownReady(config); err != nil {
		return err
	}
	return config.validateInputs()
}

func (config Config) validateInputs() error {
	if err := validateDirectory("firmware", config.FirmwarePath); err != nil {
		return err
	}
	for _, input := range []struct {
		label string
		path  string
	}{
		{label: "QEMU", path: config.QEMUPath},
		{label: "kernel", path: config.KernelPath},
		{label: "rootfs", path: config.RootFSPath},
		{label: "state", path: config.StatePath},
		{label: "serial", path: config.SerialPath},
	} {
		if err := validateRegularFile(input.label, input.path); err != nil {
			return err
		}
	}
	if err := validateShutdownInput(config); err != nil {
		return err
	}
	if err := tlsmaterial.Validate(config.TLS, time.Now()); err != nil {
		return fmt.Errorf("validate TLS material: %w", err)
	}
	if config.ProxyFile == "" {
		return nil
	}
	if err := validateRegularFile("proxy", config.ProxyFile); err != nil {
		return err
	}
	info, err := os.Lstat(config.ProxyFile)
	if err != nil {
		return fmt.Errorf("inspect proxy file: %w", err)
	}
	if err := platformfs.ValidatePrivateFile(info); err != nil {
		return fmt.Errorf("proxy file mode: %w", err)
	}
	return nil
}

// Args returns a new fixed TCG-only QEMU argument slice.
func (config Config) Args() ([]string, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	shutdown, err := shutdownChardev(config)
	if err != nil {
		return nil, err
	}
	args := []string{
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
		"-drive", "file=" + escapeQEMUOptionValue(config.RootFSPath) + ",format=raw,if=virtio,readonly=on",
		"-drive", "file=" + escapeQEMUOptionValue(config.StatePath) + ",format=qcow2,if=virtio",
		"-netdev", "user,id=net0,hostfwd=tcp:127.0.0.1:" + strconv.FormatUint(uint64(config.HostPort), 10) + "-:1234",
		"-device", "virtio-net-pci,netdev=net0",
		"-chardev", shutdown,
		"-device", "virtio-serial-pci",
		"-device", "virtserialport,chardev=shutdown,name=org.sealbuild.shutdown",
		"-fw_cfg", "name=opt/sealbuild/tls/ca.crt,file=" + escapeQEMUOptionValue(config.TLS.CA),
		"-fw_cfg", "name=opt/sealbuild/tls/server.crt,file=" + escapeQEMUOptionValue(config.TLS.ServerCert),
		"-fw_cfg", "name=opt/sealbuild/tls/server.key,file=" + escapeQEMUOptionValue(config.TLS.ServerKey),
		"-serial", "file:" + config.SerialPath,
	}
	if config.ProxyFile != "" {
		args = append(args, "-fw_cfg", "name=opt/sealbuild/proxy/url,file="+escapeQEMUOptionValue(config.ProxyFile))
	}
	return args, nil
}

func escapeQEMUOptionValue(value string) string {
	return strings.ReplaceAll(value, ",", ",,")
}

func validateDirectory(label, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s path must be absolute", label)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s directory: %w", label, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s path must be a directory", label)
	}
	return nil
}

func validateRegularFile(label, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s path must be absolute", label)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s file: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s path must be a regular file", label)
	}
	return nil
}
