//go:build linux && amd64

package runtime

func expectedHostPlatform() Platform {
	return Platform{OS: "linux", Architecture: "amd64"}
}
