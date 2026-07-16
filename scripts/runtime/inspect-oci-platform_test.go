package main

import (
	"archive/tar"
	"bytes"
	"strings"
	"testing"
)

func TestInspectOCIArchiveAcceptsSingleLinuxAMD64Manifest(t *testing.T) {
	archive := ociArchive(t, `{
  "schemaVersion": 2,
  "manifests": [
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
      "size": 123,
      "platform": {"os": "linux", "architecture": "amd64"}
    }
  ]
}`)

	if err := inspectOCIArchive(bytes.NewReader(archive)); err != nil {
		t.Fatalf("inspectOCIArchive() error = %v", err)
	}
}

func TestInspectOCIArchiveRejectsWrongOrAmbiguousPlatform(t *testing.T) {
	tests := []struct {
		name      string
		indexJSON string
		wantError string
	}{
		{
			name:      "arm64 manifest",
			indexJSON: `{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","size":123,"platform":{"os":"linux","architecture":"arm64"}}]}`,
			wantError: "manifest platform is linux/arm64, want linux/amd64",
		},
		{
			name:      "missing platform",
			indexJSON: `{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","size":123}]}`,
			wantError: "manifest platform is required",
		},
		{
			name:      "multiple manifests",
			indexJSON: `{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","size":123,"platform":{"os":"linux","architecture":"amd64"}},{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789","size":123,"platform":{"os":"linux","architecture":"amd64"}}]}`,
			wantError: "OCI index must contain exactly one manifest",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := ociArchive(t, test.indexJSON)
			err := inspectOCIArchive(bytes.NewReader(archive))
			if err == nil {
				t.Fatal("inspectOCIArchive() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("inspectOCIArchive() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func ociArchive(t *testing.T, indexJSON string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	tarWriter := tar.NewWriter(&buffer)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "index.json", Mode: 0o644, Size: int64(len(indexJSON))}); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if _, err := tarWriter.Write([]byte(indexJSON)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return buffer.Bytes()
}
