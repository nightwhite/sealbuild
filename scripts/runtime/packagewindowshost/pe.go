package main

import (
	"debug/pe"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PEFile contains the architecture-relevant imports of one PE image.
type PEFile struct {
	Path    string
	Imports []string
}

// ResolvePEClosure returns the root executable followed by every private DLL.
func ResolvePEClosure(rootExecutable string, searchDirectories []string) ([]string, error) {
	return resolvePEClosure(rootExecutable, searchDirectories, inspectPE)
}

func resolvePEClosure(rootExecutable string, searchDirectories []string, inspect func(string) (PEFile, error)) ([]string, error) {
	rootPath, err := filepath.Abs(rootExecutable)
	if err != nil {
		return nil, fmt.Errorf("resolve root PE path: %w", err)
	}
	index, err := indexDLLs(searchDirectories)
	if err != nil {
		return nil, err
	}

	queue := []string{rootPath}
	visited := make(map[string]struct{})
	privateDLLs := make(map[string]string)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		key := canonicalPEPath(current)
		if _, exists := visited[key]; exists {
			continue
		}
		visited[key] = struct{}{}

		image, err := inspect(current)
		if err != nil {
			return nil, fmt.Errorf("inspect PE %s: %w", current, err)
		}
		for _, importedName := range image.Imports {
			if isWindowsSystemDLL(importedName) {
				continue
			}
			dependency, exists := index[strings.ToLower(importedName)]
			if !exists {
				return nil, fmt.Errorf("private DLL %s imported by %s is missing", importedName, current)
			}
			privateDLLs[canonicalPEPath(dependency)] = dependency
			queue = append(queue, dependency)
		}
	}

	dependencies := make([]string, 0, len(privateDLLs))
	for _, path := range privateDLLs {
		dependencies = append(dependencies, path)
	}
	sort.Slice(dependencies, func(first, second int) bool {
		return strings.ToLower(filepath.Base(dependencies[first])) < strings.ToLower(filepath.Base(dependencies[second]))
	})
	return append([]string{rootPath}, dependencies...), nil
}

func inspectPE(path string) (PEFile, error) {
	image, err := pe.Open(path)
	if err != nil {
		return PEFile{}, err
	}
	defer image.Close()
	if image.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return PEFile{}, fmt.Errorf("machine is %#x, expected AMD64", image.FileHeader.Machine)
	}
	imports, err := image.ImportedLibraries()
	if err != nil {
		return PEFile{}, fmt.Errorf("read import table: %w", err)
	}
	return PEFile{Path: path, Imports: imports}, nil
}

func indexDLLs(searchDirectories []string) (map[string]string, error) {
	if len(searchDirectories) == 0 {
		return nil, fmt.Errorf("at least one DLL search directory is required")
	}
	index := make(map[string]string)
	for _, directory := range searchDirectories {
		absoluteDirectory, err := filepath.Abs(directory)
		if err != nil {
			return nil, fmt.Errorf("resolve DLL directory %s: %w", directory, err)
		}
		entries, err := os.ReadDir(absoluteDirectory)
		if err != nil {
			return nil, fmt.Errorf("read DLL directory %s: %w", absoluteDirectory, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".dll") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return nil, fmt.Errorf("inspect DLL %s: %w", entry.Name(), err)
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("DLL %s must be a regular file", entry.Name())
			}
			name := strings.ToLower(entry.Name())
			path := filepath.Join(absoluteDirectory, entry.Name())
			if existing, exists := index[name]; exists && canonicalPEPath(existing) != canonicalPEPath(path) {
				return nil, fmt.Errorf("conflicting DLL %s exists at %s and %s", entry.Name(), existing, path)
			}
			index[name] = path
		}
	}
	return index, nil
}

func canonicalPEPath(path string) string {
	return strings.ToLower(filepath.Clean(path))
}
