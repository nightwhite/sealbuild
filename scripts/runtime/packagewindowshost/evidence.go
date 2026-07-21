package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func runCollectRuntimePackages(args []string) error {
	flags := flag.NewFlagSet("packagewindowshost collect-runtime-packages", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var qemuPath, pacmanPath, cygpathPath, outputPath string
	var dllDirectories stringList
	flags.StringVar(&qemuPath, "qemu", "", "QEMU executable path")
	flags.Var(&dllDirectories, "dll-dir", "DLL search directory; repeatable")
	flags.StringVar(&pacmanPath, "pacman", "", "fixed pacman executable path")
	flags.StringVar(&cygpathPath, "cygpath", "", "fixed cygpath executable path")
	flags.StringVar(&outputPath, "output", "", "runtime package evidence output path")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse collect-runtime-packages arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional argument: %s", flags.Arg(0))
	}
	for _, required := range []struct{ name, value string }{
		{"--qemu", qemuPath},
		{"--pacman", pacmanPath},
		{"--cygpath", cygpathPath},
		{"--output", outputPath},
	} {
		if required.value == "" {
			return fmt.Errorf("%s is required", required.name)
		}
	}
	if len(dllDirectories) == 0 {
		return fmt.Errorf("--dll-dir is required")
	}
	closure, err := ResolvePEClosure(qemuPath, dllDirectories)
	if err != nil {
		return fmt.Errorf("resolve QEMU PE dependency closure: %w", err)
	}
	evidence, err := collectRuntimePackageEvidence(closure, func(path string) (LockedRuntimePackage, error) {
		return queryMSYS2PackageOwner(path, cygpathPath, pacmanPath)
	})
	if err != nil {
		return err
	}
	return writeRuntimePackageEvidence(outputPath, evidence)
}

func collectRuntimePackageEvidence(closure []string, owner func(string) (LockedRuntimePackage, error)) (RuntimePackageEvidence, error) {
	if len(closure) < 2 {
		return RuntimePackageEvidence{}, fmt.Errorf("QEMU PE dependency closure contains no private DLLs")
	}
	dlls := make([]RuntimePackageDLL, 0, len(closure)-1)
	for _, path := range closure[1:] {
		runtimePackage, err := owner(path)
		if err != nil {
			return RuntimePackageEvidence{}, fmt.Errorf("resolve MSYS2 package owner for %s: %w", filepath.Base(path), err)
		}
		dlls = append(dlls, RuntimePackageDLL{
			Name: filepath.Base(path), Package: runtimePackage.Name, Version: runtimePackage.Version,
		})
	}
	sort.Slice(dlls, func(first, second int) bool {
		return strings.ToLower(dlls[first].Name) < strings.ToLower(dlls[second].Name)
	})
	evidence := RuntimePackageEvidence{SchemaVersion: 1, DLLs: dlls}
	if err := evidence.validate(); err != nil {
		return RuntimePackageEvidence{}, err
	}
	return evidence, nil
}

func queryMSYS2PackageOwner(path, cygpathPath, pacmanPath string) (LockedRuntimePackage, error) {
	unixPathOutput, err := exec.Command(cygpathPath, "-u", path).Output()
	if err != nil {
		return LockedRuntimePackage{}, fmt.Errorf("convert DLL path with cygpath: %w", err)
	}
	unixPath := strings.TrimSpace(string(unixPathOutput))
	if unixPath == "" || strings.ContainsAny(unixPath, "\r\n") {
		return LockedRuntimePackage{}, fmt.Errorf("cygpath returned invalid path %q", unixPath)
	}
	packageNameOutput, err := exec.Command(pacmanPath, "-Qoq", unixPath).Output()
	if err != nil {
		return LockedRuntimePackage{}, fmt.Errorf("query package owner with pacman: %w", err)
	}
	packageName := strings.TrimSpace(string(packageNameOutput))
	if packageName == "" || strings.ContainsAny(packageName, " \t\r\n") {
		return LockedRuntimePackage{}, fmt.Errorf("pacman -Qoq returned invalid package name %q", packageName)
	}
	packageOutput, err := exec.Command(pacmanPath, "-Q", packageName).Output()
	if err != nil {
		return LockedRuntimePackage{}, fmt.Errorf("query package version with pacman: %w", err)
	}
	runtimePackage, err := parsePacmanPackage(string(packageOutput))
	if err != nil {
		return LockedRuntimePackage{}, err
	}
	if runtimePackage.Name != packageName {
		return LockedRuntimePackage{}, fmt.Errorf("pacman package name is %s, expected %s", runtimePackage.Name, packageName)
	}
	return runtimePackage, nil
}

func parsePacmanPackage(output string) (LockedRuntimePackage, error) {
	fields := strings.Fields(output)
	if len(fields) != 2 {
		return LockedRuntimePackage{}, fmt.Errorf("pacman -Q output must contain exactly package and version")
	}
	return LockedRuntimePackage{Name: fields[0], Version: fields[1]}, nil
}

func writeRuntimePackageEvidence(path string, evidence RuntimePackageEvidence) (returnErr error) {
	output, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create Windows runtime package evidence: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, output.Close()) }()
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(evidence); err != nil {
		return fmt.Errorf("encode Windows runtime package evidence: %w", err)
	}
	if err := output.Sync(); err != nil {
		return fmt.Errorf("sync Windows runtime package evidence: %w", err)
	}
	return nil
}
