package build

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	proxyconfig "github.com/labring/sealbuild/internal/proxy"
)

var buildArgumentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Request describes one local Dockerfile build and OCI output.
type Request struct {
	ContextDir string
	Dockerfile string
	OutputPath string
	BuildArgs  map[string]string
	Proxy      *proxyconfig.Config
}

// PreparedRequest contains validated paths and fixed Dockerfile frontend attributes.
type PreparedRequest struct {
	ContextDir    string
	DockerfileDir string
	OutputPath    string
	FrontendAttrs map[string]string
	HostProxy     string
}

// Prepare validates one build request without contacting BuildKit.
func Prepare(request Request) (PreparedRequest, error) {
	contextDirectory, err := canonicalDirectory(request.ContextDir, "build context")
	if err != nil {
		return PreparedRequest{}, err
	}
	dockerfilePath := request.Dockerfile
	if dockerfilePath == "" {
		dockerfilePath = filepath.Join(contextDirectory, "Dockerfile")
	} else if !filepath.IsAbs(dockerfilePath) {
		dockerfilePath = filepath.Join(contextDirectory, dockerfilePath)
	}
	if !pathWithin(contextDirectory, filepath.Clean(dockerfilePath)) {
		return PreparedRequest{}, fmt.Errorf("Dockerfile must be inside context")
	}
	dockerfilePath, err = filepath.EvalSymlinks(dockerfilePath)
	if err != nil {
		return PreparedRequest{}, fmt.Errorf("resolve Dockerfile: %w", err)
	}
	if !pathWithin(contextDirectory, dockerfilePath) {
		return PreparedRequest{}, fmt.Errorf("Dockerfile must be inside context")
	}
	dockerfileInfo, err := os.Lstat(dockerfilePath)
	if err != nil {
		return PreparedRequest{}, fmt.Errorf("inspect Dockerfile: %w", err)
	}
	if !dockerfileInfo.Mode().IsRegular() {
		return PreparedRequest{}, fmt.Errorf("Dockerfile must be a regular file")
	}

	outputPath, err := filepath.Abs(request.OutputPath)
	if err != nil || request.OutputPath == "" {
		return PreparedRequest{}, fmt.Errorf("output path is required")
	}
	outputPath = filepath.Clean(outputPath)
	if _, err := os.Lstat(outputPath); err == nil {
		return PreparedRequest{}, fmt.Errorf("output already exists: %s", outputPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return PreparedRequest{}, fmt.Errorf("inspect output: %w", err)
	}
	outputParent := filepath.Dir(outputPath)
	parentInfo, err := os.Stat(outputParent)
	if err != nil {
		return PreparedRequest{}, fmt.Errorf("inspect output parent: %w", err)
	}
	if !parentInfo.IsDir() {
		return PreparedRequest{}, fmt.Errorf("output parent must be a directory")
	}

	frontendAttrs := map[string]string{
		"filename": filepath.Base(dockerfilePath),
		"platform": "linux/amd64",
	}
	for name, value := range request.BuildArgs {
		if !buildArgumentName.MatchString(name) {
			return PreparedRequest{}, fmt.Errorf("build argument name %q is invalid", name)
		}
		frontendAttrs["build-arg:"+name] = value
	}
	hostProxy := ""
	if request.Proxy != nil {
		proxy, err := proxyconfig.Parse(request.Proxy.Raw)
		if err != nil {
			return PreparedRequest{}, fmt.Errorf("validate build proxy: %w", err)
		}
		for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
			if _, exists := request.BuildArgs[name]; exists {
				return PreparedRequest{}, fmt.Errorf("build argument %s conflicts with --proxy", name)
			}
			frontendAttrs["build-arg:"+name] = proxy.Guest
		}
		hostProxy = proxy.Raw
	}

	return PreparedRequest{
		ContextDir: contextDirectory, DockerfileDir: filepath.Dir(dockerfilePath),
		OutputPath: outputPath, FrontendAttrs: frontendAttrs, HostProxy: hostProxy,
	}, nil
}

func pathWithin(root, candidate string) bool {
	relativePath, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) && !filepath.IsAbs(relativePath)
}

func canonicalDirectory(path, label string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	canonicalPath, err := filepath.EvalSymlinks(absolutePath)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	info, err := os.Stat(canonicalPath)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", label, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s must be a directory", label)
	}
	return canonicalPath, nil
}
