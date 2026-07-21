//go:build !(darwin && arm64) && !(darwin && amd64) && !(linux && amd64) && !(windows && amd64)

package runtime

func expectedHostPlatform() Platform {
	return Platform{}
}
