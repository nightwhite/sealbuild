package runtime

import (
	"os"
	"strings"
	"testing"
)

const validSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestLoadLockAcceptsPinnedLinuxAMD64Runtime(t *testing.T) {
	lockJSON := `{
  "schemaVersion": 1,
  "guestPlatform": {"os": "linux", "architecture": "amd64"},
  "components": [
    {
      "name": "buildkit",
      "version": "v0.31.1",
      "source": "https://github.com/moby/buildkit/releases/download/v0.31.1/buildkit-v0.31.1.linux-amd64.tar.gz",
      "sha256": "` + validSHA256 + `"
    }
  ]
}`

	lock, err := LoadLock(strings.NewReader(lockJSON))
	if err != nil {
		t.Fatalf("LoadLock() error = %v", err)
	}

	if lock.GuestPlatform.OS != "linux" {
		t.Fatalf("GuestPlatform.OS = %q, want linux", lock.GuestPlatform.OS)
	}
	if lock.GuestPlatform.Architecture != "amd64" {
		t.Fatalf("GuestPlatform.Architecture = %q, want amd64", lock.GuestPlatform.Architecture)
	}
}

func TestLoadLockRejectsInvalidRuntime(t *testing.T) {
	tests := []struct {
		name      string
		lockJSON  string
		wantError string
	}{
		{
			name:      "unknown schema",
			lockJSON:  `{"schemaVersion":2,"guestPlatform":{"os":"linux","architecture":"amd64"},"components":[{"name":"buildkit","version":"v0.31.1","source":"https://example.invalid/buildkit","sha256":"` + validSHA256 + `"}]}`,
			wantError: "schemaVersion must be 1",
		},
		{
			name:      "arm target",
			lockJSON:  `{"schemaVersion":1,"guestPlatform":{"os":"linux","architecture":"arm64"},"components":[{"name":"buildkit","version":"v0.31.1","source":"https://example.invalid/buildkit","sha256":"` + validSHA256 + `"}]}`,
			wantError: "guestPlatform must be linux/amd64",
		},
		{
			name:      "missing component version",
			lockJSON:  `{"schemaVersion":1,"guestPlatform":{"os":"linux","architecture":"amd64"},"components":[{"name":"buildkit","version":"","source":"https://example.invalid/buildkit","sha256":"` + validSHA256 + `"}]}`,
			wantError: "component buildkit version is required",
		},
		{
			name:      "uppercase checksum",
			lockJSON:  `{"schemaVersion":1,"guestPlatform":{"os":"linux","architecture":"amd64"},"components":[{"name":"buildkit","version":"v0.31.1","source":"https://example.invalid/buildkit","sha256":"ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789"}]}`,
			wantError: "component buildkit sha256 must be 64 lowercase hexadecimal characters",
		},
		{
			name:      "trailing JSON",
			lockJSON:  `{"schemaVersion":1,"guestPlatform":{"os":"linux","architecture":"amd64"},"components":[{"name":"buildkit","version":"v0.31.1","source":"https://example.invalid/buildkit","sha256":"` + validSHA256 + `"}]} {}`,
			wantError: "decode runtime lock",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadLock(strings.NewReader(test.lockJSON))
			if err == nil {
				t.Fatal("LoadLock() error = nil, want error")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("LoadLock() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestRepositoryRuntimeLockIsValid(t *testing.T) {
	manifest, err := os.Open("../../runtime/manifest.lock.json")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer manifest.Close()

	if _, err := LoadLock(manifest); err != nil {
		t.Fatalf("LoadLock() error = %v", err)
	}
}
