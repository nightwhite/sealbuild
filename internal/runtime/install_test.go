package runtime_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labring/sealbuild/internal/cache"
	"github.com/labring/sealbuild/internal/lockfile"
	runtimepkg "github.com/labring/sealbuild/internal/runtime"
	"github.com/labring/sealbuild/internal/tlsmaterial"
	"github.com/labring/sealbuild/scripts/runtime/artifact"
)

func TestInstallerInstallsAndReusesVerifiedRuntime(t *testing.T) {
	workspace := t.TempDir()
	bundle := installerBundle(t, workspace)
	layout := cache.Layout{Root: filepath.Join(workspace, "cache")}
	installer := runtimepkg.Installer{Layout: layout}

	first, err := installer.Install(t.Context(), bundle)
	if err != nil {
		t.Fatalf("first Install() error = %v", err)
	}
	compatibilityID, err := bundle.CompatibilityID()
	if err != nil {
		t.Fatalf("CompatibilityID() error = %v", err)
	}
	if first.CompatibilityID != compatibilityID {
		t.Fatalf("CompatibilityID = %q, want %q", first.CompatibilityID, compatibilityID)
	}
	for _, requiredPath := range []string{
		filepath.Join(first.Host, "bin", "qemu-system-x86_64"),
		filepath.Join(first.Guest, "bzImage"),
		filepath.Join(first.Root, "installation.json"),
		filepath.Join(first.Root, "complete"),
		first.StateDisk,
	} {
		if _, err := os.Stat(requiredPath); err != nil {
			t.Errorf("Stat(%s) error = %v", requiredPath, err)
		}
	}
	if err := tlsmaterial.Validate(first.TLS, time.Now()); err != nil {
		t.Fatalf("Validate(TLS) error = %v", err)
	}
	if err := os.WriteFile(first.StateDisk, []byte("persistent build state"), 0o600); err != nil {
		t.Fatalf("WriteFile(state) error = %v", err)
	}

	second, err := installer.Install(t.Context(), bundle)
	if err != nil {
		t.Fatalf("second Install() error = %v", err)
	}
	if second.Root != first.Root || second.StateDisk != first.StateDisk {
		t.Fatalf("second installation = %#v, want same paths as %#v", second, first)
	}
	stateContents, err := os.ReadFile(second.StateDisk)
	if err != nil {
		t.Fatalf("ReadFile(state) error = %v", err)
	}
	if string(stateContents) != "persistent build state" {
		t.Fatalf("state contents = %q, want preserved state", stateContents)
	}
}

func TestInstallerRejectsCorruptExistingRuntime(t *testing.T) {
	workspace := t.TempDir()
	bundle := installerBundle(t, workspace)
	installer := runtimepkg.Installer{Layout: cache.Layout{Root: filepath.Join(workspace, "cache")}}
	installation, err := installer.Install(t.Context(), bundle)
	if err != nil {
		t.Fatalf("first Install() error = %v", err)
	}
	qemuPath := filepath.Join(installation.Host, "bin", "qemu-system-x86_64")
	if err := os.WriteFile(qemuPath, []byte("corrupt"), 0o755); err != nil {
		t.Fatalf("WriteFile(qemu) error = %v", err)
	}

	_, err = installer.Install(t.Context(), bundle)
	if err == nil || !strings.Contains(err.Error(), "verify installed Host Runtime") {
		t.Fatalf("second Install() error = %v, want corrupt Runtime error", err)
	}
}

func TestInstallerRejectsHostRuntimeForAnotherPlatform(t *testing.T) {
	workspace := t.TempDir()
	bundle := installerBundle(t, workspace)
	bundle.Host = installerArtifactForPlatform(t, workspace, "wrong-host", runtimepkg.ArtifactKindHost, runtimepkg.Platform{OS: "windows", Architecture: "amd64"}, map[string]installerFile{
		"bin/qemu-system-x86_64.exe": {contents: "qemu", mode: 0o755},
	})
	installer := runtimepkg.Installer{Layout: cache.Layout{Root: filepath.Join(workspace, "cache")}}

	_, err := installer.Install(t.Context(), bundle)
	if err == nil || !strings.Contains(err.Error(), "Host Runtime platform") {
		t.Fatalf("Install() error = %v, want Host Runtime platform error", err)
	}
}

func TestInstallerReturnsRuntimeLockContention(t *testing.T) {
	workspace := t.TempDir()
	bundle := installerBundle(t, workspace)
	layout := cache.Layout{Root: filepath.Join(workspace, "cache")}
	compatibilityID, err := bundle.CompatibilityID()
	if err != nil {
		t.Fatalf("CompatibilityID() error = %v", err)
	}
	lockPath, err := layout.RuntimeLockPath(compatibilityID)
	if err != nil {
		t.Fatalf("RuntimeLockPath() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	lock, err := lockfile.TryAcquire(lockPath)
	if err != nil {
		t.Fatalf("TryAcquire() error = %v", err)
	}
	defer lock.Close()

	_, err = (runtimepkg.Installer{Layout: layout}).Install(t.Context(), bundle)
	if !errors.Is(err, lockfile.ErrContended) {
		t.Fatalf("Install() error = %v, want ErrContended", err)
	}
}

func TestInstallerRemovesTemporaryRuntimeAfterFailure(t *testing.T) {
	workspace := t.TempDir()
	bundle := installerBundle(t, workspace)
	bundle.Guest.Open = func() (io.ReadCloser, error) {
		return nil, errors.New("guest asset unavailable")
	}
	layout := cache.Layout{Root: filepath.Join(workspace, "cache")}
	compatibilityID, err := bundle.CompatibilityID()
	if err != nil {
		t.Fatalf("CompatibilityID() error = %v", err)
	}

	_, err = (runtimepkg.Installer{Layout: layout}).Install(t.Context(), bundle)
	if err == nil || !strings.Contains(err.Error(), "guest asset unavailable") {
		t.Fatalf("Install() error = %v, want Guest asset error", err)
	}
	runtimeDirectory, err := layout.RuntimeDir(compatibilityID)
	if err != nil {
		t.Fatalf("RuntimeDir() error = %v", err)
	}
	if _, err := os.Stat(runtimeDirectory); !os.IsNotExist(err) {
		t.Fatalf("Runtime Stat() error = %v, want not exist", err)
	}
	entries, err := os.ReadDir(filepath.Dir(runtimeDirectory))
	if err != nil {
		t.Fatalf("ReadDir(runtime parent) error = %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Errorf("temporary Runtime remains: %s", entry.Name())
		}
	}
}

func installerBundle(t *testing.T, workspace string) runtimepkg.Bundle {
	t.Helper()
	return runtimepkg.Bundle{
		Host: installerArtifact(t, workspace, "host", runtimepkg.ArtifactKindHost, map[string]installerFile{
			"bin/qemu-system-x86_64": {contents: "qemu", mode: 0o755},
		}),
		Guest: installerArtifact(t, workspace, "guest", runtimepkg.ArtifactKindGuest, map[string]installerFile{
			"buildkit-state.qcow2": {contents: "qcow2 template", mode: 0o600},
			"bzImage":              {contents: "kernel", mode: 0o644},
			"rootfs.ext4":          {contents: "rootfs", mode: 0o644},
		}),
	}
}

type installerFile struct {
	contents string
	mode     os.FileMode
}

func installerArtifact(t *testing.T, workspace, name string, kind runtimepkg.ArtifactKind, files map[string]installerFile) runtimepkg.Asset {
	t.Helper()
	platform := runtimepkg.Platform{OS: "darwin", Architecture: "arm64"}
	if kind == runtimepkg.ArtifactKindGuest {
		platform = runtimepkg.Platform{OS: "linux", Architecture: "amd64"}
	}
	return installerArtifactForPlatform(t, workspace, name, kind, platform, files)
}

func installerArtifactForPlatform(t *testing.T, workspace, name string, kind runtimepkg.ArtifactKind, platform runtimepkg.Platform, files map[string]installerFile) runtimepkg.Asset {
	t.Helper()
	payload := filepath.Join(workspace, name+"-payload")
	for relativePath, file := range files {
		writeExtractFixture(t, payload, relativePath, file.contents, file.mode)
	}
	archivePath := filepath.Join(workspace, name+".tar.zst")
	result, err := artifact.Build(artifact.BuildConfig{
		PayloadDir: payload,
		OutputPath: archivePath,
		Manifest: runtimepkg.ArtifactManifest{
			SchemaVersion: 1, Kind: kind, Platform: platform,
			Components: []runtimepkg.Component{{
				Name: name, Version: "v1", Source: "https://example.invalid/" + name, SHA256: strings.Repeat("a", 64),
			}},
		},
	})
	if err != nil {
		t.Fatalf("artifact.Build(%s) error = %v", name, err)
	}
	return fileAsset(t, archivePath, result.ArchiveSHA256, result.ArchiveSize)
}
