package runtime

import (
	"os"
	"strings"
	"testing"
)

func TestBuildrootDefconfigPinsKernelHeadersToGuestKernelSeries(t *testing.T) {
	defconfig, err := os.ReadFile("../../runtime/buildroot/configs/sealbuild_x86_64_defconfig")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	const requiredSetting = "BR2_PACKAGE_HOST_LINUX_HEADERS_CUSTOM_6_18=y"
	if !strings.Contains(string(defconfig), requiredSetting+"\n") {
		t.Fatalf("defconfig is missing %q", requiredSetting)
	}
}
