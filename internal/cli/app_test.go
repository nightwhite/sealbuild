package cli

import (
	"bytes"
	"testing"

	"github.com/labring/sealbuild/internal/version"
)

func TestRunVersion(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	info := version.Info{
		Version: "v1.2.3",
		Commit:  "abc123",
		BuiltAt: "2026-07-16T14:00:00Z",
	}

	exitCode := Run([]string{"version"}, &stdout, &stderr, info)

	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, want 0", exitCode)
	}
	if actual, expected := stdout.String(), info.String(); actual != expected {
		t.Fatalf("stdout = %q, want %q", actual, expected)
	}
	if actual := stderr.String(); actual != "" {
		t.Fatalf("stderr = %q, want empty", actual)
	}
}

func TestRunHelp(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		args []string
	}{
		{name: "no arguments"},
		{name: "help command", args: []string{"help"}},
		{name: "short flag", args: []string{"-h"}},
		{name: "long flag", args: []string{"--help"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := Run(testCase.args, &stdout, &stderr, version.Info{})

			if exitCode != 0 {
				t.Fatalf("Run() exit code = %d, want 0", exitCode)
			}
			if actual := stdout.String(); actual != usage {
				t.Fatalf("stdout = %q, want %q", actual, usage)
			}
			if actual := stderr.String(); actual != "" {
				t.Fatalf("stderr = %q, want empty", actual)
			}
		})
	}
}

func TestRunUnknownCommand(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := Run([]string{"unknown"}, &stdout, &stderr, version.Info{})

	if exitCode != 2 {
		t.Fatalf("Run() exit code = %d, want 2", exitCode)
	}
	if actual := stdout.String(); actual != "" {
		t.Fatalf("stdout = %q, want empty", actual)
	}
	const expected = "sealbuild: unknown command \"unknown\"\nRun 'sealbuild help' for usage.\n"
	if actual := stderr.String(); actual != expected {
		t.Fatalf("stderr = %q, want %q", actual, expected)
	}
}
