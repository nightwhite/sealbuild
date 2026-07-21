package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
	"github.com/labring/sealbuild/scripts/runtime/artifact"
)

type windowsPackageConfig struct {
	QEMUPath          string
	DLLDirectories    []string
	QEMUDataDirectory string
	LicenseDirectory  string
	LockPath          string
	OutputPath        string
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }
func (values *stringList) Set(value string) error {
	if value == "" {
		return fmt.Errorf("value must not be empty")
	}
	*values = append(*values, value)
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("packagewindowshost", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var config windowsPackageConfig
	var dllDirectories stringList
	flags.StringVar(&config.QEMUPath, "qemu", "", "QEMU executable path")
	flags.Var(&dllDirectories, "dll-dir", "DLL search directory; repeatable")
	flags.StringVar(&config.QEMUDataDirectory, "qemu-data-dir", "", "QEMU firmware directory")
	flags.StringVar(&config.LicenseDirectory, "license-dir", "", "collected license directory")
	flags.StringVar(&config.LockPath, "lock", "", "Windows Host Build Lock path")
	flags.StringVar(&config.OutputPath, "output", "", "Host Runtime artifact output path")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse packagewindowshost arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional argument: %s", flags.Arg(0))
	}
	config.DLLDirectories = dllDirectories
	for _, required := range []struct{ name, value string }{
		{"--qemu", config.QEMUPath},
		{"--qemu-data-dir", config.QEMUDataDirectory},
		{"--license-dir", config.LicenseDirectory},
		{"--lock", config.LockPath},
		{"--output", config.OutputPath},
	} {
		if required.value == "" {
			return fmt.Errorf("%s is required", required.name)
		}
	}
	if len(config.DLLDirectories) == 0 {
		return fmt.Errorf("--dll-dir is required")
	}
	_, err := packageWindowsHost(config, ResolvePEClosure)
	return err
}

func packageWindowsHost(config windowsPackageConfig, resolve func(string, []string) ([]string, error)) (result artifact.BuildResult, returnErr error) {
	lockFile, err := os.Open(config.LockPath)
	if err != nil {
		return artifact.BuildResult{}, fmt.Errorf("open Windows Host Build Lock: %w", err)
	}
	lock, loadErr := loadWindowsBuildLock(lockFile)
	if err := errors.Join(loadErr, lockFile.Close()); err != nil {
		return artifact.BuildResult{}, err
	}
	closure, err := resolve(config.QEMUPath, config.DLLDirectories)
	if err != nil {
		return artifact.BuildResult{}, fmt.Errorf("resolve QEMU PE dependency closure: %w", err)
	}
	if len(closure) == 0 {
		return artifact.BuildResult{}, fmt.Errorf("QEMU PE dependency closure is empty")
	}

	payload, err := os.MkdirTemp("", "sealbuild-windows-host-payload-*")
	if err != nil {
		return artifact.BuildResult{}, fmt.Errorf("create Windows Host payload: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, os.RemoveAll(payload)) }()
	for index, source := range closure {
		name := filepath.Base(source)
		if index == 0 {
			name = "qemu-system-x86_64.exe"
		}
		mode := os.FileMode(0o644)
		if strings.EqualFold(filepath.Ext(name), ".exe") {
			mode = 0o755
		}
		if err := copyRegularFile(source, filepath.Join(payload, "bin", name), mode); err != nil {
			return artifact.BuildResult{}, fmt.Errorf("copy PE payload %s: %w", name, err)
		}
	}
	for _, name := range lock.FirmwareFiles {
		if err := copyRegularFile(
			filepath.Join(config.QEMUDataDirectory, filepath.FromSlash(name)),
			filepath.Join(payload, "share", "qemu", filepath.FromSlash(name)),
			0o644,
		); err != nil {
			return artifact.BuildResult{}, fmt.Errorf("copy QEMU firmware %s: %w", name, err)
		}
	}
	if err := copyDirectory(config.LicenseDirectory, filepath.Join(payload, "licenses")); err != nil {
		return artifact.BuildResult{}, fmt.Errorf("copy Windows Host licenses: %w", err)
	}
	return artifact.Build(artifact.BuildConfig{
		PayloadDir: payload,
		OutputPath: config.OutputPath,
		Manifest: runtimepkg.ArtifactManifest{
			SchemaVersion: 1,
			Kind:          runtimepkg.ArtifactKindHost,
			Platform:      lock.HostPlatform,
			Components:    lock.Components,
		},
	})
}

func copyDirectory(sourceRoot, destinationRoot string) error {
	return filepath.WalkDir(sourceRoot, func(source string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(sourceRoot, source)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("license path %s must not be a symbolic link", source)
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(destinationRoot, relative), 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("license path %s must be a regular file or directory", source)
		}
		return copyRegularFile(source, filepath.Join(destinationRoot, relative), 0o644)
	})
}

func copyRegularFile(source, destination string, mode os.FileMode) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source must be a regular file")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	return errors.Join(copyErr, output.Sync(), output.Close())
}
