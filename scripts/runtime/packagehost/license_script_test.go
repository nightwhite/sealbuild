package main

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchHostLicensesExtractsExactLockedFiles(t *testing.T) {
	workspace := t.TempDir()
	archivePath := filepath.Join(workspace, "demo.tar")
	writeLicenseArchive(t, archivePath, map[string]string{
		"demo-1.0/COPYING":        "license text",
		"demo-1.0/docs/NOTICE.md": "notice text",
		"demo-1.0/UNLOCKED":       "must not be copied",
	})
	checksum := hashTestFile(t, archivePath)
	lockPath := writeLicenseScriptLock(t, workspace, archivePath, checksum)
	sourceDirectory := filepath.Join(workspace, "sources")
	licenseDirectory := filepath.Join(workspace, "licenses")

	command := exec.Command("../fetch-host-licenses.sh", lockPath, sourceDirectory, licenseDirectory)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("fetch-host-licenses.sh error = %v\n%s", err, output)
	}
	assertFileContents(t, filepath.Join(licenseDirectory, "demo", "COPYING"), "license text")
	assertFileContents(t, filepath.Join(licenseDirectory, "demo", "docs", "NOTICE.md"), "notice text")
	if _, err := os.Stat(filepath.Join(licenseDirectory, "demo", "UNLOCKED")); !os.IsNotExist(err) {
		t.Fatalf("unlocked file Stat() error = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(sourceDirectory, "demo.archive")); err != nil {
		t.Fatalf("downloaded archive Stat() error = %v", err)
	}
}

func TestFetchHostLicensesRejectsChecksumMismatch(t *testing.T) {
	workspace := t.TempDir()
	archivePath := filepath.Join(workspace, "demo.tar")
	writeLicenseArchive(t, archivePath, map[string]string{"demo-1.0/COPYING": "license text"})
	lockPath := writeLicenseScriptLock(t, workspace, archivePath, strings.Repeat("0", 64))
	sourceDirectory := filepath.Join(workspace, "sources")
	licenseDirectory := filepath.Join(workspace, "licenses")

	command := exec.Command("../fetch-host-licenses.sh", lockPath, sourceDirectory, licenseDirectory)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("fetch-host-licenses.sh error = nil, want checksum failure")
	}
	if !strings.Contains(string(output), "SHA-256 mismatch for demo") {
		t.Fatalf("fetch-host-licenses.sh output = %q, want checksum mismatch", output)
	}
	if _, err := os.Stat(filepath.Join(sourceDirectory, "demo.archive")); !os.IsNotExist(err) {
		t.Fatalf("downloaded archive Stat() error = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(licenseDirectory, "demo")); !os.IsNotExist(err) {
		t.Fatalf("license directory Stat() error = %v, want not exist", err)
	}
}

func TestFetchHostLicensesUsesVerifiedExistingArchive(t *testing.T) {
	workspace := t.TempDir()
	archivePath := filepath.Join(workspace, "demo.tar")
	writeLicenseArchive(t, archivePath, map[string]string{
		"demo-1.0/COPYING":        "license text",
		"demo-1.0/docs/NOTICE.md": "notice text",
	})
	lockPath := writeLicenseScriptLock(t, workspace, archivePath, hashTestFile(t, archivePath))
	sourceDirectory := filepath.Join(workspace, "sources")
	if err := os.Mkdir(sourceDirectory, 0o755); err != nil {
		t.Fatalf("Mkdir(sources) error = %v", err)
	}
	if err := os.Rename(archivePath, filepath.Join(sourceDirectory, "demo.archive")); err != nil {
		t.Fatalf("Rename(existing archive) error = %v", err)
	}
	licenseDirectory := filepath.Join(workspace, "licenses")

	command := exec.Command("../fetch-host-licenses.sh", lockPath, sourceDirectory, licenseDirectory)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("fetch-host-licenses.sh error = %v\n%s", err, output)
	}
	assertFileContents(t, filepath.Join(licenseDirectory, "demo", "COPYING"), "license text")
	assertFileContents(t, filepath.Join(licenseDirectory, "demo", "docs", "NOTICE.md"), "notice text")
}

func TestFetchHostLicensesRejectsUnverifiedExistingArchiveWithoutDownloading(t *testing.T) {
	workspace := t.TempDir()
	archivePath := filepath.Join(workspace, "demo.tar")
	writeLicenseArchive(t, archivePath, map[string]string{"demo-1.0/COPYING": "license text"})
	lockPath := writeLicenseScriptLock(t, workspace, archivePath, strings.Repeat("0", 64))
	sourceDirectory := filepath.Join(workspace, "sources")
	if err := os.Mkdir(sourceDirectory, 0o755); err != nil {
		t.Fatalf("Mkdir(sources) error = %v", err)
	}
	if err := os.Rename(archivePath, filepath.Join(sourceDirectory, "demo.archive")); err != nil {
		t.Fatalf("Rename(existing archive) error = %v", err)
	}
	licenseDirectory := filepath.Join(workspace, "licenses")

	command := exec.Command("../fetch-host-licenses.sh", lockPath, sourceDirectory, licenseDirectory)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("fetch-host-licenses.sh error = nil, want checksum failure")
	}
	if !strings.Contains(string(output), "SHA-256 mismatch for demo") {
		t.Fatalf("fetch-host-licenses.sh output = %q, want checksum mismatch", output)
	}
	if _, err := os.Stat(filepath.Join(licenseDirectory, "demo")); !os.IsNotExist(err) {
		t.Fatalf("license directory Stat() error = %v, want not exist", err)
	}
}

func writeLicenseScriptLock(t *testing.T, workspace, archivePath, checksum string) string {
	t.Helper()
	archiveURL := (&url.URL{Scheme: "file", Path: archivePath}).String()
	lock := map[string]any{
		"schemaVersion": 1,
		"components": []map[string]any{{
			"name":         "demo",
			"source":       archiveURL,
			"sha256":       checksum,
			"licenseFiles": []string{"COPYING", "docs/NOTICE.md"},
		}},
	}
	contents, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	lockPath := filepath.Join(workspace, "build.lock.json")
	if err := os.WriteFile(lockPath, contents, 0o644); err != nil {
		t.Fatalf("WriteFile(lock) error = %v", err)
	}
	return lockPath
}

func writeLicenseArchive(t *testing.T, archivePath string, files map[string]string) {
	t.Helper()
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("Create(archive) error = %v", err)
	}
	writer := tar.NewWriter(archiveFile)
	for name, contents := range files {
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(contents))}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("WriteHeader(%s) error = %v", name, err)
		}
		if _, err := writer.Write([]byte(contents)); err != nil {
			t.Fatalf("Write(%s) error = %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close(tar) error = %v", err)
	}
	if err := archiveFile.Close(); err != nil {
		t.Fatalf("Close(archive) error = %v", err)
	}
}

func hashTestFile(t *testing.T, filePath string) string {
	t.Helper()
	contents, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", filePath, err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(contents))
}

func assertFileContents(t *testing.T, filePath, want string) {
	t.Helper()
	contents, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", filePath, err)
	}
	if string(contents) != want {
		t.Fatalf("%s contents = %q, want %q", filePath, contents, want)
	}
}
