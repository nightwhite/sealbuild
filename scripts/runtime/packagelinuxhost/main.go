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

type linuxPackageConfig struct {
	QEMUPath                   string
	LibraryDirectories         []string
	QEMUDataDirectory          string
	LicenseDirectory           string
	LockPath                   string
	RuntimePackageEvidencePath string
	OutputPath                 string
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
	flags := flag.NewFlagSet("packagelinuxhost", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var config linuxPackageConfig
	var libraryDirectories stringList
	flags.StringVar(&config.QEMUPath, "qemu", "", "QEMU executable path")
	flags.Var(&libraryDirectories, "library-dir", "ELF library search directory; repeatable")
	flags.StringVar(&config.QEMUDataDirectory, "qemu-data-dir", "", "QEMU firmware directory")
	flags.StringVar(&config.LicenseDirectory, "license-dir", "", "collected license directory")
	flags.StringVar(&config.LockPath, "lock", "", "Linux Host Build Lock path")
	flags.StringVar(&config.RuntimePackageEvidencePath, "runtime-package-evidence", "", "dpkg runtime package evidence path")
	flags.StringVar(&config.OutputPath, "output", "", "Host Runtime artifact output path")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse packagelinuxhost arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional argument: %s", flags.Arg(0))
	}
	config.LibraryDirectories = libraryDirectories
	for _, required := range []struct{ name, value string }{
		{"--qemu", config.QEMUPath},
		{"--qemu-data-dir", config.QEMUDataDirectory},
		{"--license-dir", config.LicenseDirectory},
		{"--lock", config.LockPath},
		{"--runtime-package-evidence", config.RuntimePackageEvidencePath},
		{"--output", config.OutputPath},
	} {
		if required.value == "" {
			return fmt.Errorf("%s is required", required.name)
		}
	}
	if len(config.LibraryDirectories) == 0 {
		return fmt.Errorf("--library-dir is required")
	}
	_, err := packageLinuxHost(config, ResolveELFClosure)
	return err
}

func packageLinuxHost(config linuxPackageConfig, resolve func(string, []string) (ELFClosure, error)) (result artifact.BuildResult, returnErr error) {
	lockFile, err := os.Open(config.LockPath)
	if err != nil {
		return artifact.BuildResult{}, fmt.Errorf("open Linux Host Build Lock: %w", err)
	}
	lock, loadErr := loadLinuxBuildLock(lockFile)
	if err := errors.Join(loadErr, lockFile.Close()); err != nil {
		return artifact.BuildResult{}, err
	}
	evidenceFile, err := os.Open(config.RuntimePackageEvidencePath)
	if err != nil {
		return artifact.BuildResult{}, fmt.Errorf("open Linux runtime package evidence: %w", err)
	}
	actualPackages, loadEvidenceErr := loadLinuxRuntimePackageEvidence(evidenceFile)
	if err := errors.Join(loadEvidenceErr, evidenceFile.Close()); err != nil {
		return artifact.BuildResult{}, err
	}
	if err := validateLinuxRuntimePackageEvidence(actualPackages, lock.RuntimePackages); err != nil {
		return artifact.BuildResult{}, err
	}
	closure, err := resolve(config.QEMUPath, config.LibraryDirectories)
	if err != nil {
		return artifact.BuildResult{}, fmt.Errorf("resolve QEMU ELF dependency closure: %w", err)
	}

	payload, err := os.MkdirTemp("", "sealbuild-linux-host-payload-*")
	if err != nil {
		return artifact.BuildResult{}, fmt.Errorf("create Linux Host payload: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, os.RemoveAll(payload)) }()
	if err := copyRegularFile(closure.Executable, filepath.Join(payload, "bin", "qemu-system-x86_64"), 0o755); err != nil {
		return artifact.BuildResult{}, fmt.Errorf("copy Linux QEMU: %w", err)
	}
	if err := copyRegularFile(closure.Loader, filepath.Join(payload, "lib", "ld-linux-x86-64.so.2"), 0o755); err != nil {
		return artifact.BuildResult{}, fmt.Errorf("copy Linux ELF loader: %w", err)
	}
	for _, library := range closure.Libraries {
		if library.Name == "ld-linux-x86-64.so.2" {
			if library.SourcePath != closure.Loader {
				return artifact.BuildResult{}, fmt.Errorf("ELF loader dependency resolves to %s, expected %s", library.SourcePath, closure.Loader)
			}
			continue
		}
		if filepath.Base(library.Name) != library.Name {
			return artifact.BuildResult{}, fmt.Errorf("ELF library name is reserved or unsafe: %s", library.Name)
		}
		if err := copyRegularFile(library.SourcePath, filepath.Join(payload, "lib", library.Name), 0o644); err != nil {
			return artifact.BuildResult{}, fmt.Errorf("copy ELF library %s: %w", library.Name, err)
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
		return artifact.BuildResult{}, fmt.Errorf("copy Linux Host licenses: %w", err)
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

func copyRegularFile(source, destination string, mode os.FileMode) (returnErr error) {
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
	defer func() { returnErr = errors.Join(returnErr, input.Close()) }()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	return errors.Join(copyErr, output.Sync(), output.Close())
}
