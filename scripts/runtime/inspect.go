package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	buildpkg "github.com/labring/sealbuild/internal/build"
	proxyconfig "github.com/labring/sealbuild/internal/proxy"
)

type workerInfo struct {
	ID        string        `json:"id"`
	Platforms []ociPlatform `json:"platforms"`
}

type ociPlatform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant,omitempty"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s worker WORKER_JSON | oci OCI_ARCHIVE | proxy OUTPUT_FILE\n", os.Args[0])
		os.Exit(2)
	}

	switch os.Args[1] {
	case "worker":
		input, err := os.Open(os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "open worker input: %v\n", err)
			os.Exit(1)
		}
		defer input.Close()
		if err := inspectWorkerJSON(input); err != nil {
			fmt.Fprintf(os.Stderr, "inspect worker: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("BuildKit worker: linux/amd64")
	case "oci":
		if err := buildpkg.VerifyOCIArchive(os.Args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "inspect OCI archive: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("OCI platform: linux/amd64")
	case "proxy":
		proxyURL, err := transformProxyURL(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "validate proxy URL: %v\n", err)
			os.Exit(1)
		}
		if err := writeProxyFile(os.Args[2], proxyURL); err != nil {
			fmt.Fprintf(os.Stderr, "write Guest proxy file: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown inspection type %q\n", os.Args[1])
		os.Exit(2)
	}
}

func transformProxyURL(reader io.Reader) (string, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, 4097))
	if err != nil {
		return "", fmt.Errorf("read proxy URL: %w", err)
	}
	if len(contents) == 0 || len(contents) > 4096 {
		return "", fmt.Errorf("proxy URL length must be between 1 and 4096 bytes")
	}
	config, err := proxyconfig.Parse(string(contents))
	if err != nil {
		return "", err
	}
	return config.Guest, nil
}

func writeProxyFile(filePath, proxyURL string) (returnErr error) {
	info, err := os.Lstat(filePath)
	if err != nil {
		return fmt.Errorf("inspect output file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("output path must be a regular file")
	}
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open output file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); returnErr == nil && closeErr != nil {
			returnErr = closeErr
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("set output permissions: %w", err)
	}
	if _, err := io.WriteString(file, proxyURL); err != nil {
		return fmt.Errorf("write output file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync output file: %w", err)
	}
	return nil
}

func inspectWorkerJSON(reader io.Reader) error {
	var workers []workerInfo
	if err := json.NewDecoder(reader).Decode(&workers); err != nil {
		return fmt.Errorf("decode worker JSON: %w", err)
	}
	if len(workers) != 1 {
		return fmt.Errorf("expected exactly one worker, got %d", len(workers))
	}

	hasBasePlatform := false
	for _, platform := range workers[0].Platforms {
		if platform.OS != "linux" || platform.Architecture != "amd64" {
			return fmt.Errorf("worker platform %s/%s is not allowed", platform.OS, platform.Architecture)
		}
		if platform.Variant == "" {
			hasBasePlatform = true
		}
	}
	if !hasBasePlatform {
		return fmt.Errorf("base linux/amd64 platform is required")
	}
	return nil
}
