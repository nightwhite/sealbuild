package main

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCollectRuntimePackageEvidenceRecordsEveryDLLInStableOrder(t *testing.T) {
	root := t.TempDir()
	closure := []string{
		filepath.Join(root, "qemu-system-x86_64.exe"),
		filepath.Join(root, "libzstd.dll"),
		filepath.Join(root, "libglib-2.0-0.dll"),
		filepath.Join(root, "libgio-2.0-0.dll"),
	}
	evidence, err := collectRuntimePackageEvidence(closure, func(path string) (LockedRuntimePackage, error) {
		switch strings.ToLower(filepath.Base(path)) {
		case "libgio-2.0-0.dll", "libglib-2.0-0.dll":
			return LockedRuntimePackage{Name: "mingw-glib2", Version: "2.88.2-1"}, nil
		case "libzstd.dll":
			return LockedRuntimePackage{Name: "mingw-zstd", Version: "1.5.7-2"}, nil
		default:
			return LockedRuntimePackage{}, fmt.Errorf("unexpected path %s", path)
		}
	})
	if err != nil {
		t.Fatalf("collectRuntimePackageEvidence() error = %v", err)
	}
	want := RuntimePackageEvidence{SchemaVersion: 1, DLLs: []RuntimePackageDLL{
		{Name: "libgio-2.0-0.dll", Package: "mingw-glib2", Version: "2.88.2-1"},
		{Name: "libglib-2.0-0.dll", Package: "mingw-glib2", Version: "2.88.2-1"},
		{Name: "libzstd.dll", Package: "mingw-zstd", Version: "1.5.7-2"},
	}}
	if !reflect.DeepEqual(evidence, want) {
		t.Fatalf("evidence = %#v, want %#v", evidence, want)
	}
}

func TestCollectRuntimePackageEvidenceRejectsUnownedDLL(t *testing.T) {
	root := t.TempDir()
	closure := []string{
		filepath.Join(root, "qemu-system-x86_64.exe"),
		filepath.Join(root, "private.dll"),
	}
	_, err := collectRuntimePackageEvidence(closure, func(string) (LockedRuntimePackage, error) {
		return LockedRuntimePackage{}, fmt.Errorf("no package owns file")
	})
	if err == nil || !strings.Contains(err.Error(), "private.dll") || !strings.Contains(err.Error(), "no package owns file") {
		t.Fatalf("collectRuntimePackageEvidence() error = %v, want owner failure", err)
	}
}

func TestParsePacmanPackageRejectsUnexpectedOutput(t *testing.T) {
	for _, output := range []string{"", "package-only", "package version extra"} {
		if _, err := parsePacmanPackage(output); err == nil {
			t.Errorf("parsePacmanPackage(%q) error = nil", output)
		}
	}
	got, err := parsePacmanPackage("mingw-glib2 2.88.2-1\n")
	if err != nil {
		t.Fatalf("parsePacmanPackage() error = %v", err)
	}
	if want := (LockedRuntimePackage{Name: "mingw-glib2", Version: "2.88.2-1"}); got != want {
		t.Fatalf("parsePacmanPackage() = %#v, want %#v", got, want)
	}
}
