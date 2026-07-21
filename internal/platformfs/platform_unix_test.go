//go:build !windows

package platformfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateFilePrivacyUsesUnixPermissions(t *testing.T) {
	directory := t.TempDir()
	privatePath := filepath.Join(directory, "private")
	publicPath := filepath.Join(directory, "public")
	if err := os.WriteFile(privatePath, []byte("private"), 0o600); err != nil {
		t.Fatalf("WriteFile(private) error = %v", err)
	}
	if err := os.WriteFile(publicPath, []byte("public"), 0o644); err != nil {
		t.Fatalf("WriteFile(public) error = %v", err)
	}
	privateInfo, _ := os.Stat(privatePath)
	publicInfo, _ := os.Stat(publicPath)
	if err := ValidatePrivateFile(privateInfo); err != nil {
		t.Fatalf("ValidatePrivateFile() error = %v", err)
	}
	if err := ValidatePublicFile(publicInfo); err != nil {
		t.Fatalf("ValidatePublicFile() error = %v", err)
	}
	if err := ValidatePrivateFile(publicInfo); err == nil {
		t.Fatal("ValidatePrivateFile(public) error = nil, want permission error")
	}
}
