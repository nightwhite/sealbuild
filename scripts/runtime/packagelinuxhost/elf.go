package main

import (
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ELFImage struct {
	Path        string
	Interpreter string
	Needed      []string
	RPath       []string
	RunPath     []string
}

type ELFLibrary struct {
	Name       string
	SourcePath string
}

type ELFClosure struct {
	Executable string
	Loader     string
	Libraries  []ELFLibrary
}

type inspectELFFunc func(string) (ELFImage, error)

func ResolveELFClosure(executable string, searchDirectories []string) (ELFClosure, error) {
	return resolveELFClosure(executable, searchDirectories, inspectELF)
}

func resolveELFClosure(executable string, searchDirectories []string, inspect inspectELFFunc) (ELFClosure, error) {
	index, roots, err := indexELFLibraries(searchDirectories)
	if err != nil {
		return ELFClosure{}, err
	}
	resolvedExecutable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return ELFClosure{}, fmt.Errorf("resolve QEMU ELF path: %w", err)
	}
	if inspect == nil {
		return ELFClosure{}, fmt.Errorf("ELF inspector is required")
	}
	rootImage, err := inspect(resolvedExecutable)
	if err != nil {
		return ELFClosure{}, fmt.Errorf("inspect QEMU ELF: %w", err)
	}
	if rootImage.Interpreter == "" {
		return ELFClosure{}, fmt.Errorf("QEMU ELF interpreter is required")
	}
	if !filepath.IsAbs(rootImage.Interpreter) {
		return ELFClosure{}, fmt.Errorf("QEMU ELF interpreter must be absolute")
	}
	loader, err := resolveAllowedFile(rootImage.Interpreter, roots)
	if err != nil {
		return ELFClosure{}, fmt.Errorf("resolve QEMU ELF interpreter: %w", err)
	}

	libraries := make(map[string]string)
	visited := make(map[string]struct{})
	queue := append([]string(nil), rootImage.Needed...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		sourcePath, exists := index[name]
		if !exists {
			return ELFClosure{}, fmt.Errorf("ELF dependency %s is missing", name)
		}
		if previous, exists := libraries[name]; exists && previous != sourcePath {
			return ELFClosure{}, fmt.Errorf("conflicting ELF library %s resolves to %s and %s", name, previous, sourcePath)
		}
		libraries[name] = sourcePath
		if _, exists := visited[sourcePath]; exists {
			continue
		}
		visited[sourcePath] = struct{}{}
		image, err := inspect(sourcePath)
		if err != nil {
			return ELFClosure{}, fmt.Errorf("inspect ELF dependency %s: %w", name, err)
		}
		queue = append(queue, image.Needed...)
	}

	closure := ELFClosure{Executable: resolvedExecutable, Loader: loader}
	for name, sourcePath := range libraries {
		closure.Libraries = append(closure.Libraries, ELFLibrary{Name: name, SourcePath: sourcePath})
	}
	sort.Slice(closure.Libraries, func(first, second int) bool {
		return closure.Libraries[first].Name < closure.Libraries[second].Name
	})
	return closure, nil
}

func inspectELF(path string) (image ELFImage, returnErr error) {
	file, err := elf.Open(path)
	if err != nil {
		return ELFImage{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	if file.FileHeader.Machine != elf.EM_X86_64 {
		return ELFImage{}, fmt.Errorf("machine is %s, expected EM_X86_64", file.FileHeader.Machine)
	}
	image.Path = path
	if section := file.Section(".interp"); section != nil {
		contents, err := section.Data()
		if err != nil {
			return ELFImage{}, fmt.Errorf("read ELF interpreter: %w", err)
		}
		interpreter := strings.TrimSuffix(string(contents), "\x00")
		if interpreter == "" || strings.ContainsRune(interpreter, '\x00') {
			return ELFImage{}, fmt.Errorf("ELF interpreter is empty or malformed")
		}
		image.Interpreter = interpreter
	}
	image.Needed, err = file.ImportedLibraries()
	if err != nil {
		return ELFImage{}, fmt.Errorf("read ELF imported libraries: %w", err)
	}
	image.RPath, err = file.DynString(elf.DT_RPATH)
	if err != nil {
		return ELFImage{}, fmt.Errorf("read ELF RPATH: %w", err)
	}
	image.RunPath, err = file.DynString(elf.DT_RUNPATH)
	if err != nil {
		return ELFImage{}, fmt.Errorf("read ELF RUNPATH: %w", err)
	}
	return image, nil
}

func indexELFLibraries(searchDirectories []string) (map[string]string, []string, error) {
	if len(searchDirectories) == 0 {
		return nil, nil, fmt.Errorf("at least one ELF library directory is required")
	}
	roots := make([]string, 0, len(searchDirectories))
	for _, directory := range searchDirectories {
		root, err := filepath.EvalSymlinks(directory)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve ELF library directory %s: %w", directory, err)
		}
		info, err := os.Stat(root)
		if err != nil {
			return nil, nil, fmt.Errorf("inspect ELF library directory %s: %w", root, err)
		}
		if !info.IsDir() {
			return nil, nil, fmt.Errorf("ELF library path must be a directory: %s", root)
		}
		roots = append(roots, root)
	}

	index := make(map[string]string)
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, nil, fmt.Errorf("read ELF library directory %s: %w", root, err)
		}
		for _, entry := range entries {
			path := filepath.Join(root, entry.Name())
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil, nil, fmt.Errorf("resolve ELF library %s: %w", path, err)
			}
			info, err := os.Stat(resolved)
			if err != nil {
				return nil, nil, fmt.Errorf("inspect ELF library %s: %w", resolved, err)
			}
			if !info.Mode().IsRegular() {
				continue
			}
			if !pathWithinAnyRoot(resolved, roots) {
				continue
			}
			if previous, exists := index[entry.Name()]; exists && previous != resolved {
				return nil, nil, fmt.Errorf("conflicting ELF library %s exists at %s and %s", entry.Name(), previous, resolved)
			}
			index[entry.Name()] = resolved
		}
	}
	return index, roots, nil
}

func resolveAllowedFile(path string, roots []string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path must be a regular file")
	}
	if !pathWithinAnyRoot(resolved, roots) {
		return "", fmt.Errorf("path resolves outside allowed directories")
	}
	return resolved, nil
}

func pathWithinAnyRoot(path string, roots []string) bool {
	for _, root := range roots {
		relative, err := filepath.Rel(root, path)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
