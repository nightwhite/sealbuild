package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultLayoutUsesUserCacheDirectory(t *testing.T) {
	userCache, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("UserCacheDir() error = %v", err)
	}
	layout, err := DefaultLayout()
	if err != nil {
		t.Fatalf("DefaultLayout() error = %v", err)
	}
	want := filepath.Join(userCache, "sealbuild")
	if layout.Root != want {
		t.Fatalf("layout.Root = %q, want %q", layout.Root, want)
	}
}

func TestLayoutReturnsOwnedPaths(t *testing.T) {
	layout := Layout{Root: t.TempDir()}
	compatibilityID := strings.Repeat("a", 64)

	runtimeDirectory, err := layout.RuntimeDir(compatibilityID)
	if err != nil {
		t.Fatalf("RuntimeDir() error = %v", err)
	}
	stateDirectory, err := layout.StateDir(compatibilityID)
	if err != nil {
		t.Fatalf("StateDir() error = %v", err)
	}
	runtimeLock, err := layout.RuntimeLockPath(compatibilityID)
	if err != nil {
		t.Fatalf("RuntimeLockPath() error = %v", err)
	}

	want := map[string]string{
		"runtime":     filepath.Join(layout.Root, "runtime", compatibilityID),
		"state":       filepath.Join(layout.Root, "state", compatibilityID),
		"runtimeLock": filepath.Join(layout.Root, "locks", "runtime-"+compatibilityID+".lock"),
		"buildLock":   filepath.Join(layout.Root, "locks", "build.lock"),
		"logs":        filepath.Join(layout.Root, "logs"),
	}
	got := map[string]string{
		"runtime": runtimeDirectory, "state": stateDirectory, "runtimeLock": runtimeLock,
		"buildLock": layout.BuildLockPath(), "logs": layout.LogDir(),
	}
	for name, expected := range want {
		if got[name] != expected {
			t.Errorf("%s path = %q, want %q", name, got[name], expected)
		}
	}
}

func TestLayoutRejectsUnsafeRootAndCompatibilityID(t *testing.T) {
	tests := []struct {
		name      string
		layout    Layout
		id        string
		wantError string
	}{
		{name: "relative root", layout: Layout{Root: "cache"}, id: strings.Repeat("a", 64), wantError: "cache root must be an absolute clean path"},
		{name: "filesystem root", layout: Layout{Root: string(filepath.Separator)}, id: strings.Repeat("a", 64), wantError: "cache root must not be the filesystem root"},
		{name: "short id", layout: Layout{Root: t.TempDir()}, id: "abc", wantError: "compatibility ID must be 64 lowercase hexadecimal characters"},
		{name: "path id", layout: Layout{Root: t.TempDir()}, id: "../" + strings.Repeat("a", 61), wantError: "compatibility ID must be 64 lowercase hexadecimal characters"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.layout.RuntimeDir(test.id)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("RuntimeDir() error = %v, want %q", err, test.wantError)
			}
		})
	}
}
