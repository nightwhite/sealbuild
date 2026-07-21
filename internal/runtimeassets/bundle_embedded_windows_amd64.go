//go:build sealbuild_runtime && windows && amd64

package runtimeassets

import runtimepkg "github.com/labring/sealbuild/internal/runtime"

// Bundle returns the immutable Windows AMD64 Host and Linux AMD64 Guest archives.
func Bundle() (runtimepkg.Bundle, error) {
	return embeddedBundle("sealbuild-host-runtime-windows-amd64.tar.zst"), nil
}
