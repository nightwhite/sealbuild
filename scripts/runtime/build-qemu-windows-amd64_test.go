package main

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsQEMUBuildNormalizesCRLFBeforeAcceleratorCheck(t *testing.T) {
	contents, err := os.ReadFile("build-qemu-windows-amd64.sh")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(contents), `-accel help | tr -d '\r'`) {
		t.Fatal("Windows QEMU build must remove CR from accelerator output")
	}
}
