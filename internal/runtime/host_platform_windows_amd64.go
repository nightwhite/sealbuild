//go:build windows && amd64

package runtime

func expectedHostPlatform() Platform {
	return Platform{OS: "windows", Architecture: "amd64"}
}
