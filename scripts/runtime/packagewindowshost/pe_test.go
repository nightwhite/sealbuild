package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolvePEClosureCollectsRecursivePrivateDLLs(t *testing.T) {
	root := t.TempDir()
	qemu := writePEFixture(t, root, "qemu-system-x86_64.exe")
	glib := writePEFixture(t, root, "libglib-2.0-0.dll")
	ffi := writePEFixture(t, root, "libffi-8.dll")
	files := map[string]PEFile{
		canonicalPEPath(qemu): {Path: qemu, Imports: []string{"KERNEL32.dll", "LIBGLIB-2.0-0.DLL"}},
		canonicalPEPath(glib): {Path: glib, Imports: []string{"libffi-8.dll", "api-ms-win-core-file-l1-1-0.dll"}},
		canonicalPEPath(ffi):  {Path: ffi, Imports: []string{"ntdll.dll"}},
	}

	closure, err := resolvePEClosure(qemu, []string{root}, func(path string) (PEFile, error) {
		file, exists := files[canonicalPEPath(path)]
		if !exists {
			return PEFile{}, fmt.Errorf("unexpected inspect %s", path)
		}
		return file, nil
	})
	if err != nil {
		t.Fatalf("resolvePEClosure() error = %v", err)
	}
	want := []string{qemu, ffi, glib}
	if !reflect.DeepEqual(closure, want) {
		t.Fatalf("closure = %#v, want %#v", closure, want)
	}
}

func TestResolvePEClosureRejectsMissingPrivateDLL(t *testing.T) {
	root := t.TempDir()
	qemu := writePEFixture(t, root, "qemu-system-x86_64.exe")
	_, err := resolvePEClosure(qemu, []string{root}, func(path string) (PEFile, error) {
		return PEFile{Path: path, Imports: []string{"missing.dll"}}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "missing.dll") {
		t.Fatalf("resolvePEClosure() error = %v, want missing DLL", err)
	}
}

func TestResolvePEClosureRejectsCaseInsensitiveNameConflict(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	qemu := writePEFixture(t, first, "qemu-system-x86_64.exe")
	writePEFixture(t, first, "libglib.dll")
	writePEFixture(t, second, "LIBGLIB.DLL")
	_, err := resolvePEClosure(qemu, []string{first, second}, func(path string) (PEFile, error) {
		return PEFile{Path: path}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "conflicting DLL") {
		t.Fatalf("resolvePEClosure() error = %v, want conflict", err)
	}
}

func writePEFixture(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
