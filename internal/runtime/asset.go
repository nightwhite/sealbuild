package runtime

import (
	"crypto/sha256"
	"fmt"
	"io"
)

// Asset describes one immutable compressed Runtime artifact.
type Asset struct {
	Name   string
	SHA256 string
	Size   int64
	Open   func() (io.ReadCloser, error)
}

// Bundle contains the Host and Guest artifacts installed together.
type Bundle struct {
	Host  Asset
	Guest Asset
}

// CompatibilityID returns the content identity shared by Runtime and state paths.
func (bundle Bundle) CompatibilityID() (string, error) {
	if err := validateAsset("host", bundle.Host); err != nil {
		return "", err
	}
	if err := validateAsset("guest", bundle.Guest); err != nil {
		return "", err
	}
	contents := "sealbuild-runtime-v1\n" + bundle.Host.SHA256 + "\n" + bundle.Guest.SHA256 + "\n"
	return fmt.Sprintf("%x", sha256.Sum256([]byte(contents))), nil
}

func validateAsset(kind string, asset Asset) error {
	if asset.Name == "" {
		return fmt.Errorf("%s asset name is required", kind)
	}
	if !sha256Pattern.MatchString(asset.SHA256) {
		return fmt.Errorf("%s asset sha256 must be 64 lowercase hexadecimal characters", kind)
	}
	if asset.Size <= 0 {
		return fmt.Errorf("%s asset size must be greater than zero", kind)
	}
	if asset.Open == nil {
		return fmt.Errorf("%s asset opener is required", kind)
	}
	return nil
}
