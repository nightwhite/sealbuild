package build

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/labring/sealbuild/internal/platformfs"
)

// ArchiveOutput owns one temporary OCI archive until publish or abort.
type ArchiveOutput struct {
	mutex          sync.Mutex
	file           *os.File
	temporaryPath  string
	finalPath      string
	writerAcquired bool
	writerClosed   bool
	writerCloseErr error
	published      bool
}

// NewArchiveOutput creates a private temporary file beside a non-existing target.
func NewArchiveOutput(finalPath string) (*ArchiveOutput, error) {
	if finalPath == "" {
		return nil, fmt.Errorf("OCI output path is required")
	}
	if _, err := os.Lstat(finalPath); err == nil {
		return nil, fmt.Errorf("OCI output already exists: %s", finalPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect OCI output: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(finalPath), ".sealbuild-oci-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create temporary OCI output: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		return nil, errors.Join(fmt.Errorf("set temporary OCI output permissions: %w", err), file.Close(), os.Remove(file.Name()))
	}
	return &ArchiveOutput{file: file, temporaryPath: file.Name(), finalPath: finalPath}, nil
}

// Writer returns the only BuildKit exporter writer for this output.
func (output *ArchiveOutput) Writer(map[string]string) (io.WriteCloser, error) {
	output.mutex.Lock()
	defer output.mutex.Unlock()
	if output.writerAcquired {
		return nil, fmt.Errorf("OCI output writer was already acquired")
	}
	if output.published {
		return nil, fmt.Errorf("OCI output was already published")
	}
	output.writerAcquired = true
	return &archiveWriter{output: output}, nil
}

// Publish verifies and atomically links the completed archive without overwriting.
func (output *ArchiveOutput) Publish() error {
	output.mutex.Lock()
	defer output.mutex.Unlock()
	if output.published {
		return nil
	}
	if !output.writerAcquired || !output.writerClosed {
		return fmt.Errorf("OCI output writer must be closed before publish")
	}
	if output.writerCloseErr != nil {
		return output.writerCloseErr
	}
	if err := VerifyOCIArchive(output.temporaryPath); err != nil {
		return fmt.Errorf("verify OCI output: %w", err)
	}
	if err := platformfs.PublishFileNoReplace(output.temporaryPath, output.finalPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("OCI output already exists: %s", output.finalPath)
		}
		return fmt.Errorf("publish OCI output: %w", err)
	}
	if err := syncOutputDirectory(filepath.Dir(output.finalPath)); err != nil {
		return errors.Join(err, os.Remove(output.finalPath))
	}
	output.published = true
	return nil
}

// Abort closes and removes an unpublished temporary archive.
func (output *ArchiveOutput) Abort() error {
	output.mutex.Lock()
	defer output.mutex.Unlock()
	if output.published {
		return nil
	}
	var closeErr error
	if !output.writerClosed {
		closeErr = errors.Join(output.file.Sync(), output.file.Close())
		output.writerClosed = true
		output.writerCloseErr = closeErr
	}
	removeErr := os.Remove(output.temporaryPath)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(closeErr, removeErr)
}

type archiveWriter struct {
	output *ArchiveOutput
	once   sync.Once
	err    error
}

func (writer *archiveWriter) Write(contents []byte) (int, error) {
	return writer.output.file.Write(contents)
}

func (writer *archiveWriter) Close() error {
	writer.once.Do(func() {
		writer.output.mutex.Lock()
		defer writer.output.mutex.Unlock()
		writer.err = errors.Join(writer.output.file.Sync(), writer.output.file.Close())
		writer.output.writerClosed = true
		writer.output.writerCloseErr = writer.err
	})
	return writer.err
}

func syncOutputDirectory(directory string) error {
	return platformfs.SyncDirectory(directory)
}
