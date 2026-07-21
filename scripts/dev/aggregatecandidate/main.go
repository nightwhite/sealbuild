package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxCandidateSize = int64(150 * 1024 * 1024)

var (
	candidateNames = []string{
		"sealbuild-darwin-amd64",
		"sealbuild-darwin-arm64",
		"sealbuild-linux-amd64",
		"sealbuild-windows-amd64.exe",
	}
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[1-9][0-9]*$`)
)

type aggregateConfig struct {
	InputDirectory  string
	OutputDirectory string
	Version         string
	Commit          string
	BuiltAt         string
}

type candidateArtifact struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type candidateMetadata struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Version       string              `json:"version"`
	Commit        string              `json:"commit"`
	BuiltAt       string              `json:"builtAt"`
	Artifacts     []candidateArtifact `json:"artifacts"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("aggregatecandidate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var config aggregateConfig
	flags.StringVar(&config.InputDirectory, "input", "", "candidate input directory")
	flags.StringVar(&config.OutputDirectory, "output", "", "release output directory")
	flags.StringVar(&config.Version, "version", "", "candidate version")
	flags.StringVar(&config.Commit, "commit", "", "candidate Git commit")
	flags.StringVar(&config.BuiltAt, "built-at", "", "candidate UTC build time")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse aggregatecandidate arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional argument: %s", flags.Arg(0))
	}
	_, err := aggregate(config)
	return err
}

func aggregate(config aggregateConfig) (metadata candidateMetadata, returnErr error) {
	if err := validateAggregateConfig(config); err != nil {
		return candidateMetadata{}, err
	}
	inputDirectory, err := filepath.Abs(config.InputDirectory)
	if err != nil {
		return candidateMetadata{}, fmt.Errorf("resolve candidate input directory: %w", err)
	}
	outputDirectory, err := filepath.Abs(config.OutputDirectory)
	if err != nil {
		return candidateMetadata{}, fmt.Errorf("resolve candidate output directory: %w", err)
	}
	entries, err := os.ReadDir(inputDirectory)
	if err != nil {
		return candidateMetadata{}, fmt.Errorf("read candidate input directory: %w", err)
	}
	if len(entries) != len(candidateNames) {
		return candidateMetadata{}, fmt.Errorf("candidate directory contains %d entries, expected %d", len(entries), len(candidateNames))
	}
	if _, err := os.Lstat(outputDirectory); err == nil {
		return candidateMetadata{}, fmt.Errorf("candidate output already exists: %s", outputDirectory)
	} else if !errors.Is(err, os.ErrNotExist) {
		return candidateMetadata{}, fmt.Errorf("inspect candidate output: %w", err)
	}
	outputParent := filepath.Dir(outputDirectory)
	parentInfo, err := os.Stat(outputParent)
	if err != nil {
		return candidateMetadata{}, fmt.Errorf("inspect candidate output parent: %w", err)
	}
	if !parentInfo.IsDir() {
		return candidateMetadata{}, fmt.Errorf("candidate output parent must be a directory")
	}
	temporaryDirectory, err := os.MkdirTemp(outputParent, ".sealbuild-candidate-*")
	if err != nil {
		return candidateMetadata{}, fmt.Errorf("create temporary candidate directory: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, os.RemoveAll(temporaryDirectory)) }()

	metadata = candidateMetadata{
		SchemaVersion: 1,
		Version:       config.Version,
		Commit:        config.Commit,
		BuiltAt:       config.BuiltAt,
	}
	var checksums strings.Builder
	for _, name := range candidateNames {
		sourcePath := filepath.Join(inputDirectory, name)
		info, err := os.Lstat(sourcePath)
		if err != nil {
			return candidateMetadata{}, fmt.Errorf("inspect candidate %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return candidateMetadata{}, fmt.Errorf("candidate %s must be a regular file", name)
		}
		if info.Size() == 0 {
			return candidateMetadata{}, fmt.Errorf("candidate %s must not be empty", name)
		}
		if info.Size() >= maxCandidateSize {
			return candidateMetadata{}, fmt.Errorf("candidate %s must be smaller than 150 MiB", name)
		}
		digest, err := copyCandidate(sourcePath, filepath.Join(temporaryDirectory, name), candidateMode(name))
		if err != nil {
			return candidateMetadata{}, fmt.Errorf("copy candidate %s: %w", name, err)
		}
		metadata.Artifacts = append(metadata.Artifacts, candidateArtifact{Name: name, SHA256: digest, Size: info.Size()})
		fmt.Fprintf(&checksums, "%s  %s\n", digest, name)
	}
	if err := writeSyncedFile(filepath.Join(temporaryDirectory, "checksums.txt"), []byte(checksums.String()), 0o644); err != nil {
		return candidateMetadata{}, err
	}
	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return candidateMetadata{}, fmt.Errorf("encode candidate metadata: %w", err)
	}
	metadataBytes = append(metadataBytes, '\n')
	if err := writeSyncedFile(filepath.Join(temporaryDirectory, "candidate.json"), metadataBytes, 0o644); err != nil {
		return candidateMetadata{}, err
	}
	if err := syncDirectory(temporaryDirectory); err != nil {
		return candidateMetadata{}, err
	}
	if err := os.Rename(temporaryDirectory, outputDirectory); err != nil {
		return candidateMetadata{}, fmt.Errorf("publish candidate directory: %w", err)
	}
	if err := syncDirectory(outputParent); err != nil {
		return candidateMetadata{}, err
	}
	return metadata, nil
}

func validateAggregateConfig(config aggregateConfig) error {
	for _, required := range []struct{ name, value string }{
		{"--input", config.InputDirectory},
		{"--output", config.OutputDirectory},
		{"--version", config.Version},
		{"--commit", config.Commit},
		{"--built-at", config.BuiltAt},
	} {
		if required.value == "" {
			return fmt.Errorf("%s is required", required.name)
		}
	}
	if config.Version != "dev" && !versionPattern.MatchString(config.Version) {
		return fmt.Errorf("version must be dev or an RC tag")
	}
	if !commitPattern.MatchString(config.Commit) {
		return fmt.Errorf("commit must be 40 lowercase hexadecimal characters")
	}
	builtAt, err := time.Parse(time.RFC3339, config.BuiltAt)
	if err != nil || !strings.HasSuffix(config.BuiltAt, "Z") || builtAt.UTC().Format(time.RFC3339) != config.BuiltAt {
		return fmt.Errorf("built-at must be canonical UTC RFC3339")
	}
	return nil
}

func copyCandidate(sourcePath, destinationPath string, mode os.FileMode) (digest string, returnErr error) {
	input, err := os.Open(sourcePath)
	if err != nil {
		return "", err
	}
	defer func() { returnErr = errors.Join(returnErr, input.Close()) }()
	output, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(output, hash), input)
	if err := errors.Join(copyErr, output.Sync(), output.Close()); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func writeSyncedFile(path string, contents []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", filepath.Base(path), err)
	}
	_, writeErr := file.Write(contents)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func candidateMode(name string) os.FileMode {
	if strings.HasSuffix(name, ".exe") {
		return 0o644
	}
	return 0o755
}
