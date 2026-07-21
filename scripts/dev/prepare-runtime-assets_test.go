package dev

import (
	"os"
	"strings"
	"testing"
)

func TestPrepareRuntimeAssetsValidatesAndInstallsFixedArchives(t *testing.T) {
	script, err := os.ReadFile("prepare-runtime-assets.sh")
	if err != nil {
		t.Fatalf("ReadFile(prepare-runtime-assets.sh) error = %v", err)
	}

	contents := string(script)
	for _, required := range []string{
		`if [ "$#" -ne 2 ]`,
		"go run ./scripts/dev/verify-runtime",
		`--host "$1"`,
		`--guest "$2"`,
		"internal/runtimeassets/generated",
		"host.tar.zst",
		"guest.tar.zst",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("prepare-runtime-assets.sh is missing %q", required)
		}
	}

	for _, forbidden := range []string{"find ", "curl ", "wget ", "HTTP_PROXY", "HTTPS_PROXY"} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("prepare-runtime-assets.sh contains forbidden fragment %q", forbidden)
		}
	}
}
