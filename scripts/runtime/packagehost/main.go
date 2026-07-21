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

var qemuFirmwareFiles = []string{
	"bios-256k.bin",
	"efi-virtio.rom",
	"kvmvapic.bin",
	"linuxboot_dma.bin",
}

type hostPackageConfig struct {
	HostArchitecture           string
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
	var config hostPackageConfig
	flags.StringVar(&config.HostArchitecture, "host-architecture", "", "Darwin Host architecture")
	flags.StringVar(&config.HomebrewRoot, "homebrew-root", "", "Homebrew root")
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
		{"--host-architecture", config.HostArchitecture},
		{"--homebrew-root", config.HomebrewRoot},
	} {
		if required.value == "" {
			return fmt.Errorf("%s is required", required.name)
		}
	}
	if err := validateDarwinTarget(config.HostArchitecture, config.HomebrewRoot); err != nil {
		return err
	}

	_, err := packageHost(ctx, runner, config)
	return err
}

func validateDarwinTarget(architecture, homebrewRoot string) error {
	switch architecture {
	case "arm64":
		if homebrewRoot != "/opt/homebrew" {
			return fmt.Errorf("darwin/arm64 requires Homebrew root /opt/homebrew")
		}
	case "amd64":
		if homebrewRoot != "/usr/local" {
			return fmt.Errorf("darwin/amd64 requires Homebrew root /usr/local")
		}
	default:
		return fmt.Errorf("Darwin Host architecture must be arm64 or amd64")
	}
	return nil
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
	if lock.HostPlatform.Architecture != config.HostArchitecture {
		return artifact.BuildResult{}, fmt.Errorf(
			"Host Build Lock architecture is %s, expected %s",
			lock.HostPlatform.Architecture,
			config.HostArchitecture,
		)
	}
	if err := ValidateMachOArchitecture(ctx, runner, config.QEMUPath, config.HostArchitecture); err != nil {
		return artifact.BuildResult{}, fmt.Errorf("validate QEMU architecture: %w", err)
	}

	resolvedHomebrewRoot, err := filepath.EvalSymlinks(config.HomebrewRoot)
	if err != nil {
		return artifact.BuildResult{}, fmt.Errorf("resolve Homebrew root: %w", err)
	}
	graph, err := ResolveDependencies(ctx, runner, config.QEMUPath, resolvedHomebrewRoot)
	if err != nil {
		return artifact.BuildResult{}, err
	}
	for _, library := range graph.Libraries {
		if err := ValidateMachOArchitecture(ctx, runner, library.SourcePath, config.HostArchitecture); err != nil {
			return artifact.BuildResult{}, fmt.Errorf("validate dependency architecture: %w", err)
		}
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
