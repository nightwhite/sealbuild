//go:build darwin && amd64

package runtime

func expectedHostPlatform() Platform {
	return Platform{OS: "darwin", Architecture: "amd64"}
}
