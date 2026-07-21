package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	proxyconfig "github.com/labring/sealbuild/internal/proxy"
)

func TestPrepareBuildRequestCreatesFixedLinuxAMD64Frontend(t *testing.T) {
	workspace := t.TempDir()
	contextDirectory := filepath.Join(workspace, "context")
	if err := os.MkdirAll(filepath.Join(contextDirectory, "docker"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	dockerfile := filepath.Join(contextDirectory, "docker", "Buildfile")
	if err := os.WriteFile(dockerfile, []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(Dockerfile) error = %v", err)
	}
	proxy, err := proxyconfig.Parse("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("Parse(proxy) error = %v", err)
	}
	request := Request{
		ContextDir: contextDirectory,
		Dockerfile: filepath.Join("docker", "Buildfile"),
		OutputPath: filepath.Join(workspace, "image.oci.tar"),
		BuildArgs:  map[string]string{"VERSION": "1.2.3"},
		Proxy:      &proxy,
	}

	prepared, err := Prepare(request)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	canonicalContext, err := filepath.EvalSymlinks(contextDirectory)
	if err != nil {
		t.Fatalf("EvalSymlinks(context) error = %v", err)
	}
	if prepared.ContextDir != canonicalContext || prepared.DockerfileDir != filepath.Join(canonicalContext, "docker") || prepared.OutputPath != request.OutputPath {
		t.Fatalf("Prepare() paths = %#v", prepared)
	}
	if prepared.HostProxy != "http://127.0.0.1:7890" {
		t.Fatalf("HostProxy = %q", prepared.HostProxy)
	}
	wantAttrs := map[string]string{
		"filename":              "Buildfile",
		"platform":              "linux/amd64",
		"build-arg:VERSION":     "1.2.3",
		"build-arg:HTTP_PROXY":  "http://10.0.2.2:7890",
		"build-arg:HTTPS_PROXY": "http://10.0.2.2:7890",
		"build-arg:http_proxy":  "http://10.0.2.2:7890",
		"build-arg:https_proxy": "http://10.0.2.2:7890",
	}
	if len(prepared.FrontendAttrs) != len(wantAttrs) {
		t.Fatalf("FrontendAttrs = %#v", prepared.FrontendAttrs)
	}
	for key, want := range wantAttrs {
		if prepared.FrontendAttrs[key] != want {
			t.Errorf("FrontendAttrs[%q] = %q, want %q", key, prepared.FrontendAttrs[key], want)
		}
	}
	prepared.FrontendAttrs["platform"] = "linux/arm64"
	if request.BuildArgs["platform"] != "" || request.BuildArgs["VERSION"] != "1.2.3" {
		t.Fatalf("Prepare() mutated request BuildArgs: %#v", request.BuildArgs)
	}
}

func TestPrepareBuildRequestUsesDefaultDockerfile(t *testing.T) {
	contextDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(contextDirectory, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	prepared, err := Prepare(Request{ContextDir: contextDirectory, OutputPath: filepath.Join(t.TempDir(), "image.tar")})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	canonicalContext, err := filepath.EvalSymlinks(contextDirectory)
	if err != nil {
		t.Fatalf("EvalSymlinks(context) error = %v", err)
	}
	if prepared.DockerfileDir != canonicalContext || prepared.FrontendAttrs["filename"] != "Dockerfile" {
		t.Fatalf("Prepare() = %#v", prepared)
	}
}

func TestPrepareBuildRequestRejectsInvalidInputs(t *testing.T) {
	validContext := t.TempDir()
	if err := os.WriteFile(filepath.Join(validContext, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	proxy, err := proxyconfig.Parse("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("Parse(proxy) error = %v", err)
	}
	tests := []struct {
		name      string
		request   func(*testing.T) Request
		wantError string
	}{
		{name: "missing context", request: func(t *testing.T) Request {
			return Request{ContextDir: filepath.Join(t.TempDir(), "missing"), OutputPath: filepath.Join(t.TempDir(), "out.tar")}
		}, wantError: "context"},
		{name: "context file", request: func(t *testing.T) Request {
			return Request{ContextDir: writeRequestFile(t, filepath.Join(t.TempDir(), "context")), OutputPath: filepath.Join(t.TempDir(), "out.tar")}
		}, wantError: "context must be a directory"},
		{name: "Dockerfile escape", request: func(t *testing.T) Request {
			return Request{ContextDir: validContext, Dockerfile: "../Dockerfile", OutputPath: filepath.Join(t.TempDir(), "out.tar")}
		}, wantError: "inside context"},
		{name: "Dockerfile directory", request: func(t *testing.T) Request {
			return Request{ContextDir: validContext, Dockerfile: ".", OutputPath: filepath.Join(t.TempDir(), "out.tar")}
		}, wantError: "regular file"},
		{name: "missing output parent", request: func(t *testing.T) Request {
			return Request{ContextDir: validContext, OutputPath: filepath.Join(t.TempDir(), "missing", "out.tar")}
		}, wantError: "output parent"},
		{name: "existing output", request: func(t *testing.T) Request {
			return Request{ContextDir: validContext, OutputPath: writeRequestFile(t, filepath.Join(t.TempDir(), "out.tar"))}
		}, wantError: "already exists"},
		{name: "invalid build arg", request: func(t *testing.T) Request {
			return Request{ContextDir: validContext, OutputPath: filepath.Join(t.TempDir(), "out.tar"), BuildArgs: map[string]string{"BAD-NAME": "x"}}
		}, wantError: "build argument name"},
		{name: "proxy build arg conflict", request: func(t *testing.T) Request {
			return Request{ContextDir: validContext, OutputPath: filepath.Join(t.TempDir(), "out.tar"), Proxy: &proxy, BuildArgs: map[string]string{"HTTP_PROXY": "other"}}
		}, wantError: "conflicts with --proxy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Prepare(test.request(t))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.wantError)) {
				t.Fatalf("Prepare() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func writeRequestFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	return path
}
