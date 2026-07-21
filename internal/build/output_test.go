package build

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveOutputPublishesVerifiedOCIAtomically(t *testing.T) {
	workspace := t.TempDir()
	sourcePath := writeOCIArchive(t, nil)
	finalPath := filepath.Join(workspace, "image.oci.tar")
	output, err := NewArchiveOutput(finalPath)
	if err != nil {
		t.Fatalf("NewArchiveOutput() error = %v", err)
	}
	writer, err := output.Writer(nil)
	if err != nil {
		t.Fatalf("Writer() error = %v", err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatalf("Open(source) error = %v", err)
	}
	if _, err := io.Copy(writer, source); err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("source Close() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer Close() error = %v", err)
	}
	if err := output.Publish(); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err := VerifyOCIArchive(finalPath); err != nil {
		t.Fatalf("VerifyOCIArchive(final) error = %v", err)
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		t.Fatalf("Stat(final) error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("final mode = %#o, want 0600", info.Mode().Perm())
	}
	if err := output.Abort(); err != nil {
		t.Fatalf("Abort(after publish) error = %v", err)
	}
}

func TestArchiveOutputRejectsExistingTargetAndRepeatedWriter(t *testing.T) {
	workspace := t.TempDir()
	existing := filepath.Join(workspace, "existing.tar")
	if err := os.WriteFile(existing, []byte("existing"), 0o600); err != nil {
		t.Fatalf("WriteFile(existing) error = %v", err)
	}
	if _, err := NewArchiveOutput(existing); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("NewArchiveOutput(existing) error = %v", err)
	}

	output, err := NewArchiveOutput(filepath.Join(workspace, "new.tar"))
	if err != nil {
		t.Fatalf("NewArchiveOutput() error = %v", err)
	}
	writer, err := output.Writer(nil)
	if err != nil {
		t.Fatalf("first Writer() error = %v", err)
	}
	if _, err := output.Writer(nil); err == nil || !strings.Contains(err.Error(), "already acquired") {
		t.Fatalf("second Writer() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := output.Abort(); err != nil {
		t.Fatalf("Abort() error = %v", err)
	}
	if err := output.Abort(); err != nil {
		t.Fatalf("repeated Abort() error = %v", err)
	}
}

func TestArchiveOutputDoesNotPublishInvalidOCI(t *testing.T) {
	finalPath := filepath.Join(t.TempDir(), "invalid.tar")
	output, err := NewArchiveOutput(finalPath)
	if err != nil {
		t.Fatalf("NewArchiveOutput() error = %v", err)
	}
	writer, err := output.Writer(nil)
	if err != nil {
		t.Fatalf("Writer() error = %v", err)
	}
	if _, err := writer.Write([]byte("not an OCI archive")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := output.Publish(); err == nil || !strings.Contains(err.Error(), "verify OCI output") {
		t.Fatalf("Publish() error = %v", err)
	}
	if _, err := os.Stat(finalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat(final) error = %v, want not exist", err)
	}
	if err := output.Abort(); err != nil {
		t.Fatalf("Abort() error = %v", err)
	}
}
