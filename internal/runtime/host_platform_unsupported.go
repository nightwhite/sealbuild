//go:build !(darwin && arm64) && !(windows && amd64)

package runtime

func expectedHostPlatform() Platform {
	return Platform{}
}
