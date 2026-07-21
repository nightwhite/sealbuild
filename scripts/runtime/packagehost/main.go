package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/labring/sealbuild/scripts/runtime/artifact"
)

const darwinARMHomebrewRoot = "/opt/homebrew"

var qemuFirmwareFiles = []string{
	"bios-256k.bin",
	"efi-virtio.rom",
	"kvmvapic.bin",
	"linuxboot_dma.bin",
}

type hostPackageConfig struct {
	QEMUPath                   string
	QEMUDataDirectory          string
	LockPath                   string
	QEMULicenseDirectory       string
	DependencyLicenseDirectory string
	OutputPath                 string
	HomebrewRoot               string
}

func main() {
	if err := run(context.Background(), os.Args[1:], commandRunner{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, runner Runner) error {
	flags := flag.NewFlagSet("packagehost", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	config := hostPackageConfig{HomebrewRoot: darwinARMHomebrewRoot}
	flags.StringVar(&config.QEMUPath, "qemu", "", "QEMU executable path")
	flags.StringVar(&config.QEMUDataDirectory, "qemu-data-dir", "", "QEMU firmware directory")
	flags.StringVar(&config.LockPath, "lock", "", "Host Build Lock path")
	flags.StringVar(&config.QEMULicenseDirectory, "qemu-license-dir", "", "QEMU license source directory")
	flags.StringVar(&config.DependencyLicenseDirectory, "dependency-license-dir", "", "dependency license source directory")
	flags.StringVar(&config.OutputPath, "output", "", "Host Runtime artifact output path")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse packagehost arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional argument: %s", flags.Arg(0))
	}
	for _, required := range []struct {
		name  string
		value string
	}{
		{"--qemu", config.QEMUPath},
		{"--lock", config.LockPath},
		{"--qemu-data-dir", config.QEMUDataDirectory},
		{"--qemu-license-dir", config.QEMULicenseDirectory},
		{"--dependency-license-dir", config.DependencyLicenseDirectory},
		{"--output", config.OutputPath},
	} {
		if required.value == "" {
			return fmt.Errorf("%s is required", required.name)
		}
	}

	_, err := packageHost(ctx, runner, config)
	return err
}

func packageHost(ctx context.Context, runner Runner, config hostPackageConfig) (result artifact.BuildResult, returnErr error) {
	lockFile, err := os.Open(config.LockPath)
	if err != nil {
		return artifact.BuildResult{}, fmt.Errorf("open Host Build Lock: %w", err)
	}
	lock, err := LoadBuildLock(lockFile)
	closeErr := lockFile.Close()
	if err != nil {
		return artifact.BuildResult{}, errors.Join(err, closeErr)
	}
	if closeErr != nil {
		return artifact.BuildResult{}, fmt.Errorf("close Host Build Lock: %w", closeErr)
	}

	resolvedHomebrewRoot, err := filepath.EvalSymlinks(config.HomebrewRoot)
	if err != nil {
		return artifact.BuildResult{}, fmt.Errorf("resolve Homebrew root: %w", err)
	}
	graph, err := ResolveDependencies(ctx, runner, config.QEMUPath, resolvedHomebrewRoot)
	if err != nil {
		return artifact.BuildResult{}, err
	}
	if err := validateDependencyComponents(graph, resolvedHomebrewRoot, lock); err != nil {
		return artifact.BuildResult{}, err
	}

	payloadDirectory, err := os.MkdirTemp("", "sealbuild-host-payload-*")
	if err != nil {
		return artifact.BuildResult{}, fmt.Errorf("create Host Runtime payload directory: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, os.RemoveAll(payloadDirectory))
	}()

	if err := copyLockedLicenses(
		lock,
		config.QEMULicenseDirectory,
		config.DependencyLicenseDirectory,
		payloadDirectory,
	); err != nil {
		return artifact.BuildResult{}, err
	}
	if err := RelocateMachO(ctx, runner, graph, payloadDirectory); err != nil {
		return artifact.BuildResult{}, err
	}
	firmwareDirectory := filepath.Join(payloadDirectory, "share", "qemu")
	if err := os.MkdirAll(firmwareDirectory, 0o755); err != nil {
		return artifact.BuildResult{}, fmt.Errorf("create QEMU firmware directory: %w", err)
	}
	for _, name := range qemuFirmwareFiles {
		if err := copyRegularFile(
			filepath.Join(config.QEMUDataDirectory, name),
			filepath.Join(firmwareDirectory, name),
			0o644,
		); err != nil {
			return artifact.BuildResult{}, fmt.Errorf("copy QEMU firmware %s: %w", name, err)
		}
	}

	return artifact.Build(artifact.BuildConfig{
		PayloadDir: payloadDirectory,
		OutputPath: config.OutputPath,
		Manifest:   buildHostManifest(lock),
	})
}
