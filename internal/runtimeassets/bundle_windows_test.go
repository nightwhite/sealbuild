//go:build sealbuild_runtime && windows && amd64

package runtimeassets

import "testing"

func TestWindowsBundleNamesPlatformArtifacts(t *testing.T) {
	bundle, err := Bundle()
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}
	if bundle.Host.Name != "sealbuild-host-runtime-windows-amd64.tar.zst" {
		t.Fatalf("Host.Name = %q", bundle.Host.Name)
	}
	if bundle.Guest.Name != "sealbuild-guest-runtime-linux-amd64.tar.zst" {
		t.Fatalf("Guest.Name = %q", bundle.Guest.Name)
	}
}
