package runtimeassets

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestEmbeddedBundleSourcesCoverExactlyFourHosts(t *testing.T) {
	expectedBundles := map[string]string{
		"bundle_embedded_darwin_amd64.go":  "sealbuild-host-runtime-darwin-amd64.tar.zst",
		"bundle_embedded_darwin_arm64.go":  "sealbuild-host-runtime-darwin-arm64.tar.zst",
		"bundle_embedded_linux_amd64.go":   "sealbuild-host-runtime-linux-amd64.tar.zst",
		"bundle_embedded_windows_amd64.go": "sealbuild-host-runtime-windows-amd64.tar.zst",
	}

	paths, err := filepath.Glob("bundle_embedded_*.go")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	actualNames := make([]string, 0, len(paths))
	for _, sourcePath := range paths {
		actualNames = append(actualNames, filepath.Base(sourcePath))
	}
	slices.Sort(actualNames)
	expectedNames := make([]string, 0, len(expectedBundles))
	for name := range expectedBundles {
		expectedNames = append(expectedNames, name)
	}
	slices.Sort(expectedNames)
	if !slices.Equal(actualNames, expectedNames) {
		t.Fatalf("embedded bundle sources = %v, want %v", actualNames, expectedNames)
	}

	for sourceName, hostArchiveName := range expectedBundles {
		contents, err := os.ReadFile(sourceName)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", sourceName, err)
		}
		if !strings.Contains(string(contents), hostArchiveName) {
			t.Errorf("%s does not return %q", sourceName, hostArchiveName)
		}
	}

	expectedExpression := "((darwin && (arm64 || amd64)) || (linux && amd64) || (windows && amd64))"
	for _, sourceName := range []string{"embedded.go", "bundle_stub.go"} {
		contents, err := os.ReadFile(sourceName)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", sourceName, err)
		}
		if !strings.Contains(string(contents), expectedExpression) {
			t.Errorf("%s does not contain exact four-host expression %q", sourceName, expectedExpression)
		}
	}
}
