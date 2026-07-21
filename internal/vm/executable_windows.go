//go:build windows

package vm

// QEMUExecutableName returns the packaged QEMU executable name.
func QEMUExecutableName() string {
	return "qemu-system-x86_64.exe"
}
