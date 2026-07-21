package runtime

import (
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestBundleCompatibilityIDUsesBothPinnedAssets(t *testing.T) {
	hostSHA := strings.Repeat("a", 64)
	guestSHA := strings.Repeat("b", 64)
	bundle := Bundle{
		Host:  validAsset("host-runtime-darwin-arm64.tar.zst", hostSHA),
		Guest: validAsset("guest-runtime-linux-amd64.tar.zst", guestSHA),
	}

	got, err := bundle.CompatibilityID()
	if err != nil {
		t.Fatalf("CompatibilityID() error = %v", err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte("sealbuild-runtime-v1\n"+hostSHA+"\n"+guestSHA+"\n")))
	if got != want {
		t.Fatalf("CompatibilityID() = %q, want %q", got, want)
	}
}

func TestBundleCompatibilityIDRejectsInvalidAsset(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Bundle)
		wantError string
	}{
		{name: "empty name", mutate: func(bundle *Bundle) { bundle.Host.Name = "" }, wantError: "host asset name is required"},
		{name: "invalid sha", mutate: func(bundle *Bundle) { bundle.Guest.SHA256 = "ABC" }, wantError: "guest asset sha256"},
		{name: "empty size", mutate: func(bundle *Bundle) { bundle.Host.Size = 0 }, wantError: "host asset size must be greater than zero"},
		{name: "nil opener", mutate: func(bundle *Bundle) { bundle.Guest.Open = nil }, wantError: "guest asset opener is required"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bundle := Bundle{
				Host:  validAsset("host.tar.zst", strings.Repeat("a", 64)),
				Guest: validAsset("guest.tar.zst", strings.Repeat("b", 64)),
			}
			test.mutate(&bundle)
			_, err := bundle.CompatibilityID()
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("CompatibilityID() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func validAsset(name, checksum string) Asset {
	return Asset{
		Name:   name,
		SHA256: checksum,
		Size:   1024,
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("fixture")), nil
		},
	}
}
