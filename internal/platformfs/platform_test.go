package platformfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishFileNoReplaceConsumesTemporaryFile(t *testing.T) {
	directory := t.TempDir()
	temporaryPath := filepath.Join(directory, "temporary")
	finalPath := filepath.Join(directory, "final")
	if err := os.WriteFile(temporaryPath, []byte("payload"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := PublishFileNoReplace(temporaryPath, finalPath); err != nil {
		t.Fatalf("PublishFileNoReplace() error = %v", err)
	}
	if _, err := os.Stat(temporaryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary Stat() error = %v, want not exist", err)
	}
	contents, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("ReadFile(final) error = %v", err)
	}
	if string(contents) != "payload" {
		t.Fatalf("final contents = %q, want payload", contents)
	}
}

func TestPublishFileNoReplacePreservesExistingTarget(t *testing.T) {
	directory := t.TempDir()
	temporaryPath := filepath.Join(directory, "temporary")
	finalPath := filepath.Join(directory, "final")
	if err := os.WriteFile(temporaryPath, []byte("new"), 0o600); err != nil {
		t.Fatalf("WriteFile(temporary) error = %v", err)
	}
	if err := os.WriteFile(finalPath, []byte("existing"), 0o600); err != nil {
		t.Fatalf("WriteFile(final) error = %v", err)
	}

	err := PublishFileNoReplace(temporaryPath, finalPath)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("PublishFileNoReplace() error = %v, want os.ErrExist", err)
	}
	contents, readErr := os.ReadFile(finalPath)
	if readErr != nil {
		t.Fatalf("ReadFile(final) error = %v", readErr)
	}
	if string(contents) != "existing" {
		t.Fatalf("final contents = %q, want existing", contents)
	}
	if _, statErr := os.Stat(temporaryPath); statErr != nil {
		t.Fatalf("temporary Stat() error = %v, want preserved temporary", statErr)
	}
}

func TestPublishDirectoryNoReplaceMovesDirectory(t *testing.T) {
	parent := t.TempDir()
	temporaryPath := filepath.Join(parent, "temporary")
	finalPath := filepath.Join(parent, "final")
	if err := os.Mkdir(temporaryPath, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(temporaryPath, "payload"), []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := PublishDirectoryNoReplace(temporaryPath, finalPath); err != nil {
		t.Fatalf("PublishDirectoryNoReplace() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(finalPath, "payload")); err != nil {
		t.Fatalf("published payload Stat() error = %v", err)
	}
}

func TestSyncDirectoryAcceptsDirectory(t *testing.T) {
	if err := SyncDirectory(t.TempDir()); err != nil {
		t.Fatalf("SyncDirectory() error = %v", err)
	}
}
