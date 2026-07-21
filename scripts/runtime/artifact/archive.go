// Package artifact builds verified Runtime artifact archives.
package artifact

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/labring/sealbuild/internal/platformfs"
	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

// BuildConfig identifies the payload and metadata used to create an artifact.
type BuildConfig struct {
	PayloadDir string
	OutputPath string
	Manifest   runtimepkg.ArtifactManifest
}

// BuildResult records the verified artifact metadata.
type BuildResult struct {
	Manifest      runtimepkg.ArtifactManifest
	ArchiveSHA256 string
	ArchiveSize   int64
}

// ScanPayload returns the verified regular files below root in manifest order.
func ScanPayload(root string) ([]runtimepkg.ArtifactFile, error) {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect payload root: %w", err)
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("payload root must be a directory")
	}

	var files []runtimepkg.ArtifactFile
	if err := scanDirectory(root, "", &files); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("payload must contain at least one file")
	}
	sort.Slice(files, func(first, second int) bool {
		return files[first].Path < files[second].Path
	})
	return files, nil
}

// HashFile returns the SHA-256 and logical size of one regular file.
func HashFile(filePath string) (string, int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", 0, fmt.Errorf("open file for hashing: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, fmt.Errorf("hash file: %w", err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), size, nil
}

// Build creates one deterministic verified Runtime artifact archive.
func Build(config BuildConfig) (BuildResult, error) {
	if config.OutputPath == "" {
		return BuildResult{}, fmt.Errorf("artifact output path is required")
	}
	if _, err := os.Lstat(config.OutputPath); err == nil {
		return BuildResult{}, fmt.Errorf("artifact output already exists: %s", config.OutputPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return BuildResult{}, fmt.Errorf("inspect artifact output: %w", err)
	}

	files, err := ScanPayload(config.PayloadDir)
	if err != nil {
		return BuildResult{}, err
	}
	manifest := config.Manifest
	manifest.Files = files
	if err := manifest.Validate(); err != nil {
		return BuildResult{}, fmt.Errorf("validate artifact manifest: %w", err)
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return BuildResult{}, fmt.Errorf("encode artifact manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	manifestHash := sha256.Sum256(manifestBytes)
	checksumsBytes := buildChecksums(files, fmt.Sprintf("%x", manifestHash))

	outputDirectory := filepath.Dir(config.OutputPath)
	temporaryFile, err := os.CreateTemp(outputDirectory, ".sealbuild-artifact-*.tmp")
	if err != nil {
		return BuildResult{}, fmt.Errorf("create temporary artifact: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporaryFile.Chmod(0o600); err != nil {
		_ = temporaryFile.Close()
		return BuildResult{}, fmt.Errorf("set temporary artifact permissions: %w", err)
	}
	if err := writeArchive(temporaryFile, config.PayloadDir, files, manifestBytes, checksumsBytes); err != nil {
		_ = temporaryFile.Close()
		return BuildResult{}, err
	}
	if err := temporaryFile.Sync(); err != nil {
		_ = temporaryFile.Close()
		return BuildResult{}, fmt.Errorf("sync temporary artifact: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		return BuildResult{}, fmt.Errorf("close temporary artifact: %w", err)
	}

	archiveSHA256, archiveSize, err := HashFile(temporaryPath)
	if err != nil {
		return BuildResult{}, fmt.Errorf("verify temporary artifact: %w", err)
	}
	if err := platformfs.PublishFileNoReplace(temporaryPath, config.OutputPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return BuildResult{}, fmt.Errorf("artifact output already exists: %s", config.OutputPath)
		}
		return BuildResult{}, fmt.Errorf("publish artifact: %w", err)
	}
	removeTemporary = false
	if err := syncDirectory(outputDirectory); err != nil {
		_ = os.Remove(config.OutputPath)
		return BuildResult{}, err
	}

	return BuildResult{
		Manifest:      manifest,
		ArchiveSHA256: archiveSHA256,
		ArchiveSize:   archiveSize,
	}, nil
}

func writeArchive(
	output io.Writer,
	payloadDirectory string,
	files []runtimepkg.ArtifactFile,
	manifestBytes []byte,
	checksumsBytes []byte,
) error {
	encoder, err := zstd.NewWriter(
		output,
		zstd.WithEncoderLevel(zstd.SpeedBestCompression),
		zstd.WithEncoderConcurrency(1),
	)
	if err != nil {
		return fmt.Errorf("create zstd encoder: %w", err)
	}
	tarWriter := tar.NewWriter(encoder)

	for _, file := range files {
		filePath := filepath.Join(payloadDirectory, filepath.FromSlash(file.Path))
		if err := writeFileEntry(tarWriter, file.Path, filePath, file.Size, file.Mode); err != nil {
			return errors.Join(err, tarWriter.Close(), encoder.Close())
		}
	}
	if err := writeBytesEntry(tarWriter, "manifest.json", manifestBytes, 0o644); err != nil {
		return errors.Join(err, tarWriter.Close(), encoder.Close())
	}
	if err := writeBytesEntry(tarWriter, "checksums.txt", checksumsBytes, 0o644); err != nil {
		return errors.Join(err, tarWriter.Close(), encoder.Close())
	}
	if err := tarWriter.Close(); err != nil {
		return errors.Join(fmt.Errorf("close tar archive: %w", err), encoder.Close())
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("close zstd archive: %w", err)
	}
	return nil
}

func writeFileEntry(writer *tar.Writer, name, filePath string, size int64, mode uint32) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open payload file %s: %w", name, err)
	}
	defer file.Close()

	if err := writer.WriteHeader(deterministicHeader(name, size, mode)); err != nil {
		return fmt.Errorf("write tar header %s: %w", name, err)
	}
	if _, err := io.CopyN(writer, file, size); err != nil {
		return fmt.Errorf("write tar payload %s: %w", name, err)
	}
	return nil
}

func writeBytesEntry(writer *tar.Writer, name string, contents []byte, mode uint32) error {
	if err := writer.WriteHeader(deterministicHeader(name, int64(len(contents)), mode)); err != nil {
		return fmt.Errorf("write tar header %s: %w", name, err)
	}
	if _, err := writer.Write(contents); err != nil {
		return fmt.Errorf("write tar payload %s: %w", name, err)
	}
	return nil
}

func deterministicHeader(name string, size int64, mode uint32) *tar.Header {
	return &tar.Header{
		Name:       name,
		Mode:       int64(mode),
		Uid:        0,
		Gid:        0,
		Size:       size,
		ModTime:    time.Unix(0, 0).UTC(),
		AccessTime: time.Time{},
		ChangeTime: time.Time{},
		Typeflag:   tar.TypeReg,
		Uname:      "root",
		Gname:      "root",
		Format:     tar.FormatUSTAR,
	}
}

func buildChecksums(files []runtimepkg.ArtifactFile, manifestSHA256 string) []byte {
	var builder strings.Builder
	for _, file := range files {
		fmt.Fprintf(&builder, "%s  %s\n", file.SHA256, file.Path)
	}
	fmt.Fprintf(&builder, "%s  manifest.json\n", manifestSHA256)
	return []byte(builder.String())
}

func syncDirectory(directoryPath string) error {
	if err := platformfs.SyncDirectory(directoryPath); err != nil {
		return fmt.Errorf("sync artifact output directory: %w", err)
	}
	return nil
}

func scanDirectory(root, relativeDirectory string, files *[]runtimepkg.ArtifactFile) error {
	directoryPath := filepath.Join(root, filepath.FromSlash(relativeDirectory))
	entries, err := os.ReadDir(directoryPath)
	if err != nil {
		return fmt.Errorf("read payload directory %s: %w", relativeDirectory, err)
	}

	for _, entry := range entries {
		relativePath := path.Join(relativeDirectory, entry.Name())
		if entry.IsDir() {
			if err := scanDirectory(root, relativePath, files); err != nil {
				return err
			}
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect payload entry %s: %w", relativePath, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("payload entry %s must be a regular file or directory", relativePath)
		}
		if relativePath == "manifest.json" || relativePath == "checksums.txt" {
			return fmt.Errorf("payload path %s is reserved", relativePath)
		}

		mode, err := platformfs.ArtifactMode(relativePath, info)
		if err != nil {
			return fmt.Errorf("resolve payload file %s mode: %w", relativePath, err)
		}
		switch mode {
		case 0o600, 0o644, 0o755:
		default:
			return fmt.Errorf("payload file %s mode must be 0600, 0644, or 0755", relativePath)
		}

		checksum, size, err := HashFile(filepath.Join(root, filepath.FromSlash(relativePath)))
		if err != nil {
			return fmt.Errorf("hash payload file %s: %w", relativePath, err)
		}
		if size <= 0 {
			return fmt.Errorf("payload file %s must not be empty", relativePath)
		}

		*files = append(*files, runtimepkg.ArtifactFile{
			Path:   relativePath,
			SHA256: checksum,
			Size:   size,
			Mode:   mode,
		})
	}
	return nil
}
