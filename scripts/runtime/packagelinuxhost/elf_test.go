package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestResolveELFClosureReturnsLoaderAndRecursiveLibraries(t *testing.T) {
	root := t.TempDir()
	qemu := writeLinuxPackageFile(t, filepath.Join(root, "bin", "qemu"), "qemu")
	loader := writeLinuxPackageFile(t, filepath.Join(root, "lib", "ld-linux-x86-64.so.2"), "loader")
	glib := writeLinuxPackageFile(t, filepath.Join(root, "lib", "libglib-2.0.so.0"), "glib")
	slirp := writeLinuxPackageFile(t, filepath.Join(root, "lib", "libslirp.so.0"), "slirp")
	libc := writeLinuxPackageFile(t, filepath.Join(root, "lib", "libc.so.6"), "libc")
	images := map[string]ELFImage{
		qemu:  {Path: qemu, Interpreter: loader, Needed: []string{"libglib-2.0.so.0", "libslirp.so.0"}},
		glib:  {Path: glib, Needed: []string{"libc.so.6"}},
		slirp: {Path: slirp, Needed: []string{"libc.so.6"}},
		libc:  {Path: libc},
	}

	closure, err := resolveELFClosure(qemu, []string{filepath.Join(root, "lib")}, func(path string) (ELFImage, error) {
		image, exists := images[path]
		if !exists {
			return ELFImage{}, fmt.Errorf("unexpected inspect path %s", path)
		}
		return image, nil
	})
	if err != nil {
		t.Fatalf("resolveELFClosure() error = %v", err)
	}
	if closure.Executable != qemu || closure.Loader != loader {
		t.Fatalf("closure identity = %#v", closure)
	}
	wantLibraries := []ELFLibrary{
		{Name: "libc.so.6", SourcePath: libc},
		{Name: "libglib-2.0.so.0", SourcePath: glib},
		{Name: "libslirp.so.0", SourcePath: slirp},
	}
	if !slices.Equal(closure.Libraries, wantLibraries) {
		t.Fatalf("Libraries = %#v, want %#v", closure.Libraries, wantLibraries)
	}
}

func TestResolveELFClosureRejectsInvalidDependencies(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(*testing.T, string) (string, []string, inspectELFFunc)
		wantError string
	}{
		{
			name: "missing interpreter",
			prepare: func(t *testing.T, root string) (string, []string, inspectELFFunc) {
				qemu := writeLinuxPackageFile(t, filepath.Join(root, "qemu"), "qemu")
				return qemu, []string{root}, func(string) (ELFImage, error) { return ELFImage{Path: qemu}, nil }
			},
			wantError: "ELF interpreter is required",
		},
		{
			name: "missing library",
			prepare: func(t *testing.T, root string) (string, []string, inspectELFFunc) {
				qemu := writeLinuxPackageFile(t, filepath.Join(root, "qemu"), "qemu")
				loader := writeLinuxPackageFile(t, filepath.Join(root, "ld-linux-x86-64.so.2"), "loader")
				return qemu, []string{root}, func(string) (ELFImage, error) {
					return ELFImage{Path: qemu, Interpreter: loader, Needed: []string{"libmissing.so"}}, nil
				}
			},
			wantError: "ELF dependency libmissing.so is missing",
		},
		{
			name: "conflicting library",
			prepare: func(t *testing.T, root string) (string, []string, inspectELFFunc) {
				firstRoot := filepath.Join(root, "first")
				secondRoot := filepath.Join(root, "second")
				writeLinuxPackageFile(t, filepath.Join(firstRoot, "libsame.so"), "first")
				writeLinuxPackageFile(t, filepath.Join(secondRoot, "libsame.so"), "second")
				qemu := writeLinuxPackageFile(t, filepath.Join(root, "qemu"), "qemu")
				return qemu, []string{firstRoot, secondRoot}, nil
			},
			wantError: "conflicting ELF library libsame.so",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			qemu, searchDirectories, inspect := test.prepare(t, t.TempDir())
			_, err := resolveELFClosure(qemu, searchDirectories, inspect)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("resolveELFClosure() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}
