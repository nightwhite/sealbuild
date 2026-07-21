//go:build !sealbuild_runtime

package runtimeassets

import (
	"strings"
	"testing"
)

func TestBundleWithoutRuntimeTagReturnsExplicitError(t *testing.T) {
	_, err := Bundle()
	if err == nil || !strings.Contains(err.Error(), "Runtime assets are not embedded") {
		t.Fatalf("Bundle() error = %v, want not embedded", err)
	}
}
