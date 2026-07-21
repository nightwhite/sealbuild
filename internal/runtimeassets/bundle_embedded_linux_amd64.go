//go:build sealbuild_runtime && linux && amd64

package runtimeassets

import runtimepkg "github.com/labring/sealbuild/internal/runtime"

// Bundle returns the immutable Linux AMD64 Host and Linux AMD64 Guest archives.
func Bundle() (runtimepkg.Bundle, error) {
	return embeddedBundle("sealbuild-host-runtime-linux-amd64.tar.zst"), nil
}
