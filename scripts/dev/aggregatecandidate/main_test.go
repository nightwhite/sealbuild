package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAggregateCandidateWritesExactReleaseSet(t *testing.T) {
	input := writeCandidateSet(t)
	output := filepath.Join(t.TempDir(), "release")
	metadata, err := aggregate(aggregateConfig{
		InputDirectory:  input,
		OutputDirectory: output,
		Version:         "v0.1.0-rc.1",
		Commit:          strings.Repeat("a", 40),
		BuiltAt:         "2026-07-21T09:00:00Z",
	})
	if err != nil {
		t.Fatalf("aggregate() error = %v", err)
	}
	if len(metadata.Artifacts) != len(candidateNames) {
		t.Fatalf("len(Artifacts) = %d, want %d", len(metadata.Artifacts), len(candidateNames))
	}

	var expectedChecksums strings.Builder
	for index, name := range candidateNames {
		contents := []byte("candidate:" + name)
		digest := fmt.Sprintf("%x", sha256.Sum256(contents))
		fmt.Fprintf(&expectedChecksums, "%s  %s\n", digest, name)
		if metadata.Artifacts[index].Name != name || metadata.Artifacts[index].SHA256 != digest || metadata.Artifacts[index].Size != int64(len(contents)) {
			t.Errorf("Artifacts[%d] = %#v", index, metadata.Artifacts[index])
		}
		copied, err := os.ReadFile(filepath.Join(output, name))
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", name, err)
		}
		if string(copied) != string(contents) {
			t.Errorf("%s contents = %q, want %q", name, copied, contents)
		}
	}
	checksums, err := os.ReadFile(filepath.Join(output, "checksums.txt"))
	if err != nil {
		t.Fatalf("ReadFile(checksums.txt) error = %v", err)
	}
	if string(checksums) != expectedChecksums.String() {
		t.Fatalf("checksums.txt = %q, want %q", checksums, expectedChecksums.String())
	}
	metadataBytes, err := os.ReadFile(filepath.Join(output, "candidate.json"))
	if err != nil {
		t.Fatalf("ReadFile(candidate.json) error = %v", err)
	}
	var decoded candidateMetadata
	if err := json.Unmarshal(metadataBytes, &decoded); err != nil {
		t.Fatalf("Unmarshal(candidate.json) error = %v", err)
	}
	if decoded.Version != "v0.1.0-rc.1" || decoded.Commit != strings.Repeat("a", 40) || decoded.BuiltAt != "2026-07-21T09:00:00Z" {
		t.Fatalf("candidate metadata = %#v", decoded)
	}
}

func TestAggregateCandidateRejectsInvalidReleaseSet(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*testing.T, string)
		wantError string
	}{
		{
			name: "missing candidate",
			mutate: func(t *testing.T, input string) {
				if err := os.Remove(filepath.Join(input, candidateNames[0])); err != nil {
					t.Fatalf("Remove() error = %v", err)
				}
			},
			wantError: "candidate directory contains 3 entries, expected 4",
		},
		{
			name: "extra file",
			mutate: func(t *testing.T, input string) {
				writeCandidateFile(t, filepath.Join(input, "extra.txt"), "extra")
			},
			wantError: "candidate directory contains 5 entries, expected 4",
		},
		{
			name: "empty candidate",
			mutate: func(t *testing.T, input string) {
				if err := os.WriteFile(filepath.Join(input, candidateNames[0]), nil, 0o755); err != nil {
					t.Fatalf("WriteFile(empty) error = %v", err)
				}
			},
			wantError: "must not be empty",
		},
		{
			name: "candidate at size limit",
			mutate: func(t *testing.T, input string) {
				if err := os.Truncate(filepath.Join(input, candidateNames[0]), maxCandidateSize); err != nil {
					t.Fatalf("Truncate() error = %v", err)
				}
			},
			wantError: "must be smaller than 150 MiB",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := writeCandidateSet(t)
			test.mutate(t, input)
			_, err := aggregate(aggregateConfig{
				InputDirectory:  input,
				OutputDirectory: filepath.Join(t.TempDir(), "release"),
				Version:         "dev",
				Commit:          strings.Repeat("b", 40),
				BuiltAt:         "2026-07-21T09:00:00Z",
			})
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("aggregate() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func writeCandidateSet(t *testing.T) string {
	t.Helper()
	input := t.TempDir()
	for _, name := range candidateNames {
		writeCandidateFile(t, filepath.Join(input, name), "candidate:"+name)
	}
	return input
}

func writeCandidateFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}
