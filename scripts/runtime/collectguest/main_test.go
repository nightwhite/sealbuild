package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectGuestLicensesCopiesLegalInfoAndAllSourceLicenses(t *testing.T) {
	workspace := t.TempDir()
	legalDirectory := filepath.Join(workspace, "legal-info")
	writeCollectorFile(t, legalDirectory, "busybox/COPYING", "busybox")
	archives := map[string][]byte{
		"https://example.invalid/buildkit.tar.gz": licenseArchive(t, []tarEntry{
			{name: "pax_global_header", typeFlag: tar.TypeXGlobalHeader, paxRecords: map[string]string{"comment": "github archive metadata"}},
			{name: "buildkit-1/LICENSE", contents: "buildkit"},
			{name: "buildkit-1/vendor/module/LICENSE.txt", contents: "module"},
			{name: "buildkit-1/README.md", contents: "ignored"},
		}),
		"https://example.invalid/runc.tar.gz": licenseArchive(t, []tarEntry{
			{name: "runc-1/COPYING", contents: "runc"},
		}),
		"https://example.invalid/cni.tar.gz": licenseArchive(t, []tarEntry{
			{name: "plugins-1/LICENSE", contents: "cni"},
		}),
	}
	lockPath := writeCollectorLock(t, workspace, archives)
	outputDirectory := filepath.Join(workspace, "guest-licenses")

	err := collectGuestLicenses(t.Context(), collectConfig{
		LockPath:             lockPath,
		BuildrootLicensePath: legalDirectory,
		SourceDirectory:      filepath.Join(workspace, "sources"),
		OutputDirectory:      outputDirectory,
	}, fakeDownloader{archives: archives})
	if err != nil {
		t.Fatalf("collectGuestLicenses() error = %v", err)
	}

	wantFiles := map[string]string{
		"buildroot/busybox/COPYING":          "busybox",
		"buildkit/LICENSE":                   "buildkit",
		"buildkit/vendor/module/LICENSE.txt": "module",
		"runc/COPYING":                       "runc",
		"cni-plugins/LICENSE":                "cni",
	}
	for relativePath, want := range wantFiles {
		contents, err := os.ReadFile(filepath.Join(outputDirectory, filepath.FromSlash(relativePath)))
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", relativePath, err)
		}
		if string(contents) != want {
			t.Errorf("%s contents = %q, want %q", relativePath, contents, want)
		}
	}
	if _, err := os.Stat(filepath.Join(outputDirectory, "buildkit", "README.md")); !os.IsNotExist(err) {
		t.Fatalf("README Stat() error = %v, want not exist", err)
	}
}

func TestCollectGuestLicensesRejectsMatchingSymlinkWithoutPublishing(t *testing.T) {
	workspace := t.TempDir()
	legalDirectory := filepath.Join(workspace, "legal-info")
	writeCollectorFile(t, legalDirectory, "busybox/COPYING", "busybox")
	archives := map[string][]byte{
		"https://example.invalid/buildkit.tar.gz": licenseArchive(t, []tarEntry{
			{name: "buildkit-1/LICENSE", linkName: "COPYING", typeFlag: tar.TypeSymlink},
		}),
		"https://example.invalid/runc.tar.gz": licenseArchive(t, []tarEntry{{name: "runc-1/LICENSE", contents: "runc"}}),
		"https://example.invalid/cni.tar.gz":  licenseArchive(t, []tarEntry{{name: "plugins-1/LICENSE", contents: "cni"}}),
	}
	lockPath := writeCollectorLock(t, workspace, archives)
	outputDirectory := filepath.Join(workspace, "guest-licenses")

	err := collectGuestLicenses(t.Context(), collectConfig{
		LockPath:             lockPath,
		BuildrootLicensePath: legalDirectory,
		SourceDirectory:      filepath.Join(workspace, "sources"),
		OutputDirectory:      outputDirectory,
	}, fakeDownloader{archives: archives})
	if err == nil {
		t.Fatal("collectGuestLicenses() error = nil, want symlink rejection")
	}
	if !strings.Contains(err.Error(), "license entry buildkit-1/LICENSE must be a regular file") {
		t.Fatalf("collectGuestLicenses() error = %q, want symlink error", err)
	}
	if _, err := os.Stat(outputDirectory); !os.IsNotExist(err) {
		t.Fatalf("output Stat() error = %v, want not exist", err)
	}
}

type fakeDownloader struct {
	archives map[string][]byte
}

func (downloader fakeDownloader) Download(_ context.Context, source string, destination io.Writer) error {
	archive, exists := downloader.archives[source]
	if !exists {
		return fmt.Errorf("unexpected source %s", source)
	}
	_, err := destination.Write(archive)
	return err
}

type tarEntry struct {
	name       string
	contents   string
	linkName   string
	typeFlag   byte
	paxRecords map[string]string
}

func licenseArchive(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		typeFlag := entry.typeFlag
		if typeFlag == 0 {
			typeFlag = tar.TypeReg
		}
		header := &tar.Header{Typeflag: typeFlag, PAXRecords: entry.paxRecords}
		if typeFlag != tar.TypeXGlobalHeader {
			header.Name = entry.name
			header.Mode = 0o644
			header.Size = int64(len(entry.contents))
			header.Linkname = entry.linkName
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("WriteHeader() error = %v", err)
		}
		if typeFlag == tar.TypeReg {
			if _, err := tarWriter.Write([]byte(entry.contents)); err != nil {
				t.Fatalf("Write() error = %v", err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("Close(tar) error = %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("Close(gzip) error = %v", err)
	}
	return buffer.Bytes()
}

func writeCollectorLock(t *testing.T, workspace string, archives map[string][]byte) string {
	t.Helper()
	components := make([]map[string]string, 0, 3)
	for _, component := range []struct {
		name   string
		source string
	}{
		{"buildkit-source", "https://example.invalid/buildkit.tar.gz"},
		{"runc-source", "https://example.invalid/runc.tar.gz"},
		{"cni-plugins-source", "https://example.invalid/cni.tar.gz"},
	} {
		checksum := sha256.Sum256(archives[component.source])
		components = append(components, map[string]string{
			"name": component.name, "version": "v1", "source": component.source,
			"sha256": fmt.Sprintf("%x", checksum),
		})
	}
	lock := map[string]any{
		"schemaVersion": 1,
		"guestPlatform": map[string]string{"os": "linux", "architecture": "amd64"},
		"components":    components,
	}
	contents, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	lockPath := filepath.Join(workspace, "manifest.lock.json")
	if err := os.WriteFile(lockPath, contents, 0o644); err != nil {
		t.Fatalf("WriteFile(lock) error = %v", err)
	}
	return lockPath
}

func writeCollectorFile(t *testing.T, root, relativePath, contents string) {
	t.Helper()
	filePath := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filePath, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
