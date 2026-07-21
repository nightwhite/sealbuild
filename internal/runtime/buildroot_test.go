package runtime

import (
	"encoding/json"
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

func TestBuildrootDownloadsDoNotRetryOrUseBackupSite(t *testing.T) {
	defconfig, err := os.ReadFile("../../runtime/buildroot/configs/sealbuild_x86_64_defconfig")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	for _, requiredSetting := range []string{
		`BR2_CURL="curl -q --ftp-pasv --retry 0 --connect-timeout 10"`,
		`BR2_WGET="wget -nd -t 1 --connect-timeout=10"`,
		`BR2_BACKUP_SITE=""`,
		`BR2_GNU_MIRROR="https://ftp.gnu.org/gnu"`,
	} {
		if !strings.Contains(string(defconfig), requiredSetting+"\n") {
			t.Errorf("defconfig is missing %q", requiredSetting)
		}
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

func TestGuestKernelEnablesLegacyIPTablesNATDependencies(t *testing.T) {
	kernelConfig, err := os.ReadFile("../../runtime/buildroot/board/sealbuild/x86_64/linux.config")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	for _, requiredSetting := range []string{
		"CONFIG_NETFILTER_XTABLES_LEGACY=y",
		"CONFIG_IP_NF_IPTABLES_LEGACY=y",
		"CONFIG_IP_NF_NAT=y",
	} {
		if !strings.Contains(string(kernelConfig), requiredSetting+"\n") {
			t.Errorf("kernel config is missing %q", requiredSetting)
		}
	}
}

func TestGuestKernelEnablesFWCfg(t *testing.T) {
	kernelConfig, err := os.ReadFile("../../runtime/buildroot/board/sealbuild/x86_64/linux.config")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	for _, requiredSetting := range []string{
		"CONFIG_FW_CFG_SYSFS=y",
		"CONFIG_FW_CFG_SYSFS_CMDLINE=y",
	} {
		if !strings.Contains(string(kernelConfig), requiredSetting+"\n") {
			t.Errorf("kernel config is missing %q", requiredSetting)
		}
	}
}

func TestGuestRootfsContainsNoTLSPrivateKey(t *testing.T) {
	postBuild, err := os.ReadFile("../../runtime/buildroot/board/sealbuild/x86_64/post-build.sh")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	contents := string(postBuild)
	for _, forbidden := range []string{
		"SEALBUILD_TLS_DIR",
		"server.key",
		"server.crt",
		"ca.crt",
	} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("post-build script must not contain %q", forbidden)
		}
	}
	const stateMountPoint = `install -d -m 0755 "${target_dir}/var/lib/buildkit"`
	if !strings.Contains(contents, stateMountPoint+"\n") {
		t.Fatalf("post-build script is missing %q", stateMountPoint)
	}
}

func TestGuestInitLoadsFWCfg(t *testing.T) {
	initScript, err := os.ReadFile("../../runtime/buildroot/board/sealbuild/x86_64/rootfs-overlay/etc/init.d/S50sealbuild-runtime")
	if err != nil {
		t.Fatalf("ReadFile(init) error = %v", err)
	}
	buildkitConfig, err := os.ReadFile("../../runtime/buildroot/board/sealbuild/x86_64/rootfs-overlay/etc/buildkit/buildkitd.toml")
	if err != nil {
		t.Fatalf("ReadFile(buildkitd.toml) error = %v", err)
	}

	initContents := string(initScript)
	for _, requiredFragment := range []string{
		`runtime_dir="${state_dir}/runtime"`,
		`tls_dir="${runtime_dir}/tls"`,
		`source="/sys/firmware/qemu_fw_cfg/by_name/${name}/raw"`,
		`install_fw_cfg opt/sealbuild/tls/ca.crt "${tls_dir}/ca.crt" 0644`,
		`install_fw_cfg opt/sealbuild/tls/server.crt "${tls_dir}/server.crt" 0644`,
		`install_fw_cfg opt/sealbuild/tls/server.key "${tls_dir}/server.key" 0600`,
		`proxy_source=/sys/firmware/qemu_fw_cfg/by_name/opt/sealbuild/proxy/url/raw`,
		`export HTTP_PROXY="${proxy_url}"`,
		`export HTTPS_PROXY="${proxy_url}"`,
		`export http_proxy="${proxy_url}"`,
		`export https_proxy="${proxy_url}"`,
		`export NO_PROXY="docker.1ms.run,.1ms.run"`,
		`export no_proxy="${NO_PROXY}"`,
		`shutdown_device=/dev/vport1p1`,
		`while ! IFS= read -r command <"${shutdown_device}"; do`,
		`kill "${buildkitd_pid}"`,
		`umount /var/lib/cni`,
		`umount "${state_dir}"`,
		`printf 'SEALBUILD_RUNTIME_SHUTDOWN\n'`,
		`poweroff -f`,
	} {
		if !strings.Contains(initContents, requiredFragment) {
			t.Errorf("init script is missing %q", requiredFragment)
		}
	}

	configContents := string(buildkitConfig)
	for _, requiredPath := range []string{
		`cert = "/var/lib/buildkit/runtime/tls/server.crt"`,
		`key = "/var/lib/buildkit/runtime/tls/server.key"`,
		`ca = "/var/lib/buildkit/runtime/tls/ca.crt"`,
	} {
		if !strings.Contains(configContents, requiredPath) {
			t.Errorf("BuildKit config is missing %q", requiredPath)
		}
	}
	if strings.Contains(configContents, "/etc/buildkit/tls/") {
		t.Fatal("BuildKit config must not reference rootfs TLS files")
	}
	for _, requiredMirror := range []string{
		`[registry."docker.io"]`,
		`mirrors = ["docker.1ms.run"]`,
	} {
		if !strings.Contains(configContents, requiredMirror) {
			t.Errorf("BuildKit config is missing %q", requiredMirror)
		}
	}
	if strings.Contains(configContents, "insecure = true") || strings.Contains(configContents, "http = true") {
		t.Fatal("Docker Hub mirror must use verified HTTPS")
	}
}

func TestGuestKernelEnablesVirtioShutdownPort(t *testing.T) {
	kernelConfig, err := os.ReadFile("../../runtime/buildroot/board/sealbuild/x86_64/linux.config")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(kernelConfig), "CONFIG_VIRTIO_CONSOLE=y\n") {
		t.Fatal("kernel config is missing CONFIG_VIRTIO_CONSOLE=y")
	}
}

func TestGuestBuildUsesPinnedQEMUImg(t *testing.T) {
	buildScript, err := os.ReadFile("../../scripts/runtime/build-guest.sh")
	if err != nil {
		t.Fatalf("ReadFile(build-guest.sh) error = %v", err)
	}

	contents := string(buildScript)
	for _, requiredFragment := range []string{
		`if [ "$#" -ne 3 ]`,
		`qemu_img=$3`,
		`"${qemu_img}" --version`,
		`grep -F 'version 11.0.2'`,
		`truncate --size 32G "${raw_state_image}"`,
		`mkfs.ext4 -F -L sealbuild-state "${raw_state_image}"`,
		`"${qemu_img}" convert`,
		`-o compat=1.1,lazy_refcounts=on`,
		`"${qemu_img}" info --output=json "${state_image}"`,
		`legal-info`,
		`collect-guest-licenses.sh`,
	} {
		if !strings.Contains(contents, requiredFragment) {
			t.Errorf("build-guest script is missing %q", requiredFragment)
		}
	}
}

func TestGuestArtifactUsesQCOW2(t *testing.T) {
	packageScript, err := os.ReadFile("../../scripts/runtime/package-guest.sh")
	if err != nil {
		t.Fatalf("ReadFile(package-guest.sh) error = %v", err)
	}

	contents := string(packageScript)
	for _, requiredFragment := range []string{
		"buildkit-state.qcow2",
		"guest-licenses",
		"go run ./scripts/runtime/packageguest",
	} {
		if !strings.Contains(contents, requiredFragment) {
			t.Errorf("package-guest script is missing %q", requiredFragment)
		}
	}
	for _, forbiddenFragment := range []string{
		"buildkit-state.ext4",
		`/tls/`,
		"cp --sparse",
	} {
		if strings.Contains(contents, forbiddenFragment) {
			t.Errorf("package-guest script must not contain %q", forbiddenFragment)
		}
	}

	collectorScript, err := os.ReadFile("../../scripts/runtime/collect-guest-licenses.sh")
	if err != nil {
		t.Fatalf("ReadFile(collect-guest-licenses.sh) error = %v", err)
	}
	if !strings.Contains(string(collectorScript), "go run ./scripts/runtime/collectguest") {
		t.Fatal("collect-guest-licenses script must invoke the structured Go collector")
	}
}

func TestGuestRuntimeLockPinsLicenseSourcesAndExcludesQEMU(t *testing.T) {
	contents, err := os.ReadFile("../../runtime/manifest.lock.json")
	if err != nil {
		t.Fatalf("ReadFile(manifest.lock.json) error = %v", err)
	}
	var lock Lock
	if err := json.Unmarshal(contents, &lock); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	wantSources := map[string]string{
		"buildkit-source":    "b733b9243017cb2b8f9cb1a6bd5125a2bde5680d4063412dbc159402bffbaf1e",
		"runc-source":        "32286f18899a644ec7c1589688a9600ba54cc65264f23f1f5877ba214ca76e75",
		"cni-plugins-source": "34bd82d47e981940751619c9cc44c095bb90bfcaf8d71865cbb822c37690a764",
	}
	for _, component := range lock.Components {
		if component.Name == "qemu" {
			t.Fatal("Guest Runtime Lock must not contain Host QEMU")
		}
		if wantSHA, exists := wantSources[component.Name]; exists {
			if component.SHA256 != wantSHA {
				t.Errorf("component %s sha256 = %q, want %q", component.Name, component.SHA256, wantSHA)
			}
			delete(wantSources, component.Name)
		}
	}
	for missing := range wantSources {
		t.Errorf("Guest Runtime Lock is missing %s", missing)
	}
}

func TestSmokeGuestUsesFWCfgAndQCOW2(t *testing.T) {
	smokeScript, err := os.ReadFile("../../scripts/runtime/smoke-guest.sh")
	if err != nil {
		t.Fatalf("ReadFile(smoke-guest.sh) error = %v", err)
	}
	contents := string(smokeScript)
	for _, requiredFragment := range []string{
		`usage: %s QEMU BUILDKCTL ARTIFACT_DIR TLS_DIR OUTPUT_DIR HOST_PORT [PROXY_URL]`,
		`state_image="${output_dir}/buildkit-state.qcow2"`,
		`file=${state_image},format=qcow2,if=virtio`,
		`name=opt/sealbuild/tls/ca.crt,file=${tls_dir}/ca.crt`,
		`name=opt/sealbuild/tls/server.crt,file=${tls_dir}/server.crt`,
		`name=opt/sealbuild/tls/server.key,file=${tls_dir}/server.key`,
		`name=opt/sealbuild/proxy/url,file=${proxy_file}`,
		`go run "${project_dir}/scripts/runtime/inspect.go" proxy "${proxy_file}"`,
		`chmod 0600 "${proxy_file}"`,
		`HTTP_PROXY="${proxy_url}"`,
		`HTTPS_PROXY="${proxy_url}"`,
	} {
		if !strings.Contains(contents, requiredFragment) {
			t.Errorf("smoke script is missing %q", requiredFragment)
		}
	}
	if strings.Contains(contents, `name=opt/sealbuild/proxy/url,string=${proxy_url}`) {
		t.Fatal("smoke script must not place the proxy URL in QEMU arguments")
	}

	workflow, err := os.ReadFile("../../.github/workflows/runtime-spike.yml")
	if err != nil {
		t.Fatalf("ReadFile(runtime-spike.yml) error = %v", err)
	}
	workflowContents := string(workflow)
	for _, requiredFragment := range []string{
		"ninja -C build qemu-system-x86_64 qemu-img",
		`"$RUNNER_TEMP/qemu/build/qemu-img"`,
		"generate-spike-certs.sh",
		`"$RUNNER_TEMP/runtime-spike/tls"`,
		"artifact/manifest.json",
		"artifact/checksums.txt",
	} {
		if !strings.Contains(workflowContents, requiredFragment) {
			t.Errorf("workflow is missing %q", requiredFragment)
		}
	}
	if strings.Contains(workflowContents, "client.key\n") || strings.Contains(workflowContents, "server.key\n") {
		t.Fatal("workflow must not upload TLS private keys")
	}
}

func TestDarwinARMQEMUBuildScriptUsesPinnedTCGOnlyConfiguration(t *testing.T) {
	buildScript, err := os.ReadFile("../../scripts/runtime/build-qemu-darwin-arm64.sh")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	contents := string(buildScript)
	for _, requiredFragment := range []string{
		"e545d8bb9d63e9dd61542b88463183314cff9482",
		"Python 3.14.6",
		"setuptools-79.0.1-py3-none-any.whl",
		"e147c0549f27767ba362f9da434eab9c5dc0045d5304feb602a0af001089fc51",
		`-m venv --system-site-packages "${bootstrap_dir}"`,
		`-m pip install --disable-pip-version-check --no-index --no-deps "${setuptools_wheel}"`,
		`--python="${bootstrap_python}"`,
		`[ "$(uname -s)" = Darwin ]`,
		`[ "$(uname -m)" = arm64 ]`,
		"--target-list=x86_64-softmmu",
		"--enable-tcg",
		"--enable-slirp",
		"--disable-hvf",
		"--disable-cocoa",
		"--disable-gtk",
		"--disable-sdl",
		"--disable-docs",
		"--disable-guest-agent",
		"--disable-tools",
		"--disable-user",
		"--disable-bsd-user",
		"--disable-linux-user",
		"--disable-download",
		"ninja -C \"${build_dir}\" qemu-system-x86_64",
		"Accelerators supported in QEMU binary:",
	} {
		if !strings.Contains(contents, requiredFragment) {
			t.Errorf("build script is missing %q", requiredFragment)
		}
	}
	if strings.Contains(contents, "--enable-hvf") {
		t.Fatal("build script must not enable HVF")
	}
}
