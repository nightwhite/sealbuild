package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
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
	imports, err := importedLibraries(image)
	if err != nil {
		return PEFile{}, fmt.Errorf("read import table: %w", err)
	}
	return PEFile{Path: path, Imports: imports}, nil
}

func importedLibraries(image *pe.File) ([]string, error) {
	var directory pe.DataDirectory
	switch optionalHeader := image.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		if optionalHeader.NumberOfRvaAndSizes <= pe.IMAGE_DIRECTORY_ENTRY_IMPORT {
			return nil, nil
		}
		directory = optionalHeader.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_IMPORT]
	case *pe.OptionalHeader64:
		if optionalHeader.NumberOfRvaAndSizes <= pe.IMAGE_DIRECTORY_ENTRY_IMPORT {
			return nil, nil
		}
		directory = optionalHeader.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_IMPORT]
	default:
		return nil, fmt.Errorf("optional header has unsupported type %T", image.OptionalHeader)
	}
	if directory.VirtualAddress == 0 || directory.Size == 0 {
		return nil, nil
	}
	descriptors, err := peDataAtRVA(image, directory.VirtualAddress)
	if err != nil {
		return nil, fmt.Errorf("read import descriptors: %w", err)
	}
	if directory.Size > uint32(len(descriptors)) {
		return nil, fmt.Errorf("import directory size %d exceeds section data %d", directory.Size, len(descriptors))
	}
	return parsePEImportLibraries(descriptors[:directory.Size], func(rva uint32) (string, error) {
		data, err := peDataAtRVA(image, rva)
		if err != nil {
			return "", err
		}
		terminator := bytes.IndexByte(data, 0)
		if terminator < 1 {
			return "", fmt.Errorf("DLL name at RVA %#x is empty or unterminated", rva)
		}
		if terminator > 4096 {
			return "", fmt.Errorf("DLL name at RVA %#x exceeds 4096 bytes", rva)
		}
		return string(data[:terminator]), nil
	})
}

func parsePEImportLibraries(descriptors []byte, readName func(uint32) (string, error)) ([]string, error) {
	const descriptorSize = 20
	libraries := make([]string, 0)
	seen := make(map[string]struct{})
	for offset := 0; offset+descriptorSize <= len(descriptors); offset += descriptorSize {
		descriptor := descriptors[offset : offset+descriptorSize]
		if bytes.Equal(descriptor, make([]byte, descriptorSize)) {
			return libraries, nil
		}
		nameRVA := binary.LittleEndian.Uint32(descriptor[12:16])
		if nameRVA == 0 {
			return nil, fmt.Errorf("import descriptor at offset %d has no DLL name", offset)
		}
		name, err := readName(nameRVA)
		if err != nil {
			return nil, fmt.Errorf("read DLL name at RVA %#x: %w", nameRVA, err)
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		libraries = append(libraries, name)
	}
	return nil, fmt.Errorf("import directory is not terminated")
}

func peDataAtRVA(image *pe.File, rva uint32) ([]byte, error) {
	for _, section := range image.Sections {
		if rva < section.VirtualAddress || rva-section.VirtualAddress >= section.Size {
			continue
		}
		data, err := section.Data()
		if err != nil {
			return nil, err
		}
		return data[rva-section.VirtualAddress:], nil
	}
	return nil, fmt.Errorf("RVA %#x is outside file-backed sections", rva)
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
