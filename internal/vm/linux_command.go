package vm

import (
	"fmt"
	"path/filepath"
)

func linuxQEMUCommand(qemuPath string, qemuArguments []string) (string, []string, error) {
	binDirectory := filepath.Dir(qemuPath)
	if filepath.Base(qemuPath) != "qemu-system-x86_64" || filepath.Base(binDirectory) != "bin" {
		return "", nil, fmt.Errorf("Linux QEMU path must end with bin/qemu-system-x86_64")
	}
	hostRoot := filepath.Dir(binDirectory)
	libraryPath := filepath.Join(hostRoot, "lib")
	if err := validateDirectory("Linux QEMU library", libraryPath); err != nil {
		return "", nil, err
	}
	loaderPath := filepath.Join(libraryPath, "ld-linux-x86-64.so.2")
	if err := validateRegularFile("Linux QEMU loader", loaderPath); err != nil {
		return "", nil, err
	}
	launchArguments := []string{
		"--inhibit-cache",
		"--library-path", libraryPath,
		qemuPath,
	}
	launchArguments = append(launchArguments, qemuArguments...)
	return loaderPath, launchArguments, nil
}
