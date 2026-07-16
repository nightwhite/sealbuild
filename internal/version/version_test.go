package version

import "testing"

func TestInfoString(t *testing.T) {
	t.Parallel()

	info := Info{
		Version: "v1.2.3",
		Commit:  "abc123",
		BuiltAt: "2026-07-16T14:00:00Z",
	}

	const expected = "sealbuild v1.2.3\ncommit: abc123\nbuilt: 2026-07-16T14:00:00Z\n"
	if actual := info.String(); actual != expected {
		t.Fatalf("Info.String() = %q, want %q", actual, expected)
	}
}
