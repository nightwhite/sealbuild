package runtime

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/labring/sealbuild/internal/platformfs"
)

const maximumArtifactMetadataSize = 16 * 1024 * 1024

// ExtractResult records one verified extracted Runtime artifact.
type ExtractResult struct {
	Manifest ArtifactManifest
	SHA256   string
	Size     int64
}

// ExtractAsset verifies and extracts one immutable Runtime artifact.
func ExtractAsset(ctx context.Context, asset Asset, kind ArtifactKind, destination string) (result ExtractResult, returnErr error) {
	if err := validateAsset(string(kind), asset); err != nil {
		return ExtractResult{}, err
	}
	if err := ensureEmptyDirectory(destination); err != nil {
		return ExtractResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ExtractResult{}, fmt.Errorf("extract Runtime asset: %w", err)
	}

	temporaryArchive, err := os.CreateTemp(filepath.Dir(destination), ".sealbuild-runtime-*.tar.zst.tmp")
	if err != nil {
		return ExtractResult{}, fmt.Errorf("create temporary Runtime archive: %w", err)
	}
	temporaryArchivePath := temporaryArchive.Name()
	defer func() {
		returnErr = errors.Join(returnErr, os.Remove(temporaryArchivePath))
	}()
	source, err := asset.Open()
	if err != nil {
		_ = temporaryArchive.Close()
		return ExtractResult{}, fmt.Errorf("open Runtime asset %s: %w", asset.Name, err)
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(temporaryArchive, hash), contextReader{ctx: ctx, reader: source})
	closeErr := errors.Join(source.Close(), temporaryArchive.Sync(), temporaryArchive.Close())
	if copyErr != nil {
		return ExtractResult{}, errors.Join(fmt.Errorf("copy Runtime asset: %w", copyErr), closeErr)
	}
	if closeErr != nil {
		return ExtractResult{}, fmt.Errorf("close temporary Runtime archive: %w", closeErr)
	}
	if size != asset.Size {
		return ExtractResult{}, fmt.Errorf("Runtime asset size is %d, expected %d", size, asset.Size)
	}
	actualSHA256 := fmt.Sprintf("%x", hash.Sum(nil))
	if actualSHA256 != asset.SHA256 {
		return ExtractResult{}, fmt.Errorf("Runtime asset SHA-256 is %s, expected %s", actualSHA256, asset.SHA256)
	}

	archiveFile, err := os.Open(temporaryArchivePath)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("open verified Runtime archive: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, archiveFile.Close())
	}()
	decoder, err := zstd.NewReader(archiveFile)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("open Runtime zstd stream: %w", err)
	}
	defer decoder.Close()

	observed := make(map[string]ArtifactFile)
	entries := make(map[string]struct{})
	directories := map[string]struct{}{destination: {}}
	var manifestBytes []byte
	var checksumsBytes []byte
	tarReader := tar.NewReader(decoder)
	for {
		if err := ctx.Err(); err != nil {
			return ExtractResult{}, fmt.Errorf("extract Runtime archive: %w", err)
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ExtractResult{}, fmt.Errorf("read Runtime tar entry: %w", err)
		}
		if header.Typeflag == tar.TypeXGlobalHeader || header.Typeflag == tar.TypeXHeader {
			continue
		}
		entryName, err := validateArchivePath(destination, header.Name)
		if err != nil {
			return ExtractResult{}, err
		}
		if _, exists := entries[entryName]; exists {
			return ExtractResult{}, fmt.Errorf("archive entry %s is duplicated", entryName)
		}
		entries[entryName] = struct{}{}

		destinationPath := filepath.Join(destination, filepath.FromSlash(entryName))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destinationPath, 0o755); err != nil {
				return ExtractResult{}, fmt.Errorf("create Runtime directory %s: %w", entryName, err)
			}
			directories[destinationPath] = struct{}{}
		case tar.TypeReg, tar.TypeRegA:
			mode := uint32(header.Mode)
			if mode != 0o600 && mode != 0o644 && mode != 0o755 {
				return ExtractResult{}, fmt.Errorf("archive entry %s has unsupported mode %#o", entryName, mode)
			}
			if entryName == "manifest.json" || entryName == "checksums.txt" {
				if mode != 0o644 || header.Size <= 0 || header.Size > maximumArtifactMetadataSize {
					return ExtractResult{}, fmt.Errorf("archive metadata %s has invalid size or mode", entryName)
				}
			}
			if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
				return ExtractResult{}, fmt.Errorf("create Runtime file parent %s: %w", entryName, err)
			}
			directories[filepath.Dir(destinationPath)] = struct{}{}
			file, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(mode))
			if err != nil {
				return ExtractResult{}, fmt.Errorf("create Runtime file %s: %w", entryName, err)
			}
			fileHash := sha256.New()
			var metadata bytes.Buffer
			writers := []io.Writer{file, fileHash}
			if entryName == "manifest.json" || entryName == "checksums.txt" {
				writers = append(writers, &metadata)
			}
			_, copyErr := io.CopyN(io.MultiWriter(writers...), contextReader{ctx: ctx, reader: tarReader}, header.Size)
			fileCloseErr := errors.Join(file.Sync(), file.Close())
			if copyErr != nil {
				return ExtractResult{}, errors.Join(fmt.Errorf("copy Runtime file %s: %w", entryName, copyErr), fileCloseErr)
			}
			if fileCloseErr != nil {
				return ExtractResult{}, fmt.Errorf("close Runtime file %s: %w", entryName, fileCloseErr)
			}
			if entryName == "manifest.json" {
				manifestBytes = metadata.Bytes()
			} else if entryName == "checksums.txt" {
				checksumsBytes = metadata.Bytes()
			} else {
				observed[entryName] = ArtifactFile{
					Path: entryName, SHA256: fmt.Sprintf("%x", fileHash.Sum(nil)), Size: header.Size, Mode: mode,
				}
			}
		default:
			return ExtractResult{}, fmt.Errorf("unsupported archive entry type %d for %s", header.Typeflag, entryName)
		}
	}

	if len(manifestBytes) == 0 {
		return ExtractResult{}, fmt.Errorf("Runtime archive manifest.json is missing")
	}
	if len(checksumsBytes) == 0 {
		return ExtractResult{}, fmt.Errorf("Runtime archive checksums.txt is missing")
	}
	manifest, err := LoadArtifactManifest(bytes.NewReader(manifestBytes))
	if err != nil {
		return ExtractResult{}, err
	}
	if manifest.Kind != kind {
		return ExtractResult{}, fmt.Errorf("Runtime artifact kind is %s, expected %s", manifest.Kind, kind)
	}
	if err := compareObservedFiles(manifest.Files, observed); err != nil {
		return ExtractResult{}, err
	}
	manifestHash := sha256.Sum256(manifestBytes)
	if want := expectedChecksums(manifest.Files, fmt.Sprintf("%x", manifestHash)); !bytes.Equal(checksumsBytes, want) {
		return ExtractResult{}, fmt.Errorf("Runtime archive checksums.txt does not match payload and manifest")
	}
	if err := syncDirectories(directories); err != nil {
		return ExtractResult{}, err
	}

	return ExtractResult{Manifest: manifest, SHA256: actualSHA256, Size: size}, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func ensureEmptyDirectory(directory string) error {
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("inspect Runtime destination: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("Runtime destination must be a directory")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read Runtime destination: %w", err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("Runtime destination must be empty")
	}
	return nil
}

func validateArchivePath(root, name string) (string, error) {
	cleaned := strings.TrimSuffix(name, "/")
	if cleaned == "" || cleaned == "." || path.IsAbs(cleaned) || strings.Contains(cleaned, `\`) || path.Clean(cleaned) != cleaned || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	candidate := filepath.Join(root, filepath.FromSlash(cleaned))
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return cleaned, nil
}

func compareObservedFiles(expected []ArtifactFile, observed map[string]ArtifactFile) error {
	if len(expected) != len(observed) {
		return fmt.Errorf("Runtime payload file count is %d, expected %d", len(observed), len(expected))
	}
	for _, expectedFile := range expected {
		observedFile, exists := observed[expectedFile.Path]
		if !exists {
			return fmt.Errorf("Runtime payload file %s is missing", expectedFile.Path)
		}
		if observedFile != expectedFile {
			return fmt.Errorf("Runtime payload file %s metadata does not match manifest", expectedFile.Path)
		}
	}
	return nil
}

func expectedChecksums(files []ArtifactFile, manifestSHA256 string) []byte {
	var builder strings.Builder
	for _, file := range files {
		fmt.Fprintf(&builder, "%s  %s\n", file.SHA256, file.Path)
	}
	fmt.Fprintf(&builder, "%s  manifest.json\n", manifestSHA256)
	return []byte(builder.String())
}

func syncDirectories(directories map[string]struct{}) error {
	paths := make([]string, 0, len(directories))
	for directory := range directories {
		paths = append(paths, directory)
	}
	sort.Slice(paths, func(first, second int) bool {
		return len(paths[first]) > len(paths[second])
	})
	for _, directoryPath := range paths {
		if err := platformfs.SyncDirectory(directoryPath); err != nil {
			return fmt.Errorf("sync Runtime directory %s: %w", directoryPath, err)
		}
	}
	return nil
}
