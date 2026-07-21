package main

import (
	"archive/tar"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
	runtimepkg "github.com/labring/sealbuild/internal/runtime"
	"github.com/labring/sealbuild/scripts/runtime/artifact"
)

const (
	guestArchiveName  = "sealbuild-guest-runtime.tar.zst"
	guestArchiveLimit = 89128960
)

type guestPackageConfig struct {
	OutputDirectory string
	LockPath        string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("packageguest", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var config guestPackageConfig
	flags.StringVar(&config.OutputDirectory, "output-dir", "", "Guest build output directory")
	flags.StringVar(&config.LockPath, "lock", "", "Guest Runtime Lock path")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse packageguest arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional argument: %s", flags.Arg(0))
	}
	if config.OutputDirectory == "" {
		return fmt.Errorf("--output-dir is required")
	}
	if config.LockPath == "" {
		return fmt.Errorf("--lock is required")
	}
	_, err := packageGuest(config)
	return err
}

func packageGuest(config guestPackageConfig) (result artifact.BuildResult, returnErr error) {
	lockFile, err := os.Open(config.LockPath)
	if err != nil {
		return artifact.BuildResult{}, fmt.Errorf("open Guest Runtime Lock: %w", err)
	}
	lock, err := runtimepkg.LoadLock(lockFile)
	closeErr := lockFile.Close()
	if err != nil {
		return artifact.BuildResult{}, errors.Join(err, closeErr)
	}
	if closeErr != nil {
		return artifact.BuildResult{}, fmt.Errorf("close Guest Runtime Lock: %w", closeErr)
	}

	payloadDirectory := filepath.Join(config.OutputDirectory, ".artifact.tmp")
	artifactDirectory := filepath.Join(config.OutputDirectory, "artifact")
	archivePath := filepath.Join(config.OutputDirectory, guestArchiveName)
	for _, outputPath := range []string{payloadDirectory, artifactDirectory, archivePath} {
		if _, err := os.Lstat(outputPath); err == nil {
			return artifact.BuildResult{}, fmt.Errorf("Guest Runtime output already exists: %s", outputPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return artifact.BuildResult{}, fmt.Errorf("inspect Guest Runtime output %s: %w", outputPath, err)
		}
	}
	if err := os.Mkdir(payloadDirectory, 0o700); err != nil {
		return artifact.BuildResult{}, fmt.Errorf("create Guest Runtime payload: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, os.RemoveAll(payloadDirectory))
	}()

	inputs := []struct {
		source      string
		destination string
		mode        os.FileMode
	}{
		{filepath.Join(config.OutputDirectory, "buildroot", "images", "bzImage"), "bzImage", 0o644},
		{filepath.Join(config.OutputDirectory, "buildroot", "images", "rootfs.ext2"), "rootfs.ext4", 0o644},
		{filepath.Join(config.OutputDirectory, "buildkit-state.qcow2"), "buildkit-state.qcow2", 0o600},
		{config.LockPath, "manifest.lock.json", 0o644},
	}
	for _, input := range inputs {
		if err := copyGuestFile(input.source, filepath.Join(payloadDirectory, input.destination), input.mode); err != nil {
			return artifact.BuildResult{}, fmt.Errorf("copy Guest Runtime file %s: %w", input.destination, err)
		}
	}

	licenseCount, err := copyGuestTree(
		filepath.Join(config.OutputDirectory, "guest-licenses"),
		filepath.Join(payloadDirectory, "licenses"),
	)
	if err != nil {
		return artifact.BuildResult{}, fmt.Errorf("copy Guest Runtime licenses: %w", err)
	}
	if licenseCount == 0 {
		return artifact.BuildResult{}, fmt.Errorf("Guest Runtime licenses must not be empty")
	}

	components := make([]runtimepkg.Component, len(lock.Components))
	copy(components, lock.Components)
	result, err = artifact.Build(artifact.BuildConfig{
		PayloadDir: payloadDirectory,
		OutputPath: archivePath,
		Manifest: runtimepkg.ArtifactManifest{
			SchemaVersion: 1,
			Kind:          runtimepkg.ArtifactKindGuest,
			Platform:      lock.GuestPlatform,
			Components:    components,
		},
	})
	if err != nil {
		return artifact.BuildResult{}, err
	}
	removeArchive := true
	defer func() {
		if removeArchive {
			returnErr = errors.Join(returnErr, os.Remove(archivePath))
		}
	}()
	if result.ArchiveSize > guestArchiveLimit {
		return artifact.BuildResult{}, fmt.Errorf("compressed Guest Runtime is %d bytes, limit is %d bytes", result.ArchiveSize, guestArchiveLimit)
	}
	if err := extractArtifactMetadata(archivePath, payloadDirectory); err != nil {
		return artifact.BuildResult{}, err
	}
	if err := os.Rename(payloadDirectory, artifactDirectory); err != nil {
		return artifact.BuildResult{}, fmt.Errorf("publish Guest Runtime payload: %w", err)
	}
	removeArchive = false
	return result, nil
}

func extractArtifactMetadata(archivePath, payloadDirectory string) (returnErr error) {
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open Guest Runtime archive metadata: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, archiveFile.Close()) }()
	decoder, err := zstd.NewReader(archiveFile)
	if err != nil {
		return fmt.Errorf("open Guest Runtime zstd stream: %w", err)
	}
	defer decoder.Close()

	found := make(map[string]struct{}, 2)
	tarReader := tar.NewReader(decoder)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read Guest Runtime archive metadata: %w", err)
		}
		if header.Name != "manifest.json" && header.Name != "checksums.txt" {
			continue
		}
		if _, exists := found[header.Name]; exists {
			return fmt.Errorf("Guest Runtime archive metadata %s is duplicated", header.Name)
		}
		found[header.Name] = struct{}{}
		destinationPath := filepath.Join(payloadDirectory, header.Name)
		destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("create Guest Runtime metadata %s: %w", header.Name, err)
		}
		if _, err := io.CopyN(destination, tarReader, header.Size); err != nil {
			_ = destination.Close()
			return fmt.Errorf("copy Guest Runtime metadata %s: %w", header.Name, err)
		}
		if err := destination.Sync(); err != nil {
			_ = destination.Close()
			return fmt.Errorf("sync Guest Runtime metadata %s: %w", header.Name, err)
		}
		if err := destination.Close(); err != nil {
			return fmt.Errorf("close Guest Runtime metadata %s: %w", header.Name, err)
		}
	}
	for _, name := range []string{"manifest.json", "checksums.txt"} {
		if _, exists := found[name]; !exists {
			return fmt.Errorf("Guest Runtime archive metadata %s is missing", name)
		}
	}
	return nil
}

func copyGuestTree(sourceDirectory, destinationDirectory string) (int, error) {
	entries, err := os.ReadDir(sourceDirectory)
	if err != nil {
		return 0, fmt.Errorf("read source directory: %w", err)
	}
	if err := os.Mkdir(destinationDirectory, 0o755); err != nil {
		return 0, fmt.Errorf("create destination directory: %w", err)
	}

	fileCount := 0
	for _, entry := range entries {
		sourcePath := filepath.Join(sourceDirectory, entry.Name())
		destinationPath := filepath.Join(destinationDirectory, entry.Name())
		info, err := os.Lstat(sourcePath)
		if err != nil {
			return 0, fmt.Errorf("inspect %s: %w", sourcePath, err)
		}
		switch {
		case info.IsDir():
			childCount, err := copyGuestTree(sourcePath, destinationPath)
			if err != nil {
				return 0, err
			}
			fileCount += childCount
		case info.Mode().IsRegular():
			if err := copyGuestFile(sourcePath, destinationPath, 0o644); err != nil {
				return 0, err
			}
			fileCount++
		default:
			return 0, fmt.Errorf("source entry %s must be a regular file or directory", sourcePath)
		}
	}
	return fileCount, nil
}

func copyGuestFile(sourcePath, destinationPath string, mode os.FileMode) (returnErr error) {
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return fmt.Errorf("inspect source file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source entry %s must be a regular file or directory", sourcePath)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, source.Close()) }()
	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, destination.Close()) }()
	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("copy file: %w", err)
	}
	if err := destination.Chmod(mode); err != nil {
		return fmt.Errorf("set destination permissions: %w", err)
	}
	if err := destination.Sync(); err != nil {
		return fmt.Errorf("sync destination file: %w", err)
	}
	return nil
}
