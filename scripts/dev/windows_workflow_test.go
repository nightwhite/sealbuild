package dev

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsWorkflowDefinesIsolatedRuntimeAndProductAcceptance(t *testing.T) {
	contents, err := os.ReadFile("../../.github/workflows/windows-amd64.yml")
	if err != nil {
		t.Fatalf("ReadFile(windows-amd64.yml) error = %v", err)
	}
	workflow := string(contents)
	for _, required := range []string{
		"build-guest-runtime:",
		"build-windows-runtime:",
		"test-windows-product:",
		"runs-on: windows-2025",
		"msys2/setup-msys2@66cd2cce69caa17b53920067426061ca1de3a884",
		"https://download.qemu.org/qemu-11.0.2.tar.xz",
		"3745f6ea88e2e87fe0dc838b2b1d4e0a770bf48e01a1d5a186842a1fff76ccf5",
		"./scripts/runtime/build-qemu-windows-amd64.sh",
		"sealbuild-windows-amd64.exe\" build",
		"first.oci.tar",
		"cached.oci.tar",
		"-SimpleMatch 'CACHED'",
		"verify-oci",
		"Get-Process -Name qemu-system-x86_64",
		"150MB",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("windows-amd64.yml is missing %q", required)
		}
	}
	productIndex := strings.Index(workflow, "test-windows-product:")
	if productIndex < 0 {
		return
	}
	product := workflow[productIndex:]
	for _, forbidden := range []string{"setup-msys2", "docker ", "wsl ", "whpx", "hyper-v", "remote builder"} {
		if strings.Contains(strings.ToLower(product), forbidden) {
			t.Errorf("Windows product job contains forbidden fragment %q", forbidden)
		}
	}
}
