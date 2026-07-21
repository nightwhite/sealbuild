package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

func TestVerifyGuestManifestRejectsTLSPrivateKey(t *testing.T) {
	manifest := runtimepkg.ArtifactManifest{
		Files: []runtimepkg.ArtifactFile{{Path: "tls/server.key"}},
	}

	err := verifyGuestManifest(manifest)
	if err == nil || !strings.Contains(err.Error(), "private key") {
		t.Fatalf("verifyGuestManifest() error = %v, want private key rejection", err)
	}
}

func TestVerifyGuestStateDiskRejectsNonQCOW2File(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "buildkit-state.qcow2"), []byte("not qcow2"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := verifyGuestStateDisk(directory)
	if err == nil || !strings.Contains(err.Error(), "qcow2") {
		t.Fatalf("verifyGuestStateDisk() error = %v, want qcow2 rejection", err)
	}
}
