package main

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const (
	imageManifestMediaType = "application/vnd.oci.image.manifest.v1+json"
	maxIndexSize           = 1024 * 1024
)

type workerInfo struct {
	ID        string        `json:"id"`
	Platforms []ociPlatform `json:"platforms"`
}

type ociIndex struct {
	SchemaVersion int             `json:"schemaVersion"`
	Manifests     []ociDescriptor `json:"manifests"`
}

type ociDescriptor struct {
	MediaType string       `json:"mediaType"`
	Platform  *ociPlatform `json:"platform"`
}

type ociPlatform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant,omitempty"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s worker WORKER_JSON | oci OCI_ARCHIVE\n", os.Args[0])
		os.Exit(2)
	}

	input, err := os.Open(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "open %s input: %v\n", os.Args[1], err)
		os.Exit(1)
	}
	defer input.Close()

	switch os.Args[1] {
	case "worker":
		if err := inspectWorkerJSON(input); err != nil {
			fmt.Fprintf(os.Stderr, "inspect worker: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("BuildKit worker: linux/amd64")
	case "oci":
		if err := inspectOCIArchive(input); err != nil {
			fmt.Fprintf(os.Stderr, "inspect OCI archive: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("OCI platform: linux/amd64")
	default:
		fmt.Fprintf(os.Stderr, "unknown inspection type %q\n", os.Args[1])
		os.Exit(2)
	}
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

func inspectOCIArchive(reader io.Reader) error {
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return fmt.Errorf("index.json is missing")
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}
		if header.Name != "index.json" {
			continue
		}
		if header.Size > maxIndexSize {
			return fmt.Errorf("index.json exceeds %d bytes", maxIndexSize)
		}

		var index ociIndex
		if err := json.NewDecoder(tarReader).Decode(&index); err != nil {
			return fmt.Errorf("decode index.json: %w", err)
		}
		if index.SchemaVersion != 2 {
			return fmt.Errorf("OCI index schemaVersion is %d, want 2", index.SchemaVersion)
		}
		if len(index.Manifests) != 1 {
			return fmt.Errorf("OCI index must contain exactly one manifest")
		}

		manifest := index.Manifests[0]
		if manifest.MediaType != imageManifestMediaType {
			return fmt.Errorf("manifest mediaType is %q, want %q", manifest.MediaType, imageManifestMediaType)
		}
		if manifest.Platform == nil {
			return fmt.Errorf("manifest platform is required")
		}
		if manifest.Platform.OS != "linux" || manifest.Platform.Architecture != "amd64" {
			return fmt.Errorf(
				"manifest platform is %s/%s, want linux/amd64",
				manifest.Platform.OS,
				manifest.Platform.Architecture,
			)
		}
		return nil
	}
}
