package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

func TestBuildHostManifestUsesLockedComponents(t *testing.T) {
	lock := validBuildLock()

	manifest := buildHostManifest(lock)
	if manifest.SchemaVersion != 1 || manifest.Kind != runtimepkg.ArtifactKindHost {
		t.Fatalf("manifest identity = schema %d kind %q", manifest.SchemaVersion, manifest.Kind)
	}
	if manifest.Platform != (runtimepkg.Platform{OS: "darwin", Architecture: "arm64"}) {
		t.Fatalf("manifest platform = %#v", manifest.Platform)
	}
	if len(manifest.Components) != len(lock.Components) {
		t.Fatalf("len(Components) = %d, want %d", len(manifest.Components), len(lock.Components))
	}
	for index, component := range manifest.Components {
		locked := lock.Components[index]
		if component.Name != locked.Name || component.Version != locked.Version || component.Source != locked.Source || component.Revision != locked.Revision || component.SHA256 != locked.SHA256 {
			t.Errorf("Components[%d] = %#v, want lock %#v", index, component, locked)
		}
	}
	if manifest.Files != nil {
		t.Fatalf("manifest files = %#v, want nil before payload scan", manifest.Files)
	}
}

func TestValidateDependencyComponentsAcceptsLockedHomebrewClosure(t *testing.T) {
	root := "/opt/homebrew"
	graph := lockedDependencyGraph(root)

	if err := validateDependencyComponents(graph, root, dependencyBuildLock()); err != nil {
		t.Fatalf("validateDependencyComponents() error = %v", err)
	}
}

func TestValidateDependencyComponentsRejectsDrift(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*DependencyGraph)
		wantError string
	}{
		{
			name: "missing component",
			mutate: func(graph *DependencyGraph) {
				graph.Libraries = graph.Libraries[:len(graph.Libraries)-1]
			},
			wantError: "locked dependency pcre2 is missing",
		},
		{
			name: "unexpected component",
			mutate: func(graph *DependencyGraph) {
				graph.Libraries = append(graph.Libraries, MachOFile{SourcePath: "/opt/homebrew/Cellar/openssl/3.6.3/lib/libssl.dylib"})
			},
			wantError: "dependency openssl is not present in Host Build Lock",
		},
		{
			name: "version drift",
			mutate: func(graph *DependencyGraph) {
				graph.Libraries[0].SourcePath = "/opt/homebrew/Cellar/glib/2.89.0/lib/libglib.dylib"
			},
			wantError: "dependency glib version is 2.89.0, expected 2.88.2",
		},
		{
			name: "path outside Cellar",
			mutate: func(graph *DependencyGraph) {
				graph.Libraries[0].SourcePath = "/opt/homebrew/lib/libglib.dylib"
			},
			wantError: "does not use a Homebrew Cellar path",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph := lockedDependencyGraph("/opt/homebrew")
			test.mutate(&graph)

			err := validateDependencyComponents(graph, "/opt/homebrew", dependencyBuildLock())
			if err == nil {
				t.Fatal("validateDependencyComponents() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("validateDependencyComponents() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestCopyLockedLicensesCopiesExactDeclaredFiles(t *testing.T) {
	lock := validBuildLock()
	lock.Components = lock.Components[:2]
	lock.Components[1] = LockedComponent{
		Name:         "glib",
		Version:      "2.88.2",
		Source:       "https://example.invalid/glib",
		SHA256:       strings.Repeat("b", 64),
		License:      "LGPL-2.1-or-later",
		LicenseFiles: []string{"COPYING", "docs/COPYING.extra"},
	}
	qemuLicenses := t.TempDir()
	dependencyLicenses := t.TempDir()
	writeLicenseFile(t, qemuLicenses, "COPYING", "qemu-gpl")
	writeLicenseFile(t, qemuLicenses, "COPYING.LIB", "qemu-lgpl")
	writeLicenseFile(t, qemuLicenses, "LICENSE", "qemu-license-map")
	writeLicenseFile(t, filepath.Join(dependencyLicenses, "glib"), "COPYING", "glib-lgpl")
	writeLicenseFile(t, filepath.Join(dependencyLicenses, "glib"), "docs/COPYING.extra", "glib-extra")
	payload := t.TempDir()

	if err := copyLockedLicenses(lock, qemuLicenses, dependencyLicenses, payload); err != nil {
		t.Fatalf("copyLockedLicenses() error = %v", err)
	}
	wantContents := map[string]string{
		"licenses/qemu/COPYING":            "qemu-gpl",
		"licenses/qemu/COPYING.LIB":        "qemu-lgpl",
		"licenses/qemu/LICENSE":            "qemu-license-map",
		"licenses/glib/COPYING":            "glib-lgpl",
		"licenses/glib/docs/COPYING.extra": "glib-extra",
	}
	for relativePath, want := range wantContents {
		filePath := filepath.Join(payload, filepath.FromSlash(relativePath))
		contents, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", relativePath, err)
		}
		if string(contents) != want {
			t.Errorf("%s contents = %q, want %q", relativePath, contents, want)
		}
		info, err := os.Stat(filePath)
		if err != nil {
			t.Fatalf("Stat(%s) error = %v", relativePath, err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s mode = %#o, want 0644", relativePath, info.Mode().Perm())
		}
	}
}

func TestCopyLockedLicensesRejectsMissingDeclaredFile(t *testing.T) {
	lock := validBuildLock()
	qemuLicenses := t.TempDir()
	dependencyLicenses := t.TempDir()
	payload := t.TempDir()

	err := copyLockedLicenses(lock, qemuLicenses, dependencyLicenses, payload)
	if err == nil {
		t.Fatal("copyLockedLicenses() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "copy qemu license COPYING") {
		t.Fatalf("copyLockedLicenses() error = %q, want missing QEMU license", err)
	}
}

func lockedDependencyGraph(root string) DependencyGraph {
	cellar := filepath.Join(root, "Cellar")
	return DependencyGraph{Libraries: []MachOFile{
		{SourcePath: filepath.Join(cellar, "glib", "2.88.2", "lib", "libglib-2.0.0.dylib")},
		{SourcePath: filepath.Join(cellar, "glib", "2.88.2", "lib", "libgio-2.0.0.dylib")},
		{SourcePath: filepath.Join(cellar, "pixman", "0.46.4", "lib", "libpixman-1.0.dylib")},
		{SourcePath: filepath.Join(cellar, "libslirp", "4.9.3", "lib", "libslirp.0.dylib")},
		{SourcePath: filepath.Join(cellar, "zstd", "1.5.7_1", "lib", "libzstd.1.dylib")},
		{SourcePath: filepath.Join(cellar, "gettext", "1.0", "lib", "libintl.8.dylib")},
		{SourcePath: filepath.Join(cellar, "gmp", "6.3.0", "lib", "libgmp.10.dylib")},
		{SourcePath: filepath.Join(cellar, "pcre2", "10.47_1", "lib", "libpcre2-8.0.dylib")},
	}}
}

func dependencyBuildLock() BuildLock {
	lock := validBuildLock()
	versions := map[string]string{
		"glib":     "2.88.2",
		"pixman":   "0.46.4",
		"libslirp": "4.9.3",
		"zstd":     "1.5.7",
		"gettext":  "1.0",
		"gmp":      "6.3.0",
		"pcre2":    "10.47",
	}
	for index := range lock.Components {
		if version, exists := versions[lock.Components[index].Name]; exists {
			lock.Components[index].Version = version
		}
	}
	return lock
}

func writeLicenseFile(t *testing.T, root, relativePath, contents string) {
	t.Helper()
	filePath := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filePath, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
