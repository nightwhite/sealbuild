package cli

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

	buildpkg "github.com/labring/sealbuild/internal/build"
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

	exitCode := Run(t.Context(), []string{"version"}, &stdout, &stderr, info, nil)

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

			exitCode := Run(t.Context(), testCase.args, &stdout, &stderr, version.Info{}, nil)

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

	exitCode := Run(t.Context(), []string{"unknown"}, &stdout, &stderr, version.Info{}, nil)

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

func TestRunBuildParsesAndExecutesRequest(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	builder := &fakeBuilder{}
	contextDirectory := t.TempDir()
	outputPath := filepath.Join(t.TempDir(), "image.tar")
	exitCode := Run(t.Context(), []string{
		"build", "--dockerfile", "docker/Buildfile",
		"--build-arg", "VERSION=1.2.3", "--build-arg", "EMPTY=",
		"--proxy", "http://127.0.0.1:7890", "--no-proxy", "deb.debian.org,.debian.org", "--output", outputPath,
		contextDirectory,
	}, &stdout, &stderr, version.Info{}, builder)
	if exitCode != 0 {
		t.Fatalf("Run(build) exit code = %d stderr = %q", exitCode, stderr.String())
	}
	if builder.calls != 1 || builder.request.ContextDir != contextDirectory || builder.request.Dockerfile != "docker/Buildfile" || builder.request.OutputPath != outputPath {
		t.Fatalf("Builder request = %#v calls = %d", builder.request, builder.calls)
	}
	if builder.request.BuildArgs["VERSION"] != "1.2.3" || builder.request.BuildArgs["EMPTY"] != "" {
		t.Fatalf("BuildArgs = %#v", builder.request.BuildArgs)
	}
	if builder.request.BuildArgs["NO_PROXY"] != "deb.debian.org,.debian.org" || builder.request.BuildArgs["no_proxy"] != "deb.debian.org,.debian.org" {
		t.Fatalf("proxy bypass BuildArgs = %#v", builder.request.BuildArgs)
	}
	if builder.request.Proxy == nil || builder.request.Proxy.Raw != "http://127.0.0.1:7890" {
		t.Fatalf("Proxy = %#v", builder.request.Proxy)
	}
	if builder.progress != &stdout || stderr.Len() != 0 {
		t.Fatalf("progress writer or stderr mismatch: stderr = %q", stderr.String())
	}
}

func TestRunBuildRejectsInvalidArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing output", args: []string{"build", "."}},
		{name: "missing context", args: []string{"build", "--output", "image.tar"}},
		{name: "multiple contexts", args: []string{"build", "--output", "image.tar", ".", "other"}},
		{name: "invalid build arg", args: []string{"build", "--output", "image.tar", "--build-arg", "INVALID", "."}},
		{name: "duplicate build arg", args: []string{"build", "--output", "image.tar", "--build-arg", "A=1", "--build-arg", "A=2", "."}},
		{name: "invalid proxy", args: []string{"build", "--output", "image.tar", "--proxy", "socks5://127.0.0.1:1080", "."}},
		{name: "empty no proxy", args: []string{"build", "--output", "image.tar", "--no-proxy", "", "."}},
		{name: "conflicting no proxy build arg", args: []string{"build", "--output", "image.tar", "--no-proxy", "deb.debian.org", "--build-arg", "NO_PROXY=example.com", "."}},
		{name: "unknown flag", args: []string{"build", "--platform", "linux/arm64", "--output", "image.tar", "."}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			builder := &fakeBuilder{}
			exitCode := Run(t.Context(), test.args, &stdout, &stderr, version.Info{}, builder)
			if exitCode != 2 || builder.calls != 0 || stderr.Len() == 0 {
				t.Fatalf("Run() exit = %d calls = %d stderr = %q", exitCode, builder.calls, stderr.String())
			}
		})
	}
}

type fakeBuilder struct {
	err      error
	calls    int
	request  buildpkg.Request
	progress io.Writer
}

func (builder *fakeBuilder) Build(_ context.Context, request buildpkg.Request, progress io.Writer) error {
	builder.calls++
	builder.request = request
	builder.progress = progress
	return builder.err
}
