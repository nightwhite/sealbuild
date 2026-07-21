//go:build linux && amd64

package main

import "testing"

func TestInspectELFReadsAMD64DynamicExecutable(t *testing.T) {
	image, err := inspectELF("/bin/sh")
	if err != nil {
		t.Fatalf("inspectELF(/bin/sh) error = %v", err)
	}
	if image.Interpreter == "" {
		t.Fatal("inspectELF(/bin/sh) interpreter is empty")
	}
	if len(image.Needed) == 0 {
		t.Fatal("inspectELF(/bin/sh) imported libraries are empty")
	}
}
