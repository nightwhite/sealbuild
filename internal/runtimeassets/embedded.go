//go:build sealbuild_runtime && ((darwin && arm64) || (windows && amd64))

package runtimeassets

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"io"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

//go:embed generated/host.tar.zst
var hostArchive []byte

//go:embed generated/guest.tar.zst
var guestArchive []byte

func embeddedBundle(hostName string) runtimepkg.Bundle {
	return runtimepkg.Bundle{
		Host:  embeddedAsset(hostName, hostArchive),
		Guest: embeddedAsset("sealbuild-guest-runtime-linux-amd64.tar.zst", guestArchive),
	}
}

func embeddedAsset(name string, contents []byte) runtimepkg.Asset {
	checksum := sha256.Sum256(contents)
	return runtimepkg.Asset{
		Name: name, SHA256: fmt.Sprintf("%x", checksum), Size: int64(len(contents)),
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(contents)), nil
		},
	}
}
