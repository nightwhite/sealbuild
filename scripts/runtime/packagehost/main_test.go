package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRejectsMissingRequiredOption(t *testing.T) {
	err := run(t.Context(), []string{"--qemu", "/tmp/qemu"}, &packageRunner{})
	if err == nil {
		t.Fatal("run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "--lock is required") {
		t.Fatalf("run() error = %q, want missing --lock", err)
	}
}

func TestPackageHostBuildsArtifactFromLockedClosure(t *testing.T) {
	lock := dependencyBuildLock()
	workspace := t.TempDir()
	homebrewRoot := filepath.Join(workspace, "homebrew")
	qemuPath := createEmptyFile(t, workspace, "qemu-system-x86_64")
	qemuDataDirectory := filepath.Join(workspace, "pc-bios")
	for _, name := range qemuFirmwareFiles {
		writeTestFile(t, filepath.Join(qemuDataDirectory, name), name, 0o644)
	}
	graph := lockedDependencyGraph(homebrewRoot)
	for _, library := range graph.Libraries {
		writeTestFile(t, library.SourcePath, "mach-o", 0o755)
	}

	lockPath := filepath.Join(workspace, "build.lock.json")
	lockBytes, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	writeTestFile(t, lockPath, string(lockBytes), 0o644)

	qemuLicenseDirectory := filepath.Join(workspace, "qemu-licenses")
	dependencyLicenseDirectory := filepath.Join(workspace, "dependency-licenses")
	for _, component := range lock.Components {
		licenseRoot := filepath.Join(dependencyLicenseDirectory, component.Name)
		if component.Name == "qemu" {
			licenseRoot = qemuLicenseDirectory
		}
		for _, licensePath := range component.LicenseFiles {
			writeLicenseFile(t, licenseRoot, licensePath, component.Name+":"+licensePath)
		}
	}

	outputPath := filepath.Join(workspace, "host-runtime.tar.zst")
	runner := newPackageRunner(t, qemuPath, graph)
	result, err := packageHost(t.Context(), runner, hostPackageConfig{
		QEMUPath:                   qemuPath,
		QEMUDataDirectory:          qemuDataDirectory,
		LockPath:                   lockPath,
		QEMULicenseDirectory:       qemuLicenseDirectory,
		DependencyLicenseDirectory: dependencyLicenseDirectory,
		OutputPath:                 outputPath,
		HomebrewRoot:               homebrewRoot,
	})
	if err != nil {
		t.Fatalf("packageHost() error = %v", err)
	}
	if result.ArchiveSHA256 == "" || result.ArchiveSize <= 0 {
		t.Fatalf("packageHost() result = %#v, want archive metadata", result)
	}
	if result.Manifest.Platform != lock.HostPlatform {
		t.Fatalf("manifest platform = %#v, want %#v", result.Manifest.Platform, lock.HostPlatform)
	}
	wantFiles := 1 + len(graph.Libraries) + len(qemuFirmwareFiles)
	for _, component := range lock.Components {
		wantFiles += len(component.LicenseFiles)
	}
	if len(result.Manifest.Files) != wantFiles {
		t.Fatalf("len(manifest files) = %d, want %d", len(result.Manifest.Files), wantFiles)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("Stat(output) error = %v", err)
	}
}

type packageRunner struct {
	qemuPath  string
	libraries map[string]MachOFile
}

func newPackageRunner(t *testing.T, qemuPath string, graph DependencyGraph) *packageRunner {
	t.Helper()
	libraries := make(map[string]MachOFile, len(graph.Libraries))
	for _, library := range graph.Libraries {
		resolvedPath, err := filepath.EvalSymlinks(library.SourcePath)
		if err != nil {
			t.Fatalf("resolve test library path: %v", err)
		}
		library.SourcePath = resolvedPath
		libraries[resolvedPath] = library
	}
	resolvedQEMUPath, err := filepath.EvalSymlinks(qemuPath)
	if err != nil {
		t.Fatalf("resolve test QEMU path: %v", err)
	}
	return &packageRunner{qemuPath: resolvedQEMUPath, libraries: libraries}
}

func (runner *packageRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "otool" {
		return nil, nil
	}
	if len(args) != 2 || args[0] != "-L" {
		return nil, fmt.Errorf("unexpected otool arguments: %q", args)
	}
	machOPath := args[1]
	if machOPath == runner.qemuPath {
		var output strings.Builder
		fmt.Fprintf(&output, "%s:\n", machOPath)
		for sourcePath := range runner.libraries {
			fmt.Fprintf(&output, "\t%s (compatibility version 1.0.0, current version 1.0.0)\n", sourcePath)
		}
		return []byte(output.String()), nil
	}
	if _, exists := runner.libraries[machOPath]; exists {
		return []byte(fmt.Sprintf(
			"%s:\n\t%s (compatibility version 1.0.0, current version 1.0.0)\n",
			machOPath,
			machOPath,
		)), nil
	}

	baseName := filepath.Base(machOPath)
	if baseName == "qemu-system-x86_64" {
		var output strings.Builder
		fmt.Fprintf(&output, "%s:\n", machOPath)
		for sourcePath := range runner.libraries {
			fmt.Fprintf(&output, "\t@loader_path/../lib/%s (compatibility version 1.0.0, current version 1.0.0)\n", filepath.Base(sourcePath))
		}
		return []byte(output.String()), nil
	}
	return []byte(fmt.Sprintf(
		"%s:\n\t@loader_path/%s (compatibility version 1.0.0, current version 1.0.0)\n",
		machOPath,
		baseName,
	)), nil
}

func writeTestFile(t *testing.T, filePath, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filePath, err)
	}
	if err := os.WriteFile(filePath, []byte(contents), mode); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", filePath, err)
	}
}
