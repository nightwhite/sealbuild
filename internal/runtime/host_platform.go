package runtime

import "fmt"

func validateHostRuntimePlatform(platform Platform) error {
	expected := expectedHostPlatform()
	if expected.OS == "" || expected.Architecture == "" {
		return fmt.Errorf("Host Runtime is unsupported on this Sealbuild host platform")
	}
	if platform != expected {
		return fmt.Errorf("Host Runtime platform is %s/%s, expected %s/%s", platform.OS, platform.Architecture, expected.OS, expected.Architecture)
	}
	return nil
}
