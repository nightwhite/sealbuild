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

func TestBuildrootPostBuildCreatesStateMountPointOnReadOnlyRootfs(t *testing.T) {
	postBuild, err := os.ReadFile("../../runtime/buildroot/board/sealbuild/x86_64/post-build.sh")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	const requiredCommand = `install -d -m 0755 "${target_dir}/var/lib/buildkit"`
	if !strings.Contains(string(postBuild), requiredCommand+"\n") {
		t.Fatalf("post-build script is missing %q", requiredCommand)
	}
}

func TestBuildrootKeepsCNIStateOnWritableStateDisk(t *testing.T) {
	postBuild, err := os.ReadFile("../../runtime/buildroot/board/sealbuild/x86_64/post-build.sh")
	if err != nil {
		t.Fatalf("ReadFile(post-build.sh) error = %v", err)
	}
	initScript, err := os.ReadFile("../../runtime/buildroot/board/sealbuild/x86_64/rootfs-overlay/etc/init.d/S50sealbuild-runtime")
	if err != nil {
		t.Fatalf("ReadFile(S50sealbuild-runtime) error = %v", err)
	}

	const requiredMountPoint = `install -d -m 0755 "${target_dir}/var/lib/cni"`
	if !strings.Contains(string(postBuild), requiredMountPoint+"\n") {
		t.Fatalf("post-build script is missing %q", requiredMountPoint)
	}

	const requiredStateDirectory = `mkdir -p "${state_dir}/cni"`
	if !strings.Contains(string(initScript), requiredStateDirectory+"\n") {
		t.Fatalf("init script is missing %q", requiredStateDirectory)
	}
	const requiredBindMount = `mount --bind "${state_dir}/cni" /var/lib/cni || fail cni-state-mount`
	if !strings.Contains(string(initScript), requiredBindMount+"\n") {
		t.Fatalf("init script is missing %q", requiredBindMount)
	}
}
