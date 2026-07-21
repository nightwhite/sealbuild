package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type MachODependency struct {
	InstallName string
	SourcePath  string
	BaseName    string
}

type MachOFile struct {
	SourcePath   string
	Dependencies []MachODependency
}

type DependencyGraph struct {
	Executable MachOFile
	Libraries  []MachOFile
}

func ResolveDependencies(ctx context.Context, runner Runner, executable, libraryRoot string) (DependencyGraph, error) {
	resolvedExecutable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return DependencyGraph{}, fmt.Errorf("resolve QEMU executable path: %w", err)
	}
	resolvedLibraryRoot, err := filepath.EvalSymlinks(libraryRoot)
	if err != nil {
		return DependencyGraph{}, fmt.Errorf("resolve allowed library root: %w", err)
	}

	executableDependencies, err := inspectDependencies(ctx, runner, resolvedExecutable, resolvedLibraryRoot)
	if err != nil {
		return DependencyGraph{}, err
	}
	graph := DependencyGraph{
		Executable: MachOFile{SourcePath: resolvedExecutable, Dependencies: executableDependencies},
	}

	libraryFiles := make(map[string]MachOFile)
	baseNames := make(map[string]string)
	queue := append([]MachODependency(nil), executableDependencies...)
	for len(queue) > 0 {
		dependency := queue[0]
		queue = queue[1:]

		if previousPath, exists := baseNames[dependency.BaseName]; exists && previousPath != dependency.SourcePath {
			return DependencyGraph{}, fmt.Errorf(
				"library basename %s resolves to multiple files: %s and %s",
				dependency.BaseName,
				previousPath,
				dependency.SourcePath,
			)
		}
		baseNames[dependency.BaseName] = dependency.SourcePath
		if _, exists := libraryFiles[dependency.SourcePath]; exists {
			continue
		}

		dependencies, err := inspectDependencies(ctx, runner, dependency.SourcePath, resolvedLibraryRoot)
		if err != nil {
			return DependencyGraph{}, err
		}
		libraryFiles[dependency.SourcePath] = MachOFile{
			SourcePath:   dependency.SourcePath,
			Dependencies: dependencies,
		}
		queue = append(queue, dependencies...)
	}

	graph.Libraries = make([]MachOFile, 0, len(libraryFiles))
	for _, library := range libraryFiles {
		graph.Libraries = append(graph.Libraries, library)
	}
	sort.Slice(graph.Libraries, func(first, second int) bool {
		return filepath.Base(graph.Libraries[first].SourcePath) < filepath.Base(graph.Libraries[second].SourcePath)
	})
	return graph, nil
}

func IsSystemDependency(installName string) bool {
	return strings.HasPrefix(installName, "/usr/lib/") || strings.HasPrefix(installName, "/System/Library/")
}

func RelocateMachO(ctx context.Context, runner Runner, graph DependencyGraph, payloadRoot string) error {
	binDirectory := filepath.Join(payloadRoot, "bin")
	libraryDirectory := filepath.Join(payloadRoot, "lib")
	if err := os.MkdirAll(binDirectory, 0o755); err != nil {
		return fmt.Errorf("create Host Runtime bin directory: %w", err)
	}
	if err := os.MkdirAll(libraryDirectory, 0o755); err != nil {
		return fmt.Errorf("create Host Runtime library directory: %w", err)
	}

	qemuOutput := filepath.Join(binDirectory, "qemu-system-x86_64")
	if err := copyRegularFile(graph.Executable.SourcePath, qemuOutput, 0o755); err != nil {
		return fmt.Errorf("copy QEMU executable: %w", err)
	}
	libraryOutputs := make(map[string]string, len(graph.Libraries))
	for _, library := range graph.Libraries {
		baseName := filepath.Base(library.SourcePath)
		outputPath := filepath.Join(libraryDirectory, baseName)
		if err := copyRegularFile(library.SourcePath, outputPath, 0o644); err != nil {
			return fmt.Errorf("copy QEMU library %s: %w", baseName, err)
		}
		libraryOutputs[library.SourcePath] = outputPath
	}

	if _, err := runner.Run(ctx, "strip", "-x", qemuOutput); err != nil {
		return fmt.Errorf("strip QEMU executable: %w", err)
	}
	for _, library := range graph.Libraries {
		outputPath := libraryOutputs[library.SourcePath]
		if _, err := runner.Run(ctx, "strip", "-x", outputPath); err != nil {
			return fmt.Errorf("strip QEMU library %s: %w", filepath.Base(outputPath), err)
		}
	}

	for _, library := range graph.Libraries {
		outputPath := libraryOutputs[library.SourcePath]
		installID := "@loader_path/" + filepath.Base(outputPath)
		if _, err := runner.Run(ctx, "install_name_tool", "-id", installID, outputPath); err != nil {
			return fmt.Errorf("rewrite QEMU library ID %s: %w", filepath.Base(outputPath), err)
		}
	}
	for _, dependency := range graph.Executable.Dependencies {
		newInstallName := "@loader_path/../lib/" + dependency.BaseName
		if _, err := runner.Run(ctx, "install_name_tool", "-change", dependency.InstallName, newInstallName, qemuOutput); err != nil {
			return fmt.Errorf("rewrite QEMU dependency %s: %w", dependency.InstallName, err)
		}
	}
	for _, library := range graph.Libraries {
		outputPath := libraryOutputs[library.SourcePath]
		for _, dependency := range library.Dependencies {
			newInstallName := "@loader_path/" + dependency.BaseName
			if _, err := runner.Run(ctx, "install_name_tool", "-change", dependency.InstallName, newInstallName, outputPath); err != nil {
				return fmt.Errorf("rewrite %s dependency %s: %w", filepath.Base(outputPath), dependency.InstallName, err)
			}
		}
	}

	for _, library := range graph.Libraries {
		outputPath := libraryOutputs[library.SourcePath]
		if _, err := runner.Run(ctx, "codesign", "--force", "--sign", "-", outputPath); err != nil {
			return fmt.Errorf("sign QEMU library %s: %w", filepath.Base(outputPath), err)
		}
	}
	if _, err := runner.Run(ctx, "codesign", "--force", "--sign", "-", qemuOutput); err != nil {
		return fmt.Errorf("sign QEMU executable: %w", err)
	}

	for _, library := range graph.Libraries {
		outputPath := libraryOutputs[library.SourcePath]
		if _, err := runner.Run(ctx, "codesign", "--verify", "--strict", outputPath); err != nil {
			return fmt.Errorf("verify QEMU library signature %s: %w", filepath.Base(outputPath), err)
		}
	}
	if _, err := runner.Run(ctx, "codesign", "--verify", "--strict", qemuOutput); err != nil {
		return fmt.Errorf("verify QEMU executable signature: %w", err)
	}

	for _, library := range graph.Libraries {
		outputPath := libraryOutputs[library.SourcePath]
		if err := verifyRelocatedDependencies(ctx, runner, outputPath, libraryDirectory, false); err != nil {
			return err
		}
	}
	if err := verifyRelocatedDependencies(ctx, runner, qemuOutput, libraryDirectory, true); err != nil {
		return err
	}
	return nil
}

func inspectDependencies(ctx context.Context, runner Runner, machOPath, libraryRoot string) ([]MachODependency, error) {
	output, err := runner.Run(ctx, "otool", "-L", machOPath)
	if err != nil {
		return nil, fmt.Errorf("inspect Mach-O dependencies for %s: %w", machOPath, err)
	}
	installNames, err := parseOtoolLibraries(output)
	if err != nil {
		return nil, fmt.Errorf("parse Mach-O dependencies for %s: %w", machOPath, err)
	}

	dependencies := make([]MachODependency, 0, len(installNames))
	for _, installName := range installNames {
		if IsSystemDependency(installName) {
			continue
		}
		if !filepath.IsAbs(installName) {
			return nil, fmt.Errorf("unsupported non-absolute dependency %s in %s", installName, machOPath)
		}
		resolvedPath, err := filepath.EvalSymlinks(installName)
		if err != nil {
			return nil, fmt.Errorf("resolve Mach-O dependency %s: %w", installName, err)
		}
		if resolvedPath == machOPath {
			continue
		}
		if !pathWithin(libraryRoot, resolvedPath) {
			return nil, fmt.Errorf("Mach-O dependency %s is outside allowed library root %s", resolvedPath, libraryRoot)
		}
		dependencies = append(dependencies, MachODependency{
			InstallName: installName,
			SourcePath:  resolvedPath,
			BaseName:    filepath.Base(resolvedPath),
		})
	}
	sort.Slice(dependencies, func(first, second int) bool {
		return dependencies[first].BaseName < dependencies[second].BaseName
	})
	return dependencies, nil
}

func parseOtoolLibraries(output []byte) ([]string, error) {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 1 || !strings.HasSuffix(strings.TrimSpace(lines[0]), ":") {
		return nil, fmt.Errorf("unexpected otool output")
	}

	installNames := make([]string, 0, len(lines)-1)
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		metadataStart := strings.Index(line, " (")
		if metadataStart <= 0 {
			return nil, fmt.Errorf("unexpected otool dependency line %q", line)
		}
		installNames = append(installNames, line[:metadataStart])
	}
	return installNames, nil
}

func verifyRelocatedDependencies(
	ctx context.Context,
	runner Runner,
	machOPath string,
	libraryDirectory string,
	executable bool,
) error {
	output, err := runner.Run(ctx, "otool", "-L", machOPath)
	if err != nil {
		return fmt.Errorf("verify relocated dependencies for %s: %w", machOPath, err)
	}
	installNames, err := parseOtoolLibraries(output)
	if err != nil {
		return fmt.Errorf("parse relocated dependencies for %s: %w", machOPath, err)
	}

	for _, installName := range installNames {
		if IsSystemDependency(installName) {
			continue
		}
		prefix := "@loader_path/"
		if executable {
			prefix = "@loader_path/../lib/"
		}
		if !strings.HasPrefix(installName, prefix) {
			return fmt.Errorf("Mach-O file %s contains non-relocated dependency %s", machOPath, installName)
		}
		baseName := strings.TrimPrefix(installName, prefix)
		if baseName == "" || filepath.Base(baseName) != baseName {
			return fmt.Errorf("Mach-O file %s contains invalid relocated dependency %s", machOPath, installName)
		}
		if _, err := os.Stat(filepath.Join(libraryDirectory, baseName)); err != nil {
			return fmt.Errorf("Mach-O file %s references missing relocated dependency %s: %w", machOPath, baseName, err)
		}
	}
	return nil
}

func copyRegularFile(sourcePath, destinationPath string, mode os.FileMode) (returnErr error) {
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return fmt.Errorf("inspect source file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source file must be regular")
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, source.Close())
	}()
	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, destination.Close())
	}()
	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("copy file contents: %w", err)
	}
	if err := destination.Chmod(mode); err != nil {
		return fmt.Errorf("set destination permissions: %w", err)
	}
	if err := destination.Sync(); err != nil {
		return fmt.Errorf("sync destination file: %w", err)
	}
	return nil
}

func pathWithin(root, candidate string) bool {
	relativePath, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(filepath.Separator))
}
