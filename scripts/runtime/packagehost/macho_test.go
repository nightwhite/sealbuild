package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestResolveDependenciesReturnsRecursiveNonSystemGraph(t *testing.T) {
	root := t.TempDir()
	qemu := createEmptyFile(t, root, "qemu-system-x86_64")
	glib := createEmptyFile(t, root, "glib/libglib-2.0.0.dylib")
	intl := createEmptyFile(t, root, "gettext/libintl.8.dylib")
	runner := &fakeRunner{outputs: map[string][]byte{
		commandKey("otool", "-L", qemu): []byte(fmt.Sprintf(
			"%s:\n\t%s (compatibility version 1.0.0, current version 1.0.0)\n\t/usr/lib/libSystem.B.dylib (compatibility version 1.0.0, current version 1.0.0)\n",
			qemu,
			glib,
		)),
		commandKey("otool", "-L", glib): []byte(fmt.Sprintf(
			"%s:\n\t%s (compatibility version 1.0.0, current version 1.0.0)\n\t%s (compatibility version 1.0.0, current version 1.0.0)\n",
			glib,
			glib,
			intl,
		)),
		commandKey("otool", "-L", intl): []byte(fmt.Sprintf(
			"%s:\n\t%s (compatibility version 1.0.0, current version 1.0.0)\n\t/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation (compatibility version 1.0.0, current version 1.0.0)\n",
			intl,
			intl,
		)),
	}}

	graph, err := ResolveDependencies(t.Context(), runner, qemu, root)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}
	if graph.Executable.SourcePath != qemu {
		t.Fatalf("Executable.SourcePath = %q, want %q", graph.Executable.SourcePath, qemu)
	}
	if len(graph.Executable.Dependencies) != 1 || graph.Executable.Dependencies[0].SourcePath != glib {
		t.Fatalf("Executable.Dependencies = %#v, want glib", graph.Executable.Dependencies)
	}
	if len(graph.Libraries) != 2 {
		t.Fatalf("len(Libraries) = %d, want 2", len(graph.Libraries))
	}
	if graph.Libraries[0].SourcePath != glib || graph.Libraries[1].SourcePath != intl {
		t.Fatalf("Libraries = %#v, want glib then intl", graph.Libraries)
	}
	if len(graph.Libraries[0].Dependencies) != 1 || graph.Libraries[0].Dependencies[0].SourcePath != intl {
		t.Fatalf("glib dependencies = %#v, want intl", graph.Libraries[0].Dependencies)
	}
	if len(graph.Libraries[1].Dependencies) != 0 {
		t.Fatalf("intl dependencies = %#v, want none", graph.Libraries[1].Dependencies)
	}
}

func TestResolveDependenciesRejectsInvalidGraph(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(*testing.T, string) (string, *fakeRunner)
		wantError string
	}{
		{
			name: "relative install name",
			prepare: func(t *testing.T, root string) (string, *fakeRunner) {
				qemu := createEmptyFile(t, root, "qemu")
				return qemu, &fakeRunner{outputs: map[string][]byte{
					commandKey("otool", "-L", qemu): []byte(qemu + ":\n\t@rpath/libglib.dylib (compatibility version 1.0.0, current version 1.0.0)\n"),
				}}
			},
			wantError: "unsupported non-absolute dependency @rpath/libglib.dylib",
		},
		{
			name: "dependency outside library root",
			prepare: func(t *testing.T, root string) (string, *fakeRunner) {
				qemu := createEmptyFile(t, root, "qemu")
				outside := createEmptyFile(t, t.TempDir(), "liboutside.dylib")
				return qemu, &fakeRunner{outputs: map[string][]byte{
					commandKey("otool", "-L", qemu): []byte(fmt.Sprintf("%s:\n\t%s (compatibility version 1.0.0, current version 1.0.0)\n", qemu, outside)),
				}}
			},
			wantError: "is outside allowed library root",
		},
		{
			name: "duplicate library basename",
			prepare: func(t *testing.T, root string) (string, *fakeRunner) {
				qemu := createEmptyFile(t, root, "qemu")
				first := createEmptyFile(t, root, "first/libsame.dylib")
				second := createEmptyFile(t, root, "second/libsame.dylib")
				return qemu, &fakeRunner{outputs: map[string][]byte{
					commandKey("otool", "-L", qemu): []byte(fmt.Sprintf(
						"%s:\n\t%s (compatibility version 1.0.0, current version 1.0.0)\n\t%s (compatibility version 1.0.0, current version 1.0.0)\n",
						qemu,
						first,
						second,
					)),
					commandKey("otool", "-L", first):  []byte(fmt.Sprintf("%s:\n\t%s (compatibility version 1.0.0, current version 1.0.0)\n", first, first)),
					commandKey("otool", "-L", second): []byte(fmt.Sprintf("%s:\n\t%s (compatibility version 1.0.0, current version 1.0.0)\n", second, second)),
				}}
			},
			wantError: "library basename libsame.dylib resolves to multiple files",
		},
		{
			name: "otool failure",
			prepare: func(t *testing.T, root string) (string, *fakeRunner) {
				qemu := createEmptyFile(t, root, "qemu")
				return qemu, &fakeRunner{errors: map[string]error{
					commandKey("otool", "-L", qemu): errors.New("otool failed"),
				}}
			},
			wantError: "inspect Mach-O dependencies",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			qemu, runner := test.prepare(t, root)

			_, err := ResolveDependencies(t.Context(), runner, qemu, root)
			if err == nil {
				t.Fatal("ResolveDependencies() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("ResolveDependencies() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestIsSystemDependency(t *testing.T) {
	tests := []struct {
		installName string
		want        bool
	}{
		{"/usr/lib/libSystem.B.dylib", true},
		{"/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation", true},
		{"/opt/homebrew/opt/glib/lib/libglib-2.0.0.dylib", false},
		{"@rpath/libglib-2.0.0.dylib", false},
	}

	for _, test := range tests {
		if got := IsSystemDependency(test.installName); got != test.want {
			t.Errorf("IsSystemDependency(%q) = %t, want %t", test.installName, got, test.want)
		}
	}
}

func TestValidateMachOArchitecture(t *testing.T) {
	tests := []struct {
		name         string
		architecture string
		output       string
		wantError    string
	}{
		{name: "ARM64", architecture: "arm64", output: "arm64\n"},
		{name: "AMD64", architecture: "amd64", output: "x86_64\n"},
		{name: "wrong architecture", architecture: "amd64", output: "arm64\n", wantError: "architectures are arm64, expected x86_64"},
		{name: "Universal rejected", architecture: "arm64", output: "x86_64 arm64\n", wantError: "architectures are x86_64 arm64, expected arm64"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := "/tmp/qemu-system-x86_64"
			runner := &fakeRunner{outputs: map[string][]byte{
				commandKey("lipo", "-archs", path): []byte(test.output),
			}}

			err := ValidateMachOArchitecture(t.Context(), runner, path, test.architecture)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("ValidateMachOArchitecture() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("ValidateMachOArchitecture() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestRelocateMachOCopiesRewritesSignsAndVerifies(t *testing.T) {
	sourceRoot := t.TempDir()
	qemuSource := createEmptyFile(t, sourceRoot, "qemu-system-x86_64")
	glibSource := createEmptyFile(t, sourceRoot, "libglib-2.0.0.dylib")
	intlSource := createEmptyFile(t, sourceRoot, "libintl.8.dylib")
	graph := DependencyGraph{
		Executable: MachOFile{
			SourcePath: qemuSource,
			Dependencies: []MachODependency{{
				InstallName: "/opt/homebrew/opt/glib/lib/libglib-2.0.0.dylib",
				SourcePath:  glibSource,
				BaseName:    "libglib-2.0.0.dylib",
			}},
		},
		Libraries: []MachOFile{
			{
				SourcePath: glibSource,
				Dependencies: []MachODependency{{
					InstallName: "/opt/homebrew/opt/gettext/lib/libintl.8.dylib",
					SourcePath:  intlSource,
					BaseName:    "libintl.8.dylib",
				}},
			},
			{SourcePath: intlSource},
		},
	}
	payloadRoot := t.TempDir()
	qemuOutput := filepath.Join(payloadRoot, "bin", "qemu-system-x86_64")
	glibOutput := filepath.Join(payloadRoot, "lib", "libglib-2.0.0.dylib")
	intlOutput := filepath.Join(payloadRoot, "lib", "libintl.8.dylib")

	runner := &fakeRunner{outputs: map[string][]byte{}}
	mutationCalls := []string{
		commandKey("strip", "-x", qemuOutput),
		commandKey("strip", "-x", glibOutput),
		commandKey("strip", "-x", intlOutput),
		commandKey("install_name_tool", "-id", "@loader_path/libglib-2.0.0.dylib", glibOutput),
		commandKey("install_name_tool", "-id", "@loader_path/libintl.8.dylib", intlOutput),
		commandKey("install_name_tool", "-change", "/opt/homebrew/opt/glib/lib/libglib-2.0.0.dylib", "@loader_path/../lib/libglib-2.0.0.dylib", qemuOutput),
		commandKey("install_name_tool", "-change", "/opt/homebrew/opt/gettext/lib/libintl.8.dylib", "@loader_path/libintl.8.dylib", glibOutput),
		commandKey("codesign", "--force", "--sign", "-", glibOutput),
		commandKey("codesign", "--force", "--sign", "-", intlOutput),
		commandKey("codesign", "--force", "--sign", "-", qemuOutput),
		commandKey("codesign", "--verify", "--strict", glibOutput),
		commandKey("codesign", "--verify", "--strict", intlOutput),
		commandKey("codesign", "--verify", "--strict", qemuOutput),
	}
	for _, call := range mutationCalls {
		runner.outputs[call] = nil
	}
	runner.outputs[commandKey("otool", "-L", intlOutput)] = []byte(fmt.Sprintf(
		"%s:\n\t@loader_path/libintl.8.dylib (compatibility version 1.0.0, current version 1.0.0)\n\t/usr/lib/libSystem.B.dylib (compatibility version 1.0.0, current version 1.0.0)\n",
		intlOutput,
	))
	runner.outputs[commandKey("otool", "-L", glibOutput)] = []byte(fmt.Sprintf(
		"%s:\n\t@loader_path/libglib-2.0.0.dylib (compatibility version 1.0.0, current version 1.0.0)\n\t@loader_path/libintl.8.dylib (compatibility version 1.0.0, current version 1.0.0)\n",
		glibOutput,
	))
	runner.outputs[commandKey("otool", "-L", qemuOutput)] = []byte(fmt.Sprintf(
		"%s:\n\t@loader_path/../lib/libglib-2.0.0.dylib (compatibility version 1.0.0, current version 1.0.0)\n\t/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation (compatibility version 1.0.0, current version 1.0.0)\n",
		qemuOutput,
	))

	if err := RelocateMachO(t.Context(), runner, graph, payloadRoot); err != nil {
		t.Fatalf("RelocateMachO() error = %v", err)
	}
	for _, outputPath := range []string{qemuOutput, glibOutput, intlOutput} {
		contents, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", outputPath, err)
		}
		if string(contents) != "mach-o" {
			t.Errorf("%s contents = %q, want mach-o", outputPath, contents)
		}
	}

	expectedCalls := append([]string(nil), mutationCalls...)
	expectedCalls = append(expectedCalls,
		commandKey("otool", "-L", glibOutput),
		commandKey("otool", "-L", intlOutput),
		commandKey("otool", "-L", qemuOutput),
	)
	if !slices.Equal(runner.calls, expectedCalls) {
		t.Fatalf("runner calls = %#v, want %#v", runner.calls, expectedCalls)
	}
}

func TestRelocateMachORejectsRemainingAbsoluteDependency(t *testing.T) {
	sourceRoot := t.TempDir()
	qemuSource := createEmptyFile(t, sourceRoot, "qemu-system-x86_64")
	payloadRoot := t.TempDir()
	qemuOutput := filepath.Join(payloadRoot, "bin", "qemu-system-x86_64")
	graph := DependencyGraph{Executable: MachOFile{SourcePath: qemuSource}}
	runner := &fakeRunner{outputs: map[string][]byte{
		commandKey("strip", "-x", qemuOutput):                        nil,
		commandKey("codesign", "--force", "--sign", "-", qemuOutput): nil,
		commandKey("codesign", "--verify", "--strict", qemuOutput):   nil,
		commandKey("otool", "-L", qemuOutput): []byte(fmt.Sprintf(
			"%s:\n\t/opt/homebrew/opt/glib/lib/libglib-2.0.0.dylib (compatibility version 1.0.0, current version 1.0.0)\n",
			qemuOutput,
		)),
	}}

	err := RelocateMachO(t.Context(), runner, graph, payloadRoot)
	if err == nil {
		t.Fatal("RelocateMachO() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "contains non-relocated dependency") {
		t.Fatalf("RelocateMachO() error = %q, want non-relocated dependency", err)
	}
}

type fakeRunner struct {
	outputs map[string][]byte
	errors  map[string]error
	calls   []string
}

func (runner *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := commandKey(name, args...)
	runner.calls = append(runner.calls, key)
	if err := runner.errors[key]; err != nil {
		return nil, err
	}
	output, exists := runner.outputs[key]
	if !exists {
		return nil, fmt.Errorf("unexpected command: %s", key)
	}
	return output, nil
}

func commandKey(name string, args ...string) string {
	return strings.Join(append([]string{name}, args...), "\x00")
}

func createEmptyFile(t *testing.T, root, relativePath string) string {
	t.Helper()
	filePath := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filePath, []byte("mach-o"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	resolved, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	return resolved
}
