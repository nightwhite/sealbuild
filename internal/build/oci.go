package build

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const maxOCIJSONSize = 1024 * 1024

// VerifyOCIArchive verifies descriptor integrity and the fixed linux/amd64 platform.
func VerifyOCIArchive(archivePath string) error {
	indexBytes, err := readOCIEntry(archivePath, "index.json", nil, maxOCIJSONSize)
	if err != nil {
		return fmt.Errorf("read OCI index: %w", err)
	}
	var index ocispec.Index
	if err := decodeOCIJSON(indexBytes, &index); err != nil {
		return fmt.Errorf("decode OCI index: %w", err)
	}
	if index.SchemaVersion != 2 || index.MediaType != ocispec.MediaTypeImageIndex {
		return fmt.Errorf("OCI index schema or media type is invalid")
	}
	if len(index.Manifests) != 1 {
		return fmt.Errorf("OCI index must contain exactly one manifest")
	}
	manifestDescriptor := index.Manifests[0]
	if manifestDescriptor.MediaType != ocispec.MediaTypeImageManifest {
		return fmt.Errorf("OCI manifest media type is %q", manifestDescriptor.MediaType)
	}
	if manifestDescriptor.Platform == nil {
		return fmt.Errorf("OCI manifest platform is required")
	}
	platform := manifestDescriptor.Platform
	if platform.OS != "linux" || platform.Architecture != "amd64" {
		return fmt.Errorf("OCI manifest platform is %s/%s, want linux/amd64", platform.OS, platform.Architecture)
	}
	if platform.Variant != "" {
		return fmt.Errorf("OCI manifest platform variant must be empty, got %q", platform.Variant)
	}

	manifestBytes, err := readOCIEntry(archivePath, descriptorPath(manifestDescriptor), &manifestDescriptor, maxOCIJSONSize)
	if err != nil {
		return fmt.Errorf("read OCI manifest blob: %w", err)
	}
	var manifest ocispec.Manifest
	if err := decodeOCIJSON(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("decode OCI manifest: %w", err)
	}
	if manifest.SchemaVersion != 2 || manifest.MediaType != ocispec.MediaTypeImageManifest {
		return fmt.Errorf("OCI manifest schema or media type is invalid")
	}
	if manifest.Config.MediaType != ocispec.MediaTypeImageConfig {
		return fmt.Errorf("OCI config media type is %q", manifest.Config.MediaType)
	}
	descriptors := make([]ocispec.Descriptor, 0, len(manifest.Layers)+1)
	descriptors = append(descriptors, manifest.Config)
	descriptors = append(descriptors, manifest.Layers...)
	configBytes, err := verifyOCIBlobs(archivePath, descriptors, manifest.Config.Digest)
	if err != nil {
		return err
	}
	var imageConfig ocispec.Image
	if err := decodeOCIJSON(configBytes, &imageConfig); err != nil {
		return fmt.Errorf("decode OCI config: %w", err)
	}
	if imageConfig.OS != "linux" || imageConfig.Architecture != "amd64" || imageConfig.Variant != "" {
		return fmt.Errorf("OCI config platform is %s/%s variant %q, want linux/amd64", imageConfig.OS, imageConfig.Architecture, imageConfig.Variant)
	}
	return nil
}

func readOCIEntry(archivePath, wanted string, descriptor *ocispec.Descriptor, maximum int64) ([]byte, error) {
	archive, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open OCI archive: %w", err)
	}
	defer archive.Close()
	reader := tar.NewReader(archive)
	seen := make(map[string]struct{})
	var contents []byte
	found := false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read OCI tar: %w", err)
		}
		if err := validateOCITarHeader(header, seen); err != nil {
			return nil, err
		}
		if header.Name != wanted {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("OCI entry %s must be a regular file", wanted)
		}
		if header.Size > maximum {
			return nil, fmt.Errorf("OCI entry %s exceeds %d bytes", wanted, maximum)
		}
		contents, err = io.ReadAll(io.LimitReader(reader, maximum+1))
		if err != nil {
			return nil, fmt.Errorf("read OCI entry %s: %w", wanted, err)
		}
		if descriptor != nil {
			if err := verifyDescriptor(*descriptor, int64(len(contents)), digest.FromBytes(contents)); err != nil {
				return nil, err
			}
		}
		found = true
	}
	if !found {
		return nil, fmt.Errorf("OCI entry %s is missing", wanted)
	}
	return contents, nil
}

func verifyOCIBlobs(archivePath string, descriptors []ocispec.Descriptor, configDigest digest.Digest) ([]byte, error) {
	wanted := make(map[string]ocispec.Descriptor, len(descriptors))
	for _, descriptor := range descriptors {
		if err := validateDescriptor(descriptor); err != nil {
			return nil, err
		}
		entryPath := descriptorPath(descriptor)
		if _, exists := wanted[entryPath]; exists {
			return nil, fmt.Errorf("OCI descriptor %s is duplicated", descriptor.Digest)
		}
		wanted[entryPath] = descriptor
	}
	archive, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open OCI archive: %w", err)
	}
	defer archive.Close()
	reader := tar.NewReader(archive)
	seen := make(map[string]struct{})
	found := make(map[string]struct{}, len(wanted))
	var configBytes []byte
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read OCI tar: %w", err)
		}
		if err := validateOCITarHeader(header, seen); err != nil {
			return nil, err
		}
		descriptor, exists := wanted[header.Name]
		if !exists {
			continue
		}
		if header.Size != descriptor.Size {
			return nil, fmt.Errorf("OCI blob %s size is %d, expected %d", descriptor.Digest, header.Size, descriptor.Size)
		}
		verifier := descriptor.Digest.Verifier()
		var destination io.Writer = verifier
		var configBuffer bytes.Buffer
		if descriptor.Digest == configDigest {
			if descriptor.Size > maxOCIJSONSize {
				return nil, fmt.Errorf("OCI config exceeds %d bytes", maxOCIJSONSize)
			}
			destination = io.MultiWriter(verifier, &configBuffer)
		}
		if _, err := io.CopyN(destination, reader, header.Size); err != nil {
			return nil, fmt.Errorf("read OCI blob %s: %w", descriptor.Digest, err)
		}
		if !verifier.Verified() {
			return nil, fmt.Errorf("OCI blob %s digest does not match", descriptor.Digest)
		}
		if descriptor.Digest == configDigest {
			configBytes = configBuffer.Bytes()
		}
		found[header.Name] = struct{}{}
	}
	for entryPath, descriptor := range wanted {
		if _, exists := found[entryPath]; !exists {
			label := "layer"
			if descriptor.Digest == configDigest {
				label = "config"
			}
			return nil, fmt.Errorf("OCI %s blob %s is missing", label, descriptor.Digest)
		}
	}
	return configBytes, nil
}

func validateOCITarHeader(header *tar.Header, seen map[string]struct{}) error {
	name := header.Name
	cleanName := path.Clean(strings.TrimSuffix(name, "/"))
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, "../") {
		return fmt.Errorf("OCI archive contains unsafe path %q", name)
	}
	if _, exists := seen[name]; exists {
		return fmt.Errorf("OCI archive entry %s is duplicated", name)
	}
	seen[name] = struct{}{}
	switch header.Typeflag {
	case tar.TypeReg, tar.TypeRegA:
	case tar.TypeDir:
		if header.Size != 0 {
			return fmt.Errorf("OCI directory %s has non-zero size", name)
		}
	default:
		return fmt.Errorf("OCI archive entry %s has unsupported type %d", name, header.Typeflag)
	}
	return nil
}

func validateDescriptor(descriptor ocispec.Descriptor) error {
	if descriptor.Digest.Algorithm() != digest.SHA256 || descriptor.Digest.Validate() != nil {
		return fmt.Errorf("OCI descriptor digest %q is invalid", descriptor.Digest)
	}
	if descriptor.Size < 0 {
		return fmt.Errorf("OCI descriptor %s size is invalid", descriptor.Digest)
	}
	return nil
}

func verifyDescriptor(descriptor ocispec.Descriptor, size int64, actual digest.Digest) error {
	if err := validateDescriptor(descriptor); err != nil {
		return err
	}
	if descriptor.Size != size {
		return fmt.Errorf("OCI descriptor %s size is %d, expected %d", descriptor.Digest, size, descriptor.Size)
	}
	if descriptor.Digest != actual {
		return fmt.Errorf("OCI descriptor digest is %s, expected %s", actual, descriptor.Digest)
	}
	return nil
}

func descriptorPath(descriptor ocispec.Descriptor) string {
	return path.Join("blobs", descriptor.Digest.Algorithm().String(), descriptor.Digest.Encoded())
}

func decodeOCIJSON(contents []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("JSON contains trailing data")
	}
	return nil
}
