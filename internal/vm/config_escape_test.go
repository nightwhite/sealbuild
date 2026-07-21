package vm

import "testing"

func TestEscapeQEMUOptionValueDoublesCommas(t *testing.T) {
	want := `C:\\Users\\Night White\\runtime,,one\\disk.qcow2`
	if got := escapeQEMUOptionValue(`C:\\Users\\Night White\\runtime,one\\disk.qcow2`); got != want {
		t.Fatalf("escapeQEMUOptionValue() = %q, want %q", got, want)
	}
}
