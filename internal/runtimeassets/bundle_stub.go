//go:build !sealbuild_runtime || !((darwin && (arm64 || amd64)) || (linux && amd64) || (windows && amd64))

// Package runtimeassets provides build-time embedded Runtime artifacts.
package runtimeassets

import (
	"fmt"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

// Bundle returns an explicit error when the binary was built without Runtime assets.
func Bundle() (runtimepkg.Bundle, error) {
	return runtimepkg.Bundle{}, fmt.Errorf("Runtime assets are not embedded; rebuild with the sealbuild_runtime tag")
}
