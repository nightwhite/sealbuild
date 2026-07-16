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
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s OCI_ARCHIVE\n", os.Args[0])
		os.Exit(2)
	}

	archive, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "open OCI archive: %v\n", err)
		os.Exit(1)
	}
	defer archive.Close()

	if err := inspectOCIArchive(archive); err != nil {
		fmt.Fprintf(os.Stderr, "inspect OCI archive: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("OCI platform: linux/amd64")
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
