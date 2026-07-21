//go:build sealbuild_runtime && darwin && arm64

package runtimeassets

import runtimepkg "github.com/labring/sealbuild/internal/runtime"

// Bundle returns the immutable Darwin ARM64 Host and Linux AMD64 Guest archives.
func Bundle() (runtimepkg.Bundle, error) {
	return embeddedBundle("sealbuild-host-runtime-darwin-arm64.tar.zst"), nil
}
