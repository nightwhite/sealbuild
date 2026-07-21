//go:build darwin && arm64

package runtime

func expectedHostPlatform() Platform {
	return Platform{OS: "darwin", Architecture: "arm64"}
}
