package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

var qcow2Magic = []byte{'Q', 'F', 'I', 0xfb}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("verify-runtime", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var hostPath string
	var guestPath string
	flags.StringVar(&hostPath, "host", "", "Host Runtime archive")
	flags.StringVar(&guestPath, "guest", "", "Guest Runtime archive")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse verify-runtime arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional argument: %s", flags.Arg(0))
	}
	if hostPath == "" && guestPath == "" {
		return fmt.Errorf("at least one of --host or --guest is required")
	}
	if hostPath != "" {
		if err := verifyArchive(ctx, hostPath, runtimepkg.ArtifactKindHost, output); err != nil {
			return err
		}
	}
	if guestPath != "" {
		if err := verifyArchive(ctx, guestPath, runtimepkg.ArtifactKindGuest, output); err != nil {
			return err
		}
	}
	return nil
}

func verifyArchive(ctx context.Context, archivePath string, kind runtimepkg.ArtifactKind, output io.Writer) (returnErr error) {
	asset, err := fileAsset(archivePath)
	if err != nil {
		return err
	}
	destination, err := os.MkdirTemp("", "sealbuild-verify-runtime-*")
	if err != nil {
		return fmt.Errorf("create Runtime verification directory: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, os.RemoveAll(destination))
	}()

	result, err := runtimepkg.ExtractAsset(ctx, asset, kind, destination)
	if err != nil {
		return fmt.Errorf("verify %s Runtime archive: %w", kind, err)
	}
	if kind == runtimepkg.ArtifactKindGuest {
		if err := verifyGuestManifest(result.Manifest); err != nil {
			return err
		}
		if err := verifyGuestStateDisk(destination); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(output, "%s Runtime: %s/%s sha256=%s size=%d\n",
		kind, result.Manifest.Platform.OS, result.Manifest.Platform.Architecture, result.SHA256, result.Size)
	if err != nil {
		return fmt.Errorf("write Runtime verification result: %w", err)
	}
	return nil
}

func fileAsset(archivePath string) (runtimepkg.Asset, error) {
	archive, err := os.Open(archivePath)
	if err != nil {
		return runtimepkg.Asset{}, fmt.Errorf("open Runtime archive: %w", err)
	}
	info, statErr := archive.Stat()
	hash := sha256.New()
	_, copyErr := io.Copy(hash, archive)
	closeErr := archive.Close()
	if err := errors.Join(statErr, copyErr, closeErr); err != nil {
		return runtimepkg.Asset{}, fmt.Errorf("inspect Runtime archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		return runtimepkg.Asset{}, fmt.Errorf("Runtime archive must be a regular file")
	}
	absolutePath, err := filepath.Abs(archivePath)
	if err != nil {
		return runtimepkg.Asset{}, fmt.Errorf("resolve Runtime archive path: %w", err)
	}
	return runtimepkg.Asset{
		Name:   filepath.Base(absolutePath),
		SHA256: fmt.Sprintf("%x", hash.Sum(nil)),
		Size:   info.Size(),
		Open: func() (io.ReadCloser, error) {
			return os.Open(absolutePath)
		},
	}, nil
}

func verifyGuestManifest(manifest runtimepkg.ArtifactManifest) error {
	required := map[string]bool{
		"bzImage":              false,
		"rootfs.ext4":          false,
		"buildkit-state.qcow2": false,
		"manifest.lock.json":   false,
	}
	for _, file := range manifest.Files {
		if _, exists := required[file.Path]; exists {
			required[file.Path] = true
		}
		cleaned := strings.ToLower(filepath.ToSlash(file.Path))
		base := filepath.Base(cleaned)
		if strings.HasPrefix(cleaned, "tls/") || strings.HasSuffix(base, ".key") || strings.Contains(base, "private-key") {
			return fmt.Errorf("Guest Runtime contains private key path %s", file.Path)
		}
	}
	for path, found := range required {
		if !found {
			return fmt.Errorf("Guest Runtime required file %s is missing", path)
		}
	}
	return nil
}

func verifyGuestStateDisk(destination string) (returnErr error) {
	stateDisk, err := os.Open(filepath.Join(destination, "buildkit-state.qcow2"))
	if err != nil {
		return fmt.Errorf("open Guest qcow2 state disk: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, stateDisk.Close())
	}()
	magic := make([]byte, len(qcow2Magic))
	if _, err := io.ReadFull(stateDisk, magic); err != nil {
		return fmt.Errorf("read Guest qcow2 state disk: %w", err)
	}
	if string(magic) != string(qcow2Magic) {
		return fmt.Errorf("Guest state disk is not qcow2")
	}
	return nil
}
