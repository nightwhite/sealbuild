package dev

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestFourHostWorkflowDefinesCompleteCandidatePipeline(t *testing.T) {
	contents, err := os.ReadFile("../../.github/workflows/four-host-candidate.yml")
	if err != nil {
		t.Fatalf("ReadFile(four-host-candidate.yml) error = %v", err)
	}
	workflow := string(contents)
	for _, required := range []string{
		"quality:",
		"prepare-runtime-sources:",
		"build-guest-runtime:",
		"build-host-linux-amd64:",
		"build-host-windows-amd64:",
		"build-host-darwin-arm64:",
		"build-host-darwin-amd64:",
		"test-linux-amd64:",
		"test-windows-amd64:",
		"test-darwin-arm64:",
		"test-darwin-amd64:",
		"aggregate:",
		"publish-rc:",
		"runs-on: ubuntu-24.04",
		"runs-on: windows-2025",
		"runs-on: macos-15",
		"runs-on: macos-15-intel",
		"sealbuild-darwin-arm64",
		"sealbuild-darwin-amd64",
		"sealbuild-linux-amd64",
		"sealbuild-windows-amd64.exe",
		"https://download.qemu.org/qemu-11.0.2.tar.xz",
		"3745f6ea88e2e87fe0dc838b2b1d4e0a770bf48e01a1d5a186842a1fff76ccf5",
		"cb857ba4c87a93e5265a9e4a3f32071abf39e14a",
		"collect-runtime-packages",
		"--runtime-package-evidence",
		"windows-runtime-packages.json",
		`--runtime-package-evidence "$RUNNER_TEMP/dpkg-runtime-packages.txt"`,
		`--pacman "$env:MSYS2_LOCATION\usr\bin\pacman.exe"`,
		`--cygpath "$env:MSYS2_LOCATION\usr\bin\cygpath.exe"`,
		"-SimpleMatch 'CACHED'",
		"grep -F 'CACHED'",
		"verify-oci",
		"150MB",
		"157286400",
		"^v[0-9]+\\.[0-9]+\\.[0-9]+-rc\\.[1-9][0-9]*$",
		"gh release create",
		"--verify-tag",
		"--prerelease",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("four-host-candidate.yml is missing %q", required)
		}
	}
	if strings.Count(workflow, "verify-oci") < 8 {
		t.Errorf("verify-oci count = %d, want at least 8", strings.Count(workflow, "verify-oci"))
	}
	if strings.Count(workflow, " build ") < 8 {
		t.Errorf("product build command count = %d, want at least 8", strings.Count(workflow, " build "))
	}
	if strings.Count(workflow, "QEMU process remains after build") != 4 {
		t.Errorf("QEMU cleanup gate count = %d, want 4", strings.Count(workflow, "QEMU process remains after build"))
	}
	verifiedQEMUArchive := `cp "$RUNNER_TEMP/qemu-11.0.2.tar.xz" "$RUNNER_TEMP/darwin-license-sources/qemu.archive"`
	if strings.Count(workflow, verifiedQEMUArchive) != 1 {
		t.Errorf("verified Darwin QEMU archive reuse count = %d, want 1", strings.Count(workflow, verifiedQEMUArchive))
	}
	if count := strings.Count(workflow, `curl --fail --output "$RUNNER_TEMP/qemu-11.0.2.tar.xz" "$QEMU_URL"`); count != 1 {
		t.Errorf("official QEMU source download count = %d, want 1", count)
	}
	if count := strings.Count(workflow, "name: sealbuild-qemu-source"); count != 6 {
		t.Errorf("shared QEMU source artifact count = %d, want 6", count)
	}
	if count := strings.Count(workflow, "name: sealbuild-darwin-host-licenses"); count != 3 {
		t.Errorf("shared Darwin license artifact count = %d, want 3", count)
	}
	if count := strings.Count(workflow, "needs: prepare-runtime-sources"); count != 5 {
		t.Errorf("Runtime source dependency count = %d, want 5", count)
	}
	for _, forbidden := range []string{"retry", "ftpmirror.gnu.org", "docker build", "docker run", "remote builder"} {
		if strings.Contains(strings.ToLower(workflow), forbidden) {
			t.Errorf("four-host-candidate.yml contains forbidden fragment %q", forbidden)
		}
	}

	actionPattern := regexp.MustCompile(`(?m)^\s+uses:\s+([^\s]+)$`)
	pinnedActionPattern := regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)
	for _, match := range actionPattern.FindAllStringSubmatch(workflow, -1) {
		if strings.HasPrefix(match[1], "./") {
			continue
		}
		if !pinnedActionPattern.MatchString(match[1]) {
			t.Errorf("Action is not pinned to a full commit: %s", match[1])
		}
	}
}

func TestFourHostProductJobsDoNotInstallProductDependencies(t *testing.T) {
	contents, err := os.ReadFile("../../.github/workflows/four-host-candidate.yml")
	if err != nil {
		t.Fatalf("ReadFile(four-host-candidate.yml) error = %v", err)
	}
	workflow := string(contents)
	for _, jobName := range []string{"test-linux-amd64", "test-windows-amd64", "test-darwin-arm64", "test-darwin-amd64"} {
		block := workflowJob(t, workflow, jobName)
		for _, forbidden := range []string{"apt-get install", "brew install", "setup-msys2", "wsl ", "docker ", "podman ", "--enable-kvm", "--enable-hvf", "--enable-whpx"} {
			if strings.Contains(strings.ToLower(block), forbidden) {
				t.Errorf("%s contains forbidden product dependency %q", jobName, forbidden)
			}
		}
	}
}

func workflowJob(t *testing.T, workflow, jobName string) string {
	t.Helper()
	startMarker := "\n  " + jobName + ":"
	start := strings.Index(workflow, startMarker)
	if start < 0 {
		t.Fatalf("workflow job %s is missing", jobName)
	}
	rest := workflow[start+len(startMarker):]
	nextJob := regexp.MustCompile(`\n  [a-z][a-z0-9-]*:\n`).FindStringIndex(rest)
	if nextJob == nil {
		return rest
	}
	return rest[:nextJob[0]]
}
