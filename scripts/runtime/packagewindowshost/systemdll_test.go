package main

import "testing"

func TestIsWindowsSystemDLLUsesFixedAllowlist(t *testing.T) {
	for _, name := range []string{"KERNEL32.dll", "ntdll.DLL", "api-ms-win-core-file-l1-1-0.dll", "ext-ms-win-ntuser-window-l1-1-0.dll"} {
		if !isWindowsSystemDLL(name) {
			t.Errorf("isWindowsSystemDLL(%q) = false", name)
		}
	}
	for _, name := range []string{"libglib-2.0-0.dll", "qemu.dll", "kernel32.dll.backup"} {
		if isWindowsSystemDLL(name) {
			t.Errorf("isWindowsSystemDLL(%q) = true", name)
		}
	}
}
